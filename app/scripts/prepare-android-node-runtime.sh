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
declare -A JNI_DIRS
JNI_DIRS[arm64-v8a]="$ANDROID_APP_DIR/src/main/jniLibs/arm64-v8a"
JNI_DIRS[x86_64]="$ANDROID_APP_DIR/src/main/jniLibs/x86_64"
declare -A LAUNCHER_OUTS
LAUNCHER_OUTS[arm64-v8a]="${JNI_DIRS[arm64-v8a]}/libnode_exec.so"
LAUNCHER_OUTS[x86_64]="${JNI_DIRS[x86_64]}/libnode_exec.so"
declare -A CLANG_TARGETS
CLANG_TARGETS[arm64-v8a]="aarch64-linux-android28-clang++"
CLANG_TARGETS[x86_64]="x86_64-linux-android28-clang++"
LAUNCHER_SRC="$ANDROID_APP_DIR/src/main/cpp/node_exec.cc"

for ABI in arm64-v8a x86_64; do
  mkdir -p "${JNI_DIRS[$ABI]}"
done
mkdir -p "$WORK_DIR" "$EXTRACT_DIR" "$ASSET_DIR"

if [[ ! -s "$ARCHIVE_PATH" ]]; then
  curl -L --fail --connect-timeout 20 --max-time 300 -o "$ARCHIVE_PATH" "$NODE_URL"
fi

unzip -q -o "$ARCHIVE_PATH" -d "$EXTRACT_DIR"

for ABI in arm64-v8a x86_64; do
  if [[ ! -s "$EXTRACT_DIR/bin/${ABI}/libnode.so" ]]; then
    printf 'nodejs-mobile %s libnode.so not found in %s; skipping\n' "$ABI" "$ARCHIVE_PATH" >&2
    continue
  fi
  cp -a "$EXTRACT_DIR/bin/${ABI}/libnode.so" "${JNI_DIRS[$ABI]}/libnode.so"
done
mkdir -p "$ASSET_DIR/lib" "$ASSET_DIR/bin" "$ASSET_DIR/etc"

if command -v npm >/dev/null 2>&1; then
  npm install -g --prefix "$ASSET_DIR" --ignore-scripts --no-audit --no-fund \
    "npm@${NPM_VERSION}" "typescript@${TYPESCRIPT_VERSION}"
else
  printf 'npm not found on host; TypeScript assets were not installed.\n' >&2
  exit 2
fi

printf 'ignore-scripts=true\naudit=false\nfund=false\nupdate-notifier=false\n' > "$ASSET_DIR/etc/npmrc"

NDK_TOOLCHAIN=""
for candidate_base in \
  "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" \
  "${ANDROID_NDK_ROOT:-}/toolchains/llvm/prebuilt/linux-x86_64/bin" \
  "${ANDROID_HOME:-}/ndk/"*/toolchains/llvm/prebuilt/linux-x86_64/bin \
  /opt/android-sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin \
  /usr/local/lib/android/sdk/ndk/*/toolchains/llvm/prebuilt/linux-x86_64/bin; do
  if [[ -d "$candidate_base" ]]; then NDK_TOOLCHAIN="$candidate_base"; break; fi
done

for ABI in arm64-v8a x86_64; do
  launcher_target="${CLANG_TARGETS[$ABI]}"
  jni_dir="${JNI_DIRS[$ABI]}"
  if [[ ! -f "$jni_dir/libnode.so" ]]; then continue; fi
  clangxx=""
  if [[ -n "$NDK_TOOLCHAIN" && -x "$NDK_TOOLCHAIN/$launcher_target" ]]; then
    clangxx="$NDK_TOOLCHAIN/$launcher_target"
  fi
  if [[ -z "$clangxx" ]]; then
    printf 'Android NDK %s not found; skipping %s launcher.\n' "$launcher_target" "$ABI" >&2
    continue
  fi
  "$clangxx" \
    -fPIE -pie \
    -I"$EXTRACT_DIR/include/node" \
    "$LAUNCHER_SRC" \
    -L"$jni_dir" \
    -Wl,-rpath,'$ORIGIN' \
    -lnode \
    -ldl -lm \
    -o "${LAUNCHER_OUTS[$ABI]}"
  chmod 755 "${LAUNCHER_OUTS[$ABI]}"
done

MANIFEST_PATH="$APP_ROOT/../runtime/manifest.json"
METADATA_PATH="$ASSET_DIR/runtime-metadata.json"
PRIMARY_ABI="arm64-v8a"
primary_launcher="${LAUNCHER_OUTS[$PRIMARY_ABI]}"
primary_libnode="${JNI_DIRS[$PRIMARY_ABI]}/libnode.so"
node - "$MANIFEST_PATH" "$METADATA_PATH" "$primary_launcher" "$primary_libnode" "$ASSET_DIR" \
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
