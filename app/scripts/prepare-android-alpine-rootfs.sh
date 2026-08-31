#!/usr/bin/env bash
# Build the Alpine (musl) rootfs used by the x86_64 Android runtime.
#
# musl is mandatory on x86_64: the app-domain seccomp allowlist
# (bionic SYSCALLS.TXT) does not include set_robust_list/rseq/clone3,
# which Ubuntu's glibc ld.so issues on startup. The zygote filter kills
# such PRoot tracees with SIGSYS before any output. musl never calls
# those syscalls, so Alpine rootfs runs cleanly inside PRoot.
#
# Produces, per ABI:
#   app/android/app/src/main/assets/android-runtime/{abi}/alpine/rootfs.tar.gz.bin
#   app/android/app/src/main/assets/android-runtime/{abi}/alpine/rootfs.tar.gz.bin.sha256
#   app/android/app/src/main/assets/android-runtime/{abi}/alpine/runtime-manifest.json
#
# arm64 is NOT built here: arm64 keeps the Ubuntu glibc rootfs.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
matrix_tool="$repo_root/scripts/android-abi-matrix.py"
asset_root="$repo_root/app/android/app/src/main/assets/android-runtime"
cache_root="${ANDROID_ALPINE_ROOTFS_CACHE:-$HOME/.cache/daidai-android-alpine-rootfs}"
alpine_version="${ALPINE_RELEASE_VERSION:-3.24.1}"
alpine_branch="v${alpine_version%.*}"
alpine_cdn="${ALPINE_CDN:-https://dl-cdn.alpinelinux.org/alpine}"
trusted_sources="$script_dir/rootfs-trusted-sources.json"
apk_packages=(bash python3 py3-pip nodejs npm ca-certificates git openssh-client tzdata)
extra_packages="${ALPINE_ROOTFS_PACKAGES:-}"
pnpm_version="9.15.9"
pnpm_sha256="cf86a7ad764406395d4286a6d09d730711720acc6d93e9dce9ac7ac4dc4a28a7"
pnpm_url="https://registry.npmjs.org/pnpm/-/pnpm-${pnpm_version}.tgz"
requested="${ANDROID_RUNTIME_ABIS:-x86_64}"

download() {
  local url="$1" output="$2"
  mkdir -p "$(dirname "$output")"
  curl -fsSL --connect-timeout 30 --max-time 900 -o "$output" "$url"
}

alpine_arch() {
  case "$1" in
    x86_64) printf 'x86_64' ;;
    *) printf 'unsupported alpine ABI: %s (arm64 keeps Ubuntu)\n' "$1" >&2; exit 1 ;;
  esac
}

apk_mirror() {
  # APK repository mirror: package data lives under {mirror}/v{branch}/main.
  # Default to Aliyun; override with ALPINE_APK_MIRROR.
  printf '%s' "${ALPINE_APK_MIRROR:-https://mirrors.aliyun.com/alpine}"
}

run_in_rootfs() {
  local root_dir="$1"; shift
  if command -v proot >/dev/null 2>&1; then
    proot -0 -r "$root_dir" -b /proc -b /dev -b /sys \
      -b /etc/resolv.conf:/etc/resolv.conf \
      -w /root "$@"
  else
    chroot "$root_dir" "$@"
  fi
}

write_rootfs_config() {
  local root_dir="$1" mirror="$2"
  mkdir -p "$root_dir/etc/apk" "$root_dir/tmp" "$root_dir/workspace" \
    "$root_dir/etc/profile.d" "$root_dir/root/.pip" "$root_dir/usr/local/bin" "$root_dir/etc/ssl/certs" 2>/dev/null || true
  mkdir -p "$root_dir/etc/apk/cache"
  chmod 1777 "$root_dir/tmp"
  printf '%s/%s/main\n%s/%s/community\n' "$mirror" "$alpine_branch" "$mirror" "$alpine_branch" \
    > "$root_dir/etc/apk/repositories"
  printf '# Managed by the Android application before each proot command.\n' > "$root_dir/etc/resolv.conf"
  printf '[global]\nindex-url = https://mirrors.aliyun.com/pypi/simple/\ntrusted-host = mirrors.aliyun.com\ntimeout = 60\n' > "$root_dir/etc/pip.conf"
  cp "$root_dir/etc/pip.conf" "$root_dir/root/.pip/pip.conf"
  printf 'registry=https://registry.npmmirror.com\nignore-scripts=true\n' > "$root_dir/etc/npmrc"
  printf 'export HOME=/root\nexport LANG=C.UTF-8\nexport PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nexport PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/\nexport PIP_TRUSTED_HOST=mirrors.aliyun.com\nexport NPM_CONFIG_REGISTRY=https://registry.npmmirror.com\n' > "$root_dir/etc/profile.d/daidai-runtime.sh"
  printf 'daidai-android\n' > "$root_dir/etc/hostname"
}

build_rootfs_for_abi() {
  local abi="$1"
  local arch
  arch="$(alpine_arch "$abi")"
  local archive_name="alpine-minirootfs-${alpine_version}-${arch}.tar.gz"
  local downloads="$cache_root/downloads"
  local archive="$downloads/$archive_name"
  local sums_url="$alpine_cdn/$alpine_branch/releases/$arch/$archive_name.sha256"
  local archive_url="$alpine_cdn/$alpine_branch/releases/$arch/$archive_name"
  local trusted_values
  trusted_values="$(python3 - "$trusted_sources" "$alpine_version" "$arch" <<'PY'
import json, pathlib, sys
contract = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
entry = contract["alpine"][sys.argv[2]][sys.argv[3]]
print(entry["name"])
print(entry["sha256"])
print(entry["digest_source"])
print(entry["archive_source"])
PY
)"
  local trusted_name trusted_sha trusted_digest_source trusted_archive_source
  mapfile -t trusted <<< "$trusted_values"
  trusted_name="${trusted[0]}"
  trusted_sha="${trusted[1]}"
  trusted_digest_source="${trusted[2]}"
  trusted_archive_source="${trusted[3]}"
  test "$trusted_name" = "$archive_name"
  test "$trusted_digest_source" = "$sums_url"
  test "$trusted_archive_source" = "$archive_url"

  download "$archive_url" "$archive"
  printf '%s  %s\n' "$trusted_sha" "$archive" | sha256sum --check --status || {
    printf 'Alpine minirootfs SHA-256 verification failed: %s\n' "$archive_name" >&2
    exit 1
  }

  local work="$cache_root/work/$abi-$(date +%s)-$$"
  local root_dir="$work/rootfs"
  local output_dir="$asset_root/$abi/alpine"
  mkdir -p "$root_dir" "$output_dir"

  tar -xzf "$archive" -C "$root_dir"
  write_rootfs_config "$root_dir" "$(apk_mirror)"
  mkdir -p "$root_dir/dev" "$root_dir/proc" "$root_dir/sys"

  run_in_rootfs "$root_dir" /sbin/apk --no-cache add "${apk_packages[@]}" ${extra_packages}

  local pnpm_archive="$downloads/pnpm-${pnpm_version}.tgz"
  download "$pnpm_url" "$pnpm_archive"
  test "$(sha256sum "$pnpm_archive" | cut -d' ' -f1)" = "$pnpm_sha256"
  mkdir -p "$root_dir/usr/local/lib/node_modules/pnpm"
  tar -xzf "$pnpm_archive" -C "$root_dir/usr/local/lib/node_modules/pnpm" --strip-components=1
  printf '#!/bin/sh\nexec node /usr/local/lib/node_modules/pnpm/bin/pnpm.cjs "$@"\n' > "$root_dir/usr/local/bin/pnpm"
  chmod 755 "$root_dir/usr/local/bin/pnpm"

  for command in bash python3 pip3 node npm apk; do
    test -x "$root_dir/usr/bin/$command" || test -x "$root_dir/bin/$command" || test -x "$root_dir/sbin/$command" || test -x "$root_dir/usr/sbin/$command" || test -x "$root_dir/usr/local/bin/$command" || {
      printf 'Required rootfs command is missing or not executable: %s\n' "$command" >&2
      exit 1
    }
  done
  test "$(run_in_rootfs "$root_dir" /usr/local/bin/pnpm --version | tr -d '\r\n')" = "$pnpm_version" || {
    printf 'pnpm version check failed\n' >&2
    exit 1
  }
  test -s "$root_dir/etc/ssl/certs/ca-certificates.crt" || {
    printf 'Required CA certificate bundle is missing.\n' >&2
    exit 1
  }

  tar --numeric-owner --sort=name --mtime='UTC 2026-01-01' \
    --exclude='./var/cache/apk/*' --exclude='./var/lib/apk/db/lock' \
    --exclude='./root/.cache/*' \
    -cf - -C "$root_dir" . | xz -9 -T2 > "$output_dir/rootfs.tar.gz.bin"
  sha256sum "$output_dir/rootfs.tar.gz.bin" | awk '{print $1}' > "$output_dir/rootfs.tar.gz.bin.sha256"
  python3 - "$output_dir/runtime-manifest.json" "$abi" "$arch" "$alpine_version" "$output_dir/rootfs.tar.gz.bin" "${apk_packages[*]} ${extra_packages}" "$pnpm_version" "$archive_name" "$trusted_sha" "$sums_url" "$archive_url" <<'PY'
import hashlib, json, pathlib, sys
target, abi, arch, version, archive, apk_packages, pnpm_version, base_archive, base_sha256, digest_source, archive_source = sys.argv[1:]
path = pathlib.Path(archive)
required_commands = ["apk", "bash", "python3", "pip3", "node", "npm", "pnpm"]
pathlib.Path(target).write_text(json.dumps({
    "schema_version": 2, "abi": abi, "distribution": "alpine", "alpine_arch": arch, "alpine_version": version,
    "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size,
    "alpine_packages": apk_packages.split(),
    "global_tools": {"pnpm": {"version": pnpm_version, "install_source": "npm-global"}},
    "required_commands": required_commands,
    "base_archive": {"name": base_archive, "sha256": base_sha256, "digest_source": digest_source, "archive_source": archive_source},
    "capabilities": {
        "package_manager": ["apk"],
        "shell": ["bash"],
        "python": ["python3", "pip3"],
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

for abi in $requested; do
  case "$abi" in
    x86_64) build_rootfs_for_abi "$abi" ;;
    arm64-v8a) log_skip=1; printf '[alpine-rootfs] skipping arm64-v8a (arm64 keeps Ubuntu)\n' ;;
    *) printf 'unsupported ABI for alpine rootfs: %s\n' "$abi" >&2; exit 1 ;;
  esac
done
