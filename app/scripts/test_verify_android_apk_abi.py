import importlib.util
import json
import pathlib
import struct
import tempfile
import unittest
import zipfile

SCRIPT = pathlib.Path(__file__).with_name("verify-android-apk-abi.py")
SPEC = importlib.util.spec_from_file_location("apk_abi", SCRIPT)
apk_abi = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(apk_abi)


def elf(machine=62, alignment=16384):
    payload = bytearray(120)
    payload[:6] = b"\x7fELF\x02\x01"
    struct.pack_into("<H", payload, 18, machine)
    struct.pack_into("<Q", payload, 32, 64)
    struct.pack_into("<HH", payload, 54, 56, 1)
    struct.pack_into("<I", payload, 64, 1)
    struct.pack_into("<Q", payload, 112, alignment)
    return bytes(payload)


class ApkAbiVerifierTest(unittest.TestCase):
    def test_rejects_foreign_runtime_assets(self):
        with tempfile.TemporaryDirectory() as directory:
            apk = pathlib.Path(directory) / "app.apk"
            payload = elf()
            manifest = {"artifacts": [{"name": "libtest.so", "sha256": __import__("hashlib").sha256(payload).hexdigest()}]}
            with zipfile.ZipFile(apk, "w") as archive:
                archive.writestr("lib/x86_64/libtest.so", payload)
                archive.writestr("assets/android-runtime/x86_64/native-runtime-manifest.json", json.dumps(manifest))
                archive.writestr("assets/android-runtime/x86_64/ubuntu/rootfs.tar.gz.bin", b"rootfs")
                archive.writestr("assets/android-runtime/arm64-v8a/native-runtime-manifest.json", "{}")
            with self.assertRaisesRegex(AssertionError, "foreign runtime"):
                apk_abi.verify(apk, "x86_64", apk_abi.pathlib.Path(__file__).resolve().parents[2] / "runtime/android-abi-matrix.json")

    def test_writes_complete_apk_elf_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            apk = root / "app.apk"
            output = root / "elf-manifest.json"
            payload = elf()
            digest = __import__("hashlib").sha256(payload).hexdigest()
            manifest = {"artifacts": [{"name": "libtest.so", "sha256": digest}]}
            with zipfile.ZipFile(apk, "w") as archive:
                archive.writestr("lib/x86_64/libtest.so", payload)
                archive.writestr("assets/android-runtime/x86_64/native-runtime-manifest.json", json.dumps(manifest))
                archive.writestr("assets/android-runtime/x86_64/ubuntu/rootfs.tar.gz.bin", b"rootfs")
            apk_abi.verify(apk, "x86_64", pathlib.Path(__file__).resolve().parents[2] / "runtime/android-abi-matrix.json", output)
            result = json.loads(output.read_text())
            self.assertEqual("x86_64", result["abi"])
            self.assertEqual(["libtest.so"], [item["name"] for item in result["artifacts"]])


if __name__ == "__main__":
    unittest.main()
