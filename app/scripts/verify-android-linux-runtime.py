#!/usr/bin/env python3
"""Fail-closed verification for packaged Android Linux runtime artifacts."""

import argparse
import hashlib
import json
import pathlib
import struct
import tarfile

DISTRO_COMMANDS = {
    "ubuntu": ("apt-get", "bash", "python3", "pip3", "node", "npm", "pnpm"),
}
DISTRO_CAPABILITIES = {
    "ubuntu": {
        "package_manager": ["apt-get"],
        "shell": ["bash"],
        "python": ["python3", "pip3"],
        "node": ["node", "npm", "pnpm"],
        "tls_ca_certificates": True,
    },
}
UNPACKAGED_CAPABILITIES = {"crypto", "typescript"}
DISTRO_PACKAGES = {
    "ubuntu": {"bash", "python3", "python3-pip", "nodejs", "npm", "ca-certificates"},
}
DISTRO_GLOBAL_TOOLS = {"ubuntu": {"pnpm": "npm-global"}}
TRUSTED_SOURCES = pathlib.Path(__file__).with_name("rootfs-trusted-sources.json")
ABI_MATRIX = pathlib.Path(__file__).resolve().parents[2] / "runtime" / "android-abi-matrix.json"


def load_abi_matrix(path: pathlib.Path = ABI_MATRIX) -> dict:
    matrix = json.loads(path.read_text(encoding="utf-8"))
    assert matrix.get("schema_version") == 1, "ABI matrix schema_version must be 1"
    assert matrix.get("default_abi") in matrix.get("abis", {}), "ABI matrix default ABI is missing"
    return matrix


def trusted_base_archive(manifest: dict, trusted_sources: pathlib.Path = TRUSTED_SOURCES) -> dict:
    contract = json.loads(trusted_sources.read_text(encoding="utf-8"))
    assert contract.get("schema_version") == 1, "trusted rootfs source schema_version must be 1"
    distribution = manifest.get("distribution")
    version = manifest.get("ubuntu_version")
    arch = manifest.get("ubuntu_arch")
    try:
        return contract[distribution][version][arch]
    except (KeyError, TypeError) as error:
        raise AssertionError(f"rootfs trusted source is missing for {distribution}/{version}/{arch}") from error


def sha256(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_rootfs(archive: pathlib.Path, checksum: pathlib.Path, manifest_path: pathlib.Path,
                  trusted_sources: pathlib.Path = TRUSTED_SOURCES,
                  abi_matrix: pathlib.Path = ABI_MATRIX) -> None:
    manifest = json.loads(manifest_path.read_text())
    abi = manifest.get("abi")
    abi_config = load_abi_matrix(abi_matrix).get("abis", {}).get(abi)
    assert abi_config and abi_config.get("rootfs") is True, f"unsupported rootfs ABI: {abi}"
    assert manifest.get("ubuntu_arch") == abi_config.get("ubuntu_arch"), "rootfs Ubuntu architecture mismatch"
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
    trusted = trusted_base_archive(manifest, trusted_sources)
    recorded_base = manifest.get("base_archive")
    assert isinstance(recorded_base, dict), "rootfs base archive provenance is missing"
    for field in ("name", "sha256", "digest_source", "archive_source"):
        assert recorded_base.get(field) == trusted.get(field), f"rootfs base archive {field} mismatch"
    assert manifest.get("required_commands") == list(commands), "required_commands contract mismatch"
    assert manifest.get("capabilities") == capabilities, "capabilities contract mismatch"
    assert not (UNPACKAGED_CAPABILITIES & set(manifest.get("capabilities", {}))), "manifest claims unpackaged runtime capabilities"
    packages = set(manifest.get("apt_packages", []))
    assert required_packages <= packages, "required packages missing"
    global_tools = manifest.get("global_tools")
    assert isinstance(global_tools, dict), "global_tools contract is missing"
    for name, install_source in DISTRO_GLOBAL_TOOLS[distribution].items():
        tool = global_tools.get(name)
        assert isinstance(tool, dict), f"global tool is missing: {name}"
        assert tool.get("install_source") == install_source, f"global tool install source mismatch: {name}"
        assert isinstance(tool.get("version"), str) and tool["version"], f"global tool version is missing: {name}"
    with tarfile.open(archive, mode="r:*") as rootfs:
        members = {member.name.removeprefix("./"): member for member in rootfs.getmembers()}
        pnpm_member = members.get("usr/local/lib/node_modules/pnpm/package.json")
        assert pnpm_member is not None, "rootfs pnpm package metadata missing"
        pnpm_package = rootfs.extractfile(pnpm_member)
        assert pnpm_package is not None, "rootfs pnpm package metadata missing"
        installed_pnpm = json.load(pnpm_package).get("version")
    assert installed_pnpm == global_tools["pnpm"]["version"], "rootfs pnpm version does not match manifest"
    for command in commands:
        candidates = (f"usr/bin/{command}", f"bin/{command}", f"usr/sbin/{command}", f"sbin/{command}", f"usr/local/bin/{command}")
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


def verify_native(native_dir: pathlib.Path, manifest_path: pathlib.Path,
                  abi_matrix: pathlib.Path = ABI_MATRIX) -> None:
    manifest = json.loads(manifest_path.read_text())
    assert manifest.get("schema_version") == 1, "native manifest schema_version must be 1"
    abi = manifest.get("abi")
    assert abi == native_dir.name, "native manifest abi does not match its directory"
    abi_config = load_abi_matrix(abi_matrix).get("abis", {}).get(abi)
    assert abi_config and abi_config.get("native") is True, f"unsupported native ABI: {abi}"
    minimum_alignment = abi_config["minimum_load_alignment"]
    assert manifest.get("minimum_load_alignment") == minimum_alignment, "native alignment policy mismatch"
    provenance = manifest.get("provenance", {})
    strategy = provenance.get("strategy")
    assert strategy == "self-contained-source-build", "unapproved PRoot provenance strategy (Termux packages are no longer supported)"
    _verify_source_build(provenance)
    artifacts = manifest.get("artifacts", [])
    assert artifacts, "native runtime manifest has no artifacts"
    names = {item.get("name") for item in artifacts}
    assert len(names) == len(artifacts), "native runtime manifest contains duplicate artifacts"
    packaged_elfs = {path.name for path in native_dir.glob("*.so") if path.is_file() and path.read_bytes()[:4] == b"\x7fELF"}
    assert names == packaged_elfs, f"native manifest ELF set mismatch: missing={sorted(packaged_elfs - names)} extra={sorted(names - packaged_elfs)}"
    assert {"libproot_loader.so"} <= names, "native runtime loader manifest is incomplete"
    assert provenance.get("runtime_overrides", {}).get("PROOT_LOADER") == "libproot_loader.so", "PRoot loader override contract is missing"
    for artifact in artifacts:
        path = native_dir / artifact["name"]
        assert path.is_file(), f"native artifact missing: {path}"
        assert artifact.get("size") == path.stat().st_size, f"native artifact size mismatch: {path.name}"
        assert artifact.get("sha256") == sha256(path), f"native artifact checksum mismatch: {path.name}"
        machine, alignments = elf_metadata(path)
        expected_machine = abi_config["elf_machine"]
        assert machine == expected_machine, f"native artifact does not match ABI {abi}: {path.name}"
        assert all(value >= minimum_alignment for value in alignments), f"native artifact has PT_LOAD alignment below policy: {path.name}"
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
    parser.add_argument("--trusted-sources", type=pathlib.Path, default=TRUSTED_SOURCES)
    parser.add_argument("--abi-matrix", type=pathlib.Path, default=ABI_MATRIX)
    parser.add_argument("--native-dir", type=pathlib.Path)
    parser.add_argument("--native-manifest", type=pathlib.Path)
    args = parser.parse_args()
    rootfs_args = (args.rootfs, args.rootfs_sha, args.rootfs_manifest)
    assert all(rootfs_args) or not any(rootfs_args), "all rootfs arguments are required together"
    native_args = (args.native_dir, args.native_manifest)
    assert all(native_args) or not any(native_args), "both native arguments are required together"
    assert any(rootfs_args) or any(native_args), "select rootfs and/or native verification"
    if args.rootfs:
        verify_rootfs(*rootfs_args, args.trusted_sources, args.abi_matrix)
    if args.native_dir:
        verify_native(*native_args, args.abi_matrix)


if __name__ == "__main__":
    main()
