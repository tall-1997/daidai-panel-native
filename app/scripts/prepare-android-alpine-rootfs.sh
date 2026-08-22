#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
asset_root="$repo_root/app/android/app/src/main/assets/android-runtime"
cache_root="${ANDROID_ALPINE_ROOTFS_CACHE:-$HOME/.cache/daidai-android-alpine-rootfs}"
mirror="${ALPINE_APK_MIRROR:-https://repo.huaweicloud.com/alpine}"
release_branch="${ALPINE_RELEASE_BRANCH:-latest-stable}"
packages="${ALPINE_ROOTFS_PACKAGES:-bash python3 py3-pip nodejs npm typescript go ca-certificates curl git openssh-client tzdata}"
abis="${ANDROID_RUNTIME_ABIS:-armeabi-v7a arm64-v8a x86_64}"
apk_static_bin=""

alpine_arch_for_abi() {
  case "$1" in
    arm64-v8a) printf '%s\n' aarch64 ;;
    x86_64) printf '%s\n' x86_64 ;;
    armeabi-v7a) printf '%s\n' armv7 ;;
    *) printf 'Unsupported ABI: %s\n' "$1" >&2; exit 1 ;;
  esac
}

download() {
  local url="$1"
  local output="$2"
  mkdir -p "$(dirname "$output")"
  curl -fL --connect-timeout 30 --max-time 600 -o "$output" "$url"
}

minirootfs_metadata() {
  local alpine_arch="$1"
  local releases_file="$cache_root/downloads/latest-releases-$alpine_arch.yaml"
  download "$mirror/$release_branch/releases/$alpine_arch/latest-releases.yaml" "$releases_file"
  awk '
    BEGIN { RS = "-\n"; FS = "\n" }
    found != 1 && $0 ~ /flavor: alpine-minirootfs/ {
      for (i = 1; i <= NF; i++) {
        line = $i
        sub(/^ +/, "", line)
        gsub(/"/, "", line)
        split(line, parts, ": ")
        if (parts[1] == "branch") branch = parts[2]
        if (parts[1] == "version") version = parts[2]
        if (parts[1] == "file") file = parts[2]
        if (parts[1] == "sha256") sha256 = parts[2]
      }
      print branch
      print version
      print file
      print sha256
      found = 1
    }
  ' "$releases_file"
}

install_apk_static() {
  local host_arch="x86_64"
  local downloads="$cache_root/downloads"
  local apk_dir="$cache_root/apk-static"
  local index="$downloads/APKINDEX-apk-tools-static.tar.gz"
  local index_text="$downloads/APKINDEX-apk-tools-static"
  mkdir -p "$downloads" "$apk_dir"
  download "$mirror/$release_branch/main/$host_arch/APKINDEX.tar.gz" "$index"
  tar -xzO -f "$index" APKINDEX > "$index_text"
  local version
  version="$(awk 'BEGIN { RS="" } $0 ~ /(^|\n)P:apk-tools-static(\n|$)/ { for (i=1; i<=NF; i++) if ($i ~ /^V:/) { sub(/^V:/, "", $i); print $i; exit } }' "$index_text")"
  test -n "$version"
  local apk="$downloads/apk-tools-static-$version.apk"
  download "$mirror/$release_branch/main/$host_arch/apk-tools-static-$version.apk" "$apk"
  apk_dir="$cache_root/apk-static-$(date +%s)-$$"
  mkdir -p "$apk_dir"
  tar -xzf "$apk" -C "$apk_dir" sbin/apk.static
  chmod 755 "$apk_dir/sbin/apk.static"
  apk_static_bin="$apk_dir/sbin/apk.static"
}

write_rootfs_config() {
  local root_dir="$1"
  local branch="$2"
  mkdir -p "$root_dir/etc/apk" "$root_dir/etc/pip.conf.d" "$root_dir/root/.pip" "$root_dir/etc/profile.d" "$root_dir/tmp" "$root_dir/workspace"
  chmod 1777 "$root_dir/tmp"
  printf '%s\n%s\n' "$mirror/$branch/main" "$mirror/$branch/community" > "$root_dir/etc/apk/repositories"
  printf '[global]\nindex-url = https://mirrors.aliyun.com/pypi/simple/\ntrusted-host = mirrors.aliyun.com\ntimeout = 60\n' > "$root_dir/etc/pip.conf"
  cp "$root_dir/etc/pip.conf" "$root_dir/root/.pip/pip.conf"
  printf 'registry=https://registry.npmmirror.com\nignore-scripts=true\n' > "$root_dir/etc/npmrc"
  printf 'export HOME=/root\nexport LANG=C.UTF-8\nexport PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nexport PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/\nexport PIP_TRUSTED_HOST=mirrors.aliyun.com\nexport NPM_CONFIG_REGISTRY=https://registry.npmmirror.com\n' > "$root_dir/etc/profile.d/daidai-runtime.sh"
  printf 'daidai-android\n' > "$root_dir/etc/hostname"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > "$root_dir/etc/resolv.conf"
}

build_rootfs_for_abi() {
  local abi="$1"
  local alpine_arch
  alpine_arch="$(alpine_arch_for_abi "$abi")"
  mapfile -t metadata < <(minirootfs_metadata "$alpine_arch")
  local branch="${metadata[0]}"
  local version="${metadata[1]}"
  local file="${metadata[2]}"
  local sha256="${metadata[3]}"
  test -n "$branch"
  test -n "$version"
  test -n "$file"
  test -n "$sha256"
  local downloads="$cache_root/downloads"
  local archive="$downloads/$file"
  download "$mirror/$branch/releases/$alpine_arch/$file" "$archive"
  printf '%s  %s\n' "$sha256" "$archive" | sha256sum -c -
  local root_dir="$cache_root/work/$abi/rootfs-$(date +%s)-$$"
  local output_dir="$asset_root/$abi"
  mkdir -p "$root_dir" "$output_dir"
  tar -xzf "$archive" -C "$root_dir"
  write_rootfs_config "$root_dir" "$branch"
  usermode_args=()
  if test "$(id -u)" != "0"; then
    usermode_args+=(--usermode)
  fi
  "$apk_static_bin" "${usermode_args[@]}" --root "$root_dir" --arch "$alpine_arch" --keys-dir "$root_dir/etc/apk/keys" --repositories-file "$root_dir/etc/apk/repositories" --no-cache --no-scripts --initdb add $packages
  tar --numeric-owner --sort=name --mtime='UTC 2026-01-01' -cf - -C "$root_dir" . | gzip -n > "$output_dir/rootfs.tar.gz.bin"
  sha256sum "$output_dir/rootfs.tar.gz.bin" | awk '{print $1}' > "$output_dir/rootfs.tar.gz.bin.sha256"
  python3 - "$output_dir/runtime-manifest.json" "$abi" "$alpine_arch" "$version" "$output_dir/rootfs.tar.gz.bin" "$packages" <<'PY'
import hashlib, json, pathlib, sys
target, abi, arch, version, archive, packages = sys.argv[1:]
path = pathlib.Path(archive)
pathlib.Path(target).write_text(json.dumps({
    "schema_version": 1, "abi": abi, "alpine_arch": arch, "alpine_version": version,
    "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size,
    "packages": packages.split(),
}, indent=2) + "\n")
PY
}

install_apk_static
for abi in $abis; do
  build_rootfs_for_abi "$abi"
done
