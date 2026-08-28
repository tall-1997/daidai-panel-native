import importlib.util
import json
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("sync-android-native-manifest.py")
SPEC = importlib.util.spec_from_file_location("native_manifest_sync", SCRIPT)
sync = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sync)


class NativeManifestSyncTest(unittest.TestCase):
    def test_synchronizes_all_and_only_elf_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            native = root / "x86_64"
            native.mkdir()
            (native / "libdaidai_proot.so").write_bytes(b"\x7fELFproot")
            (native / "libextra.so").write_bytes(b"\x7fELFextra")
            (native / "readme.so").write_text("not ELF")
            manifest = root / "manifest.json"
            manifest.write_text(json.dumps({"artifacts": [{"name": "libextra.so", "role": "compat"}]}))
            sync.synchronize(native, manifest)
            artifacts = json.loads(manifest.read_text())["artifacts"]
            self.assertEqual(["libdaidai_proot.so", "libextra.so"], [item["name"] for item in artifacts])
            self.assertEqual("proot", artifacts[0]["role"])
            self.assertEqual("compat", artifacts[1]["role"])


if __name__ == "__main__":
    unittest.main()
