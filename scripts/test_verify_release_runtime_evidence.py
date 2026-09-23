import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("verify-release-runtime-evidence.py")
SPEC = importlib.util.spec_from_file_location("verify_release_runtime_evidence", SCRIPT)
VERIFIER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFIER)


class ReleaseRuntimeEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.contract = {
            "schema_version": 1,
            "device_smoke": {"matrix": [{"id": "api35-16k", "api": 35, "page_size_bytes": 16384, "abi": "arm64-v8a"}]},
            "runtime_ids": ["python", "node", "shell"],
            "stable_required_runtime_ids": ["python", "node", "shell"],
            "runtime_entries": {
                "python": {"entry_type": "rootfs_command", "entrypoint": "python3"},
                "node": {"entry_type": "rootfs_command", "entrypoint": "node"},
                "shell": {"entry_type": "rootfs_command", "entrypoint": "bash"},
            },
            "runtime_evidence": {
                "python": {"step_id": "runtime.python.version", "command": "python3 --version", "expected_version": "3.12.3", "output_pattern": r"^Python ([0-9]+\.[0-9]+\.[0-9]+)$"},
                "node": {"step_id": "runtime.node.version", "command": "node --version", "expected_version": "18.19.1", "output_pattern": r"^v([0-9]+\.[0-9]+\.[0-9]+)$"},
                "shell": {"step_id": "runtime.shell.version", "command": "bash --version", "expected_version": "5.2.21", "output_pattern": r"^([0-9]+\.[0-9]+\.[0-9]+)\([0-9]+\)-release$"},
            },
            "release_gate_scope": {"required": ["release", "device"], "optional": ["long-running"]},
        }
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        self.app_apk = self.root / "candidate.apk"
        self.test_apk = self.root / "candidate-androidTest.apk"
        self.app_apk.write_bytes(b"signed-app")
        self.test_apk.write_bytes(b"test-app")

    def artifact_digests(self):
        return {
            "app_apk": {"name": self.app_apk.name, "size": self.app_apk.stat().st_size, "sha256": VERIFIER.sha256(self.app_apk)},
            "test_apk": {"name": self.test_apk.name, "size": self.test_apk.stat().st_size, "sha256": VERIFIER.sha256(self.test_apk)},
        }

    def tearDown(self):
        self.temp_dir.cleanup()

    def write_evidence(self, device_status="verified", runtime_status="pass", include_device=True, statuses=None):
        device = {
            "matrix_id": "api35-16k",
            "status": device_status,
            "device": {"api": 35, "page_size_bytes": 16384, "abi": "arm64-v8a"},
            "artifacts": self.artifact_digests(),
            "runtime": {
                "core": {"phase": "ready", "instance_id": "kotlin-local-fallback", "core_version": "kotlin-local-fallback", "fallback_mode": "full", "scheduler_host_state": "active", "scheduler_guarantee_state": "active"},
                "steps": [
                    {"id": "runtime.python.version", "evidence": {"command": "python3 --version", "output": "Python 3.12.3", "exit_code": 0}},
                    {"id": "runtime.node.version", "evidence": {"command": "node --version", "output": "v18.19.1", "exit_code": 0}},
                    {"id": "runtime.shell.version", "evidence": {"command": "bash --version", "output": "5.2.21(1)-release", "exit_code": 0}},
                ],
            },
        }
        if include_device:
            (self.root / "api35-16k.device.json").write_text(json.dumps(device))
        runtime = {
            "matrix": ["api35-16k"],
            "artifacts": self.artifact_digests(),
            "records": [
                {
                    "runtime_id": item,
                    "entry": {"python": "python3", "node": "node", "shell": "bash"}[item],
                    "version": {"python": "3.12.3", "node": "18.19.1", "shell": "5.2.21"}[item],
                    "status": (statuses or {}).get(item, runtime_status),
                    "evidence_source": "android-device" if (statuses or {}).get(item, runtime_status) == "pass" else "none",
                    "checks": ([{"id": "check", "status": "pass", "output": ({"python": "command=python3 --version;actual=3.12.3;raw=Python 3.12.3;proof", "node": "command=node --version;actual=18.19.1;raw=v18.19.1;proof", "shell": "command=bash --version;actual=5.2.21;raw=5.2.21(1)-release;proof"}[item])}] if (statuses or {}).get(item, runtime_status) == "pass"
                               else [{"id": "check", "status": "blocked", "reason": "runner-unavailable"}]),
                }
                for item in ("python", "node", "shell")
            ],
        }
        (self.root / "api35-16k.json").write_text(json.dumps(runtime))

    def test_stable_accepts_exact_verified_scope(self):
        self.write_evidence()
        self.assertEqual([], VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk))

    def test_stable_rejects_blocked_required_runtime(self):
        self.write_evidence(statuses={"node": "blocked"})
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("stable required runtime node must pass" in error for error in errors))

    def test_stable_rejects_unverified_device_with_required_runtime_passes(self):
        self.write_evidence(device_status="blocked")
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("status must be verified" in error for error in errors))

    def test_stable_rejects_missing_artifact(self):
        self.assertTrue(VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk))

    def test_stable_rejects_app_apk_digest_mismatch(self):
        self.write_evidence()
        self.app_apk.write_bytes(b"different release APK")
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("app_apk SHA-256 does not match" in error for error in errors))

    def test_stable_rejects_test_apk_digest_mismatch(self):
        self.write_evidence()
        self.test_apk.write_bytes(b"different test APK")
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("test_apk SHA-256 does not match" in error for error in errors))

    def test_stable_rejects_tampered_helper_runtime_version(self):
        self.write_evidence()
        path = self.root / "api35-16k.device.json"
        payload = json.loads(path.read_text())
        payload["runtime"]["steps"][0]["evidence"]["output"] = "Python 3.11.9"
        path.write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("helper version does not match" in error for error in errors))

    def test_stable_rejects_record_version_drift(self):
        self.write_evidence()
        path = self.root / "api35-16k.json"
        payload = json.loads(path.read_text())
        payload["records"][0]["version"] = "3.11.9"
        path.write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("record version does not match" in error for error in errors))

    def test_stable_rejects_tampered_fallback_identity(self):
        self.write_evidence()
        path = self.root / "api35-16k.device.json"
        payload = json.loads(path.read_text())
        payload["runtime"]["core"]["core_version"] = "gomobile"
        path.write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("fallback core identity" in error for error in errors))

    def test_stable_requires_candidate_apks(self):
        self.write_evidence()
        errors = VERIFIER.validate(self.contract, self.root, "stable")
        self.assertTrue(any("requires app and test APK" in error for error in errors))

    def test_prerelease_accepts_complete_blocked_evidence(self):
        self.write_evidence(runtime_status="blocked", include_device=False)
        self.assertEqual([], VERIFIER.validate(self.contract, self.root, "prerelease"))

    def test_rejects_duplicate_and_missing_canonical_runtime_ids(self):
        self.write_evidence(include_device=False)
        path = self.root / "api35-16k.json"
        payload = json.loads(path.read_text())
        payload["records"][2] = payload["records"][0]
        path.write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "prerelease")
        self.assertTrue(any("duplicate runtime ID" in error for error in errors))
        self.assertTrue(any("complete unique canonical set" in error for error in errors))

    def test_rejects_matrix_order_or_duplicates(self):
        self.contract["device_smoke"]["matrix"].append({"id": "api34-4k", "api": 34, "page_size_bytes": 4096, "abi": "arm64-v8a"})
        self.write_evidence(include_device=False)
        path = self.root / "api35-16k.json"
        payload = json.loads(path.read_text())
        payload["matrix"] = ["api34-4k", "api35-16k"]
        path.write_text(json.dumps(payload))
        (self.root / "api34-4k.json").write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "prerelease")
        self.assertTrue(any("matrix must exactly match" in error for error in errors))

    def test_rejects_malformed_evidence_json(self):
        (self.root / "api35-16k.json").write_text("{malformed")
        errors = VERIFIER.validate(self.contract, self.root, "prerelease")
        self.assertTrue(any("malformed evidence JSON" in error for error in errors))

    def test_rejects_invalid_blocked_and_pass_fields(self):
        self.write_evidence(runtime_status="blocked", include_device=False)
        path = self.root / "api35-16k.json"
        payload = json.loads(path.read_text())
        payload["records"][0]["evidence_source"] = "host"
        payload["records"][0]["checks"][0]["output"] = "fake"
        payload["records"][1]["status"] = "pass"
        payload["records"][1]["checks"] = [{"id": "check", "status": "pass", "reason": "wrong"}]
        path.write_text(json.dumps(payload))
        errors = VERIFIER.validate(self.contract, self.root, "prerelease")
        self.assertTrue(any("blocked evidence source is invalid" in error for error in errors))
        self.assertTrue(any("blocked checks are incomplete" in error for error in errors))
        self.assertTrue(any("pass checks are incomplete" in error for error in errors))

    def test_rejects_invalid_stable_required_runtime_ids_without_crashing(self):
        self.contract["stable_required_runtime_ids"] = None
        self.write_evidence()
        errors = VERIFIER.validate(self.contract, self.root, "stable", self.app_apk, self.test_apk)
        self.assertTrue(any("stable_required_runtime_ids" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
