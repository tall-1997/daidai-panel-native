#!/usr/bin/env python3
"""Fail-closed verification for packaged Android Linux runtime artifacts."""

import argparse
import hashlib
import json
import pathlib
import struct
import tarfile

DISTRO_COMMANDS = {
    "alpine": ("apk", "bash", "python3", "pip3", "node", "npm", "uv", "pnpm"),
    "ubuntu": ("apt-get", "bash", "python3", "pip3", "node", "npm", "pnpm"),
}
DISTRO_CAPABILITIES = {
    "alpine": {
        "package_manager": ["apk"],
        "shell": ["bash"],
        "python": ["python3", "pip3", "uv"],
        "node": ["node", "npm", "pnpm"],
        "tls_ca_certificates": True,
    },
    "ubuntu": {
        "package_manager": ["apt-get"],
        "shell": ["bash"],
        "python": ["python3", "pip3"],
        "node": ["node", "npm", "pnpm"],
        "tls_ca_certificates": True,
    },
}
DISTRO_PACKAGES = {
    "alpine": {"bash", "python3", "py3-pip", "py3-pycryptodome", "nodejs", "npm", "uv", "pnpm", "ca-certificates"},
    "ubuntu": {"bash", "python3", "python3-pip", "nodejs", "npm", "pnpm", "ca-certificates"},
}
def sha256(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_rootfs(archive: pathlib.Path, checksum: pathlib.Path, manifest_path: pathlib.Path) -> None:
    manifest = json.loads(manifest_path.read_text())
    distribution = manifest.get("distribution")
    commands = DISTRO_COMMANDS.get(distribution)
    assert commands is not None, f"unsupported distribution: {distribution}"
    capabilities = DISTRO_CAPABILITIES[distribution]
    required_packages = DISTRO_PACKAGES[distribution]
    expected_sha = checksum.read_text().strip().split()[0]
    actual_sha = sha256(archive)
    assert len(expected_sha) == 64 and expected_sha == actual_sha, "rootfs checksum mismatch"
    assert manifest.get("schema_version") == 2, "rootfs manifest schema_version must be 2"
    assert manifest.get("sha256") == actual_sha, "rootfs manifest sha256 mismatch"
    assert manifest.get("size") == archive.stat().st_size, "rootfs manifest size mismatch"
    assert manifest.get("required_commands") == list(commands), "required_commands contract mismatch"
    assert manifest.get("capabilities") == capabilities, "capabilities contract mismatch"
    packages = set(manifest.get("packages", []))
    assert required_packages <= packages, "required packages missing"
    with tarfile.open(archive, mode="r:*") as rootfs:
        members = {member.name.removeprefix("./"): member for member in rootfs.getmembers()}
    for command in commands:
        candidates = (f"usr/bin/{command}", f"bin/{command}", f"usr/sbin/{command}", f"sbin/{command}", f"usr/local/bin/{command}")
        command_member = next((members[name] for name in candidates if name in members), None)
        assert command_member is not None, f"rootfs command missing: {command}"
        assert command_member.issym() or command_member.mode & 0o111, f"rootfs command is not executable: {command}"
    ca_bundle = members.get("etc/ssl/certs/ca-certificates.crt")
    assert ca_bundle is not None and ca_bundle.size > 0, "rootfs CA certificate bundle missing"
    if distribution == "alpine":
        assert any(name.startswith("usr/lib/python3.14/site-packages/Crypto/Cipher/") for name in members), "rootfs PyCryptodome Crypto package missing"


def elf_metadata(path: pathlib.Path) -> tuple[int, list[int]]:
    data = path.read_bytes()
    assert len(data) >= 64 and data[:4] == b"\x7fELF", f"not an ELF file: {path}"
    assert data[4] == 2 and data[5] == 1, f"ELF must be little-endian 64-bit: {path}"
    machine = struct.unpack_from("<H", data, 18)[0]
    phoff = struct.unpack_from("<Q", data, 32)[0]
    phentsize, phnum = struct.unpack_from("<HH", data, 54)
    assert phentsize >= 56 and phnum > 0, f"invalid ELF program header table: {path}"
    assert phoff + phentsize * phnum <= len(data), f"truncated ELF program header table: {path}"
    alignments = []
    for index in range(phnum):
        offset = phoff + index * phentsize
        if struct.unpack_from("<I", data, offset)[0] == 1:
            alignments.append(struct.unpack_from("<Q", data, offset + 48)[0])
    assert alignments, f"ELF has no PT_LOAD segments: {path}"
    return machine, alignments


def verify_native(native_dir: pathlib.Path, manifest_path: pathlib.Path) -> None:
    manifest = json.loads(manifest_path.read_text())
    assert manifest.get("schema_version") == 1, "native manifest schema_version must be 1"
    abi = manifest.get("abi")
    assert abi == native_dir.name, "native manifest abi does not match its directory"
    assert manifest.get("minimum_load_alignment") == 16384, "native alignment policy mismatch"
    provenance = manifest.get("provenance", {})
    strategy = provenance.get("strategy")
    assert strategy == "self-contained-source-build", "unapproved PRoot provenance strategy (Termux packages are no longer supported)"
    _verify_source_build(provenance)
    artifacts = manifest.get("artifacts", [])
    assert artifacts, "native runtime manifest has no artifacts"
    names = {item.get("name") for item in artifacts}
    assert {"libproot_loader.so"} <= names, "native runtime loader manifest is incomplete"
    assert provenance.get("runtime_overrides", {}).get("PROOT_LOADER") == "libproot_loader.so", "PRoot loader override contract is missing"
    for artifact in artifacts:
        path = native_dir / artifact["name"]
        assert path.is_file(), f"native artifact missing: {path}"
        assert artifact.get("size") == path.stat().st_size, f"native artifact size mismatch: {path.name}"
        assert artifact.get("sha256") == sha256(path), f"native artifact checksum mismatch: {path.name}"
        machine, alignments = elf_metadata(path)
        expected_machine = {"arm64-v8a": 183, "x86_64": 62}.get(abi)
        assert expected_machine is not None, f"unsupported ABI: {abi}"
        assert machine == expected_machine, f"native artifact does not match ABI {abi}: {path.name}"
        assert all(value >= 16384 for value in alignments), f"native artifact has PT_LOAD alignment below 16KB: {path.name}"
    proot_name = next((name for name in names if name == "libdaidai_proot.so"), None)
    if proot_name is not None:
        proot_data = (native_dir / proot_name).read_bytes()
        assert b"PROOT_LOADER" in proot_data, "packaged PRoot does not support the required loader override"


def _verify_source_build(provenance: dict) -> None:
    assert provenance.get("source_build") is True, "source-build provenance must claim a source build"
    assert provenance.get("source_patch_applied") is False, "source-build must not claim applied patches"
    assert isinstance(provenance.get("patches_applied", []), list), "patches_applied must be a list"
    upstream = provenance.get("upstream_source", [])
    assert len(upstream) >= 3, "expected proot, talloc and busybox upstream sources to be pinned"
    assert all(len(item.get("sha256", "")) == 64 for item in upstream), "upstream sources are not pinned by SHA-256"
    assert all(item.get("name") and item.get("url") and item.get("version") for item in upstream), "incomplete upstream source pin"
    toolchain = provenance.get("toolchain", {})
    assert toolchain.get("load_alignment") == 16384, "toolchain load alignment policy mismatch"





def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--rootfs", type=pathlib.Path)
    parser.add_argument("--rootfs-sha", type=pathlib.Path)
    parser.add_argument("--rootfs-manifest", type=pathlib.Path)
    parser.add_argument("--native-dir", type=pathlib.Path)
    parser.add_argument("--native-manifest", type=pathlib.Path)
    args = parser.parse_args()
    rootfs_args = (args.rootfs, args.rootfs_sha, args.rootfs_manifest)
    assert all(rootfs_args) or not any(rootfs_args), "all rootfs arguments are required together"
    native_args = (args.native_dir, args.native_manifest)
    assert all(native_args) or not any(native_args), "both native arguments are required together"
    assert any(rootfs_args) or any(native_args), "select rootfs and/or native verification"
    if args.rootfs:
        verify_rootfs(*rootfs_args)
    if args.native_dir:
        verify_native(*native_args)


if __name__ == "__main__":
    main()
