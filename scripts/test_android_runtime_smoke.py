import importlib.util
import base64
import hashlib
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
    def instrumentation_output(self, payload, order=None):
        raw = json.dumps(payload).encode()
        encoded = base64.b64encode(raw).decode()
        chunks = [encoded[index:index + 7] for index in range(0, len(encoded), 7)]
        entries = [(f"{SMOKE.INSTRUMENTATION_EVIDENCE_PREFIX}.chunk_{index:04d}", chunk) for index, chunk in enumerate(chunks)]
        if order is not None:
            entries = [entries[index] for index in order]
        entries.extend([
            (f"{SMOKE.INSTRUMENTATION_EVIDENCE_PREFIX}.encoding", "base64"),
            (f"{SMOKE.INSTRUMENTATION_EVIDENCE_PREFIX}.sha256", hashlib.sha256(raw).hexdigest()),
            (f"{SMOKE.INSTRUMENTATION_EVIDENCE_PREFIX}.chunk_count", str(len(chunks))),
        ])
        return "\n".join(f"INSTRUMENTATION_RESULT: {key}={value}" for key, value in entries)

    def test_instrumentation_result_parser_accepts_multiple_out_of_order_chunks(self):
        payload = {"schema_version": 2, "status": "pass", "steps": ["release"]}
        normal = self.instrumentation_output(payload)
        chunk_count = sum(".chunk_" in line and ".chunk_count" not in line for line in normal.splitlines())
        output = self.instrumentation_output(payload, list(reversed(range(chunk_count))))
        self.assertEqual(payload, SMOKE.parse_instrumentation_evidence(output))

    def test_instrumentation_result_parser_rejects_missing_chunk(self):
        output = self.instrumentation_output({"schema_version": 2, "status": "pass"})
        lines = output.splitlines()
        output = "\n".join(line for line in lines if ".chunk_0001=" not in line)
        with self.assertRaisesRegex(ValueError, "chunks are missing"):
            SMOKE.parse_instrumentation_evidence(output)

    def test_instrumentation_result_parser_rejects_invalid_base64(self):
        output = self.instrumentation_output({"schema_version": 2})
        output = output.replace(".chunk_0000=", ".chunk_0000=!")
        with self.assertRaisesRegex(ValueError, "base64 is invalid"):
            SMOKE.parse_instrumentation_evidence(output)

    def test_instrumentation_result_parser_accepts_release_stdout(self):
        payload = {"schema_version": 2, "status": "pass", "failure": None, "steps": []}
        output = "INSTRUMENTATION_RESULT: stream=\n" + self.instrumentation_output(payload) + "\nOK (1 test)\n"
        self.assertEqual(payload, SMOKE.parse_instrumentation_evidence(output))

    def valid_instrumentation(self):
        steps = {step_id: {"id": step_id, "status": "pass", "evidence": {}} for step_id in SMOKE.REQUIRED_STEPS}
        steps["auth.initialize_and_login"]["evidence"] = {"admin_authenticated": True}
        steps["env.create"]["evidence"] = {"created": True}
        steps["env.read_update"]["evidence"] = {"updated": True}
        steps["runtime.dns.managed_resolver"]["evidence"] = {
            "source": "active_network", "servers": ["1.1.1.1"], "write_success": True,
            "updated_at": "2026-08-28T00:00:00Z", "error": "",
        }
        steps["runtime.dns.real_resolution"]["evidence"] = {
            "all_resolved": True,
            "resolutions": {
                mirror: {
                    "host": host,
                    "python": {"command": f"python3 socket.getaddrinfo {host}", "output": json.dumps({"host": host, "addresses": ["192.0.2.1"]}), "exit_code": 0},
                    "node": {"command": f"node dns.lookup {host}", "output": json.dumps({"host": host, "addresses": ["2001:db8::1"]}), "exit_code": 0},
                }
                for mirror, host in {"pip": "mirrors.aliyun.com", "npm": "registry.npmmirror.com"}.items()
            },
        }
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
            "previous_instance_id": "kotlin-local-fallback",
            "core": {"phase": "ready", "instance_id": "kotlin-local-fallback", "core_version": "kotlin-local-fallback", "fallback_mode": "full", "scheduler_host_state": "active", "scheduler_guarantee_state": "active"},
        }
        steps["persistence.after_restart"]["evidence"] = {
            "admin_persisted": True,
            "env_persisted": True,
            "env_deleted": True,
        }
        version_outputs = {
            "python-3.12-android-arm64": "Python 3.12.3",
            "node-lts-android-arm64": "v18.19.1",
            "shell-android-arm64": "5.2.21(1)-release",
        }
        for runtime_id, requirement in SMOKE.RUNTIME_EVIDENCE.items():
            steps[requirement["step_id"]]["evidence"] = {
                "command": requirement["command"],
                "output": version_outputs[runtime_id],
                "exit_code": 0,
            }
        return {
            "schema_version": 2,
            "status": "pass",
            "core": {"phase": "ready", "instance_id": "kotlin-local-fallback", "core_version": "kotlin-local-fallback", "fallback_mode": "full", "scheduler_host_state": "active", "scheduler_guarantee_state": "active"},
            "steps": list(steps.values()),
        }

    def test_blocked_evidence_matches_runtime_contract_shape(self):
        evidence = SMOKE.blocked_evidence("api35-16k", "runner-unavailable")
        self.assertEqual(SMOKE.MATRIX, evidence["matrix"])
        self.assertEqual(8, len(evidence["records"]))
        self.assertTrue(all(record["status"] == "blocked" for record in evidence["records"]))
        self.assertTrue(all(record["checks"][0]["reason"].endswith("matrix=api35-16k") for record in evidence["records"]))

    def test_artifact_evidence_records_sha256_and_size(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = pathlib.Path(directory) / "candidate.apk"
            artifact.write_bytes(b"signed candidate")
            evidence = SMOKE.artifact_evidence(artifact)
        self.assertEqual("candidate.apk", evidence["name"])
        self.assertEqual(16, evidence["size"])
        self.assertEqual("927241837385932044faaeec13b4d1fbb71da0bf003cec0464716528001497e2", evidence["sha256"])

    def test_dry_run_records_both_apk_digests(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app_apk = root / "app.apk"
            test_apk = root / "test.apk"
            app_apk.write_bytes(b"app")
            test_apk.write_bytes(b"test")
            args = type("Args", (), {
                "apk": app_apk, "test_apk": test_apk, "matrix_id": "api35-16k",
                "expected_api": 35, "expected_page_size": 16384, "expected_abi": "arm64-v8a",
            })()
            original = SMOKE.sys.stdout
            try:
                import io
                SMOKE.sys.stdout = io.StringIO()
                self.assertEqual(0, SMOKE.dry_run(args))
                plan = json.loads(SMOKE.sys.stdout.getvalue())
            finally:
                SMOKE.sys.stdout = original
        self.assertEqual(64, len(plan["artifacts"]["app_apk"]["sha256"]))
        self.assertEqual(64, len(plan["artifacts"]["test_apk"]["sha256"]))

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

    def test_failed_instrumentation_reports_failed_step(self):
        helper = {
            "schema_version": 2,
            "status": "failed",
            "steps": [
                {"id": "core.method_channel.ensure_started", "status": "pass"},
                {"id": "rootfs.npm_env_node", "status": "failed"},
            ],
        }
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("instrumentation-failed:rootfs.npm_env_node", reason)

    def test_stable_smoke_rejects_failed_or_stale_dns_evidence(self):
        helper = self.valid_instrumentation()
        by_id = {step["id"]: step for step in helper["steps"]}
        by_id["runtime.dns.managed_resolver"]["evidence"].update({"write_success": False, "error": "directory fsync failed"})
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("managed-dns-unverified", reason)

        helper = self.valid_instrumentation()
        by_id = {step["id"]: step for step in helper["steps"]}
        by_id["runtime.dns.real_resolution"]["evidence"]["resolutions"]["npm"]["node"]["exit_code"] = 1
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("npm-node-dns-unverified", reason)

    def test_smoke_cli_has_no_private_directory_run_as_fallback(self):
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertNotIn("run-as", source)
        parser = SMOKE.parser()
        option_strings = {
            option
            for action in parser._actions
            for option in action.option_strings
        }
        self.assertNotIn("--allow-debug-run-as-fallback", option_strings)

    def test_kotlin_fallback_is_the_accepted_android_backend(self):
        helper = self.valid_instrumentation()
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertTrue(valid, reason)

    def test_system_compensation_is_an_accepted_scheduler_state(self):
        helper = self.valid_instrumentation()
        helper["core"].update({
            "scheduler_host_state": "system_compensation",
            "scheduler_guarantee_state": "system_compensation",
        })
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertTrue(valid, reason)

    def test_wrong_fallback_identity_cannot_validate(self):
        helper = self.valid_instrumentation()
        helper["core"]["core_version"] = "unknown"
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("core-identity-unverified", reason)

    def test_runtime_version_must_come_from_matching_helper_evidence(self):
        helper = self.valid_instrumentation()
        by_id = {step["id"]: step for step in helper["steps"]}
        by_id["runtime.python.version"]["evidence"]["output"] = "Python 3.11.9"
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertIn("version-mismatch", reason)

    def test_runtime_version_rejects_hardcoded_pass_without_command_evidence(self):
        helper = self.valid_instrumentation()
        by_id = {step["id"]: step for step in helper["steps"]}
        del by_id["runtime.node.version"]["evidence"]["output"]
        valid, reason = SMOKE.validate_instrumentation(helper)
        self.assertFalse(valid)
        self.assertEqual("node-lts-android-arm64-version-evidence-invalid", reason)

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
