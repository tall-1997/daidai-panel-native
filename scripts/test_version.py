import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = REPO_ROOT / "scripts" / "version.py"


def load_version_module():
    spec = importlib.util.spec_from_file_location("version_script", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class VersionTest(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        (self.root / "app").mkdir()
        (self.root / "panel" / "web").mkdir(parents=True)
        (self.root / "panel" / "server" / "handler").mkdir(parents=True)
        (self.root / "VERSION.json").write_text(
            json.dumps(
                {
                    "schemaVersion": 1,
                    "version": "0.3.15",
                    "androidVersionCode": 30150,
                }
            )
            + "\n"
        )
        (self.root / "app" / "pubspec.yaml").write_text(
            "name: sample\nversion: 0.0.1+1\n"
        )
        (self.root / "panel" / "web" / "package.json").write_text(
            json.dumps({"name": "web", "version": "2.3.1"}, indent=2) + "\n"
        )
        (self.root / "panel" / "web" / "package-lock.json").write_text(
            json.dumps(
                {
                    "name": "web",
                    "version": "2.3.1",
                    "lockfileVersion": 3,
                    "packages": {"": {"name": "web", "version": "2.3.1"}},
                },
                indent=2,
            )
            + "\n"
        )
        (self.root / "panel" / "server" / "handler" / "version.go").write_text(
            'package handler\n\nvar Version = "2.3.1"\n'
        )

    def tearDown(self):
        self.temp_dir.cleanup()

    def run_script(self, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--root", str(self.root), *args],
            capture_output=True,
            text=True,
        )

    def test_version_code_reserves_ten_codes_per_release(self):
        module = load_version_module()
        self.assertEqual(module.android_version_code("0.3.15"), 30150)
        self.assertEqual(module.android_version_code("1.2.3"), 1020030)

    def test_sync_updates_flutter_and_web_then_check_passes(self):
        result = self.run_script("sync")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            (self.root / "app" / "pubspec.yaml").read_text(),
            "name: sample\nversion: 0.3.15+30150\n",
        )
        package = json.loads((self.root / "panel" / "web" / "package.json").read_text())
        self.assertEqual(package["version"], "0.3.15")
        package_lock = json.loads(
            (self.root / "panel" / "web" / "package-lock.json").read_text()
        )
        self.assertEqual(package_lock["version"], "0.3.15")
        self.assertEqual(package_lock["packages"][""]["version"], "0.3.15")
        self.assertIn(
            'var Version = "0.3.15"',
            (self.root / "panel" / "server" / "handler" / "version.go").read_text(),
        )
        check = self.run_script("check")
        self.assertEqual(check.returncode, 0, check.stderr)

    def test_check_reports_drift(self):
        result = self.run_script("check")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("app/pubspec.yaml", result.stderr)
        self.assertIn("panel/web/package.json", result.stderr)
        self.assertIn("panel/web/package-lock.json", result.stderr)
        self.assertIn("panel/server/handler/version.go", result.stderr)

    def test_check_reports_package_lock_root_package_drift(self):
        sync = self.run_script("sync")
        self.assertEqual(sync.returncode, 0, sync.stderr)
        package_lock_path = self.root / "panel" / "web" / "package-lock.json"
        package_lock = json.loads(package_lock_path.read_text())
        package_lock["packages"][""]["version"] = "0.3.14"
        package_lock_path.write_text(json.dumps(package_lock) + "\n")

        result = self.run_script("check")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("root package version", result.stderr)

    def test_manifest_sync_preserves_release_notes_and_assets(self):
        manifest_path = self.root / "android-update.json"
        manifest = {
            "schemaVersion": 1,
            "packageName": "com.daidai.daidai_app",
            "version": "0.3.14",
            "versionCode": 30140,
            "releaseNotes": "Keep this text",
            "full": {"url": "https://example.test/app.apk", "sha256": "abc"},
            "patches": [{"fromVersion": "0.3.14", "fromVersionCode": 30140}],
        }
        manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")

        result = self.run_script("sync", "--manifest", str(manifest_path))
        self.assertEqual(result.returncode, 0, result.stderr)
        actual = json.loads(manifest_path.read_text())
        self.assertEqual(actual["version"], "0.3.15")
        self.assertEqual(actual["versionCode"], 30150)
        self.assertEqual(actual["releaseNotes"], "Keep this text")
        self.assertEqual(actual["full"], manifest["full"])
        self.assertEqual(actual["patches"], manifest["patches"])
        check = self.run_script("check", "--manifest", str(manifest_path))
        self.assertEqual(check.returncode, 0, check.stderr)

    def test_rejects_inconsistent_android_version_code(self):
        source_path = self.root / "VERSION.json"
        source = json.loads(source_path.read_text())
        source["androidVersionCode"] = 315
        source_path.write_text(json.dumps(source) + "\n")

        result = self.run_script("sync")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("androidVersionCode", result.stderr)
        self.assertIn("30150", result.stderr)


if __name__ == "__main__":
    unittest.main()
