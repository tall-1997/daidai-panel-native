#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
abi="${ANDROID_RUNTIME_ABI:-arm64-v8a}"
termux_arch="${TERMUX_PACKAGE_ARCH:-aarch64}"
repo_base="${TERMUX_REPO_BASE:-https://packages.termux.dev/apt/termux-main}"
cache_root="${ANDROID_PROOT_BUSYBOX_CACHE:-$HOME/.cache/daidai-android-proot-busybox}"
work_dir="$cache_root/work-$(date +%s)-$$"
download_dir="$cache_root/downloads"
native_dir="$repo_root/app/android/app/src/main/jniLibs/$abi"

packages_text="$download_dir/Packages-$termux_arch"

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

package_filename() {
  local package_name="$1"
  awk -v package_name="$package_name" '
    BEGIN { RS=""; FS="\n" }
    $0 ~ ("(^|\n)Package: " package_name "(\n|$)") {
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^Filename: /) {
          sub(/^Filename: /, "", $i)
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
  filename="$(package_filename "$package_name")"
  test -n "$filename" || {
    printf 'Package not found in Termux index: %s\n' "$package_name" >&2
    exit 1
  }
  local deb="$download_dir/$(basename "$filename")"
  download "$repo_base/$filename" "$deb"
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

ensure_index
extract_deb busybox
extract_deb proot
extract_deb libtalloc

stage_binary "$work_dir/busybox/data/data/com.termux/files/usr/bin/busybox" liboperit_busybox.so
stage_binary "$work_dir/proot/data/data/com.termux/files/usr/bin/proot" liboperit_proot.so

if test -f "$work_dir/libtalloc/data/data/com.termux/files/usr/lib/libtalloc.so"; then
  stage_binary "$work_dir/libtalloc/data/data/com.termux/files/usr/lib/libtalloc.so" libtalloc.so
fi

printf 'Android runtime native tools staged in %s\n' "$native_dir"
