#!/usr/bin/env python3
"""Install and exercise the Android ARM64 runtime smoke gate on a connected device."""

import argparse
import datetime
import json
import pathlib
import subprocess
import sys


PACKAGE = "com.daidai.daidai_app"
RUNNER = f"{PACKAGE}.test/androidx.test.runner.AndroidJUnitRunner"
TEST_CLASS = f"{PACKAGE}.AndroidRuntimeSmokeTest"
MATRIX = ["api28-4k", "api35-4k", "api35-16k"]
RUNTIMES = [
    ("python-3.14-android-arm64", "3.14.6", "libpython_exec.so", "trusted-runner", 10, "PY_OK_SSL_SQLITE_VENV_PIP"),
    ("node-lts-android-arm64", "18.20.4", "libnode_exec.so", "trusted-runner", 10, "COMMONJS_ESM_HTTPS"),
    ("typescript-stable", "5.9.3", "libnode_exec.so", "trusted-runner", 10, "TS_OK"),
    ("shell-android-arm64", "blocked-placeholder", "libshell_exec.so", "trusted-runner", 10, "SHELL_PIPE_EXIT_STOP"),
    ("git-android-arm64", "blocked-placeholder", "libgit_exec.so", "broker", 30, "GIT_CLONE_FETCH_SPARSE"),
    ("ssh-android-arm64", "blocked-placeholder", "libssh_exec.so", "broker", 30, "SSH_HOSTKEY"),
    ("yaegi-go", "blocked-placeholder", "libyaegi_exec.so", "isolated-worker", 10, "GO_INTERPRET_OK"),
    ("go-builder-android-arm64", "blocked-placeholder", "libgobuilder_exec.so", "trusted-builder", 60, "GO_BUILD_EXPORT_ONLY"),
]

REQUIRED_STEPS = {
    "core.method_channel.ensure_started",
    "auth.initialize_and_login",
    "env.create",
    "env.read_update",
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


def validate_instrumentation(helper):
    steps = helper.get("steps")
    if helper.get("schema_version") != 2 or helper.get("status") != "pass" or not isinstance(steps, list):
        return False, "invalid-instrumentation-envelope"
    by_id = {step.get("id"): step for step in steps if isinstance(step, dict)}
    missing = sorted(REQUIRED_STEPS - set(by_id))
    if missing:
        return False, "missing-steps:" + ",".join(missing)
    failed = sorted(step_id for step_id in REQUIRED_STEPS if by_id[step_id].get("status") != "pass")
    if failed:
        return False, "failed-steps:" + ",".join(failed)
    core = helper.get("core", {})
    if core.get("phase") != "ready" or not core.get("instance_id") or not core.get("core_version"):
        return False, "core-not-ready"
    if "fallback" in str(core.get("instance_id", "")).lower() or "fallback" in str(core.get("core_version", "")).lower():
        return False, "fallback-core"
    if core.get("core_version") != "gomobile" or core.get("core_status") not in {"running", "ready", "degraded-ready"}:
        return False, "core-identity-unverified"
    try:
        if int(core["instance_id"]) <= 0:
            return False, "core-instance-invalid"
    except (TypeError, ValueError):
        return False, "core-instance-invalid"
    evidence = {step_id: by_id[step_id].get("evidence", {}) for step_id in REQUIRED_STEPS}
    if not evidence["auth.initialize_and_login"].get("admin_authenticated"):
        return False, "admin-auth-unverified"
    if not evidence["env.create"].get("created") or not evidence["env.read_update"].get("updated"):
        return False, "env-crud-unverified"
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
    if restart.get("phase") != "ready" or restart.get("instance_id") == evidence["core.method_channel.restart"].get("previous_instance_id"):
        return False, "core-restart-unverified"
    persistence = evidence["persistence.after_restart"]
    if not all(persistence.get(key) for key in ("admin_persisted", "env_persisted", "env_deleted")):
        return False, "restart-persistence-unverified"
    return True, "verified"


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
        "apk": str(args.apk),
        "test_apk": str(args.test_apk),
        "commands": [
            "adb get-state",
            "adb shell getprop ro.product.cpu.abi",
            "adb shell getprop ro.build.version.sdk",
            "adb shell getconf PAGESIZE",
            f"adb install -r -t {args.apk}",
            f"adb install -r -t {args.test_apk}",
            f"adb shell monkey -p {PACKAGE} 1",
            f"adb shell am instrument -w -r -e class {TEST_CLASS} {RUNNER}",
            f"adb exec-out run-as {PACKAGE} cat files/runtime-smoke/instrumentation.json",
            "validate schema_version=2 and every required E2E step",
        ],
    }
    print(json.dumps(plan, indent=2))
    return 0


def run(args):
    command(args.adb, args.serial, "get-state")
    abi = device_value(args.adb, args.serial, "getprop", "ro.product.cpu.abi")
    api = int(device_value(args.adb, args.serial, "getprop", "ro.build.version.sdk"))
    page_size = int(device_value(args.adb, args.serial, "getconf", "PAGESIZE"))
    fingerprint = device_value(args.adb, args.serial, "getprop", "ro.build.fingerprint")
    device = {"serial": args.serial or "default", "abi": abi, "api": api, "page_size_bytes": page_size, "fingerprint": fingerprint}
    if abi not in {"arm64-v8a", "arm64-v8a,armeabi-v7a", "x86_64"}:
        return fail_device_run(args, device, f"unsupported-abi:{abi}")
    if api != args.expected_api:
        return fail_device_run(args, device, f"api-mismatch:expected={args.expected_api}:actual={api}")
    if page_size != args.expected_page_size:
        return fail_device_run(args, device, f"page-size-mismatch:expected={args.expected_page_size}:actual={page_size}")

    try:
        command(args.adb, args.serial, "install", "-r", "-t", str(args.apk), timeout=300)
        command(args.adb, args.serial, "install", "-r", "-t", str(args.test_apk), timeout=300)
        command(args.adb, args.serial, "shell", "monkey", "-p", PACKAGE, "-c", "android.intent.category.LAUNCHER", "1", timeout=60)
    except RuntimeError as error:
        return fail_device_run(args, device, f"install-or-launch-failed:{str(error)[:500]}")
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
    raw = command(
        args.adb,
        args.serial,
        "exec-out",
        "run-as",
        PACKAGE,
        "cat",
        "files/runtime-smoke/instrumentation.json",
        check=False,
    )
    try:
        helper = json.loads(raw.stdout) if raw.returncode == 0 and raw.stdout.strip() else {}
    except json.JSONDecodeError:
        helper = {}
    core = helper.get("core", {})
    scenario_valid, validation_reason = validate_instrumentation(helper)
    valid = (
        instrument.returncode == 0
        and "OK (1 test)" in instrument.stdout
        and scenario_valid
    )
    contract_records = []
    context = f"matrix={args.matrix_id};api={api};page_size_bytes={page_size};core_instance={core.get('instance_id', '')};core_version={core.get('core_version', '')}"
    for runtime_id, version, entry, isolation, timeout, check_id in RUNTIMES:
        runtime_step = {"python-3.14-android-arm64": "task.python.wait_terminal_and_read_log", "node-lts-android-arm64": "task.node.wait_terminal_and_read_log", "shell-android-arm64": "task.shell.wait_terminal_and_read_log"}.get(runtime_id)
        if valid and runtime_step:
            status = "pass"
            normalized = [{"id": check_id, "status": "pass", "output": f"verified_step={runtime_step};{context}"}]
        else:
            status = "blocked"
            reason = "runtime-outside-e2e-scope" if valid else validation_reason
            normalized = [{"id": check_id, "status": "blocked", "reason": f"{reason};{context}"}]
        contract_records.append({
            "runtime_id": runtime_id, "version": version, "entry": entry, "status": status,
            "evidence_source": "android-device", "isolation_level": isolation,
            "timeout_seconds": timeout, "checks": normalized,
        })
    payload = {"version": "1", "updated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "matrix": MATRIX, "records": contract_records}
    device_payload = {
        "schema_version": 1,
        "status": "verified" if valid else "failed",
        "matrix_id": args.matrix_id,
        "device": device,
        "page_size_source": "adb shell getconf PAGESIZE",
        "instrumentation": {"return_code": instrument.returncode, "output": instrument.stdout[-8000:], "stderr": instrument.stderr[-2000:], "validation": validation_reason},
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
    return root


def main():
    args = parser().parse_args()
    try:
        return {"run": run, "dry-run": dry_run, "blocked": blocked, "external": external, "diagnose": diagnose}[args.operation](args)
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as error:
        print(f"android runtime smoke: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
