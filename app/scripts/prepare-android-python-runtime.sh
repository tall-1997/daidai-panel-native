#!/usr/bin/env bash
set -euo pipefail

PYTHON_VERSION="${PYTHON_VERSION:-3.14.6}"
PYTHON_ABI_VERSION="${PYTHON_ABI_VERSION:-3.14}"
PYTHON_ARCHIVE="python-${PYTHON_VERSION}-aarch64-linux-android.tar.gz"
PYTHON_URL="${PYTHON_URL:-https://www.python.org/ftp/python/${PYTHON_VERSION}/${PYTHON_ARCHIVE}}"

APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_APP_DIR="$APP_ROOT/android/app"
WORK_DIR="${PYTHON_RUNTIME_WORK_DIR:-/tmp/opencode/daidai-python-runtime-${PYTHON_VERSION}}"
ARCHIVE_PATH="$WORK_DIR/$PYTHON_ARCHIVE"
EXTRACT_DIR="$WORK_DIR/extracted"
PREFIX_DIR="$EXTRACT_DIR/prefix"
ASSET_DIR="$ANDROID_APP_DIR/src/main/pythonAssets/python-runtime/${PYTHON_ABI_VERSION}/prefix"
JNI_DIR="$ANDROID_APP_DIR/src/main/jniLibs/arm64-v8a"
LAUNCHER_SRC="$ANDROID_APP_DIR/src/main/cpp/python_exec.c"
LAUNCHER_OUT="$JNI_DIR/libpython_exec.so"

mkdir -p "$WORK_DIR" "$EXTRACT_DIR" "$ASSET_DIR" "$JNI_DIR"

if [[ ! -s "$ARCHIVE_PATH" ]]; then
  curl -L --fail --connect-timeout 20 --max-time 240 -o "$ARCHIVE_PATH" "$PYTHON_URL"
fi

tar -xzf "$ARCHIVE_PATH" -C "$EXTRACT_DIR"

if [[ ! -d "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}" ]]; then
  printf 'Python stdlib not found in %s\n' "$PREFIX_DIR" >&2
  exit 1
fi

mkdir -p "$ASSET_DIR/lib"
cp -a "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}" "$ASSET_DIR/lib/"
mkdir -p "$ASSET_DIR/wheelhouse"
python3 -m pip download \
  --only-binary=:all: \
  --dest "$ASSET_DIR/wheelhouse" \
  certifi==2026.5.20 \
  charset-normalizer==3.4.7 \
  idna==3.18 \
  requests==2.34.2 \
  urllib3==2.7.0 \
  beautifulsoup4==4.13.4 \
  pip==24.0

python3 - "$ASSET_DIR/wheelhouse" <<'PY'
import hashlib
import json
import pathlib
import sys

wheelhouse = pathlib.Path(sys.argv[1])
wheels = []
for wheel in sorted(wheelhouse.glob("*.whl")):
    wheels.append({
        "filename": wheel.name,
        "sha256": hashlib.sha256(wheel.read_bytes()).hexdigest(),
        "size": wheel.stat().st_size,
    })

manifest = {
    "version": "1",
    "signature_scope": "apk-signed-asset-sha256",
    "wheels": wheels,
    "android_abi_required": [
        {
            "name": "pycryptodome",
            "version": "3.23.0",
            "status": "requires-android-wheel",
            "reason": "No signed Android ABI wheel is bundled yet."
        }
    ]
}
(wheelhouse / "wheelhouse-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
PY

find "$PREFIX_DIR/lib" -maxdepth 1 -type f -name '*.so*' -exec cp -a {} "$JNI_DIR/" \;
find "$PREFIX_DIR/lib/engines-3" -maxdepth 1 -type f -name '*.so' -exec cp -a {} "$JNI_DIR/" \; 2>/dev/null || true
find "$PREFIX_DIR/lib/ossl-modules" -maxdepth 1 -type f -name '*.so' -exec cp -a {} "$JNI_DIR/" \; 2>/dev/null || true

CLANG=""
if [[ -n "${ANDROID_NDK_HOME:-}" && -x "${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang" ]]; then
  CLANG="${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang"
elif [[ -n "${ANDROID_NDK_ROOT:-}" && -x "${ANDROID_NDK_ROOT}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang" ]]; then
  CLANG="${ANDROID_NDK_ROOT}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang"
elif [[ -n "${ANDROID_HOME:-}" ]]; then
  for candidate in "${ANDROID_HOME}"/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang; do
    if [[ -x "$candidate" ]]; then
      CLANG="$candidate"
      break
    fi
  done
else
  for candidate in "$ANDROID_APP_DIR"/../../../../android-sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang /opt/android-sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang /usr/local/lib/android/sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android28-clang; do
    if [[ -x "$candidate" ]]; then
      CLANG="$candidate"
      break
    fi
  done
fi

if [[ -z "$CLANG" ]]; then
  printf 'Android NDK clang was not found; Python assets prepared but launcher was not rebuilt.\n' >&2
  exit 2
fi

"$CLANG" \
  -fPIE -pie \
  -I"$PREFIX_DIR/include/python${PYTHON_ABI_VERSION}" \
  "$LAUNCHER_SRC" \
  -L"$JNI_DIR" \
  -Wl,-rpath,'$ORIGIN' \
  -lpython${PYTHON_ABI_VERSION} \
  -ldl -lm \
  -o "$LAUNCHER_OUT"

chmod 755 "$LAUNCHER_OUT"
sha256sum "$LAUNCHER_OUT" "$JNI_DIR/libpython${PYTHON_ABI_VERSION}.so"

MANIFEST_PATH="$APP_ROOT/../runtime/manifest.json"
python3 - "$MANIFEST_PATH" "$LAUNCHER_OUT" "$PYTHON_VERSION" <<'PY'
import hashlib
import json
import pathlib
import sys

manifest_path = pathlib.Path(sys.argv[1])
launcher = pathlib.Path(sys.argv[2])
version = sys.argv[3]
data = json.loads(manifest_path.read_text())
sha = hashlib.sha256(launcher.read_bytes()).hexdigest()
for component in data.get("components", []):
    if component.get("entrypoint") == "libpython_exec.so":
        component["version"] = version
        component["sha256"] = sha
        component["capabilities"] = ["python", "pip", "venv", "ssl", "sqlite"]
manifest_path.write_text(json.dumps(data, indent=2) + "\n")
PY
