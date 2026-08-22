#!/usr/bin/env bash
set -euo pipefail

app_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
abis="${ANDROID_RUNTIME_ABIS:-armeabi-v7a arm64-v8a x86_64}"
toolchain=""
for candidate in "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" "${ANDROID_NDK_ROOT:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" "${ANDROID_HOME:-}/ndk/"*/toolchains/llvm/prebuilt/linux-x86_64/bin; do
  test -d "$candidate" && toolchain="$candidate" && break
done
test -n "$toolchain" || { printf 'Android NDK toolchain not found\n' >&2; exit 2; }

for abi in $abis; do
  case "$abi" in
    armeabi-v7a) goarch=arm; goarm=7; cc="$toolchain/armv7a-linux-androideabi28-clang" ;;
    arm64-v8a) goarch=arm64; goarm=; cc="$toolchain/aarch64-linux-android28-clang" ;;
    x86_64) goarch=amd64; goarm=; cc="$toolchain/x86_64-linux-android28-clang" ;;
    *) printf 'Unsupported ABI: %s\n' "$abi" >&2; exit 1 ;;
  esac
  output="$app_root/android/app/src/main/jniLibs/$abi/libyaegi_exec.so"
  mkdir -p "$(dirname "$output")"
  env GOWORK=off CGO_ENABLED=1 CC="$cc" GOOS=android GOARCH="$goarch" GOARM="$goarm" \
    go -C "$app_root/runtime-tools/yaegi" build -trimpath -buildmode=pie -ldflags='-s -w' -o "$output" .
  chmod 755 "$output"
done
