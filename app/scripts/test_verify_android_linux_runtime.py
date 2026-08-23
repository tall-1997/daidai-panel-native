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
                for command in runtime_verifier.REQUIRED_COMMANDS:
                    member = tarfile.TarInfo(f"usr/bin/{command}")
                    member.mode = 0o755
                    member.size = 0
                    bundle.addfile(member, io.BytesIO())
                ca_data = b"test certificate bundle"
                ca_member = tarfile.TarInfo("etc/ssl/certs/ca-certificates.crt")
                ca_member.mode = 0o644
                ca_member.size = len(ca_data)
                bundle.addfile(ca_member, io.BytesIO(ca_data))
                crypto_member = tarfile.TarInfo("usr/lib/python3.14/site-packages/Crypto/Cipher/AES.py")
                crypto_member.mode = 0o644
                crypto_member.size = 0
                bundle.addfile(crypto_member, io.BytesIO())
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksum = root / "rootfs.tar.gz.bin.sha256"
            checksum.write_text(digest + "\n")
            manifest = root / "runtime-manifest.json"
            manifest.write_text(json.dumps({
                "schema_version": 2,
                "sha256": digest,
                "size": archive.stat().st_size,
                "packages": ["bash", "python3", "py3-pip", "py3-pycryptodome", "nodejs", "npm", "uv", "pnpm", "ca-certificates"],
                "required_commands": list(runtime_verifier.REQUIRED_COMMANDS),
                "capabilities": runtime_verifier.REQUIRED_CAPABILITIES,
            }))
            runtime_verifier.verify_rootfs(archive, checksum, manifest)


if __name__ == "__main__":
    unittest.main()
