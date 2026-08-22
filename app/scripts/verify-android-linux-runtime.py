#!/usr/bin/env python3
"""Fail-closed verification for packaged Android Linux runtime artifacts."""

import argparse
import hashlib
import json
import pathlib
import struct
import tarfile

REQUIRED_COMMANDS = ("apk", "bash", "python3", "pip3", "node", "npm", "uv", "pnpm")
REQUIRED_CAPABILITIES = {
    "package_manager": ["apk"],
    "shell": ["bash"],
    "python": ["python3", "pip3", "uv"],
    "node": ["node", "npm", "pnpm"],
    "tls_ca_certificates": True,
}
def sha256(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_rootfs(archive: pathlib.Path, checksum: pathlib.Path, manifest_path: pathlib.Path) -> None:
    manifest = json.loads(manifest_path.read_text())
    expected_sha = checksum.read_text().strip().split()[0]
    actual_sha = sha256(archive)
    assert len(expected_sha) == 64 and expected_sha == actual_sha, "rootfs checksum mismatch"
    assert manifest.get("schema_version") == 2, "rootfs manifest schema_version must be 2"
    assert manifest.get("sha256") == actual_sha, "rootfs manifest sha256 mismatch"
    assert manifest.get("size") == archive.stat().st_size, "rootfs manifest size mismatch"
    assert manifest.get("required_commands") == list(REQUIRED_COMMANDS), "required_commands contract mismatch"
    assert manifest.get("capabilities") == REQUIRED_CAPABILITIES, "capabilities contract mismatch"
    packages = set(manifest.get("packages", []))
    assert {"bash", "python3", "py3-pip", "nodejs", "npm", "uv", "pnpm", "ca-certificates"} <= packages, "required packages missing"
    with tarfile.open(archive, mode="r:gz") as rootfs:
        members = {member.name.removeprefix("./"): member for member in rootfs.getmembers()}
    for command in REQUIRED_COMMANDS:
        candidates = (f"usr/bin/{command}", f"bin/{command}", f"usr/sbin/{command}", f"sbin/{command}")
        command_member = next((members[name] for name in candidates if name in members), None)
        assert command_member is not None, f"rootfs command missing: {command}"
        assert command_member.issym() or command_member.mode & 0o111, f"rootfs command is not executable: {command}"
    ca_bundle = members.get("etc/ssl/certs/ca-certificates.crt")
    assert ca_bundle is not None and ca_bundle.size > 0, "rootfs CA certificate bundle missing"


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
    assert manifest.get("abi") == "arm64-v8a", "native runtime must target arm64-v8a"
    assert manifest.get("minimum_load_alignment") == 16384, "native alignment policy mismatch"
    provenance = manifest.get("provenance", {})
    assert provenance.get("strategy") == "pinned-termux-binary-packages", "unapproved PRoot provenance strategy"
    assert provenance.get("source_build") is False, "binary import must not claim a source build"
    assert provenance.get("source_patch_applied") is False, "binary import must not claim source patches"
    assert len(provenance.get("termux_recipe", {}).get("commit", "")) == 40, "Termux recipe commit is not pinned"
    assert len(provenance.get("upstream_source", {}).get("sha256", "")) == 64, "upstream PRoot source is not pinned"
    packages = manifest.get("packages", [])
    assert packages and all(len(item.get("sha256", "")) == 64 for item in packages), "unpinned Termux package"
    artifacts = manifest.get("artifacts", [])
    names = {item.get("name") for item in artifacts}
    required = {"liboperit_proot.so", "liboperit_busybox.so", "libtalloc_2.so", "libandroid-shmem.so", "libbusybox_1_38_0.so"}
    assert required <= names, "native runtime dependency manifest is incomplete"
    expected_dependencies = {
        "liboperit_proot.so": (b"libtalloc_2.so", b"libandroid-shmem.so"),
        "liboperit_busybox.so": (b"libbusybox_1_38_0.so",),
        "libbusybox_1_38_0.so": (b"libandroid-selinux.so",),
        "libandroid-selinux.so": (b"libpcre2-8.so",),
    }
    for artifact in artifacts:
        path = native_dir / artifact["name"]
        assert path.is_file(), f"native artifact missing: {path}"
        assert artifact.get("size") == path.stat().st_size, f"native artifact size mismatch: {path.name}"
        assert artifact.get("sha256") == sha256(path), f"native artifact checksum mismatch: {path.name}"
        machine, alignments = elf_metadata(path)
        assert machine == 183, f"native artifact is not AArch64: {path.name}"
        assert all(value >= 16384 for value in alignments), f"native artifact has PT_LOAD alignment below 16KB: {path.name}"
        data = path.read_bytes()
        assert all(dependency in data for dependency in expected_dependencies.get(path.name, ())), f"native artifact dependency contract mismatch: {path.name}"


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
