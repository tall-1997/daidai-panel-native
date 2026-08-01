import importlib.util
import json
import pathlib
import subprocess
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("android-runtime-smoke.py")
SPEC = importlib.util.spec_from_file_location("android_runtime_smoke", SCRIPT)
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class AndroidRuntimeSmokeTest(unittest.TestCase):
    def valid_instrumentation(self):
        steps = {step_id: {"id": step_id, "status": "pass", "evidence": {}} for step_id in SMOKE.REQUIRED_STEPS}
        steps["auth.initialize_and_login"]["evidence"] = {"admin_authenticated": True}
        steps["env.create"]["evidence"] = {"created": True}
        steps["env.read_update"]["evidence"] = {"updated": True}
        for runtime in ("shell", "python", "node"):
            steps[f"task.{runtime}.wait_terminal_and_read_log"]["evidence"] = {
                "operation": {"kind": "task", "state": "success", "ended_at": "2026-07-30T00:00:00Z"},
                "terminal_status": 0,
                "marker_found": True,
                "log_id": 1,
                "content_sha256": "a" * 64,
            }
        for dependency in ("python_wheel", "node_tarball"):
            steps[f"dependency.{dependency}.install"]["evidence"] = {
                "terminal_status": "installed",
                "operation": {"state": "success"},
            }
            steps[f"dependency.{dependency}.uninstall"]["evidence"] = {
                "dependency_missing": True,
                "operation": {"state": "success"},
            }
        steps["core.method_channel.restart"]["evidence"] = {
            "previous_instance_id": "1",
            "core": {"phase": "ready", "instance_id": "2"},
        }
        steps["persistence.after_restart"]["evidence"] = {
            "admin_persisted": True,
            "env_persisted": True,
            "env_deleted": True,
        }
        return {
            "schema_version": 2,
            "status": "pass",
            "core": {"phase": "ready", "instance_id": "2", "core_version": "gomobile", "core_status": "ready"},
            "steps": list(steps.values()),
        }

    def test_blocked_evidence_matches_runtime_contract_shape(self):
        evidence = SMOKE.blocked_evidence("api35-16k", "runner-unavailable")
        self.assertEqual(SMOKE.MATRIX, evidence["matrix"])
        self.assertEqual(8, len(evidence["records"]))
        self.assertTrue(all(record["status"] == "blocked" for record in evidence["records"]))
        self.assertTrue(all(record["checks"][0]["reason"].endswith("matrix=api35-16k") for record in evidence["records"]))

    def test_external_pass_claims_are_downgraded_to_blocked(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / "external.json"
            output = root / "evidence.json"
            source.write_text(json.dumps({"status": "pass"}), encoding="utf-8")
            args = type("Args", (), {"input": source, "output": output, "matrix_id": "physical-api35-16k"})()
            self.assertEqual(0, SMOKE.external(args))
            evidence = json.loads(output.read_text(encoding="utf-8"))
            self.assertTrue(all(record["status"] == "blocked" for record in evidence["records"]))
            self.assertTrue(all(record["evidence_source"] == "external-unverified" for record in evidence["records"]))

    def test_adb_timeout_is_reported_as_runtime_error(self):
        original = SMOKE.subprocess.run
        SMOKE.subprocess.run = lambda *args, **kwargs: (_ for _ in ()).throw(subprocess.TimeoutExpired(args[0], 1))
        try:
            with self.assertRaisesRegex(RuntimeError, "command timed out"):
                SMOKE.command("adb", "", "get-state", timeout=1)
        finally:
            SMOKE.subprocess.run = original

    def test_instrumentation_requires_all_successful_e2e_steps(self):
        helper = self.valid_instrumentation()
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertTrue(valid, reason)

        helper["steps"][0]["status"] = "failed"
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertIn("failed-steps", reason)

    def test_fallback_core_cannot_validate(self):
        helper = self.valid_instrumentation()
        helper["core"] = {"phase": "ready", "instance_id": "kotlin-fallback", "core_version": "fallback"}
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("fallback-core", reason)

    def test_non_gomobile_core_identity_cannot_validate(self):
        helper = self.valid_instrumentation()
        helper["core"]["core_version"] = "unknown"
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("core-identity-unverified", reason)

    def test_runtime_pass_requires_operation_and_log_detail(self):
        helper = self.valid_instrumentation()
        by_id = {step["id"]: step for step in helper["steps"]}
        by_id["task.python.wait_terminal_and_read_log"]["evidence"]["operation"] = {"state": "success"}
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("python-execution-evidence-invalid", reason)

    def test_diagnostics_preserve_failed_commands(self):
        original = SMOKE.safe_command
        SMOKE.safe_command = lambda *args, **kwargs: {"return_code": 1, "stderr": "offline"}
        try:
            diagnostics = SMOKE.collect_diagnostics("adb", "serial", "api28-4k", "emulator-start-failed")
        finally:
            SMOKE.safe_command = original
        self.assertEqual("failed", diagnostics["status"])
        self.assertEqual("emulator-start-failed", diagnostics["reason"])
        self.assertTrue(all(item["return_code"] == 1 for item in diagnostics["commands"]))

    def test_evidence_collection_always_writes_diagnostics_and_logcat(self):
        original = SMOKE.collect_diagnostics
        SMOKE.collect_diagnostics = lambda *args, **kwargs: {
            "status": "captured",
            "commands": [{}, {}, {}, {}, {"stdout": "device-log", "stderr": "log-error"}],
        }
        try:
            with tempfile.TemporaryDirectory() as directory:
                output = pathlib.Path(directory) / "api35-4k.json"
                SMOKE.collect_evidence_files("adb", "serial", output, "api35-4k", "post-instrumentation")
                self.assertTrue(output.with_suffix(".diagnostics.json").is_file())
                self.assertEqual("device-log\n[stderr]\nlog-error", output.with_suffix(".logcat.txt").read_text())
        finally:
            SMOKE.collect_diagnostics = original

    def test_diagnose_uses_matrix_logcat_filename(self):
        original = SMOKE.collect_diagnostics
        SMOKE.collect_diagnostics = lambda *args, **kwargs: {
            "status": "failed",
            "commands": [{}, {}, {}, {}, {"stdout": "boot-log"}],
        }
        try:
            with tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                args = type("Args", (), {
                    "adb": "adb",
                    "serial": "",
                    "matrix_id": "api28-4k",
                    "reason": "boot-failed",
                    "output": root / "api28-4k.diagnostics.json",
                })()
                self.assertEqual(0, SMOKE.diagnose(args))
                self.assertEqual("boot-log", (root / "api28-4k.logcat.txt").read_text())
        finally:
            SMOKE.collect_diagnostics = original


    def test_pre_diagnose_writes_baseline_json(self):
        original = SMOKE.safe_command
        SMOKE.safe_command = lambda *args, **kwargs: {"return_code": 0, "stdout": "ok", "stderr": ""}
        try:
            with tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                args = type("Args", (), {
                    "adb": "adb",
                    "serial": "",
                    "matrix_id": "api35-x64",
                    "output": root / "pre-diag.json",
                })()
                self.assertEqual(0, SMOKE.pre_diagnose(args))
                diag = json.loads(args.output.read_text())
                self.assertEqual("api35-x64", diag["matrix_id"])
                self.assertEqual(8, len(diag["commands"]))
        finally:
            SMOKE.safe_command = original


if __name__ == "__main__":
    unittest.main()
