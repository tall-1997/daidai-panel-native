#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
asset_root="$repo_root/app/android/app/src/main/assets/android-runtime"
cache_root="${ANDROID_UBUNTU_ROOTFS_CACHE:-$HOME/.cache/daidai-android-ubuntu-rootfs}"
apt_mirror="${UBUNTU_APT_MIRROR:-http://mirrors.aliyun.com/ubuntu-ports}"
release_version="${UBUNTU_RELEASE_VERSION:-24.04.4}"
release_codename="noble"
required_packages=(bash python3 python3-pip nodejs npm ca-certificates curl git openssh-client tzdata)
required_commands=(apt-get bash python3 pip3 node npm pnpm)
extra_packages="${UBUNTU_ROOTFS_PACKAGES:-}"
abis="${ANDROID_RUNTIME_ABIS:-arm64-v8a}"
qemu_static="${QEMU_AARCH64_STATIC:-/usr/bin/qemu-aarch64-static}"
pnpm_major="${UBUNTU_PNPM_MAJOR:-9}"

ubuntu_arch_for_abi() {
  case "$1" in
    arm64-v8a) printf '%s\n' arm64 ;;
    *) printf 'Unsupported ABI: %s\n' "$1" >&2; exit 1 ;;
  esac
}

download() {
  local url="$1" output="$2"
  mkdir -p "$(dirname "$output")"
  curl -fsSL --connect-timeout 30 --max-time 900 -o "$output" "$url"
}

write_rootfs_config() {
  local root_dir="$1"
  rm -f "$root_dir/etc/apt/sources.list.d/ubuntu.sources"
  mkdir -p "$root_dir/etc/apt/sources.list.d" "$root_dir/tmp" "$root_dir/workspace" \
    "$root_dir/etc/profile.d" "$root_dir/root/.pip" "$root_dir/usr/local/bin"
  chmod 1777 "$root_dir/tmp"
  printf 'deb %s %s main restricted universe multiverse\ndeb %s %s-updates main restricted universe multiverse\ndeb %s %s-security main restricted universe multiverse\n' \
    "$apt_mirror" "$release_codename" "$apt_mirror" "$release_codename" "$apt_mirror" "$release_codename" \
    > "$root_dir/etc/apt/sources.list"
  cp /etc/resolv.conf "$root_dir/etc/resolv.conf"
  printf '[global]\nindex-url = https://mirrors.aliyun.com/pypi/simple/\ntrusted-host = mirrors.aliyun.com\ntimeout = 60\n' > "$root_dir/etc/pip.conf"
  cp "$root_dir/etc/pip.conf" "$root_dir/root/.pip/pip.conf"
  printf 'registry=https://registry.npmmirror.com\nignore-scripts=true\n' > "$root_dir/etc/npmrc"
  printf 'export HOME=/root\nexport LANG=C.UTF-8\nexport PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nexport PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/\nexport PIP_TRUSTED_HOST=mirrors.aliyun.com\nexport NPM_CONFIG_REGISTRY=https://registry.npmmirror.com\n' > "$root_dir/etc/profile.d/daidai-runtime.sh"
  printf 'daidai-android\n' > "$root_dir/etc/hostname"
}

create_dev_nodes() {
  local root_dir="$1"
  mkdir -p "$root_dir/dev"
  rm -f "$root_dir/dev/null"
  mknod -m 666 "$root_dir/dev/null" c 1 3 2>/dev/null || true
  mknod -m 666 "$root_dir/dev/zero" c 1 5 2>/dev/null || true
  mknod -m 666 "$root_dir/dev/random" c 1 8 2>/dev/null || true
  mknod -m 666 "$root_dir/dev/urandom" c 1 9 2>/dev/null || true
  mknod -m 666 "$root_dir/dev/tty" c 5 0 2>/dev/null || true
}

resolve_seed_deb() {
  local packages_gz="$1" pkg_name="$2"
  zcat "$packages_gz" | awk -v pkg="$pkg_name" '
    /^Package: / { name = $2 }
    /^Filename: / { if (name == pkg) { print $2; exit } }
  '
}

run_in_rootfs() {
  local root_dir="$1"; shift
  chroot "$root_dir" /bin/bash -c "$*"
}

build_rootfs_for_abi() {
  local abi="$1"
  local ubuntu_arch
  ubuntu_arch="$(ubuntu_arch_for_abi "$abi")"
  local archive_name="ubuntu-base-${release_version}-base-${ubuntu_arch}.tar.gz"
  local downloads="$cache_root/downloads"
  local archive="$downloads/$archive_name"
  download "http://mirrors.aliyun.com/ubuntu-cdimage/ubuntu-base/releases/$release_version/release/$archive_name" "$archive"

  local work="$cache_root/work/$abi"
  local root_dir="$work/rootfs"
  local output_dir="$asset_root/$abi/ubuntu"
  rm -rf "$root_dir" "$output_dir"
  mkdir -p "$root_dir" "$output_dir"

  tar -xzf "$archive" -C "$root_dir"
  write_rootfs_config "$root_dir"
  create_dev_nodes "$root_dir"

  cp "$qemu_static" "$root_dir/usr/bin/qemu-aarch64-static"
  chmod 755 "$root_dir/usr/bin/qemu-aarch64-static"

  local packages_gz="$downloads/noble-main-arm64-Packages.gz"
  download "$apt_mirror/dists/$release_codename/main/binary-arm64/Packages.gz" "$packages_gz"
  local gpgv_deb ubuntu_keyring_deb
  gpgv_deb="$(resolve_seed_deb "$packages_gz" "gpgv")"
  ubuntu_keyring_deb="$(resolve_seed_deb "$packages_gz" "ubuntu-keyring")"
  test -n "$gpgv_deb" && test -n "$ubuntu_keyring_deb"

  local gpgv_file="$downloads/$(basename "$gpgv_deb")"
  local keyring_file="$downloads/$(basename "$ubuntu_keyring_deb")"
  download "$apt_mirror/$gpgv_deb" "$gpgv_file"
  download "$apt_mirror/$ubuntu_keyring_deb" "$keyring_file"
  cp "$gpgv_file" "$keyring_file" "$root_dir/tmp/"

  run_in_rootfs "$root_dir" "dpkg -i /tmp/$(basename "$gpgv_deb") /tmp/$(basename "$ubuntu_keyring_deb")"
  run_in_rootfs "$root_dir" "export DEBIAN_FRONTEND=noninteractive; apt-get update"
  run_in_rootfs "$root_dir" "export DEBIAN_FRONTEND=noninteractive; apt-get install -y --no-install-recommends ${required_packages[*]} ${extra_packages}"

  run_in_rootfs "$root_dir" "export HOME=/root; npm install -g pnpm@${pnpm_major} --registry=https://registry.npmmirror.com"

  for command in "${required_commands[@]}"; do
    test -x "$root_dir/usr/bin/$command" || test -x "$root_dir/bin/$command" || test -x "$root_dir/sbin/$command" || test -x "$root_dir/usr/sbin/$command" || test -x "$root_dir/usr/local/bin/$command" || {
      printf 'Required rootfs command is missing or not executable: %s\n' "$command" >&2
      exit 1
    }
  done
  test -s "$root_dir/etc/ssl/certs/ca-certificates.crt" || {
    printf 'Required CA certificate bundle is missing.\n' >&2
    exit 1
  }

  rm -f "$root_dir/usr/bin/qemu-aarch64-static"
  rm -f "$root_dir/tmp"/*.deb
  rm -rf "$root_dir/var/lib/apt/lists"/* "$root_dir/var/cache/apt/archives"/*

  tar --numeric-owner --sort=name --mtime='UTC 2026-01-01' -cf - -C "$root_dir" . | xz -6 -T0 > "$output_dir/rootfs.tar.gz.bin"
  sha256sum "$output_dir/rootfs.tar.gz.bin" | awk '{print $1}' > "$output_dir/rootfs.tar.gz.bin.sha256"
  python3 - "$output_dir/runtime-manifest.json" "$abi" "$ubuntu_arch" "$release_version" "$output_dir/rootfs.tar.gz.bin" "${required_packages[*]} ${extra_packages}" "${required_commands[*]}" <<'PY'
import hashlib, json, pathlib, sys
target, abi, arch, version, archive, packages, required_commands = sys.argv[1:]
path = pathlib.Path(archive)
pathlib.Path(target).write_text(json.dumps({
    "schema_version": 2, "abi": abi, "distribution": "ubuntu", "ubuntu_arch": arch, "ubuntu_version": version,
    "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size,
    "packages": packages.split(),
    "required_commands": required_commands.split(),
    "capabilities": {
        "package_manager": ["apt-get"],
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

for abi in $abis; do
  build_rootfs_for_abi "$abi"
done
