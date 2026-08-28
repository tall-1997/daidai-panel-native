#!/usr/bin/env bash
set -euo pipefail

app_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$(cd "$app_root/.." && pwd)"
matrix_tool="$repo_root/scripts/android-abi-matrix.py"
abis="$(python3 "$matrix_tool" list yaegi --requested "${ANDROID_RUNTIME_ABIS:-}")"
toolchain=""
for candidate in "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" "${ANDROID_NDK_ROOT:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" "${ANDROID_HOME:-}/ndk/"*/toolchains/llvm/prebuilt/linux-x86_64/bin; do
  test -d "$candidate" && toolchain="$candidate" && break
done
test -n "$toolchain" || { printf 'Android NDK toolchain not found\n' >&2; exit 2; }

for abi in $abis; do
  goarch="$(python3 "$matrix_tool" get "$abi" goarch)"
  ndk_triple="$(python3 "$matrix_tool" get "$abi" ndk_triple)"
  cc="$toolchain/${ndk_triple}24-clang"
  goarm=""
  output="$app_root/android/app/src/main/jniLibs/$abi/libyaegi_exec.so"
  mkdir -p "$(dirname "$output")"
  env GOWORK=off CGO_ENABLED=1 CC="$cc" GOOS=android GOARCH="$goarch" GOARM="$goarm" \
    go -C "$app_root/runtime-tools/yaegi" build -trimpath -buildmode=pie \
      -ldflags='-s -w -linkmode external -extldflags=-Wl,-z,max-page-size=16384' -o "$output" .
  chmod 755 "$output"
  python3 - "$app_root/android/app/src/main/assets/android-runtime/$abi/native-runtime-manifest.json" "$output" <<'PY'
import hashlib
import json
import pathlib
import sys

manifest_path = pathlib.Path(sys.argv[1])
worker_path = pathlib.Path(sys.argv[2])
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
artifacts = manifest.setdefault("artifacts", [])
entry = {
    "name": worker_path.name,
    "role": "yaegi-go",
    "sha256": hashlib.sha256(worker_path.read_bytes()).hexdigest(),
    "size": worker_path.stat().st_size,
}
for index, artifact in enumerate(artifacts):
    if artifact.get("name") == worker_path.name:
        artifacts[index] = entry
        break
else:
    artifacts.append(entry)
artifacts.sort(key=lambda item: item["name"])
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
PY
done
