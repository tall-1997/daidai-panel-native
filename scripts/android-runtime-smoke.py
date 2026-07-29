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
    if abi not in {"arm64-v8a", "arm64-v8a,armeabi-v7a"}:
        return fail_device_run(args, device, f"arm64-required:{abi}")
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
    records = helper.get("records", [])
    core = helper.get("core", {})
    valid = (
        instrument.returncode == 0
        and "OK (1 test)" in instrument.stdout
        and len(records) == 8
        and {item.get("runtime_id") for item in records} == {item[0] for item in RUNTIMES}
        and all(item.get("status") in {"pass", "blocked"} for item in records)
        and core.get("phase") == "ready"
        and bool(core.get("instance_id", "").strip())
        and bool(core.get("core_version", "").strip())
        and "fallback" not in core.get("instance_id", "").lower()
        and "fallback" not in core.get("core_version", "").lower()
    )
    by_id = {item.get("runtime_id"): item for item in records}
    contract_records = []
    context = f"matrix={args.matrix_id};api={api};page_size_bytes={page_size};core_instance={core.get('instance_id', '')};core_version={core.get('core_version', '')}"
    for runtime_id, version, entry, isolation, timeout, check_id in RUNTIMES:
        item = by_id.get(runtime_id, {})
        status = item.get("status") if valid else "blocked"
        checks = item.get("checks", []) if valid else []
        if status == "pass":
            normalized = [{"id": check.get("id", check_id), "status": "pass", "output": f"{context};{check.get('output', '')}"} for check in checks]
        else:
            normalized = [{"id": check.get("id", check_id), "status": "blocked", "reason": f"{check.get('reason', 'instrumentation-invalid')};{context}"} for check in checks]
            if not normalized:
                normalized = [{"id": check_id, "status": "blocked", "reason": f"instrumentation-invalid;{context}"}]
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
        "instrumentation": {"return_code": instrument.returncode, "output": instrument.stdout[-8000:], "stderr": instrument.stderr[-2000:]},
        "runtime": helper,
    }
    write_json(args.output, payload)
    write_json(args.output.with_suffix(".device.json"), device_payload)
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
    raise RuntimeError(f"runtime smoke blocked: {reason}; evidence written to {args.output}")


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
    return root


def main():
    args = parser().parse_args()
    try:
        return {"run": run, "dry-run": dry_run, "blocked": blocked, "external": external}[args.operation](args)
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as error:
        print(f"android runtime smoke: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
