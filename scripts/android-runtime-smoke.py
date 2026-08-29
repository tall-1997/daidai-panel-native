#!/usr/bin/env python3
"""Install and exercise the Android ARM64 runtime smoke gate on a connected device."""

import argparse
import base64
import binascii
import datetime
import hashlib
import json
import pathlib
import re
import subprocess
import sys


PACKAGE = "com.daidai.daidai_app"
RUNNER = f"{PACKAGE}.test/androidx.test.runner.AndroidJUnitRunner"
TEST_CLASS = f"{PACKAGE}.AndroidRuntimeSmokeTest"
CONTRACT_PATH = pathlib.Path(__file__).with_name("release-runtime-contract.json")
RELEASE_CONTRACT = json.loads(CONTRACT_PATH.read_text(encoding="utf-8"))
RUNTIME_MANIFEST = json.loads((CONTRACT_PATH.parent.parent / "runtime" / "manifest.json").read_text(encoding="utf-8"))
MATRIX = [item["id"] for item in RELEASE_CONTRACT["device_smoke"]["matrix"]]
RUNTIME_EVIDENCE = RELEASE_CONTRACT["runtime_evidence"]
INSTRUMENTATION_EVIDENCE_PREFIX = "daidai.runtime_smoke.evidence"
RUNTIME_METADATA = {
    "python-3.12-android-arm64": (10, "PY_OK_SSL_SQLITE_VENV_PIP"),
    "node-lts-android-arm64": (10, "COMMONJS_ESM_HTTPS"),
    "typescript-stable": (10, "TS_OK"),
    "shell-android-arm64": (10, "SHELL_PIPE_EXIT_STOP"),
    "git-android-arm64": (30, "GIT_CLONE_FETCH_SPARSE"),
    "ssh-android-arm64": (30, "SSH_HOSTKEY"),
    "yaegi-go": (10, "GO_INTERPRET_OK"),
    "go-builder-android-arm64": (60, "GO_BUILD_EXPORT_ONLY"),
}


def runtime_contract_records():
    components = {item["id"]: item for item in RUNTIME_MANIFEST["components"]}
    if set(components) != set(RELEASE_CONTRACT["runtime_ids"]) or set(components) != set(RUNTIME_METADATA):
        raise ValueError("runtime manifest IDs differ from the release contract")
    records = []
    for runtime_id in RELEASE_CONTRACT["runtime_ids"]:
        component = components[runtime_id]
        entry = RELEASE_CONTRACT["runtime_entries"].get(runtime_id)
        if entry != {"entry_type": component["entry_type"], "entrypoint": component["entrypoint"]}:
            raise ValueError(f"runtime entry differs from release contract: {runtime_id}")
        requirement = RUNTIME_EVIDENCE.get(runtime_id, {})
        if requirement.get("expected_version") not in (None, component["version"]):
            raise ValueError(f"runtime version differs from release contract: {runtime_id}")
        timeout, check_id = RUNTIME_METADATA[runtime_id]
        records.append((runtime_id, component["version"], component["entrypoint"], component["isolation"], timeout, check_id))
    return records


RUNTIMES = runtime_contract_records()

REQUIRED_STEPS = {
    "core.method_channel.ensure_started",
    "auth.initialize_and_login",
    "rootfs.node_file_read_diagnostics",
    "env.create",
    "env.read_update",
    "runtime.dns.managed_resolver",
    "runtime.dns.real_resolution",
    "task.shell.wait_terminal_and_read_log",
    "task.python.wait_terminal_and_read_log",
    "task.node.wait_terminal_and_read_log",
    "tasks.shell_python_node",
    "dependency.python_wheel.install",
    "dependency.python_wheel.uninstall",
    "dependency.node_tarball.install",
    "dependency.node_tarball.uninstall",
    "core.method_channel.restart",
    "persistence.after_restart",
    *(requirement["step_id"] for requirement in RUNTIME_EVIDENCE.values()),
}


def command(adb, serial, *args, timeout=180, check=True):
    invocation = [adb]
    if serial:
        invocation += ["-s", serial]
    invocation += list(args)
    try:
        completed = subprocess.run(invocation, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        raise RuntimeError(f"command timed out after {timeout}s: {' '.join(invocation)}") from error
    if check and completed.returncode:
        raise RuntimeError(f"command failed ({completed.returncode}): {' '.join(invocation)}\n{completed.stdout}\n{completed.stderr}")
    return completed


def device_value(adb, serial, *args):
    return command(adb, serial, "shell", *args).stdout.strip().replace("\r", "")


def write_json(path, payload):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def write_text(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value, encoding="utf-8")


def artifact_evidence(path):
    digest = hashlib.sha256()
    with path.open("rb") as artifact:
        for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
            digest.update(chunk)
    return {"name": path.name, "size": path.stat().st_size, "sha256": digest.hexdigest()}


def safe_command(adb, serial, *args, timeout=30):
    try:
        result = command(adb, serial, *args, timeout=timeout, check=False)
        return {"command": [adb, *args], "return_code": result.returncode, "stdout": result.stdout[-20000:], "stderr": result.stderr[-10000:]}
    except (OSError, RuntimeError) as error:
        return {"command": [adb, *args], "error": str(error)}


def collect_diagnostics(adb, serial, matrix_id, reason, status="failed"):
    return {
        "schema_version": 1,
        "status": status,
        "matrix_id": matrix_id,
        "reason": reason,
        "captured_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "commands": [
            safe_command(adb, "", "devices", "-l"),
            safe_command(adb, serial, "get-state"),
            safe_command(adb, serial, "shell", "getprop"),
            safe_command(adb, serial, "shell", "dumpsys", "activity", "processes"),
            safe_command(adb, serial, "logcat", "-d", "-v", "threadtime", timeout=60),
        ],
    }


def collect_evidence_files(adb, serial, output, matrix_id, reason, status="captured"):
    diagnostics = collect_diagnostics(adb, serial, matrix_id, reason, status)
    write_json(output.with_suffix(".diagnostics.json"), diagnostics)
    logcat = diagnostics["commands"][-1]
    content = logcat.get("stdout", "")
    if logcat.get("stderr"):
        content += "\n[stderr]\n" + logcat["stderr"]
    if logcat.get("error"):
        content += "\n[collection-error]\n" + logcat["error"]
    write_text(output.with_suffix(".logcat.txt"), content)
    return diagnostics


def parse_instrumentation_evidence(output):
    values = {}
    pattern = re.compile(r"^INSTRUMENTATION_(?:RESULT|STATUS):\s*([^=]+?)\s*=\s*(.*)$")
    for raw_line in output.replace("\r", "").splitlines():
        match = pattern.match(raw_line)
        if match and match.group(1).strip().startswith(INSTRUMENTATION_EVIDENCE_PREFIX + "."):
            values[match.group(1).strip()] = match.group(2).strip()

    count_key = f"{INSTRUMENTATION_EVIDENCE_PREFIX}.chunk_count"
    digest_key = f"{INSTRUMENTATION_EVIDENCE_PREFIX}.sha256"
    encoding_key = f"{INSTRUMENTATION_EVIDENCE_PREFIX}.encoding"
    if values.get(encoding_key) != "base64":
        raise ValueError("instrumentation evidence encoding is missing or invalid")
    try:
        chunk_count = int(values[count_key])
    except (KeyError, ValueError) as error:
        raise ValueError("instrumentation evidence chunk count is missing or invalid") from error
    if chunk_count <= 0:
        raise ValueError("instrumentation evidence chunk count is invalid")

    expected_keys = [f"{INSTRUMENTATION_EVIDENCE_PREFIX}.chunk_{index:04d}" for index in range(chunk_count)]
    missing = [key for key in expected_keys if key not in values]
    if missing:
        raise ValueError("instrumentation evidence chunks are missing: " + ",".join(missing))
    encoded = "".join(values[key] for key in expected_keys)
    try:
        payload = base64.b64decode(encoded, validate=True)
    except (binascii.Error, ValueError) as error:
        raise ValueError("instrumentation evidence base64 is invalid") from error
    actual_digest = hashlib.sha256(payload).hexdigest()
    if values.get(digest_key) != actual_digest:
        raise ValueError("instrumentation evidence sha256 mismatch")
    try:
        decoded = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("instrumentation evidence JSON is invalid") from error
    if not isinstance(decoded, dict):
        raise ValueError("instrumentation evidence JSON must be an object")
    return decoded


def validate_instrumentation(helper):
    steps = helper.get("steps")
    if helper.get("schema_version") != 2 or not isinstance(steps, list):
        return False, "invalid-instrumentation-envelope"
    by_id = {step.get("id"): step for step in steps if isinstance(step, dict)}
    if helper.get("status") != "pass":
        failed = sorted(step_id for step_id, step in by_id.items() if step.get("status") == "failed")
        return False, "instrumentation-failed:" + (",".join(failed) if failed else "unknown-step")
    missing = sorted(REQUIRED_STEPS - set(by_id))
    if missing:
        return False, "missing-steps:" + ",".join(missing)
    failed = sorted(step_id for step_id in REQUIRED_STEPS if by_id[step_id].get("status") != "pass")
    if failed:
        return False, "failed-steps:" + ",".join(failed)
    core = helper.get("core", {})
    core_error = validate_core_identity(core)
    if core_error:
        return False, core_error
    evidence = {step_id: by_id[step_id].get("evidence", {}) for step_id in REQUIRED_STEPS}
    if not evidence["auth.initialize_and_login"].get("admin_authenticated"):
        return False, "admin-auth-unverified"
    if not evidence["env.create"].get("created") or not evidence["env.read_update"].get("updated"):
        return False, "env-crud-unverified"
    managed_dns = evidence["runtime.dns.managed_resolver"]
    if (not managed_dns.get("write_success") or not managed_dns.get("updated_at")
            or managed_dns.get("error") or not managed_dns.get("servers")):
        return False, "managed-dns-unverified"
    real_dns = evidence["runtime.dns.real_resolution"]
    resolutions = real_dns.get("resolutions", {})
    if not real_dns.get("all_resolved") or set(resolutions) != {"pip", "npm"}:
        return False, "real-dns-unverified"
    for mirror, result in resolutions.items():
        host = result.get("host")
        for runtime in ("python", "node"):
            command_evidence = result.get(runtime, {})
            if (not host or command_evidence.get("exit_code") != 0
                    or host not in command_evidence.get("command", "") or not command_evidence.get("output")):
                return False, f"{mirror}-{runtime}-dns-unverified"
            try:
                resolution = json.loads(command_evidence["output"])
            except (TypeError, json.JSONDecodeError):
                return False, f"{mirror}-{runtime}-dns-output-invalid"
            if resolution.get("host") != host or not resolution.get("addresses"):
                return False, f"{mirror}-{runtime}-dns-output-invalid"
    for runtime in ("shell", "python", "node"):
        task = evidence[f"task.{runtime}.wait_terminal_and_read_log"]
        operation = task.get("operation", {})
        if (
            operation.get("state") != "success"
            or operation.get("kind") != "task"
            or not operation.get("ended_at")
            or task.get("terminal_status") != 0
            or not task.get("marker_found")
            or not task.get("log_id")
            or len(str(task.get("content_sha256", ""))) != 64
        ):
            return False, f"{runtime}-execution-evidence-invalid"
    for dependency in ("python_wheel", "node_tarball"):
        install = evidence[f"dependency.{dependency}.install"]
        uninstall = evidence[f"dependency.{dependency}.uninstall"]
        if install.get("terminal_status") != "installed" or install.get("operation", {}).get("state") != "success":
            return False, f"{dependency}-install-unverified"
        if not uninstall.get("dependency_missing") or uninstall.get("operation", {}).get("state") != "success":
            return False, f"{dependency}-uninstall-unverified"
    restart = evidence["core.method_channel.restart"].get("core", {})
    restart_error = validate_core_identity(restart)
    if restart_error or restart.get("instance_id") != evidence["core.method_channel.restart"].get("previous_instance_id"):
        return False, "core-restart-unverified"
    persistence = evidence["persistence.after_restart"]
    if not all(persistence.get(key) for key in ("admin_persisted", "env_persisted", "env_deleted")):
        return False, "restart-persistence-unverified"
    versions, version_error = runtime_versions_from_evidence(by_id)
    if version_error:
        return False, version_error
    return True, "verified"


def validate_core_identity(core):
    if core.get("phase") != "ready":
        return "core-not-ready"
    scheduler_states = {"active", "foreground_continuous", "system_compensation"}
    if (core.get("core_version") != "kotlin-local-fallback" or core.get("fallback_mode") != "full"
            or core.get("scheduler_host_state") not in scheduler_states
            or core.get("scheduler_guarantee_state") not in scheduler_states):
        return "core-identity-unverified"
    if core.get("instance_id") != "kotlin-local-fallback":
        return "core-instance-invalid"
    return ""


def runtime_versions_from_evidence(by_id):
    versions = {}
    for runtime_id, requirement in RUNTIME_EVIDENCE.items():
        evidence = by_id.get(requirement["step_id"], {}).get("evidence", {})
        output = str(evidence.get("output", "")).strip()
        if evidence.get("command") != requirement["command"] or evidence.get("exit_code") != 0 or not output:
            return {}, f"{runtime_id}-version-evidence-invalid"
        pattern = requirement.get("output_pattern")
        match = re.fullmatch(pattern, output) if pattern else None
        actual = match.group(1) if match else output
        if pattern and match is None:
            return {}, f"{runtime_id}-version-output-invalid"
        expected = requirement.get("expected_version")
        constraint = requirement.get("version_constraint")
        if expected is not None and actual != expected:
            return {}, f"{runtime_id}-version-mismatch:expected={expected}:actual={actual}"
        if constraint is not None and re.fullmatch(constraint, actual) is None:
            return {}, f"{runtime_id}-version-constraint-mismatch:actual={actual}"
        versions[runtime_id] = {"command": requirement["command"], "output": output, "actual": actual}
    return versions, ""


def blocked_evidence(matrix_id, reason, source="none"):
    return {
        "version": "1",
        "updated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "matrix": MATRIX,
        "records": [
            {
                "runtime_id": runtime_id,
                "version": version,
                "entry": entry,
                "status": "blocked",
                "evidence_source": source,
                "isolation_level": isolation,
                "timeout_seconds": timeout,
                "checks": [{"id": check_id, "status": "blocked", "reason": f"{reason};matrix={matrix_id}"}],
            }
            for runtime_id, version, entry, isolation, timeout, check_id in RUNTIMES
        ],
    }


def blocked(args):
    write_json(args.output, blocked_evidence(args.matrix_id, args.reason))
    return 0


def external(args):
    # Parsing proves the input is JSON; its assertions remain untrusted by design.
    json.loads(args.input.read_text(encoding="utf-8"))
    write_json(args.output, blocked_evidence(args.matrix_id, "external-unverified", "external-unverified"))
    return 0


def dry_run(args):
    plan = {
        "mode": "dry-run",
        "matrix_id": args.matrix_id,
        "expected_api": args.expected_api,
        "expected_page_size": args.expected_page_size,
        "expected_abi": args.expected_abi,
        "apk": str(args.apk),
        "test_apk": str(args.test_apk),
        "artifacts": {
            "app_apk": artifact_evidence(args.apk),
            "test_apk": artifact_evidence(args.test_apk),
        },
        "commands": [
            "adb get-state",
            "adb shell getprop ro.product.cpu.abi",
            "adb shell getprop ro.build.version.sdk",
            "adb shell getconf PAGESIZE",
            f"adb install --no-streaming -r -t {args.apk}",
            f"adb install --no-streaming -r -t {args.test_apk}",
            f"adb shell monkey -p {PACKAGE} 1",
            f"adb shell am instrument -w -r -e class {TEST_CLASS} {RUNNER}",
            "parse chunked base64 evidence from am instrument stdout",
            "validate schema_version=2 and every required E2E step",
        ],
    }
    print(json.dumps(plan, indent=2))
    return 0


def run(args):
    artifacts = {
        "app_apk": artifact_evidence(args.apk),
        "test_apk": artifact_evidence(args.test_apk),
    }
    command(args.adb, args.serial, "get-state")
    abi = device_value(args.adb, args.serial, "getprop", "ro.product.cpu.abi")
    api = int(device_value(args.adb, args.serial, "getprop", "ro.build.version.sdk"))
    page_size = int(device_value(args.adb, args.serial, "getconf", "PAGESIZE"))
    fingerprint = device_value(args.adb, args.serial, "getprop", "ro.build.fingerprint")
    device = {"serial": args.serial or "default", "abi": abi, "api": api, "page_size_bytes": page_size, "fingerprint": fingerprint}
    if abi not in {"arm64-v8a", "arm64-v8a,armeabi-v7a", "x86_64"}:
        return fail_device_run(args, device, f"unsupported-abi:{abi}")
    if args.expected_abi and abi != args.expected_abi:
        return fail_device_run(args, device, f"abi-mismatch:expected={args.expected_abi}:actual={abi}")
    if api != args.expected_api:
        return fail_device_run(args, device, f"api-mismatch:expected={args.expected_api}:actual={api}")
    if page_size != args.expected_page_size:
        return fail_device_run(args, device, f"page-size-mismatch:expected={args.expected_page_size}:actual={page_size}")

    try:
        command(args.adb, args.serial, "install", "--no-streaming", "-r", "-t", str(args.apk), timeout=300)
        command(args.adb, args.serial, "install", "--no-streaming", "-r", "-t", str(args.test_apk), timeout=300)
        command(args.adb, args.serial, "shell", "monkey", "-p", PACKAGE, "-c", "android.intent.category.LAUNCHER", "1", timeout=60)
    except RuntimeError as error:
        return fail_device_run(args, device, f"install-or-launch-failed:{str(error)[:500]}")
    try:
        instrument = command(
            args.adb,
            args.serial,
            "shell",
            "am",
            "instrument",
            "-w",
            "-r",
            "-e",
            "class",
            TEST_CLASS,
            RUNNER,
            timeout=args.timeout,
            check=False,
        )
    except RuntimeError as error:
        return fail_device_run(args, device, f"instrumentation-failed:{str(error)[:500]}")
    try:
        helper = parse_instrumentation_evidence(instrument.stdout)
        evidence_transport = "instrumentation-result-bundle"
    except ValueError as bundle_error:
        helper = {}
        evidence_transport = f"unavailable:{bundle_error}"
    core = helper.get("core", {})
    scenario_valid, validation_reason = validate_instrumentation(helper)
    helper_steps = {step.get("id"): step for step in helper.get("steps", []) if isinstance(step, dict)}
    runtime_versions, _ = runtime_versions_from_evidence(helper_steps)
    valid = (
        instrument.returncode == 0
        and "OK (1 test)" in instrument.stdout
        and scenario_valid
    )
    contract_records = []
    context = f"matrix={args.matrix_id};api={api};page_size_bytes={page_size};core_instance={core.get('instance_id', '')};core_version={core.get('core_version', '')}"
    for runtime_id, version, entry, isolation, timeout, check_id in RUNTIMES:
        runtime_step = {"python-3.12-android-arm64": "task.python.wait_terminal_and_read_log", "node-lts-android-arm64": "task.node.wait_terminal_and_read_log", "shell-android-arm64": "task.shell.wait_terminal_and_read_log"}.get(runtime_id)
        if valid and runtime_step:
            status = "pass"
            measured = runtime_versions[runtime_id]
            normalized = [{"id": check_id, "status": "pass", "output": f"command={measured['command']};actual={measured['actual']};raw={measured['output']};verified_step={runtime_step};{context}"}]
        else:
            status = "blocked"
            reason = "runtime-outside-e2e-scope" if valid else validation_reason
            normalized = [{"id": check_id, "status": "blocked", "reason": f"{reason};{context}"}]
        contract_records.append({
            "runtime_id": runtime_id, "version": version, "entry": entry, "status": status,
            "evidence_source": "android-device", "isolation_level": isolation,
            "timeout_seconds": timeout, "checks": normalized,
        })
    payload = {"version": "1", "updated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "matrix": MATRIX, "artifacts": artifacts, "records": contract_records}
    device_payload = {
        "schema_version": 1,
        "status": "verified" if valid else "failed",
        "matrix_id": args.matrix_id,
        "artifacts": artifacts,
        "device": device,
        "page_size_source": "adb shell getconf PAGESIZE",
        "instrumentation": {"return_code": instrument.returncode, "output": instrument.stdout[-8000:], "stderr": instrument.stderr[-2000:], "validation": validation_reason, "evidence_transport": evidence_transport},
        "runtime": helper,
    }
    write_json(args.output, payload)
    write_json(args.output.with_suffix(".device.json"), device_payload)
    collect_evidence_files(args.adb, args.serial, args.output, args.matrix_id, "post-instrumentation", "captured")
    if not valid:
        raise RuntimeError(f"runtime smoke failed; evidence written to {args.output}")
    return 0


def fail_device_run(args, device, reason):
    write_json(args.output, blocked_evidence(args.matrix_id, reason, "android-device"))
    write_json(
        args.output.with_suffix(".device.json"),
        {
            "schema_version": 1,
            "status": "blocked",
            "matrix_id": args.matrix_id,
            "device": device,
            "page_size_source": "adb shell getconf PAGESIZE",
            "reason": reason,
        },
    )
    collect_evidence_files(args.adb, args.serial, args.output, args.matrix_id, reason)
    raise RuntimeError(f"runtime smoke blocked: {reason}; evidence written to {args.output}")


def pre_diagnose(args):
    d = {
        "schema_version": 1,
        "matrix_id": args.matrix_id,
        "captured_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "commands": [
            safe_command(args.adb, args.serial, "shell", "dumpsys", "activity", "processes"),
            safe_command(args.adb, args.serial, "shell", "dumpsys", "meminfo"),
            safe_command(args.adb, args.serial, "shell", "ls", "-la", "/data/tombstones/", timeout=15),
            safe_command(args.adb, args.serial, "shell", "cat", "/proc/version"),
            safe_command(args.adb, args.serial, "shell", "getprop", "ro.dalvik.vm.native.bridge"),
            safe_command(args.adb, args.serial, "shell", "getprop", "ro.product.cpu.abilist64"),
            safe_command(args.adb, args.serial, "shell", "getprop", "ro.product.cpu.abilist32"),
            safe_command(args.adb, args.serial, "shell", "pm", "list", "packages", "daidai"),
        ],
    }
    write_json(args.output, d)
    return 0


def diagnose(args):
    diagnostics = collect_diagnostics(args.adb, args.serial, args.matrix_id, args.reason)
    write_json(args.output, diagnostics)
    logcat = diagnostics["commands"][-1]
    base_name = args.output.name.removesuffix(".diagnostics.json")
    logcat_path = args.output.with_name(base_name + ".logcat.txt")
    content = logcat.get("stdout", "")
    if logcat.get("stderr"):
        content += "\n[stderr]\n" + logcat["stderr"]
    if logcat.get("error"):
        content += "\n[collection-error]\n" + logcat["error"]
    write_text(logcat_path, content)
    return 0


def parser():
    root = argparse.ArgumentParser()
    sub = root.add_subparsers(dest="operation", required=True)
    for name in ("run", "dry-run"):
        item = sub.add_parser(name)
        item.add_argument("--apk", type=pathlib.Path, required=True)
        item.add_argument("--test-apk", type=pathlib.Path, required=True)
        item.add_argument("--matrix-id", required=True)
        item.add_argument("--expected-api", type=int, required=True)
        item.add_argument("--expected-page-size", type=int, required=True)
        item.add_argument("--expected-abi", default="")
        item.add_argument("--serial", default="")
        item.add_argument("--adb", default="adb")
        item.add_argument("--output", type=pathlib.Path, default=pathlib.Path("android-runtime-smoke-evidence.json"))
        item.add_argument("--timeout", type=int, default=420)
    blocked_parser = sub.add_parser("blocked")
    blocked_parser.add_argument("--matrix-id", required=True)
    blocked_parser.add_argument("--reason", required=True)
    blocked_parser.add_argument("--output", type=pathlib.Path, required=True)
    external_parser = sub.add_parser("external")
    external_parser.add_argument("--input", type=pathlib.Path, required=True)
    external_parser.add_argument("--matrix-id", required=True)
    external_parser.add_argument("--output", type=pathlib.Path, required=True)
    diagnose_parser = sub.add_parser("diagnose")
    diagnose_parser.add_argument("--matrix-id", required=True)
    diagnose_parser.add_argument("--reason", required=True)
    diagnose_parser.add_argument("--serial", default="")
    diagnose_parser.add_argument("--adb", default="adb")
    diagnose_parser.add_argument("--output", type=pathlib.Path, required=True)
    pre_diag_parser = sub.add_parser("pre-diagnose")
    pre_diag_parser.add_argument("--matrix-id", required=True)
    pre_diag_parser.add_argument("--serial", default="")
    pre_diag_parser.add_argument("--adb", default="adb")
    pre_diag_parser.add_argument("--output", type=pathlib.Path, required=True)
    return root


def main():
    args = parser().parse_args()
    try:
        return {"run": run, "dry-run": dry_run, "blocked": blocked, "external": external, "diagnose": diagnose, "pre-diagnose": pre_diagnose}[args.operation](args)
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as error:
        print(f"android runtime smoke: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
