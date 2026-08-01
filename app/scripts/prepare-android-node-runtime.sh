#!/usr/bin/env bash
set -euo pipefail

NODE_MOBILE_VERSION="${NODE_MOBILE_VERSION:-18.20.4}"
NPM_VERSION="${NPM_VERSION:-10.9.4}"
TYPESCRIPT_VERSION="${TYPESCRIPT_VERSION:-5.9.3}"
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
mkdir -p "$ASSET_DIR/lib" "$ASSET_DIR/bin" "$ASSET_DIR/etc"

if command -v npm >/dev/null 2>&1; then
  npm install -g --prefix "$ASSET_DIR" --ignore-scripts --no-audit --no-fund \
    "npm@${NPM_VERSION}" "typescript@${TYPESCRIPT_VERSION}"
else
  printf 'npm not found on host; TypeScript assets were not installed.\n' >&2
  exit 2
fi

printf 'ignore-scripts=true\naudit=false\nfund=false\nupdate-notifier=false\n' > "$ASSET_DIR/etc/npmrc"

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
METADATA_PATH="$ASSET_DIR/runtime-metadata.json"
node - "$MANIFEST_PATH" "$METADATA_PATH" "$LAUNCHER_OUT" "$JNI_DIR/libnode.so" "$ASSET_DIR" \
  "$NODE_MOBILE_VERSION" "$NPM_VERSION" "$TYPESCRIPT_VERSION" <<'JS'
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

const [manifestPath, metadataPath, launcherPath, libnodePath, assetDir, nodeVersion, npmVersion, typescriptVersion] = process.argv.slice(2);
const hashFile = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const bundleFiles = [
  'lib/node_modules/npm/package.json',
  'lib/node_modules/npm/bin/npm-cli.js',
  'lib/node_modules/npm/bin/npx-cli.js',
  'lib/node_modules/typescript/package.json',
  'lib/node_modules/typescript/bin/tsc',
  'etc/npmrc',
];
const bundleHash = crypto.createHash('sha256');
for (const relative of bundleFiles) {
  bundleHash.update(relative);
  bundleHash.update(Buffer.from([0]));
  bundleHash.update(fs.readFileSync(path.join(assetDir, relative)));
}
const launcherSha256 = hashFile(launcherPath);
const metadata = {
  schema_version: 1,
  abi: 'arm64-v8a',
  node_version: nodeVersion,
  npm_version: npmVersion,
  typescript_version: typescriptVersion,
  launcher_sha256: launcherSha256,
  libnode_sha256: hashFile(libnodePath),
  bundle_sha256: bundleHash.digest('hex'),
  npm_ignore_scripts: true,
};
fs.writeFileSync(metadataPath, `${JSON.stringify(metadata, null, 2)}\n`);

const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
manifest.components = manifest.components.filter(component => !['node-lts-android-arm64', 'typescript-stable'].includes(component.id));
manifest.components.splice(1, 0,
  {
    id: 'node-lts-android-arm64', version: nodeVersion, abi: 'arm64-v8a',
    entrypoint: 'libnode_exec.so', sha256: launcherSha256,
    capabilities: ['node', 'npm', 'npx', 'commonjs', 'esm', 'https'],
  },
  {
    id: 'typescript-stable', version: typescriptVersion, abi: 'arm64-v8a',
    entrypoint: 'libnode_exec.so', sha256: launcherSha256,
    capabilities: ['typescript', 'tsc'],
  },
);
fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
JS

bash "$APP_ROOT/scripts/verify-android-node-runtime.sh"
