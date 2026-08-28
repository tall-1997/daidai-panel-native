#!/usr/bin/env python3
"""Verify schema 2 Android update assets against the ABI release matrix."""

import argparse
import hashlib
import json
import pathlib
from urllib.parse import urlparse


def sha256(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify(manifest_path: pathlib.Path, matrix_path: pathlib.Path, artifact_dir: pathlib.Path) -> None:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    matrix = json.loads(matrix_path.read_text(encoding="utf-8"))
    assert manifest.get("schemaVersion") == 2, "Android update manifest schemaVersion must be 2"
    packages = manifest.get("packages")
    assert isinstance(packages, dict), "Android update packages are missing"
    expected_abis = {abi for abi, config in matrix["abis"].items() if config["release"]}
    assert set(packages) == expected_abis, f"Android update ABI set mismatch: {sorted(packages)}"
    for abi in sorted(expected_abis):
        full = packages[abi].get("full", {})
        name = full.get("name", "")
        path = artifact_dir / name
        assert name.endswith(f"-{matrix['abis'][abi]['release_suffix']}.apk"), f"Android update APK name mismatch: {abi}"
        assert path.is_file(), f"Android update APK is missing: {name}"
        assert full.get("size") == path.stat().st_size, f"Android update APK size mismatch: {abi}"
        assert full.get("sha256") == sha256(path), f"Android update APK checksum mismatch: {abi}"
        url = urlparse(full.get("url", ""))
        assert url.scheme == "https" and url.netloc == "github.com" and url.path.endswith(f"/{name}"), f"Android update APK URL mismatch: {abi}"
        assert packages[abi].get("patches") == [], f"cross-ABI patches are forbidden: {abi}"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=pathlib.Path, required=True)
    parser.add_argument("--matrix", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[1] / "runtime/android-abi-matrix.json")
    parser.add_argument("--artifact-dir", type=pathlib.Path, default=pathlib.Path.cwd())
    args = parser.parse_args()
    verify(args.manifest, args.matrix, args.artifact_dir)


if __name__ == "__main__":
    main()
