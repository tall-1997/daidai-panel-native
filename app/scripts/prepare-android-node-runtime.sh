#!/usr/bin/env bash
set -euo pipefail

NODE_MOBILE_VERSION="${NODE_MOBILE_VERSION:-18.20.4}"
NODE_ARCHIVE="nodejs-mobile-v${NODE_MOBILE_VERSION}-android.zip"
NODE_URL="${NODE_URL:-https://github.com/nodejs-mobile/nodejs-mobile/releases/download/v${NODE_MOBILE_VERSION}/${NODE_ARCHIVE}}"

APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_APP_DIR="$APP_ROOT/android/app"
WORK_DIR="${NODE_RUNTIME_WORK_DIR:-/tmp/opencode/daidai-node-runtime-${NODE_MOBILE_VERSION}}"
ARCHIVE_PATH="$WORK_DIR/$NODE_ARCHIVE"
EXTRACT_DIR="$WORK_DIR/extracted"
ASSET_DIR="$ANDROID_APP_DIR/src/main/nodeAssets/node-runtime/${NODE_MOBILE_VERSION}/usr"
JNI_DIR="$ANDROID_APP_DIR/src/main/jniLibs/arm64-v8a"
LAUNCHER_SRC="$ANDROID_APP_DIR/src/main/cpp/node_exec.cc"
LAUNCHER_OUT="$JNI_DIR/libnode_exec.so"

mkdir -p "$WORK_DIR" "$EXTRACT_DIR" "$ASSET_DIR" "$JNI_DIR"

if [[ ! -s "$ARCHIVE_PATH" ]]; then
  curl -L --fail --connect-timeout 20 --max-time 300 -o "$ARCHIVE_PATH" "$NODE_URL"
fi

unzip -q -o "$ARCHIVE_PATH" -d "$EXTRACT_DIR"

if [[ ! -s "$EXTRACT_DIR/bin/arm64-v8a/libnode.so" ]]; then
  printf 'nodejs-mobile arm64 libnode.so not found in %s\n' "$ARCHIVE_PATH" >&2
  exit 1
fi

cp -a "$EXTRACT_DIR/bin/arm64-v8a/libnode.so" "$JNI_DIR/libnode.so"
mkdir -p "$ASSET_DIR/lib" "$ASSET_DIR/bin"
cp -a "$EXTRACT_DIR/include" "$ASSET_DIR/"

if command -v npm >/dev/null 2>&1; then
  npm install -g --prefix "$ASSET_DIR" --ignore-scripts typescript ts-node
else
  printf 'npm not found on host; TypeScript assets were not installed.\n' >&2
  exit 2
fi

CLANGXX=""
if [[ -n "${ANDROID_NDK_HOME:-}" && -x "${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++" ]]; then
  CLANGXX="${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++"
elif [[ -n "${ANDROID_NDK_ROOT:-}" && -x "${ANDROID_NDK_ROOT}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++" ]]; then
  CLANGXX="${ANDROID_NDK_ROOT}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++"
elif [[ -n "${ANDROID_HOME:-}" ]]; then
  for candidate in "${ANDROID_HOME}"/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++; do
    if [[ -x "$candidate" ]]; then
      CLANGXX="$candidate"
      break
    fi
  done
else
  for candidate in /opt/android-sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++ /usr/local/lib/android/sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang++; do
    if [[ -x "$candidate" ]]; then
      CLANGXX="$candidate"
      break
    fi
  done
fi

if [[ -z "$CLANGXX" ]]; then
  printf 'Android NDK clang++ was not found; Node assets prepared but launcher was not rebuilt.\n' >&2
  exit 2
fi

"$CLANGXX" \
  -fPIE -pie \
  -I"$EXTRACT_DIR/include/node" \
  "$LAUNCHER_SRC" \
  -L"$JNI_DIR" \
  -Wl,-rpath,'$ORIGIN' \
  -lnode \
  -ldl -lm \
  -o "$LAUNCHER_OUT"

chmod 755 "$LAUNCHER_OUT"

MANIFEST_PATH="$APP_ROOT/../runtime/manifest.json"
python3 - "$MANIFEST_PATH" "$LAUNCHER_OUT" "$NODE_MOBILE_VERSION" <<'PY'
import hashlib
import json
import pathlib
import sys

manifest_path = pathlib.Path(sys.argv[1])
launcher = pathlib.Path(sys.argv[2])
version = sys.argv[3]
data = json.loads(manifest_path.read_text())
sha = hashlib.sha256(launcher.read_bytes()).hexdigest()
components = data.setdefault("components", [])
components[:] = [item for item in components if item.get("id") not in {"node-lts-android-arm64", "typescript-stable"}]
components.extend([
    {
        "id": "node-lts-android-arm64",
        "version": version,
        "abi": "arm64-v8a",
        "entrypoint": "libnode_exec.so",
        "sha256": sha,
        "capabilities": ["node", "npm", "npx"],
    },
    {
        "id": "typescript-stable",
        "version": "bundled",
        "abi": "arm64-v8a",
        "entrypoint": "libnode_exec.so",
        "sha256": sha,
        "capabilities": ["typescript", "ts-node"],
    },
])
manifest_path.write_text(json.dumps(data, indent=2) + "\n")
PY

sha256sum "$LAUNCHER_OUT" "$JNI_DIR/libnode.so"
