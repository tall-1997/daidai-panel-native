#!/usr/bin/env bash
set -euo pipefail

APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NODE_VERSION="${NODE_MOBILE_VERSION:-18.20.4}"
NPM_VERSION="${NPM_VERSION:-10.9.4}"
TYPESCRIPT_VERSION="${TYPESCRIPT_VERSION:-5.9.3}"
JNI_DIR="${NODE_RUNTIME_JNI_DIR:-$APP_ROOT/android/app/src/main/jniLibs/arm64-v8a}"
ASSET_DIR="${NODE_RUNTIME_ASSET_DIR:-$APP_ROOT/android/app/src/main/nodeAssets/node-runtime/$NODE_VERSION/usr}"
MANIFEST_PATH="${NODE_RUNTIME_MANIFEST:-$APP_ROOT/../runtime/manifest.json}"

node - "$JNI_DIR" "$ASSET_DIR" "$MANIFEST_PATH" "$NODE_VERSION" "$NPM_VERSION" "$TYPESCRIPT_VERSION" <<'JS'
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

const [jniDir, assetDir, manifestPath, nodeVersion, npmVersion, typescriptVersion] = process.argv.slice(2);
const fail = message => { throw new Error(message); };
const read = file => fs.readFileSync(file);
const hash = file => crypto.createHash('sha256').update(read(file)).digest('hex');
const arm64Elf = file => {
  const bytes = read(file);
  return bytes.length >= 20 && bytes.subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46])) &&
    bytes[4] === 2 && bytes[5] === 1 && bytes.readUInt16LE(18) === 183 && !bytes.includes(Buffer.from('RUNTIME_STUB_OK'));
};
const launcher = path.join(jniDir, 'libnode_exec.so');
const libnode = path.join(jniDir, 'libnode.so');
if (!arm64Elf(launcher)) fail('libnode_exec.so must be a real ARM64 ELF');
if (!arm64Elf(libnode)) fail('libnode.so must be a real ARM64 ELF');

const metadata = JSON.parse(read(path.join(assetDir, 'runtime-metadata.json')));
const manifest = JSON.parse(read(manifestPath));
const npm = JSON.parse(read(path.join(assetDir, 'lib/node_modules/npm/package.json')));
const typescript = JSON.parse(read(path.join(assetDir, 'lib/node_modules/typescript/package.json')));
if (metadata.node_version !== nodeVersion || metadata.npm_version !== npmVersion || metadata.typescript_version !== typescriptVersion) fail('runtime metadata versions differ from requested versions');
if (npm.version !== npmVersion || typescript.version !== typescriptVersion) fail('packaged npm or TypeScript version mismatch');
if (metadata.launcher_sha256 !== hash(launcher) || metadata.libnode_sha256 !== hash(libnode)) fail('native runtime hash mismatch');
if (metadata.npm_ignore_scripts !== true || !read(path.join(assetDir, 'etc/npmrc')).toString().includes('ignore-scripts=true')) fail('npm ignore-scripts default is missing');

const bundleFiles = [
  'lib/node_modules/npm/package.json', 'lib/node_modules/npm/bin/npm-cli.js',
  'lib/node_modules/npm/bin/npx-cli.js', 'lib/node_modules/typescript/package.json',
  'lib/node_modules/typescript/bin/tsc', 'etc/npmrc',
];
const bundleHash = crypto.createHash('sha256');
for (const relative of bundleFiles) {
  const file = path.join(assetDir, relative);
  if (!fs.statSync(file).isFile()) fail(`missing Node runtime asset ${relative}`);
  bundleHash.update(relative);
  bundleHash.update(Buffer.from([0]));
  bundleHash.update(read(file));
}
if (metadata.bundle_sha256 !== bundleHash.digest('hex')) fail('Node asset bundle hash mismatch');

for (const [id, version] of [['node-lts-android-arm64', nodeVersion], ['typescript-stable', typescriptVersion]]) {
  const component = manifest.components.find(item => item.id === id);
  if (!component || component.version !== version || component.abi !== 'arm64-v8a' || component.entrypoint !== 'libnode_exec.so' || component.sha256 !== metadata.launcher_sha256) fail(`${id} manifest metadata mismatch`);
}
console.log(`Android Node runtime verified: node=${nodeVersion} npm=${npmVersion} typescript=${typescriptVersion}`);
JS
