#!/usr/bin/env python3
"""Verify that an APK contains exactly one complete Android runtime ABI."""

import argparse
import hashlib
import io
import json
import pathlib
import struct
import zipfile


def elf_metadata(payload: bytes) -> tuple[int, list[int]]:
    assert len(payload) >= 64 and payload[:6] == b"\x7fELF\x02\x01", "APK native entry must be little-endian ELF64"
    machine = struct.unpack_from("<H", payload, 18)[0]
    phoff = struct.unpack_from("<Q", payload, 32)[0]
    phentsize, phnum = struct.unpack_from("<HH", payload, 54)
    assert phentsize >= 56 and phnum > 0 and phoff + phentsize * phnum <= len(payload), "invalid ELF program headers"
    alignments = []
    for index in range(phnum):
        offset = phoff + index * phentsize
        if struct.unpack_from("<I", payload, offset)[0] == 1:
            alignments.append(struct.unpack_from("<Q", payload, offset + 48)[0])
    assert alignments, "ELF has no load segments"
    return machine, alignments


def verify(apk: pathlib.Path, abi: str, matrix_path: pathlib.Path, output_manifest: pathlib.Path | None = None) -> None:
    matrix = json.loads(matrix_path.read_text(encoding="utf-8"))
    config = matrix.get("abis", {}).get(abi)
    assert config and config.get("package") is True, f"unsupported packaged ABI: {abi}"
    with zipfile.ZipFile(apk) as archive:
        names = set(archive.namelist())
        manifest_name = f"assets/android-runtime/{abi}/native-runtime-manifest.json"
        rootfs_name = f"assets/android-runtime/{abi}/ubuntu/rootfs.tar.gz.bin"
        assert manifest_name in names, f"missing APK native manifest for {abi}"
        assert rootfs_name in names, f"missing APK Ubuntu rootfs for {abi}"
        foreign_runtime = [name for name in names if name.startswith("assets/android-runtime/") and not name.startswith(f"assets/android-runtime/{abi}/")]
        assert not foreign_runtime, f"APK contains foreign runtime assets: {foreign_runtime[:5]}"
        packaged_native_abis = {
            name.split("/", 2)[1]
            for name in names
            if name.startswith("lib/") and name.endswith(".so") and len(name.split("/", 2)) == 3
        }
        assert packaged_native_abis == {abi}, f"APK contains unexpected native ABIs: {sorted(packaged_native_abis)}"
        manifest = json.loads(archive.read(manifest_name))
        expected = {item["name"]: item for item in manifest["artifacts"]}
        actual = {name.rsplit("/", 1)[-1]: name for name in names if name.startswith(f"lib/{abi}/") and name.endswith(".so")}
        apk_artifacts = []
        for filename, archive_name in sorted(actual.items()):
            payload = archive.read(archive_name)
            machine, alignments = elf_metadata(payload)
            assert machine == config["elf_machine"], f"APK native artifact architecture mismatch: {filename}"
            assert min(alignments) >= config["minimum_load_alignment"], f"APK native artifact alignment mismatch: {filename}"
            apk_artifacts.append({
                "name": filename,
                "sha256": hashlib.sha256(payload).hexdigest(),
                "size": len(payload),
                "elf_machine": machine,
                "minimum_load_alignment": min(alignments),
            })
        for filename, artifact in expected.items():
            assert filename in actual, f"APK native artifact missing: {filename}"
            payload = archive.read(actual[filename])
            assert hashlib.sha256(payload).hexdigest() == artifact["sha256"], f"APK native artifact checksum mismatch: {filename}"
        if output_manifest:
            output_manifest.parent.mkdir(parents=True, exist_ok=True)
            output_manifest.write_text(json.dumps({
                "schema_version": 1,
                "abi": abi,
                "apk_sha256": hashlib.sha256(apk.read_bytes()).hexdigest(),
                "artifacts": apk_artifacts,
            }, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apk", type=pathlib.Path, required=True)
    parser.add_argument("--abi", required=True)
    parser.add_argument("--matrix", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[2] / "runtime" / "android-abi-matrix.json")
    parser.add_argument("--output-manifest", type=pathlib.Path)
    args = parser.parse_args()
    verify(args.apk, args.abi, args.matrix, args.output_manifest)


if __name__ == "__main__":
    main()
