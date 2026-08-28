#!/usr/bin/env python3
"""Synchronize every packaged Android ELF into its ABI manifest."""

import argparse
import hashlib
import json
import pathlib

KNOWN_ROLES = {
    "libandroid-shmem.so": "android-shmem-runtime",
    "libdaidai_busybox.so": "busybox",
    "libdaidai_proot.so": "proot",
    "libproot_loader.so": "proot-loader",
    "libtalloc.so": "talloc-runtime",
    "libyaegi_exec.so": "yaegi-go",
}


def synchronize(native_dir: pathlib.Path, manifest_path: pathlib.Path) -> None:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    existing_roles = {item["name"]: item.get("role") for item in manifest.get("artifacts", [])}
    artifacts = []
    for path in sorted(native_dir.glob("*.so")):
        data = path.read_bytes()
        if data[:4] != b"\x7fELF":
            continue
        artifacts.append({
            "name": path.name,
            "role": KNOWN_ROLES.get(path.name, existing_roles.get(path.name) or "packaged-native-library"),
            "sha256": hashlib.sha256(data).hexdigest(),
            "size": len(data),
        })
    if not artifacts:
        raise SystemExit(f"no packaged ELF files found in {native_dir}")
    manifest["artifacts"] = artifacts
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--native-dir", type=pathlib.Path, required=True)
    parser.add_argument("--manifest", type=pathlib.Path, required=True)
    args = parser.parse_args()
    synchronize(args.native_dir, args.manifest)


if __name__ == "__main__":
    main()
