import importlib.util
import hashlib
import json
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("verify-android-update-manifest.py")
SPEC = importlib.util.spec_from_file_location("update_manifest", SCRIPT)
update_manifest = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(update_manifest)


class AndroidUpdateManifestVerifierTest(unittest.TestCase):
    def test_verifies_every_release_abi_asset(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            matrix = {
                "abis": {
                    "arm64-v8a": {"release": True, "release_suffix": "arm64"},
                    "x86_64": {"release": True, "release_suffix": "x86_64"},
                },
            }
            packages = {}
            for abi, suffix in (("arm64-v8a", "arm64"), ("x86_64", "x86_64")):
                name = f"app-release-{suffix}.apk"
                payload = abi.encode()
                (root / name).write_bytes(payload)
                packages[abi] = {"full": {
                    "url": f"https://github.com/example/releases/download/v1/{name}",
                    "name": name,
                    "size": len(payload),
                    "sha256": hashlib.sha256(payload).hexdigest(),
                }, "patches": []}
            matrix_path = root / "matrix.json"
            manifest_path = root / "update.json"
            matrix_path.write_text(json.dumps(matrix))
            manifest_path.write_text(json.dumps({"schemaVersion": 2, "packages": packages}))
            update_manifest.verify(manifest_path, matrix_path, root)


if __name__ == "__main__":
    unittest.main()
