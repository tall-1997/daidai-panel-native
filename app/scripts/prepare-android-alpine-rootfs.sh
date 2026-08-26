#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
asset_root="$repo_root/app/android/app/src/main/assets/android-runtime"
cache_root="${ANDROID_ALPINE_ROOTFS_CACHE:-$HOME/.cache/daidai-android-alpine-rootfs}"
mirror="${ALPINE_APK_MIRROR:-https://repo.huaweicloud.com/alpine}"
release_branch="${ALPINE_RELEASE_BRANCH:-latest-stable}"
required_packages=(bash python3 py3-pip py3-pycryptodome nodejs npm uv pnpm ca-certificates)
required_commands=(apk bash python3 pip3 node npm uv pnpm)
extra_packages="${ALPINE_ROOTFS_PACKAGES:-curl git openssh-client tzdata}"
packages="$extra_packages ${required_packages[*]}"
abis="${ANDROID_RUNTIME_ABIS:-arm64-v8a}"
apk_static_bin=""

alpine_arch_for_abi() {
  case "$1" in
    arm64-v8a) printf '%s\n' aarch64 ;;
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

install_apk_tools2_rootfs() {
  local root_dir="$1"
  local alpine_arch="$2"
  # 技巧1：Alpine 3.24 的 apk-tools 3 写数据库用 hardlink 原子发布，无 root 设备
  # SELinux 禁 app_data_file link，运行时 apk add 会报 "failed to write database:
  # Permission denied"。放 apk-tools 2.x 静态版（Alpine 3.20，用 rename 写 db）到
  # /usr/local/bin/apk.static。
  local apk2_version="2.14.4-r1"
  local apk2_url="https://dl-cdn.alpinelinux.org/alpine/v3.20/main/$alpine_arch/apk-tools-static-$apk2_version.apk"
  local apk2_file="$cache_root/downloads/apk-tools-static-$apk2_version-$alpine_arch.apk"
  download "$apk2_url" "$apk2_file"
  local extract_dir="$cache_root/apk2-extract-$(date +%s)-$$"
  mkdir -p "$extract_dir"
  tar -xzf "$apk2_file" -C "$extract_dir" sbin/apk.static
  mkdir -p "$root_dir/usr/local/bin"
  cp "$extract_dir/sbin/apk.static" "$root_dir/usr/local/bin/apk.static"
  chmod 755 "$root_dir/usr/local/bin/apk.static"
  # 技巧4：并发 apk 防护——/usr/local/bin/apk 是 wrapper，用 mkdir 原子互斥锁
  # 防止并发 apk add 抢数据库锁（EAGAIN/EINTR）。PATH 里 /usr/local/bin 优先于
  # /sbin，所以运行时 apk 调用会走这个 wrapper。
  cat > "$root_dir/usr/local/bin/apk" <<'WRAPPER'
#!/bin/sh
LOCK=/tmp/daidai-apk.lock
i=0
while ! mkdir "$LOCK" 2>/dev/null; do
  if [ $i -ge 180 ]; then
    rm -rf "$LOCK" 2>/dev/null
    i=0
  fi
  sleep 1
  i=$((i + 1))
done
chmod 777 "$LOCK" 2>/dev/null
trap 'rm -rf "$LOCK"' EXIT
exec /usr/local/bin/apk.static "$@"
WRAPPER
  chmod 755 "$root_dir/usr/local/bin/apk"
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
  local output_dir="$asset_root/$abi/alpine"
  mkdir -p "$root_dir" "$output_dir"
  tar -xzf "$archive" -C "$root_dir"
  write_rootfs_config "$root_dir" "$branch"
  usermode_args=()
  if test "$(id -u)" != "0"; then
    usermode_args+=(--usermode)
  fi
  "$apk_static_bin" "${usermode_args[@]}" --root "$root_dir" --arch "$alpine_arch" --keys-dir "$root_dir/etc/apk/keys" --repositories-file "$root_dir/etc/apk/repositories" --no-cache --no-scripts --initdb add $packages
  install_apk_tools2_rootfs "$root_dir" "$alpine_arch"
  for command in "${required_commands[@]}"; do
    test -x "$root_dir/usr/bin/$command" || test -x "$root_dir/bin/$command" || test -x "$root_dir/sbin/$command" || test -x "$root_dir/usr/sbin/$command" || {
      printf 'Required rootfs command is missing or not executable: %s\n' "$command" >&2
      exit 1
    }
  done
  test -s "$root_dir/etc/ssl/certs/ca-certificates.crt" || {
    printf 'Required CA certificate bundle is missing.\n' >&2
    exit 1
  }
  test -d "$root_dir/usr/lib/python3.14/site-packages/Crypto" || {
    printf 'Required PyCryptodome Crypto package is missing.\n' >&2
    exit 1
  }
  tar --numeric-owner --sort=name --mtime='UTC 2026-01-01' -cf - -C "$root_dir" . | gzip -n > "$output_dir/rootfs.tar.gz.bin"
  sha256sum "$output_dir/rootfs.tar.gz.bin" | awk '{print $1}' > "$output_dir/rootfs.tar.gz.bin.sha256"
  python3 - "$output_dir/runtime-manifest.json" "$abi" "$alpine_arch" "$version" "$output_dir/rootfs.tar.gz.bin" "$packages" "${required_commands[*]}" <<'PY'
import hashlib, json, pathlib, sys
target, abi, arch, version, archive, packages, required_commands = sys.argv[1:]
path = pathlib.Path(archive)
pathlib.Path(target).write_text(json.dumps({
    "schema_version": 2, "abi": abi, "distribution": "alpine", "alpine_arch": arch, "alpine_version": version,
    "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size,
    "packages": packages.split(),
    "required_commands": required_commands.split(),
    "capabilities": {
        "package_manager": ["apk"],
        "shell": ["bash"],
        "python": ["python3", "pip3", "uv"],
        "node": ["node", "npm", "pnpm"],
        "tls_ca_certificates": True,
    },
}, indent=2) + "\n")
PY
  python3 "$script_dir/verify-android-linux-runtime.py" \
    --rootfs "$output_dir/rootfs.tar.gz.bin" \
    --rootfs-sha "$output_dir/rootfs.tar.gz.bin.sha256" \
    --rootfs-manifest "$output_dir/runtime-manifest.json"
}

install_apk_static
for abi in $abis; do
  build_rootfs_for_abi "$abi"
done
