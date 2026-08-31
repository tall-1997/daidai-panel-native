import json
import unittest
from pathlib import Path


APP_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = APP_ROOT / "scripts" / "prepare-android-alpine-rootfs.sh"
TRUSTED_SOURCES = APP_ROOT / "scripts" / "rootfs-trusted-sources.json"
DOWNLOADER = APP_ROOT / "android" / "app" / "src" / "main" / "kotlin" / "com" / "daidai" / "daidai_app" / "AndroidRootfsDownloader.kt"


class AlpineRootfsSupplyChainContractTest(unittest.TestCase):
    def test_script_exists_and_references_alpine_minirootfs(self):
        self.assertTrue(SCRIPT.is_file(), "prepare-android-alpine-rootfs.sh must exist")
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("alpine-minirootfs", script)
        self.assertIn("alpine-minirootfs-${alpine_version}-${arch}.tar.gz", script)

    def test_builder_skips_arm64_and_keeps_ubuntu_there(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("skipping arm64-v8a (arm64 keeps Ubuntu)", script)
        self.assertRegex(script, r"arm64-v8a\)\s*log_skip=1")
        # x86_64 is the only ABI the alpine builder produces.
        self.assertIn("x86_64) build_rootfs_for_abi", script)
        self.assertNotIn("aarch64) build_rootfs_for_abi", script)

    def test_builder_verifies_with_shared_runtime_verifier(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("verify-android-linux-runtime.py", script)
        self.assertIn("--rootfs", script)
        self.assertIn("--rootfs-sha", script)
        self.assertIn("--rootfs-manifest", script)

    def test_apk_repository_path_has_no_doubled_v_prefix(self):
        script = SCRIPT.read_text(encoding="utf-8")
        # alpine_branch="v${alpine_version%.*}" already carries the "v" prefix;
        # the repositories printf must not add another one (CI run 33310794989
        # hit HTTP 404 on /alpine/vv3.24/main/APKINDEX.tar.gz).
        self.assertIn('alpine_branch="v${alpine_version%.*}"', script)
        self.assertIn("printf '%s/%s/main\\n%s/%s/community\\n'", script)
        self.assertNotIn("printf '%s/v%s/main", script)

    def test_builder_consumes_shared_trusted_source_contract(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn('trusted_sources="$script_dir/rootfs-trusted-sources.json"', script)
        self.assertIn('contract["alpine"][sys.argv[2]][sys.argv[3]]', script)
        self.assertIn("sha256sum --check --status", script)

    def test_manifest_writes_alpine_keys(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn('"distribution": "alpine"', script)
        self.assertIn('"alpine_arch": arch', script)
        self.assertIn('"alpine_version": version', script)
        self.assertIn('"alpine_packages": apk_packages.split()', script)

    def test_trusted_sources_cover_alpine_3_24_1_both_arches(self):
        contract = json.loads(TRUSTED_SOURCES.read_text(encoding="utf-8"))
        entries = contract["alpine"]["3.24.1"]
        self.assertEqual({"x86_64", "aarch64"}, set(entries))
        for arch in ("x86_64", "aarch64"):
            entry = entries[arch]
            self.assertEqual(f"alpine-minirootfs-3.24.1-{arch}.tar.gz", entry["name"])
            self.assertRegex(entry["sha256"], r"^[0-9a-f]{64}$")
            self.assertEqual(
                f"https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/{arch}/alpine-minirootfs-3.24.1-{arch}.tar.gz.sha256",
                entry["digest_source"],
            )
            self.assertEqual(
                f"https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/{arch}/alpine-minirootfs-3.24.1-{arch}.tar.gz",
                entry["archive_source"],
            )

    def test_downloader_pins_alpine_version_and_arches(self):
        downloader = DOWNLOADER.read_text(encoding="utf-8")
        self.assertIn('ROOTFS_DOWNLOAD_VERSION_ALPINE = "3.24.1"', downloader)
        self.assertIn('"x86_64" -> "x86_64"', downloader)
        self.assertIn('"arm64-v8a" -> "aarch64"', downloader)


if __name__ == "__main__":
    unittest.main()
