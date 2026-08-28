#!/usr/bin/env bash
# Build the Android Linux runtime native tools from official upstream source
# using the Android NDK toolchain (LLVM/Clang cross-compilation).
#
# Produces, per ABI:
#   app/android/app/src/main/jniLibs/{abi}/libdaidai_proot.so   (Termux PRoot)
#   app/android/app/src/main/jniLibs/{abi}/libproot_loader.so   (PRoot loader)
#   app/android/app/src/main/jniLibs/{abi}/libandroid-shmem.so  (SysV IPC shim)
#   app/android/app/src/main/jniLibs/{abi}/libtalloc.so         (PRoot allocator)
#   app/android/app/src/main/jniLibs/{abi}/libdaidai_busybox.so (BusyBox 1.36.1)
#   app/android/app/src/main/assets/android-runtime/{abi}/native-runtime-manifest.json
#
# All binaries are linked with 16 KB PT_LOAD alignment for Android 15+ and the
# manifest is stamped with a self-contained-source-build provenance (pinned
# upstream sources + SHA-256, no prebuilt binary imports).

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
matrix_tool="$repo_root/scripts/android-abi-matrix.py"
abis="$(python3 "$matrix_tool" list native --requested "${ANDROID_RUNTIME_ABIS:-}")"
ndk="${ANDROID_NDK_HOME:-/opt/android-sdk/android-ndk-r27}"
api="${ANDROID_API_LEVEL:-24}"
cache_root="${ANDROID_NATIVE_BUILD_CACHE:-$HOME/.cache/daidai-android-native-source}"
src_dir="$cache_root/src"
work_dir="$cache_root/work"
refresh_artifacts="${ANDROID_REFRESH_NATIVE_BUILD:-1}"
jobs="${ANDROID_NATIVE_JOBS:-$(nproc)}"
page_size=16384

PROOT_VERSION=5.1.107.92
PROOT_SHA256=29385d1ddb619a9c4449ab512bfd55032034b22f724ddf98fc95ff300ea32135
PROOT_URL="https://github.com/termux/proot/archive/refs/tags/v${PROOT_VERSION}.zip"
ANDROID_SHMEM_VERSION=7f0bd7e25dbdd146265aff7c6a890029e374622d
ANDROID_SHMEM_SHA256=48e80a854df55dd8eb2a5026cb3cc90a10165aae68c591acdabad24488ce621f
ANDROID_SHMEM_URL="https://github.com/termux/libandroid-shmem/archive/${ANDROID_SHMEM_VERSION}.tar.gz"
TALLOC_VERSION=2.4.3
TALLOC_SHA256=dc46c40b9f46bb34dd97fe41f548b0e8b247b77a918576733c528e83abd854dd
TALLOC_URL="https://www.samba.org/ftp/talloc/talloc-${TALLOC_VERSION}.tar.gz"
BUSYBOX_VERSION=1.36.1
BUSYBOX_SHA256=b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314
BUSYBOX_URL="https://busybox.net/downloads/busybox-${BUSYBOX_VERSION}.tar.bz2"
UTHASH_VERSION=2.3.0
UTHASH_SHA256=344175d0ae3d0d7651887f932fc224f6d133559f0fc83ee73fe641697d77211a
UTHASH_URL="https://raw.githubusercontent.com/troydhanson/uthash/v${UTHASH_VERSION}/src/uthash.h"
LINUX_UAPI_VERSION=6.6
KD_H_SHA256=0a1622b5c1b1418f4046d52fbf67e4d6ddefc1f0f42e4af04e926d95ce269a1e
KD_H_URL="https://raw.githubusercontent.com/torvalds/linux/v${LINUX_UAPI_VERSION}/include/uapi/linux/kd.h"
VT_H_SHA256=3a0629b67ca24af1c58808095036b5b84756ed4632bf13d4a68d819142a0ad5b
VT_H_URL="https://raw.githubusercontent.com/torvalds/linux/v${LINUX_UAPI_VERSION}/include/uapi/linux/vt.h"
PKT_SCHED_H_SHA256=04c56015aa80ad1019c2f193e9de9b36b55fcf261d9ed239c9ebb709328e0d47
PKT_SCHED_H_URL="https://raw.githubusercontent.com/torvalds/linux/v${LINUX_UAPI_VERSION}/include/uapi/linux/pkt_sched.h"

log() { printf '[native-build] %s\n' "$*" >&2; }

fetch_pinned() {
  local name="$1" url="$2" expected="$3"
  local archive="$src_dir/$name"
  if test -f "$archive"; then
    local actual
    actual="$(sha256sum "$archive" | cut -d' ' -f1)"
    if test "$actual" = "$expected"; then return; fi
  fi
  mkdir -p "$src_dir"
  curl -fL --connect-timeout 30 --max-time 600 -o "$archive" "$url"
  local actual
  actual="$(sha256sum "$archive" | cut -d' ' -f1)"
  if test "$actual" != "$expected"; then
    printf 'SHA-256 mismatch for %s: expected %s got %s\n' "$name" "$expected" "$actual" >&2
    exit 1
  fi
  log "fetched $name ($actual)"
}

target_and_toolchain() {
  case "$1" in
    arm64-v8a) printf '%s\t%s\t%s\n' aarch64-linux-android aarch64-linux-android arm64 ;;
    x86_64) printf '%s\t%s\t%s\n' x86_64-linux-android x86_64-linux-android x86_64 ;;
    *) printf 'Unsupported ABI: %s\n' "$1" >&2; exit 1 ;;
  esac
}

build_talloc() {
  local target="$1" prefix="$2"
  local tool="$prefix/bin/${target}${api}-clang"
  local out="$work_dir/$target/talloc"
  if test -f "$out/lib/libtalloc.a" -a -f "$out/lib/libtalloc.so" -a -f "$out/include/talloc.h"; then return; fi
  local root="$work_dir/$target/talloc-src"
  local anchor="$work_dir/$target/tls_anchor.o"
  rm -rf "$root" "$out"
  mkdir -p "$root" "$out"
  tar -xzf "$src_dir/talloc-${TALLOC_VERSION}.tar.gz" -C "$root" --strip-components=1
  "$tool" -fno-emulated-tls -c "$script_dir/talloc-tls-anchor.c" -o "$anchor"
  cd "$root"
  log "compiling talloc for $target"
  mkdir -p "$out/include" "$out/lib"
  "$tool" -c talloc.c -o "$out/talloc.o" \
    -I. -Ilib/replace -Ibin/default \
    -DNO_CONFIG_H -D_GNU_SOURCE -D_FILE_OFFSET_BITS=64 -D__STDC_WANT_LIB_EXT1__=1 \
    -DTALLOC_BUILD_VERSION_MAJOR=2 -DTALLOC_BUILD_VERSION_MINOR=4 -DTALLOC_BUILD_VERSION_RELEASE=3 \
    -DHAVE_CONSTRUCTOR_ATTRIBUTE=1 -DHAVE_STDBOOL_H=1 -DHAVE_BOOL=1 \
    -DHAVE_INTPTR_T=1 -DHAVE_UINTPTR_T=1 -DHAVE_PTRDIFF_T=1 \
    -DHAVE_DLFCN_H=1 -DHAVE_LIMITS_H=1 -DHAVE_STRING_H=1 \
    -DHAVE_C99_VSNPRINTF=1 -DHAVE_MEMMOVE=1 -DHAVE_STRNLEN=1 -DHAVE_VSNPRINTF=1 \
    -DHAVE_USLEEP=1 -DHAVE_VA_COPY=1 \
    -O2 -fPIC
  "$prefix/bin/llvm-ar" rcs "$out/lib/libtalloc.a" "$out/talloc.o"
  "$tool" -shared -Wl,-soname,libtalloc.so -Wl,-z,max-page-size=$page_size \
    "$out/talloc.o" "$anchor" -Wl,--undefined=daidai_tls_anchor -ldl -o "$out/lib/libtalloc.so"
  cp talloc.h "$out/include/"
  cat > "$out/include/talloc_version.h" <<'H'
/* Generated by prepare-android-native-source-build.sh */
#define TALLOC_VERSION_MAJOR 2
#define TALLOC_VERSION_MINOR 4
#define TALLOC_VERSION_RELEASE 3
#define TALLOC_VERSION_STRING "2.4.3"
H
  log "talloc staged at $out"
}

build_android_shmem() {
  local target="$1" prefix="$2"
  local tool="$prefix/bin/${target}${api}-clang"
  local out="$work_dir/$target/android-shmem"
  if test -f "$out/lib/libandroid-shmem.a" -a -f "$out/lib/libandroid-shmem.so" -a -f "$out/include/sys/shm.h"; then return; fi
  local root="$work_dir/$target/android-shmem-src"
  rm -rf "$root" "$out"
  mkdir -p "$root" "$out/lib" "$out/include/sys"
  tar -xzf "$src_dir/libandroid-shmem-${ANDROID_SHMEM_VERSION}.tar.gz" -C "$root" --strip-components=1
  "$tool" -O2 -fPIC -std=c11 -Wall -Wextra -include fcntl.h '-D_PATH_TMP="/tmp/"' \
    -c "$root/shmem.c" -o "$out/shmem.o"
  "$prefix/bin/llvm-ar" rcs "$out/lib/libandroid-shmem.a" "$out/shmem.o"
  "$tool" -shared -Wl,-soname,libandroid-shmem.so -Wl,--version-script="$root/exports.txt" \
    -Wl,-z,max-page-size=$page_size "$out/shmem.o" -llog -landroid -o "$out/lib/libandroid-shmem.so"
  cp "$root/shm.h" "$out/include/sys/shm.h"
  log "libandroid-shmem staged at $out"
}

build_proot() {
  local target="$1" prefix="$2" abi="$3" native_dir="$4" asset_dir="$5"
  local tool="$prefix/bin/${target}${api}-clang"
  local strip="$prefix/bin/llvm-strip"
  local root="$work_dir/$target/proot-src"
  local out="$work_dir/$target/proot"
  if test -f "$out/libdaidai_proot.so" -a -f "$out/libproot_loader.so" -a "${FORCE_REBUILD_PROOT:-0}" != "1"; then return; fi
  rm -rf "$root" "$out"
  mkdir -p "$root" "$out"
  unzip -q "$src_dir/proot-${PROOT_VERSION}.zip" -d "$root"
  root="$root/proot-${PROOT_VERSION}"
  mkdir -p "$root/lib/uthash/include"
  cp "$src_dir/uthash.h" "$root/lib/uthash/include/uthash.h"
  local talloc_pc="$work_dir/$target/talloc.pc"
  local talloc_inc="$work_dir/$target/talloc/include"
  local talloc_lib="$work_dir/$target/talloc/lib"
  local shmem_inc="$work_dir/$target/android-shmem/include"
  local shmem_lib="$work_dir/$target/android-shmem/lib"
  # Android arm64 Bionic 强制 PT_TLS 段对齐 >= 64 字节（否则 dlopen 报 TLS underaligned），
  # 链接一个 aligned(64) 的 TLS 锚点变量把段对齐抬到 64。
  local anchor="$work_dir/$target/tls_anchor.o"
  "$tool" -fno-emulated-tls -c "$script_dir/talloc-tls-anchor.c" -o "$anchor"
  # make 命令行变量会覆盖 GNUmakefile 内 "CFLAGS/CPPFLAGS += ..."，
  # 必须补齐默认的 -I. 与 uthash 路径，以及 talloc 的 include/lib。
  local cppflags="-D_FILE_OFFSET_BITS=64 -D_GNU_SOURCE -DARG_MAX=131072 -I. -I$root/lib/uthash/include"
  local proot_cppflags="$cppflags -include string.h"
  local cflags="-O2 -g -fPIC -D_FILE_OFFSET_BITS=64 -D_GNU_SOURCE -DARG_MAX=131072 -DWITH_LIBANDROID_SHMEM -DPROOT_UNBUNDLE_LOADER=\\\"/\\\" -DVERSION=\\\"${PROOT_VERSION}\\\" -I$talloc_inc -I$shmem_inc"
  cat > "$talloc_pc" <<EOF
prefix=$work_dir/$target/talloc
exec_prefix=\${prefix}
libdir=\${prefix}/lib
includedir=\${prefix}/include
Name: talloc
Description: Samba talloc memory allocator
Version: $TALLOC_VERSION
Libs: -L\${libdir} -ltalloc
Cflags: -I\${includedir}
EOF
  cd "$root/src"
  sed -i "s|,-z,noexecstack|,-z,noexecstack,-z,max-page-size=$page_size|" GNUmakefile
  log "building Termux proot loader for $abi"
  PKG_CONFIG_PATH="$(dirname "$talloc_pc")" \
    make -C . V=1 \
      CC="$tool" STRIP="$strip" OBJCOPY="$prefix/bin/llvm-objcopy" OBJDUMP="$prefix/bin/llvm-objdump" \
      CPPFLAGS="$cppflags" CFLAGS="$cflags" \
      LDFLAGS="-Wl,-z,noexecstack -Wl,-z,max-page-size=$page_size $anchor -Wl,--undefined=daidai_tls_anchor" \
      loader/loader
  log "building Termux proot binary for $abi"
  mkdir -p extension/ashmem_memfd
  "$tool" $proot_cppflags $cflags -MD -c \
    "$root/src/extension/ashmem_memfd/ashmem_memfd.c" \
    -o extension/ashmem_memfd/ashmem_memfd.o
  PKG_CONFIG_PATH="$(dirname "$talloc_pc")" \
    make -C . V=1 \
      CC="$tool" STRIP="$strip" OBJCOPY="$prefix/bin/llvm-objcopy" OBJDUMP="$prefix/bin/llvm-objdump" \
      CPPFLAGS="$cppflags" CFLAGS="$cflags" \
      PROOT_WITH_LIBANDROID_SHMEM=1 \
      PROOT_UNBUNDLE_LOADER=/ \
      LDFLAGS="-Wl,-z,noexecstack -Wl,-z,max-page-size=$page_size -L$talloc_lib -L$shmem_lib -ltalloc -landroid-shmem $anchor -Wl,--undefined=daidai_tls_anchor" \
      proot
  cp loader/loader "$out/libproot_loader.so"
  cp proot "$out/libdaidai_proot.so"
  chmod 755 "$out/libdaidai_proot.so" "$out/libproot_loader.so"
  log "proot staged for $abi"
}

build_busybox() {
  local target="$1" prefix="$2" abi="$3"
  local tool="$prefix/bin/${target}${api}-clang"
  local root="$work_dir/$target/busybox-src"
  local out="$work_dir/$target/busybox"
  if test -f "$out/busybox"; then return; fi
  rm -rf "$root" "$out"
  mkdir -p "$root" "$out"
  tar -xjf "$src_dir/busybox-${BUSYBOX_VERSION}.tar.bz2" -C "$root" --strip-components=1
  cd "$root"
  log "configuring busybox for $abi"
  make defconfig >/dev/null 2>&1
  sed -i 's/^# CONFIG_STATIC is not set$/CONFIG_STATIC=y/' .config
  sed -i 's/^CONFIG_CROSS_COMPILER_PREFIX=.*$/CONFIG_CROSS_COMPILER_PREFIX=""/' .config || true
  # busybox 1.36 编译全部 applet 源文件（禁用的 applet 仅跳过函数体，头文件仍会包含），
  # bionic 精简的 uapi 头缺 <sys/kd.h>/<sys/vt.h> 与 <linux/pkt_sched.h> 的 CBQ 部分，
  # 从 Linux uapi 提供一份。
  mkdir -p "$out/inc/sys" "$out/inc/linux"
  cp "$src_dir/kd.h" "$out/inc/sys/kd.h"
  cp "$src_dir/vt.h" "$out/inc/sys/vt.h"
  cp "$src_dir/pkt_sched.h" "$out/inc/linux/pkt_sched.h"
  # bionic 在 API 24 下缺 gethostid/sigisemptyset 且隐藏多个 glibc 接口，
  # 注入 shim 声明头（仅 C 文件；.S 汇编预处理定义 __ASSEMBLER__ 会自动跳过），
  # 实现由链接的 bionic-compat.o 提供。
  "$tool" -O2 -fPIC -c "$script_dir/bionic-compat.c" -o "$out/bionic-compat.o"
  # Android 不适用或依赖 bionic 缺失 SysV IPC / syslog shm / swap 的 applet 禁用。
  sed -i \
    -e 's/^CONFIG_LOGREAD=y$/# CONFIG_LOGREAD is not set/' \
    -e 's/^CONFIG_FEATURE_LOGREAD_REDUCED_LOCKING=y$/# CONFIG_FEATURE_LOGREAD_REDUCED_LOCKING is not set/' \
    -e 's/^CONFIG_FEATURE_IPC_SYSLOG=y$/# CONFIG_FEATURE_IPC_SYSLOG is not set/' \
    -e 's/^CONFIG_IPCRM=y$/# CONFIG_IPCRM is not set/' \
    -e 's/^CONFIG_IPCS=y$/# CONFIG_IPCS is not set/' \
    -e 's/^CONFIG_SWAPON=y$/# CONFIG_SWAPON is not set/' \
    -e 's/^CONFIG_FEATURE_SWAPON_DISCARD=y$/# CONFIG_FEATURE_SWAPON_DISCARD is not set/' \
    -e 's/^CONFIG_FEATURE_SWAPON_PRI=y$/# CONFIG_FEATURE_SWAPON_PRI is not set/' \
    -e 's/^CONFIG_SWAPOFF=y$/# CONFIG_SWAPOFF is not set/' \
    -e 's/^CONFIG_FEATURE_SWAPONOFF_LABEL=y$/# CONFIG_FEATURE_SWAPONOFF_LABEL is not set/' \
    .config
  # bionic 的 <linux/ipv6.h> 已定义 in6_ifreq，与 busybox 内置定义冲突，跳过 Android 下定义。
  sed -i 's/^#if ENABLE_FEATURE_IPV6$/#if ENABLE_FEATURE_IPV6 \&\& !defined(__ANDROID__)/' \
    networking/ifconfig.c networking/interface.c
  # busybox 默认假设 HAVE_SETBIT/HAVE_UNLOCKED_STDIO 存在；bionic 的 sys/param.h 无 setbit/clrbit。
  sed -i 's|^#if defined(__UCLIBC__)$|#if defined(__ANDROID__)\n# undef HAVE_SETBIT\n# undef HAVE_UNLOCKED_STDIO\n#endif\n\n#if defined(__UCLIBC__)|' \
    include/platform.h
  # bionic libc API 21+ 提供 strchrnul，去掉 busybox 对它的 undef（避免 duplicate symbol）。
  sed -i '/# undef HAVE_STRCHRNUL/d' include/platform.h
  # missing_syscalls 的 getsid/sethostname/adjtimex 已由 bionic libc 提供，只保留 pivot_root。
  sed -i '/pid_t getsid/,/^}/d;/^int sethostname/,/^}/d;/^struct timex;/,/^}/d' libbb/missing_syscalls.c
  make silentoldconfig </dev/null >/dev/null 2>&1
  log "building busybox for $abi (static, 16KB-aligned)"
  # 并行 make 在 archival/libarchive 之后会无错误崩溃（疑似 host 层资源限制），串行构建稳定。
  # 注意：bionic-compat.o 只能通过 CFLAGS_busybox 进入最终链接（busybox 的 ld -r 会用
  # 非 -Wl 开头的 LDFLAGS 把对象合并进 built-in.o，造成 duplicate symbol）；-static 显式提供，
  # 否则链接会用 API 24 的 libc.so stub 而缺失 glob/syncfs 等符号。
  make -j1 \
    CC="$tool" AR="$prefix/bin/llvm-ar" NM="$prefix/bin/llvm-nm" \
    STRIP="$prefix/bin/llvm-strip" RANLIB="$prefix/bin/llvm-ranlib" \
    EXTRA_CFLAGS="-I$out/inc -include $script_dir/bionic-compat.h" \
    LDLIBS="m" \
    LDFLAGS="-Wl,-z,max-page-size=$page_size" \
    CFLAGS_busybox="-static $out/bionic-compat.o" \
    CONFIG_STATIC=y busybox >"$out/busybox-build.log" 2>&1
  cp busybox "$out/busybox"
  chmod 755 "$out/busybox"
  log "busybox staged for $abi"
}

write_manifest() {
  local abi="$1" native_dir="$2" target="$3"
  local manifest="$repo_root/app/android/app/src/main/assets/android-runtime/$abi/native-runtime-manifest.json"
  local asset_dir="$(dirname "$manifest")"
  mkdir -p "$asset_dir"
  python3 - "$manifest" "$abi" "$native_dir" "$target" <<'PY'
import hashlib, json, pathlib, sys

target, abi, native_dir, target_triple = sys.argv[1:]
root = pathlib.Path(native_dir)
roles = {
    "libdaidai_proot.so": "proot",
    "libproot_loader.so": "proot-loader",
    "libdaidai_busybox.so": "busybox",
}
artifacts = []
for path in sorted(root.glob("*.so")):
    name = path.name
    role = roles.get(name, "packaged-native-library")
    data = path.read_bytes()
    artifacts.append({"name": name, "role": role, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)})
manifest = {
    "schema_version": 1,
    "abi": abi,
    "minimum_load_alignment": 16384,
    "provenance": {
        "strategy": "self-contained-source-build",
        "source_build": True,
        "source_patch_applied": False,
        "patches_applied": [],
        "toolchain": {
            "name": "android-ndk-r27",
            "api_level": 24,
            "compiler": f"{target_triple}24-clang",
            "load_alignment": 16384,
        },
        "upstream_source": [
            {"name": "proot", "version": "5.1.107.92", "url": "https://github.com/termux/proot/archive/refs/tags/v5.1.107.92.zip", "sha256": "29385d1ddb619a9c4449ab512bfd55032034b22f724ddf98fc95ff300ea32135"},
            {"name": "libandroid-shmem", "version": "7f0bd7e25dbdd146265aff7c6a890029e374622d", "url": "https://github.com/termux/libandroid-shmem/archive/7f0bd7e25dbdd146265aff7c6a890029e374622d.tar.gz", "sha256": "48e80a854df55dd8eb2a5026cb3cc90a10165aae68c591acdabad24488ce621f"},
            {"name": "talloc", "version": "2.4.3", "url": "https://www.samba.org/ftp/talloc/talloc-2.4.3.tar.gz", "sha256": "dc46c40b9f46bb34dd97fe41f548b0e8b247b77a918576733c528e83abd854dd"},
            {"name": "busybox", "version": "1.36.1", "url": "https://busybox.net/downloads/busybox-1.36.1.tar.bz2", "sha256": "b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314"},
        ],
        "binary_transforms": [],
        "runtime_overrides": {
            "PROOT_LOADER": "libproot_loader.so",
        },
        "note": "Built from pinned termux/proot fork with SysV IPC support and other pinned upstream sources using the Android NDK.",
    },
    "artifacts": artifacts,
}
pathlib.Path(target).write_text(json.dumps(manifest, indent=2) + "\n")
PY
  log "manifest written for $abi"
}

stage_and_verify() {
  local abi="$1" native_dir="$2"
  python3 "$script_dir/verify-android-linux-runtime.py" \
    --native-dir "$native_dir" \
    --native-manifest "$repo_root/app/android/app/src/main/assets/android-runtime/$abi/native-runtime-manifest.json"
  log "verified native tools for $abi"
}

if test "$refresh_artifacts" != "1"; then
  for abi in $abis; do
    native_dir="$repo_root/app/android/app/src/main/jniLibs/$abi"
    manifest="$repo_root/app/android/app/src/main/assets/android-runtime/$abi/native-runtime-manifest.json"
    python3 "$script_dir/verify-android-linux-runtime.py" --native-dir "$native_dir" --native-manifest "$manifest"
  done
  exit 0
fi

if test ! -d "$ndk/toolchains/llvm/prebuilt/linux-x86_64/bin"; then
  printf 'Android NDK r27 not found at %s\n' "$ndk" >&2
  exit 1
fi

fetch_pinned "proot-${PROOT_VERSION}.zip" "$PROOT_URL" "$PROOT_SHA256"
fetch_pinned "libandroid-shmem-${ANDROID_SHMEM_VERSION}.tar.gz" "$ANDROID_SHMEM_URL" "$ANDROID_SHMEM_SHA256"
fetch_pinned "talloc-${TALLOC_VERSION}.tar.gz" "$TALLOC_URL" "$TALLOC_SHA256"
fetch_pinned "busybox-${BUSYBOX_VERSION}.tar.bz2" "$BUSYBOX_URL" "$BUSYBOX_SHA256"

fetch_pinned "uthash.h" "$UTHASH_URL" "$UTHASH_SHA256"
fetch_pinned "kd.h" "$KD_H_URL" "$KD_H_SHA256"
fetch_pinned "vt.h" "$VT_H_URL" "$VT_H_SHA256"
fetch_pinned "pkt_sched.h" "$PKT_SCHED_H_URL" "$PKT_SCHED_H_SHA256"

for abi in $abis; do
  IFS=$'\t' read -r target _ _ < <(target_and_toolchain "$abi")
  prefix="$ndk/toolchains/llvm/prebuilt/linux-x86_64"
  native_dir="$repo_root/app/android/app/src/main/jniLibs/$abi"
  mkdir -p "$native_dir"
  build_talloc "$target" "$prefix"
  build_android_shmem "$target" "$prefix"
  build_proot "$target" "$prefix" "$abi" "$native_dir" "$work_dir"
  build_busybox "$target" "$prefix" "$abi"
  cp "$work_dir/$target/proot/libdaidai_proot.so" "$native_dir/"
  cp "$work_dir/$target/proot/libproot_loader.so" "$native_dir/"
  cp "$work_dir/$target/talloc/lib/libtalloc.so" "$native_dir/"
  cp "$work_dir/$target/android-shmem/lib/libandroid-shmem.so" "$native_dir/"
  cp "$work_dir/$target/busybox/busybox" "$native_dir/libdaidai_busybox.so"
  chmod 755 "$native_dir/libdaidai_proot.so" "$native_dir/libproot_loader.so" \
    "$native_dir/libdaidai_busybox.so" "$native_dir/libtalloc.so" "$native_dir/libandroid-shmem.so"
  write_manifest "$abi" "$native_dir" "$target"
  python3 "$script_dir/sync-android-native-manifest.py" \
    --native-dir "$native_dir" \
    --manifest "$repo_root/app/android/app/src/main/assets/android-runtime/$abi/native-runtime-manifest.json"
  stage_and_verify "$abi" "$native_dir"
  log "Android native tools built and staged for $abi"
done

log "all native tools built successfully"
