#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
abis="${ANDROID_RUNTIME_ABIS:-arm64-v8a}"
repo_base="${TERMUX_REPO_BASE:-https://packages.termux.dev/apt/termux-main}"
cache_root="${ANDROID_PROOT_BUSYBOX_CACHE:-$HOME/.cache/daidai-android-proot-busybox}"
download_dir="$cache_root/downloads"

# Every artifact is pinned to an immutable package coordinate and SHA-256. This
# is intentionally a verified binary import: no source patch is claimed here.
package_metadata() {
  case "$1" in
    proot) printf '%s\t%s\t%s\n' '5.1.107.91' 'pool/main/p/proot/proot_5.1.107.91_aarch64.deb' '1ad09f7ddf65f297a7b59a398cd1f23a48748b1144b225f0c1e2d9f447ef3efc' ;;
    busybox) printf '%s\t%s\t%s\n' '1.38.0-1' 'pool/main/b/busybox/busybox_1.38.0-1_aarch64.deb' '1bb7f1d4c00cadd0e1117b6dd7110311b8bf749ef00b486e96cfdc11c98f8fd9' ;;
    libtalloc) printf '%s\t%s\t%s\n' '2.4.3' 'pool/main/libt/libtalloc/libtalloc_2.4.3_aarch64.deb' 'ac81ad623d74c209718b9f3acb2dd702cc8a88c431e820d212229910b4db29da' ;;
    libandroid-shmem) printf '%s\t%s\t%s\n' '0.7' 'pool/main/liba/libandroid-shmem/libandroid-shmem_0.7_aarch64.deb' '0da3a24d558b93c92bcf8d611e0826a99ff96e396b148e6cdf33b47c47c57ff6' ;;
    libandroid-selinux) printf '%s\t%s\t%s\n' '14.0.0.11-1' 'pool/main/liba/libandroid-selinux/libandroid-selinux_14.0.0.11-1_aarch64.deb' '00afd8c34087c2864737b51fd9d104dc5e955f6ec3c0f50c0c7ef5b4a56866b9' ;;
    pcre2) printf '%s\t%s\t%s\n' '10.47' 'pool/main/p/pcre2/pcre2_10.47_aarch64.deb' '51f915d22de639bfca6ec029ae613987bbe3bc73626eede13319fd2e95f50b63' ;;
    *) printf 'Unknown pinned package: %s\n' "$1" >&2; exit 1 ;;
  esac
}

download() {
  local url="$1"
  local output="$2"
  mkdir -p "$(dirname "$output")"
  curl -fL --connect-timeout 30 --max-time 600 -o "$output" "$url"
}

extract_deb() {
  local package_name="$1"
  local version filename expected
  IFS=$'\t' read -r version filename expected < <(package_metadata "$package_name")
  local deb="$download_dir/$(basename "$filename")"
  download "$repo_base/$filename" "$deb"
  printf '%s  %s\n' "$expected" "$deb" | sha256sum -c -
  local out="$work_dir/$package_name"
  mkdir -p "$out"
  if ar t "$deb" | grep -q '^data.tar.xz$'; then
    ar p "$deb" data.tar.xz | tar -xJf - -C "$out"
  elif ar t "$deb" | grep -q '^data.tar.gz$'; then
    ar p "$deb" data.tar.gz | tar -xzf - -C "$out"
  elif ar t "$deb" | grep -q '^data.tar.zst$'; then
    ar p "$deb" data.tar.zst | tar --zstd -xf - -C "$out"
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

stage_glob() {
  local pattern="$1"
  local target="$2"
  local matches=($pattern)
  test "${#matches[@]}" = 1 || {
    printf 'Expected one extracted library for %s, found %s\n' "$pattern" "${#matches[@]}" >&2
    exit 1
  }
  stage_binary "${matches[0]}" "$target"
}

rewrite_needed() {
  local binary="$1"
  local old_name="$2"
  local new_name="$3"
  python3 - "$binary" "$old_name" "$new_name" <<'PY'
import pathlib, sys

path, old_name, new_name = pathlib.Path(sys.argv[1]), sys.argv[2].encode(), sys.argv[3].encode()
if len(old_name) != len(new_name):
    raise SystemExit("DT_NEEDED rewrite names must have equal lengths")
data = path.read_bytes()
if data.count(old_name) != 1:
    raise SystemExit(f"expected exactly one {old_name.decode()} reference in {path}")
path.write_bytes(data.replace(old_name, new_name))
PY
}

termux_arch_for_abi() {
  case "$1" in
    arm64-v8a) printf aarch64 ;;
    *) printf 'Unsupported ABI: %s\n' "$1" >&2; exit 1 ;;
  esac
}

for abi in $abis; do
  termux_arch="$(termux_arch_for_abi "$abi")"
  native_dir="$repo_root/app/android/app/src/main/jniLibs/$abi"
  asset_dir="$repo_root/app/android/app/src/main/assets/android-runtime/$abi"
  work_dir="$cache_root/work-$abi-$(date +%s)-$$"
  mkdir -p "$download_dir" "$asset_dir"
  for package in busybox proot libtalloc libandroid-shmem libandroid-selinux pcre2; do extract_deb "$package"; done
  stage_binary "$work_dir/busybox/data/data/com.termux/files/usr/bin/busybox" liboperit_busybox.so
  stage_binary "$work_dir/proot/data/data/com.termux/files/usr/bin/proot" liboperit_proot.so
  stage_binary "$work_dir/proot/data/data/com.termux/files/usr/libexec/proot/loader" libproot_loader.so
  rewrite_needed "$native_dir/liboperit_proot.so" libtalloc.so.2 libtalloc_2.so
  rewrite_needed "$native_dir/liboperit_busybox.so" libbusybox.so.1.38.0 libbusybox_1_38_0.so
  stage_glob "$work_dir/libtalloc/data/data/com.termux/files/usr/lib/libtalloc.so.2.*" libtalloc_2.so
  stage_binary "$native_dir/libtalloc_2.so" libtalloc_v2.so
  stage_binary "$work_dir/libandroid-shmem/data/data/com.termux/files/usr/lib/libandroid-shmem.so" libandroid-shmem.so
  stage_glob "$work_dir/busybox/data/data/com.termux/files/usr/lib/libbusybox.so.*" libbusybox_1_38_0.so
  stage_binary "$native_dir/libbusybox_1_38_0.so" libbusybox_v138.so
  stage_binary "$work_dir/libandroid-selinux/data/data/com.termux/files/usr/lib/libandroid-selinux.so" libandroid-selinux.so
  stage_binary "$work_dir/pcre2/data/data/com.termux/files/usr/lib/libpcre2-8.so" libpcre2-8.so
  python3 - "$asset_dir/native-runtime-manifest.json" "$abi" "$repo_base" "$native_dir" <<'PY'
import hashlib, json, pathlib, sys

target, abi, repository, native_dir = sys.argv[1:]
root = pathlib.Path(native_dir)
packages = {
    "proot": ("5.1.107.91", "pool/main/p/proot/proot_5.1.107.91_aarch64.deb", "1ad09f7ddf65f297a7b59a398cd1f23a48748b1144b225f0c1e2d9f447ef3efc"),
    "busybox": ("1.38.0-1", "pool/main/b/busybox/busybox_1.38.0-1_aarch64.deb", "1bb7f1d4c00cadd0e1117b6dd7110311b8bf749ef00b486e96cfdc11c98f8fd9"),
    "libtalloc": ("2.4.3", "pool/main/libt/libtalloc/libtalloc_2.4.3_aarch64.deb", "ac81ad623d74c209718b9f3acb2dd702cc8a88c431e820d212229910b4db29da"),
    "libandroid-shmem": ("0.7", "pool/main/liba/libandroid-shmem/libandroid-shmem_0.7_aarch64.deb", "0da3a24d558b93c92bcf8d611e0826a99ff96e396b148e6cdf33b47c47c57ff6"),
    "libandroid-selinux": ("14.0.0.11-1", "pool/main/liba/libandroid-selinux/libandroid-selinux_14.0.0.11-1_aarch64.deb", "00afd8c34087c2864737b51fd9d104dc5e955f6ec3c0f50c0c7ef5b4a56866b9"),
    "pcre2": ("10.47", "pool/main/p/pcre2/pcre2_10.47_aarch64.deb", "51f915d22de639bfca6ec029ae613987bbe3bc73626eede13319fd2e95f50b63"),
}
roles = {
    "liboperit_proot.so": "proot",
    "libproot_loader.so": "proot-loader",
    "liboperit_busybox.so": "busybox-launcher",
    "libtalloc_2.so": "proot-dependency",
    "libtalloc_v2.so": "compatibility-alias",
    "libandroid-shmem.so": "proot-dependency",
    "libbusybox_1_38_0.so": "busybox-dependency",
    "libbusybox_v138.so": "compatibility-alias",
    "libandroid-selinux.so": "busybox-dependency",
    "libpcre2-8.so": "busybox-dependency",
}
artifacts = []
for name, role in roles.items():
    path = root / name
    data = path.read_bytes()
    artifacts.append({"name": name, "role": role, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)})
manifest = {
    "schema_version": 1,
    "abi": abi,
    "minimum_load_alignment": 16384,
    "provenance": {
        "strategy": "pinned-termux-binary-packages",
        "repository": repository,
        "termux_recipe": {
            "commit": "670227d182d0186b6c8112beedc1f188fcbbcf0e",
            "url": "https://github.com/termux/termux-packages/blob/670227d182d0186b6c8112beedc1f188fcbbcf0e/packages/proot/build.sh",
        },
        "upstream_source": {
            "url": "https://github.com/termux/proot/archive/v5.1.107.91.zip",
            "sha256": "a7bc2fab34bf9a39073e8291f08a662e848c61a67494e59f5f84f5ca10690128",
        },
        "source_build": False,
        "source_patch_applied": False,
        "patches_applied": [],
        "binary_transforms": [
            {"artifact": "liboperit_proot.so", "operation": "dt-needed-equal-length-rewrite", "from": "libtalloc.so.2", "to": "libtalloc_2.so"},
            {"artifact": "liboperit_busybox.so", "operation": "dt-needed-equal-length-rewrite", "from": "libbusybox.so.1.38.0", "to": "libbusybox_1_38_0.so"},
        ],
        "runtime_overrides": {
            "PROOT_LOADER": "libproot_loader.so",
        },
        "note": "Pinned upstream binaries are verified fail-closed; this manifest makes no source-patch claim.",
    },
    "packages": [
        {"name": name, "version": values[0], "url": f"{repository}/{values[1]}", "sha256": values[2]}
        for name, values in packages.items()
    ],
    "artifacts": artifacts,
}
pathlib.Path(target).write_text(json.dumps(manifest, indent=2) + "\n")
PY
  python3 "$script_dir/verify-android-linux-runtime.py" \
    --native-dir "$native_dir" \
    --native-manifest "$asset_dir/native-runtime-manifest.json"
  printf 'Android runtime native tools staged for %s in %s\n' "$abi" "$native_dir"
done
