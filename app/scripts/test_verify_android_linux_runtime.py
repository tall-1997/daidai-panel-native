import importlib.util
import hashlib
import io
import json
import pathlib
import struct
import tarfile
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("verify-android-linux-runtime.py")
SPEC = importlib.util.spec_from_file_location("runtime_verifier", SCRIPT)
runtime_verifier = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runtime_verifier)


def elf64(alignment=16384, machine=183):
    data = bytearray(120)
    data[:6] = b"\x7fELF\x02\x01"
    struct.pack_into("<H", data, 18, machine)
    struct.pack_into("<Q", data, 32, 64)
    struct.pack_into("<HH", data, 54, 56, 1)
    struct.pack_into("<I", data, 64, 1)
    struct.pack_into("<Q", data, 64 + 48, alignment)
    return data


class ElfVerificationTest(unittest.TestCase):
    def test_reads_arm64_load_alignment(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "runtime.so"
            path.write_bytes(elf64())
            self.assertEqual(runtime_verifier.elf_metadata(path), (183, [16384]))

    def test_rejects_truncated_program_headers(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "runtime.so"
            path.write_bytes(elf64()[:80])
            with self.assertRaisesRegex(AssertionError, "truncated"):
                runtime_verifier.elf_metadata(path)


class RootfsVerificationTest(unittest.TestCase):
    def test_accepts_complete_rootfs_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "rootfs.tar.gz.bin"
            with tarfile.open(archive, "w:gz") as bundle:
                for command in runtime_verifier.DISTRO_COMMANDS["ubuntu"]:
                    member = tarfile.TarInfo(f"usr/bin/{command}")
                    member.mode = 0o755
                    member.size = 0
                    bundle.addfile(member, io.BytesIO())
                ca_data = b"test certificate bundle"
                ca_member = tarfile.TarInfo("etc/ssl/certs/ca-certificates.crt")
                ca_member.mode = 0o644
                ca_member.size = len(ca_data)
                bundle.addfile(ca_member, io.BytesIO(ca_data))
                pnpm_data = json.dumps({"name": "pnpm", "version": "9.15.9"}).encode()
                pnpm_member = tarfile.TarInfo("usr/local/lib/node_modules/pnpm/package.json")
                pnpm_member.mode = 0o644
                pnpm_member.size = len(pnpm_data)
                bundle.addfile(pnpm_member, io.BytesIO(pnpm_data))
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksum = root / "rootfs.tar.gz.bin.sha256"
            checksum.write_text(digest + "\n")
            manifest = root / "runtime-manifest.json"
            manifest.write_text(json.dumps({
                "schema_version": 2,
                "distribution": "ubuntu",
                "ubuntu_version": "24.04.4",
                "ubuntu_arch": "arm64",
                "sha256": digest,
                "size": archive.stat().st_size,
                "apt_packages": ["bash", "python3", "python3-pip", "nodejs", "npm", "ca-certificates"],
                "global_tools": {"pnpm": {"version": "9.15.9", "install_source": "npm-global"}},
                "required_commands": list(runtime_verifier.DISTRO_COMMANDS["ubuntu"]),
                "capabilities": runtime_verifier.DISTRO_CAPABILITIES["ubuntu"],
                "base_archive": json.loads(runtime_verifier.TRUSTED_SOURCES.read_text())["ubuntu"]["24.04.4"]["arm64"],
            }))
            runtime_verifier.verify_rootfs(archive, checksum, manifest)
            unsupported = dict(runtime_verifier.DISTRO_CAPABILITIES["ubuntu"])
            unsupported["crypto"] = ["Crypto"]
            manifest.write_text(json.dumps({
                "schema_version": 2,
                "distribution": "ubuntu",
                "ubuntu_version": "24.04.4",
                "ubuntu_arch": "arm64",
                "sha256": digest,
                "size": archive.stat().st_size,
                "apt_packages": ["bash", "python3", "python3-pip", "nodejs", "npm", "ca-certificates"],
                "global_tools": {"pnpm": {"version": "9.15.9", "install_source": "npm-global"}},
                "required_commands": list(runtime_verifier.DISTRO_COMMANDS["ubuntu"]),
                "capabilities": unsupported,
                "base_archive": json.loads(runtime_verifier.TRUSTED_SOURCES.read_text())["ubuntu"]["24.04.4"]["arm64"],
            }))
            with self.assertRaisesRegex(AssertionError, "capabilities contract mismatch|unpackaged"):
                runtime_verifier.verify_rootfs(archive, checksum, manifest)

    def test_rejects_pnpm_manifest_version_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "rootfs.tar.gz.bin"
            with tarfile.open(archive, "w:gz") as bundle:
                for command in runtime_verifier.DISTRO_COMMANDS["ubuntu"]:
                    member = tarfile.TarInfo(f"usr/bin/{command}")
                    member.mode = 0o755
                    bundle.addfile(member, io.BytesIO())
                for name, data in (
                    ("etc/ssl/certs/ca-certificates.crt", b"certificate"),
                    ("usr/local/lib/node_modules/pnpm/package.json", b'{"version":"9.15.9"}'),
                ):
                    member = tarfile.TarInfo(name)
                    member.mode = 0o644
                    member.size = len(data)
                    bundle.addfile(member, io.BytesIO(data))
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksum = root / "rootfs.tar.gz.bin.sha256"
            checksum.write_text(digest)
            manifest = root / "runtime-manifest.json"
            manifest.write_text(json.dumps({
                "schema_version": 2, "distribution": "ubuntu", "ubuntu_version": "24.04.4", "ubuntu_arch": "arm64",
                "sha256": digest, "size": archive.stat().st_size,
                "apt_packages": sorted(runtime_verifier.DISTRO_PACKAGES["ubuntu"]),
                "global_tools": {"pnpm": {"version": "9.0.0", "install_source": "npm-global"}},
                "required_commands": list(runtime_verifier.DISTRO_COMMANDS["ubuntu"]),
                "capabilities": runtime_verifier.DISTRO_CAPABILITIES["ubuntu"],
                "base_archive": json.loads(runtime_verifier.TRUSTED_SOURCES.read_text())["ubuntu"]["24.04.4"]["arm64"],
            }))
            with self.assertRaisesRegex(AssertionError, "pnpm version"):
                runtime_verifier.verify_rootfs(archive, checksum, manifest)

    def test_rejects_base_archive_outside_trusted_contract(self):
        manifest = {
            "distribution": "ubuntu",
            "ubuntu_version": "24.04.4",
            "ubuntu_arch": "arm64",
        }
        trusted = runtime_verifier.trusted_base_archive(manifest)
        self.assertEqual("ubuntu-base-24.04.4-base-arm64.tar.gz", trusted["name"])
        manifest["ubuntu_version"] = "untrusted"
        with self.assertRaisesRegex(AssertionError, "trusted source is missing"):
            runtime_verifier.trusted_base_archive(manifest)


if __name__ == "__main__":
    unittest.main()
