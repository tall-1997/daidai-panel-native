#!/usr/bin/env bash
set -euo pipefail

APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
FIXTURE="$TEST_ROOT/fixture"
mkdir -p "$FIXTURE/jni" "$FIXTURE/assets/lib/node_modules/npm/bin" "$FIXTURE/assets/lib/node_modules/typescript/bin" "$FIXTURE/assets/etc"

make_elf() {
  local output="$1"
  dd if=/dev/zero of="$output" bs=1 count=64 status=none
  printf '\177ELF\002\001' | dd of="$output" conv=notrunc status=none
  printf '\267\000' | dd of="$output" bs=1 seek=18 conv=notrunc status=none
}

make_elf "$FIXTURE/jni/libnode_exec.so"
make_elf "$FIXTURE/jni/libnode.so"
printf '{"version":"10.9.4"}\n' > "$FIXTURE/assets/lib/node_modules/npm/package.json"
printf 'npm cli\n' > "$FIXTURE/assets/lib/node_modules/npm/bin/npm-cli.js"
printf 'npx cli\n' > "$FIXTURE/assets/lib/node_modules/npm/bin/npx-cli.js"
printf '{"version":"5.9.3"}\n' > "$FIXTURE/assets/lib/node_modules/typescript/package.json"
printf 'tsc cli\n' > "$FIXTURE/assets/lib/node_modules/typescript/bin/tsc"
printf 'ignore-scripts=true\n' > "$FIXTURE/assets/etc/npmrc"

node - "$FIXTURE" <<'JS'
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const root = process.argv[2];
const hash = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const files = ['lib/node_modules/npm/package.json', 'lib/node_modules/npm/bin/npm-cli.js', 'lib/node_modules/npm/bin/npx-cli.js', 'lib/node_modules/typescript/package.json', 'lib/node_modules/typescript/bin/tsc', 'etc/npmrc'];
const bundle = crypto.createHash('sha256');
for (const file of files) { bundle.update(file); bundle.update(Buffer.from([0])); bundle.update(fs.readFileSync(path.join(root, 'assets', file))); }
const launcher = hash(path.join(root, 'jni/libnode_exec.so'));
fs.writeFileSync(path.join(root, 'assets/runtime-metadata.json'), JSON.stringify({schema_version: 1, abi: 'arm64-v8a', node_version: '18.20.4', npm_version: '10.9.4', typescript_version: '5.9.3', launcher_sha256: launcher, libnode_sha256: hash(path.join(root, 'jni/libnode.so')), bundle_sha256: bundle.digest('hex'), npm_ignore_scripts: true}));
fs.writeFileSync(path.join(root, 'manifest.json'), JSON.stringify({components: [{id: 'node-lts-android-arm64', version: '18.20.4', abi: 'arm64-v8a', entrypoint: 'libnode_exec.so', sha256: launcher}, {id: 'typescript-stable', version: '5.9.3', abi: 'arm64-v8a', entrypoint: 'libnode_exec.so', sha256: launcher}]}));
JS

verify() {
  NODE_RUNTIME_JNI_DIR="$FIXTURE/jni" NODE_RUNTIME_ASSET_DIR="$FIXTURE/assets" \
    NODE_RUNTIME_MANIFEST="$FIXTURE/manifest.json" bash "$APP_ROOT/scripts/verify-android-node-runtime.sh"
}

verify
printf 'tampered\n' >> "$FIXTURE/assets/lib/node_modules/npm/bin/npm-cli.js"
if verify > /dev/null 2>&1; then
  printf 'expected tampered bundle verification to fail\n' >&2
  exit 1
fi
printf 'Android Node runtime verifier tests passed\n'
