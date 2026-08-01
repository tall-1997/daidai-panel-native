import hashlib
import json
import pathlib
import subprocess
import tempfile
import unittest
import zipfile


SCRIPT = pathlib.Path(__file__).with_name("prepare-android-python-runtime.sh")


class AndroidPythonRuntimeScriptTest(unittest.TestCase):
    def run_helper(self, command, *args):
        return subprocess.run(
            ["bash", str(SCRIPT), command, *map(str, args)],
            check=False,
            capture_output=True,
            text=True,
        )

    def make_wheel(self, directory, name):
        path = directory / name
        with zipfile.ZipFile(path, "w") as archive:
            archive.writestr("example/__init__.py", "")
        return path

    def test_wheel_gate_accepts_pure_python_and_rejects_foreign_abi(self):
        with tempfile.TemporaryDirectory() as temporary:
            wheelhouse = pathlib.Path(temporary)
            self.make_wheel(wheelhouse, "example-1.0-py3-none-any.whl")
            accepted = self.run_helper("--verify-wheelhouse", wheelhouse)
            self.assertEqual(0, accepted.returncode, accepted.stderr)

            self.make_wheel(wheelhouse, "bad-1.0-cp312-cp312-manylinux_2_17_x86_64.whl")
            rejected = self.run_helper("--verify-wheelhouse", wheelhouse)
            self.assertNotEqual(0, rejected.returncode)
            self.assertIn("incompatible wheel", rejected.stderr)

    def test_wheel_gate_rejects_empty_and_fake_pure_python_wheelhouse(self):
        with tempfile.TemporaryDirectory() as temporary:
            wheelhouse = pathlib.Path(temporary)
            empty = self.run_helper("--verify-wheelhouse", wheelhouse)
            self.assertNotEqual(0, empty.returncode)
            self.assertIn("wheelhouse is empty", empty.stderr)

            wheel = self.make_wheel(wheelhouse, "native-1.0-py3-none-any.whl")
            with zipfile.ZipFile(wheel, "a") as archive:
                archive.writestr("native/module.so", b"ELF")
            native = self.run_helper("--verify-wheelhouse", wheelhouse)
            self.assertNotEqual(0, native.returncode)
            self.assertIn("contains native code", native.stderr)

    def test_wheel_gate_accepts_cp314_android_arm64(self):
        with tempfile.TemporaryDirectory() as temporary:
            wheelhouse = pathlib.Path(temporary)
            self.make_wheel(wheelhouse, "native-1.0-cp314-abi3-android_28_arm64_v8a.whl")
            accepted = self.run_helper("--verify-wheelhouse", wheelhouse)
            self.assertEqual(0, accepted.returncode, accepted.stderr)

    def test_metadata_generation_hashes_every_runtime_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            assets = root / "assets"
            native = root / "native"
            runtime = root / "runtime"
            assets.mkdir()
            native.mkdir()
            runtime.mkdir()
            (assets / "stdlib.py").write_text("pass\n")
            launcher = native / "libpython_exec.so"
            launcher.write_bytes(b"launcher")
            (native / "libpython3.14.so").write_bytes(b"python")
            (runtime / "manifest.json").write_text(json.dumps({"version": "1", "components": []}))
            (runtime / "compatibility.json").write_text(json.dumps({"version": "1", "runtimes": []}))
            (runtime / "dependencies.json").write_text(json.dumps({"version": "1"}))
            (runtime / "smoke-evidence.json").write_text(json.dumps({"version": "1", "records": []}))

            result = self.run_helper(
                "--generate-metadata", assets, native, runtime, "3.14.6", "test-revision"
            )
            self.assertEqual(0, result.returncode, result.stderr)
            manifest = json.loads((runtime / "manifest.json").read_text())
            python = next(item for item in manifest["components"] if item["id"] == "python-3.14-android-arm64")
            self.assertEqual(hashlib.sha256(launcher.read_bytes()).hexdigest(), python["sha256"])
            self.assertEqual(3, python["artifact_count"])
            self.assertEqual("test-revision", python["asset_revision"])
            smoke = json.loads((runtime / "smoke-evidence.json").read_text())
            record = next(item for item in smoke["records"] if item["runtime_id"] == python["id"])
            self.assertEqual("blocked", record["status"])
            self.assertEqual("device-smoke-required", record["checks"][0]["reason"])

    def test_runtime_digest_is_stable_and_content_sensitive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            assets = root / "assets"
            native = root / "native"
            assets.mkdir()
            native.mkdir()
            (assets / "stdlib.py").write_text("pass\n")
            (native / "libpython3.14.so").write_bytes(b"python")

            first = self.run_helper("--runtime-digest", assets, native)
            second = self.run_helper("--runtime-digest", assets, native)
            self.assertEqual(0, first.returncode, first.stderr)
            self.assertEqual(first.stdout, second.stdout)

            (assets / "stdlib.py").write_text("changed\n")
            changed = self.run_helper("--runtime-digest", assets, native)
            self.assertNotEqual(first.stdout, changed.stdout)

    def test_prepare_script_bootstraps_missing_ensurepip_bundle(self):
        script = SCRIPT.read_text()
        self.assertIn('ENSUREPIP_BUNDLED=', script)
        self.assertIn('--no-deps', script)
        self.assertIn('pip==24.0', script)
        self.assertIn('verify_ensurepip_bundle "$ENSUREPIP_BUNDLED"', script)
        bootstrap = script.index('pip==24.0')
        validation = script.index('verify_ensurepip_bundle "$ENSUREPIP_BUNDLED"')
        self.assertLess(bootstrap, validation)

    def test_ensurepip_bundle_validation_expands_globs(self):
        with tempfile.TemporaryDirectory() as temporary:
            bundled = pathlib.Path(temporary)
            missing = self.run_helper("--verify-ensurepip-bundle", bundled)
            self.assertNotEqual(0, missing.returncode)
            self.assertIn("ensurepip bundle is missing", missing.stderr)

            (bundled / "pip-24.0-py3-none-any.whl").write_bytes(b"wheel")
            present = self.run_helper("--verify-ensurepip-bundle", bundled)
            self.assertEqual(0, present.returncode, present.stderr)


if __name__ == "__main__":
    unittest.main()
