#!/usr/bin/env bash
set -euo pipefail

PYTHON_VERSION="${PYTHON_VERSION:-3.14.6}"
PYTHON_ABI_VERSION="${PYTHON_ABI_VERSION:-3.14}"
PYTHON_ARCHIVE="python-${PYTHON_VERSION}-aarch64-linux-android.tar.gz"
PYTHON_URL="${PYTHON_URL:-https://www.python.org/ftp/python/${PYTHON_VERSION}/${PYTHON_ARCHIVE}}"
PYTHON_ARCHIVE_SHA256="${PYTHON_ARCHIVE_SHA256:-38bbe77d3167b5cd554e03b1021324926f09f3825202b065951dd7638e9c37e5}"
PYTHON_X64_URL="${PYTHON_X64_URL:-}"
PYTHON_X64_ARCHIVE_SHA256="${PYTHON_X64_ARCHIVE_SHA256:-}"
ASSET_REVISION="${PYTHON_ASSET_REVISION:-${PYTHON_VERSION}-android-arm64-r1}"

APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_APP_DIR="$APP_ROOT/android/app"
WORK_DIR="${PYTHON_RUNTIME_WORK_DIR:-/tmp/opencode/daidai-python-runtime-${PYTHON_VERSION}}"
ARCHIVE_PATH="$WORK_DIR/$PYTHON_ARCHIVE"
EXTRACT_DIR="$WORK_DIR/extracted"
PREFIX_DIR="$EXTRACT_DIR/prefix"
STAGE_DIR="$WORK_DIR/stage-prefix"
ASSET_DIR="${PYTHON_ASSET_DIR:-$ANDROID_APP_DIR/src/main/pythonAssets/python-runtime/${PYTHON_ABI_VERSION}/prefix}"
JNI_DIR="${PYTHON_JNI_DIR:-$ANDROID_APP_DIR/src/main/jniLibs/arm64-v8a}"
LAUNCHER_SRC="$ANDROID_APP_DIR/src/main/cpp/python_exec.c"
LAUNCHER_OUT="$JNI_DIR/libpython_exec.so"
RUNTIME_DIR="${PYTHON_METADATA_DIR:-$APP_ROOT/../runtime}"

verify_wheelhouse() {
  python3 - "$1" <<'PY'
import pathlib
import re
import sys
import zipfile

wheelhouse = pathlib.Path(sys.argv[1])
allowed = re.compile(r"^.+-(?:py3-none-any|cp314-(?:cp314|abi3)-android_[0-9]+_arm64_v8a)\.whl$", re.IGNORECASE)
wheels = sorted(wheelhouse.glob("*.whl"))
if not wheels:
    raise SystemExit("Python wheelhouse is empty")
for wheel in wheels:
    if "x86_64" in wheel.name.lower() or "cp312" in wheel.name.lower() or not allowed.match(wheel.name):
        raise SystemExit(f"incompatible wheel for CPython 3.14 Android ARM64: {wheel.name}")
    with zipfile.ZipFile(wheel) as archive:
        native = [name for name in archive.namelist() if name.lower().endswith((".so", ".pyd", ".dll", ".dylib"))]
    if native and wheel.name.lower().endswith("-py3-none-any.whl"):
        raise SystemExit(f"pure Python wheel contains native code: {wheel.name}")
PY
}

verify_sha256() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(sha256sum "$path" | cut -d ' ' -f 1)"
  [[ "$actual" == "$expected" ]] || { printf 'SHA-256 mismatch for %s: expected %s, got %s\n' "$path" "$expected" "$actual" >&2; exit 1; }
}

verify_ensurepip_bundle() {
  local bundled="$1"
  compgen -G "$bundled/pip-*-py3-none-any.whl" >/dev/null || { printf 'ensurepip bundle is missing\n' >&2; exit 1; }
}

generate_metadata() {
  python3 - "$1" "$2" "$3" "$4" "$5" <<'PY'
import hashlib
import json
import pathlib
import sys

assets, native, runtime, version, revision = map(pathlib.Path, sys.argv[1:4]) + [sys.argv[4], sys.argv[5]] if False else (
    pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]), sys.argv[4], sys.argv[5]
)

def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

artifacts = []
for root_name, root in (("assets", assets), ("jniLibs/arm64-v8a", native)):
    for path in sorted(item for item in root.rglob("*") if item.is_file()):
        if root == native and not path.name.startswith(("libpython", "libssl", "libcrypto", "libsqlite", "libpython_exec", "legacy", "default")):
            continue
        relative = f"{root_name}/{path.relative_to(root).as_posix()}"
        artifacts.append({"path": relative, "sha256": sha256(path), "size": path.stat().st_size})

tree = hashlib.sha256()
for item in artifacts:
    tree.update(f"{item['path']}\0{item['sha256']}\0{item['size']}\n".encode())

launcher = native / "libpython_exec.so"
component = {
    "id": "python-3.14-android-arm64",
    "version": version,
    "abi": "arm64-v8a",
    "python_tag": "cp314",
    "entrypoint": launcher.name,
    "sha256": sha256(launcher),
    "runtime_sha256": tree.hexdigest(),
    "artifact_count": len(artifacts),
    "asset_revision": revision,
    "capabilities": ["python", "pip", "venv", "ssl", "sqlite", "ca-certificates"],
    "artifacts": artifacts,
}

manifest_path = runtime / "manifest.json"
manifest = json.loads(manifest_path.read_text())
components = [item for item in manifest.get("components", []) if item.get("id") != component["id"]]
manifest["components"] = [component] + components
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")

compatibility_path = runtime / "compatibility.json"
compatibility = json.loads(compatibility_path.read_text())
compatibility["abi"] = "arm64-v8a"
compatibility["python_wheel_policy"] = {
    "python_tag": "cp314",
    "offline": ["py3-none-any", "cp314-cp314-android_API_arm64_v8a", "cp314-abi3-android_API_arm64_v8a"],
    "network": "binary-only; package index must publish a CPython 3.14 Android ARM64 wheel",
    "rejected": ["x86_64", "cp312", "sdist", "manylinux", "musllinux"],
}
compatibility["runtimes"] = [
    {"id": component["id"], "version": version, "entry": launcher.name}
] + [item for item in compatibility.get("runtimes", []) if item.get("id") != component["id"]]
compatibility_path.write_text(json.dumps(compatibility, indent=2) + "\n")

dependencies_path = runtime / "dependencies.json"
dependencies = json.loads(dependencies_path.read_text())
dependencies.setdefault("python", {})["runtime"] = component["id"]
dependencies["python"]["install_policy"] = compatibility["python_wheel_policy"]
dependencies_path.write_text(json.dumps(dependencies, indent=2) + "\n")

smoke_path = runtime / "smoke-evidence.json"
smoke = json.loads(smoke_path.read_text())
record = {
    "runtime_id": component["id"],
    "version": version,
    "entry": launcher.name,
    "status": "blocked",
    "evidence_source": "build-static-verification",
    "isolation_level": "trusted-runner",
    "timeout_seconds": 30,
    "checks": [{"id": "PY_OK_SSL_SQLITE_VENV_PIP_CA", "status": "blocked", "reason": "device-smoke-required"}],
}
smoke["records"] = [record] + [item for item in smoke.get("records", []) if item.get("runtime_id") != component["id"]]
smoke_path.write_text(json.dumps(smoke, indent=2) + "\n")

(assets / "runtime-manifest.json").write_text(json.dumps({
    "version": version,
    "asset_revision": revision,
    "runtime_sha256": component["runtime_sha256"],
    "artifact_count": component["artifact_count"],
}, indent=2) + "\n")
PY
}

runtime_digest() {
  python3 - "$1" "$2" <<'PY'
import hashlib, pathlib, sys

digest = hashlib.sha256()
for label, root in (("assets", pathlib.Path(sys.argv[1])), ("jni", pathlib.Path(sys.argv[2]))):
    for path in sorted(item for item in root.rglob("*") if item.is_file()):
        relative = f"{label}/{path.relative_to(root).as_posix()}"
        digest.update(relative.encode())
        digest.update(b"\0")
        digest.update(hashlib.sha256(path.read_bytes()).digest())
print(digest.hexdigest())
PY
}

case "${1:-}" in
  --verify-wheelhouse)
    verify_wheelhouse "${2:?wheelhouse path is required}"
    exit 0
    ;;
  --generate-metadata)
    generate_metadata "${2:?assets path is required}" "${3:?native path is required}" "${4:?runtime path is required}" "${5:?version is required}" "${6:?revision is required}"
    exit 0
    ;;
  --runtime-digest)
    runtime_digest "${2:?assets path is required}" "${3:?native path is required}"
    exit 0
    ;;
  --verify-ensurepip-bundle)
    verify_ensurepip_bundle "${2:?ensurepip bundled path is required}"
    exit 0
    ;;
esac

mkdir -p "$WORK_DIR" "$EXTRACT_DIR" "$JNI_DIR"

if [[ ! -s "$ARCHIVE_PATH" ]]; then
  curl -L --fail --connect-timeout 20 --max-time 240 -o "$ARCHIVE_PATH.part" "$PYTHON_URL"
  mv "$ARCHIVE_PATH.part" "$ARCHIVE_PATH"
fi
verify_sha256 "$ARCHIVE_PATH" "$PYTHON_ARCHIVE_SHA256"

python3 - "$ARCHIVE_PATH" "$EXTRACT_DIR" <<'PY'
import pathlib
import shutil
import sys
import tarfile

archive, destination = map(pathlib.Path, sys.argv[1:])
shutil.rmtree(destination, ignore_errors=True)
destination.mkdir(parents=True)
with tarfile.open(archive) as source:
    for member in source.getmembers():
        target = (destination / member.name).resolve()
        if not target.is_relative_to(destination.resolve()):
            raise SystemExit(f"unsafe archive entry: {member.name}")
    source.extractall(destination, filter="data")
PY

[[ -d "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/lib-dynload" ]] || { printf 'Python stdlib not found in %s\n' "$PREFIX_DIR" >&2; exit 1; }
[[ -f "$PREFIX_DIR/lib/libpython${PYTHON_ABI_VERSION}.so" ]] || { printf 'libpython is missing\n' >&2; exit 1; }
[[ -f "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/lib-dynload/_ssl.cpython-314-aarch64-linux-android.so" ]] || { printf 'CPython 3.14 Android SSL module is missing\n' >&2; exit 1; }
[[ -f "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/lib-dynload/_sqlite3.cpython-314-aarch64-linux-android.so" ]] || { printf 'CPython 3.14 Android SQLite module is missing\n' >&2; exit 1; }
ENSUREPIP_BUNDLED="$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/ensurepip/_bundled"
mkdir -p "$ENSUREPIP_BUNDLED"
if ! compgen -G "$ENSUREPIP_BUNDLED/pip-*-py3-none-any.whl" >/dev/null; then
  python3 -m pip download --disable-pip-version-check --only-binary=:all: --platform any --python-version 3.14 --implementation py --abi none --no-deps \
    --dest "$ENSUREPIP_BUNDLED" pip==24.0
fi
verify_ensurepip_bundle "$ENSUREPIP_BUNDLED"
[[ -d "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/venv" ]] || { printf 'venv stdlib is missing\n' >&2; exit 1; }

python3 - "$PREFIX_DIR" <<'PY'
import pathlib
import struct
import sys

root = pathlib.Path(sys.argv[1])
for path in root.rglob("*.so"):
    header = path.read_bytes()[:20]
    if len(header) < 20 or header[:4] != b"\x7fELF" or header[4:6] != b"\x02\x01" or struct.unpack("<H", header[18:20])[0] != 183:
        raise SystemExit(f"non-ARM64 ELF in Android Python archive: {path}")
for path in root.rglob("*"):
    lowered = path.name.lower()
    if "x86_64" in lowered or "cp312" in lowered or "cpython-312" in lowered:
        raise SystemExit(f"foreign Python artifact in Android runtime: {path}")
PY

python3 - "$PREFIX_DIR" "$STAGE_DIR" <<'PY'
import pathlib
import shutil
import sys

source, stage = map(pathlib.Path, sys.argv[1:])
shutil.rmtree(stage, ignore_errors=True)
(stage / "lib").mkdir(parents=True)
shutil.copytree(source / "lib" / "python3.14", stage / "lib" / "python3.14")
(stage / "wheelhouse").mkdir()
PY

python3 -m pip download --disable-pip-version-check --only-binary=:all: --platform any --python-version 3.14 --implementation py --abi none --dest "$STAGE_DIR/wheelhouse" \
  certifi==2026.5.20 charset-normalizer==3.3.2 idna==3.18 requests==2.34.2 urllib3==2.7.0 beautifulsoup4==4.13.4 soupsieve==2.8.1 typing-extensions==4.15.0
cp "$PREFIX_DIR/lib/python${PYTHON_ABI_VERSION}/ensurepip/_bundled/"pip-*-py3-none-any.whl "$STAGE_DIR/wheelhouse/"

PYYAML_VERSION="6.0.3"
PYYAML_SDIST="$WORK_DIR/PyYAML-${PYYAML_VERSION}.tar.gz"
PYYAML_SHA256="${PYYAML_SHA256:-d76623373421df22fb4cf8817020cbb7ef15c725b9d5e45f17e189bfc384190f}"
if [[ ! -s "$PYYAML_SDIST" ]]; then
  curl -L --fail --connect-timeout 20 --max-time 120 -o "$PYYAML_SDIST.part" "https://files.pythonhosted.org/packages/source/P/PyYAML/pyyaml-${PYYAML_VERSION}.tar.gz"
  mv "$PYYAML_SDIST.part" "$PYYAML_SDIST"
fi
verify_sha256 "$PYYAML_SDIST" "$PYYAML_SHA256"
python3 - "$PYYAML_SDIST" "$STAGE_DIR/wheelhouse" "$PYYAML_VERSION" <<'PY'
import base64, csv, hashlib, io, pathlib, sys, tarfile, zipfile

sdist, wheelhouse, version = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
wheel = wheelhouse / f"PyYAML-{version}-py3-none-any.whl"
dist = f"PyYAML-{version}.dist-info"
records = []
def add(archive, name, data):
    archive.writestr(name, data, compress_type=zipfile.ZIP_DEFLATED)
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    records.append((name, f"sha256={digest}", str(len(data))))
with tarfile.open(sdist) as source, zipfile.ZipFile(wheel, "w") as output:
    for member in sorted(source.getmembers(), key=lambda item: item.name):
        marker = f"PyYAML-{version}/lib/"
        if member.isfile() and member.name.startswith(marker):
            add(output, member.name[len(marker):], source.extractfile(member).read())
    add(output, f"{dist}/WHEEL", b"Wheel-Version: 1.0\nGenerator: daidai-android-runtime\nRoot-Is-Purelib: true\nTag: py3-none-any\n")
    add(output, f"{dist}/METADATA", f"Metadata-Version: 2.1\nName: PyYAML\nVersion: {version}\n".encode())
    record = f"{dist}/RECORD"
    buffer = io.StringIO()
    csv.writer(buffer, lineterminator="\n").writerows(records + [(record, "", "")])
    output.writestr(record, buffer.getvalue())
PY

verify_wheelhouse "$STAGE_DIR/wheelhouse"
python3 - "$STAGE_DIR/wheelhouse" <<'PY'
import hashlib, json, pathlib, sys
wheelhouse = pathlib.Path(sys.argv[1])
wheels = [{"filename": path.name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "size": path.stat().st_size} for path in sorted(wheelhouse.glob("*.whl"))]
(wheelhouse / "wheelhouse-manifest.json").write_text(json.dumps({
    "version": "2", "python_tag": "cp314", "abi": "arm64-v8a", "install_mode": "offline-pure-python-extraction",
    "network_policy": "binary-only Android ARM64 cp314/abi3 wheels", "wheels": wheels
}, indent=2) + "\n")
PY

python3 - "$STAGE_DIR" <<'PY'
import pathlib, sys, zipfile
stage = pathlib.Path(sys.argv[1])
certifi = next((stage / "wheelhouse").glob("certifi-*.whl"))
with zipfile.ZipFile(certifi) as archive:
    data = archive.read("certifi/cacert.pem")
target = stage / "etc/ssl/certs/cacert.pem"
target.parent.mkdir(parents=True)
target.write_bytes(data)
PY

STAGE_JNI_DIR="$WORK_DIR/stage-jni"
python3 - "$PREFIX_DIR/lib" "$STAGE_JNI_DIR" <<'PY'
import pathlib, shutil, sys
source, target = map(pathlib.Path, sys.argv[1:])
shutil.rmtree(target, ignore_errors=True)
target.mkdir(parents=True, exist_ok=True)
names = {path.name for path in source.glob("*.so*") if path.is_file()}
for directory in (source / "engines-3", source / "ossl-modules"):
    if directory.is_dir():
        names.update(path.name for path in directory.glob("*.so") if path.is_file())
for name in names:
    candidates = list(source.glob(name)) + list((source / "engines-3").glob(name)) + list((source / "ossl-modules").glob(name))
    shutil.copy2(candidates[0], target / name)
PY

CLANG=""
for candidate in \
  "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang" \
  "${ANDROID_NDK_ROOT:-}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang" \
  "${ANDROID_HOME:-}/ndk/"*/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang; do
  if [[ -x "$candidate" ]]; then CLANG="$candidate"; break; fi
done
[[ -n "$CLANG" ]] || { printf 'Android NDK clang was not found; set ANDROID_NDK_HOME or ANDROID_NDK_ROOT.\n' >&2; exit 2; }

STAGE_LAUNCHER_OUT="$STAGE_JNI_DIR/libpython_exec.so"
"$CLANG" -fPIE -pie -Wl,-z,relro,-z,now -Wl,-rpath,'$ORIGIN' \
  -I"$PREFIX_DIR/include/python${PYTHON_ABI_VERSION}" "$LAUNCHER_SRC" -L"$STAGE_JNI_DIR" \
  -lpython${PYTHON_ABI_VERSION} -ldl -lm -llog -o "$STAGE_LAUNCHER_OUT"
chmod 755 "$STAGE_LAUNCHER_OUT"

python3 - "$STAGE_LAUNCHER_OUT" "$STAGE_JNI_DIR/libpython${PYTHON_ABI_VERSION}.so" <<'PY'
import pathlib, struct, subprocess, sys
for path in map(pathlib.Path, sys.argv[1:]):
    header = path.read_bytes()[:20]
    if len(header) < 20 or header[:6] != b"\x7fELF\x02\x01" or struct.unpack("<H", header[18:20])[0] != 183:
        raise SystemExit(f"expected Android ARM64 ELF: {path}")
dynamic = subprocess.run(["readelf", "-d", sys.argv[1]], check=True, capture_output=True, text=True).stdout
if "libpython3.14.so" not in dynamic:
    raise SystemExit("Python launcher is not linked to libpython3.14.so")
PY

python3 - "$STAGE_DIR" "$ASSET_DIR" "$STAGE_JNI_DIR" "$JNI_DIR" <<'PY'
import pathlib, shutil, sys
stage_assets, assets, stage_jni, jni = map(pathlib.Path, sys.argv[1:])
shutil.rmtree(assets, ignore_errors=True)
assets.parent.mkdir(parents=True, exist_ok=True)
shutil.copytree(stage_assets, assets)
jni.mkdir(parents=True, exist_ok=True)
for source in stage_jni.iterdir():
    if source.is_file():
        shutil.copy2(source, jni / source.name)
PY

generate_metadata "$ASSET_DIR" "$JNI_DIR" "$RUNTIME_DIR" "$PYTHON_VERSION" "$ASSET_REVISION"

printf 'Prepared CPython %s Android ARM64 runtime (%s).\n' "$PYTHON_VERSION" "$ASSET_REVISION"
