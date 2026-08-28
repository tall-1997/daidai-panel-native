import json
import unittest
from pathlib import Path


APP_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = APP_ROOT / "scripts" / "prepare-android-ubuntu-rootfs.sh"
TRUSTED_SOURCES = APP_ROOT / "scripts" / "rootfs-trusted-sources.json"
MANIFEST = APP_ROOT / "android" / "app" / "src" / "main" / "assets" / "android-runtime" / "arm64-v8a" / "ubuntu" / "runtime-manifest.json"
DOWNLOADER = APP_ROOT / "android" / "app" / "src" / "main" / "kotlin" / "com" / "daidai" / "daidai_app" / "AndroidRootfsDownloader.kt"


class UbuntuRootfsSupplyChainContractTest(unittest.TestCase):
    def test_builder_uses_https_and_verifies_pinned_publisher_digest(self):
        script = SCRIPT.read_text(encoding="utf-8")
        trusted = json.loads(TRUSTED_SOURCES.read_text(encoding="utf-8"))["ubuntu"]["24.04.4"]["arm64"]
        self.assertNotIn(':-http://', script)
        self.assertIn("release/SHA256SUMS", script)
        self.assertEqual(64, len(trusted["sha256"]))
        self.assertIn("sha256sum --check --status", script)

    def test_packaged_manifest_records_base_archive_digest_source(self):
        manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
        trusted = json.loads(TRUSTED_SOURCES.read_text(encoding="utf-8"))["ubuntu"]["24.04.4"]["arm64"]
        base = manifest["base_archive"]
        self.assertEqual(trusted, base)
        self.assertEqual(64, len(base["sha256"]))

    def test_builder_consumes_shared_trusted_source_contract(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertIn('trusted_sources="$script_dir/rootfs-trusted-sources.json"', script)
        self.assertNotIn("UBUNTU_BASE_ARM64_SHA256", script)

    def test_builder_packages_managed_dns_placeholder(self):
        script = SCRIPT.read_text(encoding="utf-8")
        self.assertNotIn('cp /etc/resolv.conf', script)
        self.assertIn('Managed by the Android application before each proot command.', script)

    def test_manifest_separates_apt_packages_from_npm_global_tools(self):
        manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
        self.assertNotIn("pnpm", manifest["apt_packages"])
        self.assertEqual("npm-global", manifest["global_tools"]["pnpm"]["install_source"])
        self.assertRegex(manifest["global_tools"]["pnpm"]["version"], r"^[0-9]+(?:\.[0-9]+){2}$")

    def test_downloader_has_no_structural_checksum_fallback(self):
        downloader = DOWNLOADER.read_text(encoding="utf-8")
        self.assertNotIn("structural-fallback", downloader)
        self.assertIn("无法取得发布方可信 SHA-256，已保留部分下载文件以便重试。", downloader)


if __name__ == "__main__":
    unittest.main()
