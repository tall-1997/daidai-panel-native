#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
abis="${ANDROID_RUNTIME_ABIS:-armeabi-v7a arm64-v8a x86_64}"
repo_base="${TERMUX_REPO_BASE:-https://packages.termux.dev/apt/termux-main}"
cache_root="${ANDROID_PROOT_BUSYBOX_CACHE:-$HOME/.cache/daidai-android-proot-busybox}"
work_dir="$cache_root/work-$(date +%s)-$$"
download_dir="$cache_root/downloads"

download() {
  local url="$1"
  local output="$2"
  mkdir -p "$(dirname "$output")"
  curl -fL --connect-timeout 30 --max-time 600 -o "$output" "$url"
}

ensure_index() {
  mkdir -p "$download_dir"
  download "$repo_base/dists/stable/main/binary-$termux_arch/Packages" "$packages_text"
}

package_field() {
  local package_name="$1"
  local field="$2"
  awk -v package_name="$package_name" -v field="$field" '
    BEGIN { RS=""; FS="\n" }
    $0 ~ ("(^|\n)Package: " package_name "(\n|$)") {
      for (i = 1; i <= NF; i++) {
        if ($i ~ ("^" field ": ")) {
          sub("^" field ": ", "", $i)
          print $i
          exit
        }
      }
    }
  ' "$packages_text"
}

extract_deb() {
  local package_name="$1"
  local filename
  filename="$(package_field "$package_name" Filename)"
  test -n "$filename" || {
    printf 'Package not found in Termux index: %s\n' "$package_name" >&2
    exit 1
  }
  local deb="$download_dir/$(basename "$filename")"
  download "$repo_base/$filename" "$deb"
  expected="$(package_field "$package_name" SHA256)"
  test -n "$expected"
  printf '%s  %s\n' "$expected" "$deb" | sha256sum -c -
  local out="$work_dir/$package_name"
  mkdir -p "$out"
  if ar t "$deb" | grep -q '^data.tar.xz$'; then
    ar p "$deb" data.tar.xz | tar -xJf - -C "$out"
  elif ar t "$deb" | grep -q '^data.tar.gz$'; then
    ar p "$deb" data.tar.gz | tar -xzf - -C "$out"
  else
    printf 'Unsupported deb payload format: %s\n' "$deb" >&2
    exit 1
  fi
}

stage_binary() {
  local source="$1"
  local target="$2"
  test -f "$source" || {
    printf 'Missing extracted binary: %s\n' "$source" >&2
    exit 1
  }
  mkdir -p "$native_dir"
  cp "$source" "$native_dir/$target"
  chmod 755 "$native_dir/$target"
}

termux_arch_for_abi() {
  case "$1" in
    armeabi-v7a) printf arm ;;
    arm64-v8a) printf aarch64 ;;
    x86_64) printf x86_64 ;;
    *) printf 'Unsupported ABI: %s\n' "$1" >&2; exit 1 ;;
  esac
}

for abi in $abis; do
  termux_arch="$(termux_arch_for_abi "$abi")"
  native_dir="$repo_root/app/android/app/src/main/jniLibs/$abi"
  packages_text="$download_dir/Packages-$termux_arch"
  work_dir="$cache_root/work-$abi-$(date +%s)-$$"
  ensure_index
  for package in busybox proot libtalloc libandroid-shmem; do extract_deb "$package"; done
  stage_binary "$work_dir/busybox/data/data/com.termux/files/usr/bin/busybox" liboperit_busybox.so
  stage_binary "$work_dir/proot/data/data/com.termux/files/usr/bin/proot" liboperit_proot.so
  test -f "$work_dir/libtalloc/data/data/com.termux/files/usr/lib/libtalloc.so" && stage_binary "$work_dir/libtalloc/data/data/com.termux/files/usr/lib/libtalloc.so" libtalloc_v2.so
  test -f "$work_dir/libandroid-shmem/data/data/com.termux/files/usr/lib/libandroid-shmem.so" && stage_binary "$work_dir/libandroid-shmem/data/data/com.termux/files/usr/lib/libandroid-shmem.so" libandroid-shmem.so
  for source in "$work_dir/busybox/data/data/com.termux/files/usr/lib/"libbusybox.so*; do
    test -f "$source" && stage_binary "$source" libbusybox_v138.so
  done
  printf 'Android runtime native tools staged for %s in %s\n' "$abi" "$native_dir"
done
