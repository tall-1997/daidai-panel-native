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


if __name__ == "__main__":
    unittest.main()
