import json
import re
import unittest
from pathlib import Path


WORKFLOW = Path(__file__).resolve().parents[1] / ".github" / "workflows" / "android-release.yml"
ROOT = WORKFLOW.parents[2]


class AndroidReleaseWorkflowContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = WORKFLOW.read_text(encoding="utf-8")
        cls.gradle = (ROOT / "app/android/app/build.gradle.kts").read_text(encoding="utf-8")
        cls.version = json.loads((ROOT / "VERSION.json").read_text(encoding="utf-8"))["version"]

    def test_release_android_test_targets_and_uses_release_candidate(self):
        self.assertRegex(self.gradle, r'(?m)^\s*testBuildType\s*=\s*"release"\s*$')
        self.assertIn(":app:testReleaseUnitTest", self.workflow)
        self.assertNotIn(":app:testDebugUnitTest", self.workflow)
        self.assertIn(":app:assembleReleaseAndroidTest", self.workflow)
        self.assertIn(
            'cp build/app/outputs/apk/androidTest/release/app-release-androidTest.apk "../daidai-panel-native-${VERSION}-${RELEASE_CHANNEL}-${SUFFIX}-androidTest.apk"',
            self.workflow,
        )

    def test_device_smoke_builds_and_runs_release_variants(self):
        workflow = (ROOT / ".github/workflows/android-device-smoke.yml").read_text(encoding="utf-8")
        self.assertIn("flutter build apk --release --target-platform android-arm64", workflow)
        self.assertIn(":app:assembleReleaseAndroidTest", workflow)
        self.assertGreaterEqual(workflow.count("app-release.apk"), 3)
        self.assertGreaterEqual(workflow.count("app-release-androidTest.apk"), 3)
        self.assertNotIn("app-debug.apk", workflow)
        self.assertNotIn("assembleDebugAndroidTest", workflow)
        self.assertNotIn("--allow-debug-run-as-fallback", workflow)
        release_build_type = self.gradle.split("    buildTypes {", 1)[1].split("    packaging {", 1)[0]
        self.assertIn("isDebuggable = false", release_build_type)

    def test_gradle_owns_release_android_test_signing(self):
        release_build_type = self.gradle.split("    buildTypes {", 1)[1].split("    packaging {", 1)[0]
        self.assertIn('signingConfig = signingConfigs.getByName("release")', release_build_type)
        self.assertIn('signingConfig = signingConfigs.getByName("debug")', release_build_type)
        self.assertNotIn("Sign release instrumentation APK", self.workflow)
        self.assertNotRegex(self.workflow, r'(?m)^\s*"\$\{APKSIGNER\}" sign \\\s*$')
        self.assertIn('test "${TEST_CERT_SHA256}" = "${APK_CERT_SHA256}"', self.workflow)

    def test_version_file_is_the_only_release_tag_policy_source(self):
        self.assertEqual(self.version, "1.0.20")
        self.assertIn('RELEASE_TAG="v${VERSION}"', self.workflow)
        self.assertNotRegex(self.workflow, r'RELEASE_TAG=.*-rc[.$]')
        self.assertNotIn('RELEASE_TAG="v1.0.19"', self.workflow)

    def test_release_builds_all_abis_from_shared_matrix(self):
        matrix = json.loads((ROOT / "runtime/android-abi-matrix.json").read_text(encoding="utf-8"))
        self.assertEqual({"arm64-v8a", "x86_64"}, {abi for abi, config in matrix["abis"].items() if config["release"]})
        self.assertIn("android-abi-matrix.py list release", self.workflow)
        self.assertIn('get "${ABI}" flutter_target', self.workflow)
        self.assertIn('get "${ABI}" release_suffix', self.workflow)
        self.assertIn('--split-per-abi', self.workflow)
        self.assertIn('FLUTTER_SPLIT_PER_ABI=true', self.workflow)
        self.assertIn('app-${ABI}-release.apk', self.workflow)
        self.assertIn("schemaVersion: 2", self.workflow)
        self.assertIn('"x86_64": {full:', self.workflow)

    def test_yaegi_build_updates_arm64_runtime_contract_before_verification(self):
        script = (ROOT / "app/scripts/prepare-android-yaegi-runtime.sh").read_text(encoding="utf-8")
        self.assertIn('component.get("id") == "yaegi-go"', script)
        self.assertIn('component.get("abi") == "arm64-v8a"', script)
        self.assertLess(
            self.workflow.index('bash scripts/prepare-android-yaegi-runtime.sh'),
            self.workflow.index('Verify runtime contract before APK build'),
        )

    def test_release_and_x86_smoke_rebuild_pinned_native_runtime(self):
        device_workflow = (ROOT / ".github/workflows/android-device-smoke.yml").read_text(encoding="utf-8")
        native_script = (ROOT / "app/scripts/prepare-android-native-source-build.sh").read_text(encoding="utf-8")
        native_build = "FORCE_REBUILD_PROOT=1 bash scripts/prepare-android-native-source-build.sh"
        self.assertIn(native_build, self.workflow)
        self.assertIn(native_build, device_workflow)
        release_native_build = self.workflow.index(native_build)
        device_native_build = device_workflow.index(native_build)
        self.assertGreater(
            self.workflow.index('bash scripts/prepare-android-yaegi-runtime.sh', release_native_build),
            release_native_build,
        )
        self.assertGreater(
            device_workflow.index('bash scripts/prepare-android-yaegi-runtime.sh', device_native_build),
            device_native_build,
        )
        self.assertIn("ashmem-memfd-ftruncate-no-fallthrough", native_script)
        self.assertIn("Pinned PRoot ashmem ftruncate source no longer matches the audited patch", native_script)
        self.assertIn('source.count(old) != 1', native_script)

    def test_prerelease_fixed_tag_can_be_safely_reused(self):
        self.assertIn("EXISTING_TAG=", self.workflow)
        self.assertIn("EXISTING_PRERELEASE=", self.workflow)
        self.assertIn("EXISTING_TARGET=", self.workflow)
        self.assertIn('TAG_COMMIT="$(jq -er \'.object.sha\' "${TAG_LOOKUP}")"', self.workflow)
        self.assertIn('[[ "${TAG_TYPE}" != "commit" ]]', self.workflow)
        self.assertIn(
            '[[ "${EXISTING_TARGET}" != "main" && "${EXISTING_TARGET}" != "${TAG_COMMIT}" ]]',
            self.workflow,
        )
        self.assertNotIn('[[ ! "${EXISTING_TARGET}" =~ ^[0-9a-f]{40}$ ]]', self.workflow)
        self.assertIn('gh api --method PATCH "/repos/${GITHUB_REPOSITORY}/git/refs/tags/${RELEASE_TAG}"', self.workflow)
        self.assertIn('-f sha="${GITHUB_SHA}" -F force=true', self.workflow)
        self.assertIn('test "$(jq -er \'.object.sha\' "${UPDATED_TAG}")" = "${GITHUB_SHA}"', self.workflow)
        self.assertIn('gh release edit "${RELEASE_TAG}"', self.workflow)
        self.assertIn('--target "${GITHUB_SHA}"', self.workflow)
        self.assertIn('gh release upload "${RELEASE_TAG}"', self.workflow)
        self.assertIn("--clobber", self.workflow)

    def test_prerelease_fixed_tag_migrates_legacy_main_target(self):
        update = self.workflow.rsplit('test "${CHANNEL}" = prerelease', 1)[1].split("exit 0", 1)[0]
        self.assertIn('"${EXISTING_TARGET}" != "main"', update)
        self.assertIn('"${EXISTING_TARGET}" != "${TAG_COMMIT}"', update)
        self.assertLess(update.index('TAG_COMMIT="$(jq -er'), update.index('"${EXISTING_TARGET}" != "main"'))
        self.assertLess(update.index('-F force=true'), update.index('gh release edit "${RELEASE_TAG}"'))
        self.assertIn('--target "${GITHUB_SHA}"', update)

    def test_prerelease_create_targets_current_commit(self):
        self.assertIn('Refusing to create prerelease: tag %s already exists without a matching release', self.workflow)
        self.assertIn('gh release create "${RELEASE_TAG}"', self.workflow)
        self.assertRegex(self.workflow, r'--prerelease \\\n\s+--target "\$\{GITHUB_SHA\}"')

    def test_stable_release_remains_fail_closed(self):
        self.assertIn('TAG_COMMIT="$(git rev-list -n 1 "${RELEASE_TAG}" 2>/dev/null || true)"', self.workflow)
        self.assertIn('if [[ "${TAG_COMMIT}" != "${GITHUB_SHA}" ]]', self.workflow)
        self.assertIn("Refusing to overwrite existing stable GitHub Release", self.workflow)
        self.assertIn("Could not prove stable GitHub Release tag %s is unused; failing closed", self.workflow)
        self.assertIn("--verify-tag", self.workflow)

    def test_release_lookup_fails_closed(self):
        self.assertIn(
            'gh api "/repos/${GITHUB_REPOSITORY}/releases/tags/${RELEASE_TAG}"',
            self.workflow,
        )
        self.assertIn("Could not determine prerelease state for tag %s; failing closed", self.workflow)

    def test_clobber_is_scoped_to_current_named_assets(self):
        upload = self.workflow.split('gh release upload "${RELEASE_TAG}"', 1)[1].split("exit 0", 1)[0]
        for asset in (
            '"${APP_APK}"',
            '"${APP_APK}.sha256"',
            '"${TEST_APK}"',
            '"${TEST_APK}.sha256"',
            "candidate/android-update.json",
            '"release-evidence-${VERSION}.tar.gz"',
        ):
            self.assertIn(asset, upload)
        self.assertIn("--clobber", upload)
        self.assertNotIn("gh release delete-asset", self.workflow)

    def test_all_actions_artifacts_include_attempt_and_download_via_outputs(self):
        self.assertIn("RUN_ATTEMPT: ${{ github.run_attempt }}", self.workflow)
        for variable in (
            "CANDIDATE_ARTIFACT_NAME",
            "KOTLIN_REPORTS_ARTIFACT_NAME",
            "STABLE_DEVICE_EVIDENCE_ARTIFACT_NAME",
        ):
            assignment = re.search(rf'^\s*{variable}="([^"]+)"$', self.workflow, re.MULTILINE)
            self.assertIsNotNone(assignment, variable)
            self.assertIn("${RUN_ATTEMPT}", assignment.group(1), variable)

        self.assertIn("kotlin_reports_artifact_name: ${{ steps.version.outputs.kotlin_reports_artifact_name }}", self.workflow)
        self.assertIn("name: ${{ steps.version.outputs.candidate_artifact_name }}", self.workflow)
        self.assertIn("evidence_artifact_name: ${{ needs.verify-build.outputs.stable_device_evidence_artifact_name }}", self.workflow)
        self.assertIn("name: ${{ needs.stable-device.outputs.evidence_artifact_name }}", self.workflow)

    def test_downstream_jobs_construct_candidate_apk_paths_without_name_outputs(self):
        self.assertNotIn("app_apk_name: ${{ steps.version.outputs.app_apk_name }}", self.workflow)
        self.assertNotIn("test_apk_name: ${{ steps.version.outputs.test_apk_name }}", self.workflow)
        self.assertNotIn("needs.verify-build.outputs.app_apk_name", self.workflow)
        self.assertNotIn("needs.verify-build.outputs.test_apk_name", self.workflow)

        stable_job = self.workflow.split("  stable-device:", 1)[1].split("  release:", 1)[0]
        release_job = self.workflow.split("  release:", 1)[1]
        app_assignment = 'APP_APK_NAME="daidai-panel-native-${VERSION}-${CHANNEL}-arm64.apk"'
        test_assignment = 'TEST_APK_NAME="daidai-panel-native-${VERSION}-${CHANNEL}-arm64-androidTest.apk"'
        for job in (stable_job, release_job):
            self.assertIn("VERSION: ${{ needs.verify-build.outputs.version }}", job)
            self.assertIn("CHANNEL: ${{ needs.verify-build.outputs.release_channel }}", job)
            self.assertIn(app_assignment, job)
            self.assertIn(test_assignment, job)
            for channel in ("stable", "prerelease"):
                app_name = app_assignment.split('"', 1)[1].rsplit('"', 1)[0]
                test_name = test_assignment.split('"', 1)[1].rsplit('"', 1)[0]
                app_path = "candidate/" + app_name.replace("${VERSION}", self.version).replace("${CHANNEL}", channel)
                test_path = "candidate/" + test_name.replace("${VERSION}", self.version).replace("${CHANNEL}", channel)
                self.assertEqual(app_path, f"candidate/daidai-panel-native-{self.version}-{channel}-arm64.apk")
                self.assertEqual(test_path, f"candidate/daidai-panel-native-{self.version}-{channel}-arm64-androidTest.apk")

    def test_device_evidence_remains_bound_to_candidate_apk_digests(self):
        self.assertGreaterEqual(self.workflow.count("--app-apk \"${APP_APK}\""), 2)
        self.assertGreaterEqual(self.workflow.count("--test-apk \"${TEST_APK}\""), 2)
        self.assertIn('EXPECTED_APK_SHA="$(cut -d \' \' -f1 "${APP_APK}.sha256")"', self.workflow)
        self.assertIn('test "$(sha256sum "${APP_APK}" | cut -d \' \' -f1)" = "${EXPECTED_APK_SHA}"', self.workflow)

    def test_stable_runtime_contract_receives_strict_scope(self):
        stable_job = self.workflow.split("  stable-device:", 1)[1].split("  release:", 1)[0]
        self.assertIn("--strict \\", stable_job)
        self.assertIn('--strict-runtime-ids="$(jq -er', stable_job)
        self.assertIn('--smoke-evidence "evidence/${MATRIX_ID}.json"', stable_job)
        self.assertIn('--apk "${APP_APK}"', stable_job)
        self.assertNotIn("--native-lib-dir", stable_job)

    def test_toolchain_and_evidence_scope_are_explicit(self):
        self.assertIn("NODE_VERSION: '20.19.x'", self.workflow)
        self.assertIn("NPM_VERSION: '10.8.2'", self.workflow)
        self.assertIn(".status_scope", self.workflow)
        self.assertIn(".pending_evidence.long_running_device_samples.status", self.workflow)
        self.assertIn("required-gates-only", self.workflow)

    def test_evidence_and_release_retain_test_apk_and_audit_metadata(self):
        self.assertIn("release/apk-metadata/**", self.workflow)
        self.assertIn('cp "${APP_APK}" "${APP_APK}.sha256" "${TEST_APK}" "${TEST_APK}.sha256" \\', self.workflow)
        self.assertIn('"candidate/${X64_TEST_APK_NAME}" "candidate/${X64_TEST_APK_NAME}.sha256" release/artifacts/', self.workflow)
        self.assertGreaterEqual(self.workflow.count('"${TEST_APK}.sha256"'), 2)

    def test_gradle_web_jobs_pin_node_and_npm_contract(self):
        workflow_paths = (
            ROOT / ".github/workflows/android-release.yml",
            ROOT / ".github/workflows/android-device-smoke.yml",
            ROOT / ".github/workflows/ci.yml",
            ROOT / "app/.github/workflows/release.yml",
            ROOT / "app/.github/workflows/build.yml",
            ROOT / "app/.github/workflows/android-build.yml",
            ROOT / "app/.github/workflows/mobile-core-source-check.yml",
        )
        for path in workflow_paths:
            workflow = path.read_text(encoding="utf-8")
            self.assertIn("NODE_VERSION: '20.19.x'", workflow, path)
            self.assertIn("NPM_VERSION: '10.8.2'", workflow, path)
            self.assertIn("npm install -g", workflow, path)

    def test_release_pushes_have_path_filters(self):
        push_section = self.workflow.split("  workflow_dispatch:", 1)[0]
        self.assertIn("    paths:", push_section)
        self.assertIn("      - 'scripts/**'", push_section)

    def test_default_root_workflows_do_not_prepare_optional_python_launcher(self):
        workflow_paths = (
            ROOT / ".github/workflows/android-release.yml",
            ROOT / ".github/workflows/android-device-smoke.yml",
            ROOT / ".github/workflows/ci.yml",
        )
        for path in workflow_paths:
            workflow = path.read_text(encoding="utf-8")
            self.assertNotIn("bash scripts/prepare-android-python-runtime.sh", workflow, path)

    def test_default_android_packaging_excludes_optional_python_launcher(self):
        self.assertIn('excludes += setOf("**/libpython_exec.so")', self.gradle)
        prebuild = self.gradle.split('tasks.named("preBuild")', 1)[1].split("}", 1)[0]
        self.assertIn("dependsOn(verifyLinuxRootfsRuntime)", prebuild)
        self.assertNotIn("prepare-android-python-runtime", prebuild)


if __name__ == "__main__":
    unittest.main()
