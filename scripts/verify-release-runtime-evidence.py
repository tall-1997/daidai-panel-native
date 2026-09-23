#!/usr/bin/env python3
"""Validate release/device-smoke artifacts against the shared release contract."""

import argparse
import hashlib
import json
import re
from pathlib import Path


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def load_json(path, errors):
    try:
        payload = read_json(path)
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        errors.append(f"{path}: malformed evidence JSON: {error}")
        return None
    if not isinstance(payload, dict):
        errors.append(f"{path}: evidence root must be an object")
        return None
    return payload


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as artifact:
        for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def validate_artifacts(payload, path, expected_artifacts):
    errors = []
    artifacts = payload.get("artifacts")
    if not isinstance(artifacts, dict):
        return [f"{path}: artifacts must record app_apk and test_apk"]
    for artifact_id, expected in expected_artifacts.items():
        artifact = artifacts.get(artifact_id)
        if not isinstance(artifact, dict):
            errors.append(f"{path}: missing {artifact_id} evidence")
            continue
        actual = str(artifact.get("sha256", "")).lower()
        if len(actual) != 64 or any(character not in "0123456789abcdef" for character in actual):
            errors.append(f"{path}: {artifact_id} SHA-256 is invalid")
        elif actual != expected["sha256"]:
            errors.append(f"{path}: {artifact_id} SHA-256 does not match candidate APK")
        if artifact.get("name") != expected["name"] or artifact.get("size") != expected["size"]:
            errors.append(f"{path}: {artifact_id} metadata is incomplete")
    return errors


def validate_runtime_payload(payload, path, matrix_ids, runtime_ids, stable_ids, runtime_requirements, runtime_entries, channel):
    errors = []
    matrix = payload.get("matrix")
    if matrix != matrix_ids:
        errors.append(f"{path}: matrix must exactly match release runtime contract")
    records = payload.get("records")
    if not isinstance(records, list):
        return errors + [f"{path}: records must be a list"]

    seen = set()
    records_by_id = {}
    for index, record in enumerate(records):
        if not isinstance(record, dict):
            errors.append(f"{path}: record {index} must be an object")
            continue
        runtime_id = record.get("runtime_id")
        if runtime_id in seen:
            errors.append(f"{path}: duplicate runtime ID {runtime_id!r}")
        seen.add(runtime_id)
        records_by_id[runtime_id] = record
    if set(records_by_id) != set(runtime_ids) or len(records) != len(runtime_ids):
        errors.append(f"{path}: runtime IDs must be the complete unique canonical set")

    for runtime_id in runtime_ids:
        record = records_by_id.get(runtime_id)
        if not isinstance(record, dict):
            continue
        status = record.get("status")
        source = record.get("evidence_source")
        checks = record.get("checks")
        entry = runtime_entries.get(runtime_id, {})
        if record.get("entry") != entry.get("entrypoint"):
            errors.append(f"{path}: {runtime_id} entry does not match release contract")
        requirement = runtime_requirements.get(runtime_id, {})
        expected_version = requirement.get("expected_version")
        if expected_version is not None and record.get("version") != expected_version:
            errors.append(f"{path}: {runtime_id} record version does not match measured contract version")
        if status not in ("pass", "blocked"):
            errors.append(f"{path}: {runtime_id} has invalid status {status!r}")
            continue
        if channel == "stable" and runtime_id in stable_ids and status != "pass":
            errors.append(f"{path}: stable required runtime {runtime_id} must pass")
        if not isinstance(checks, list) or not checks:
            errors.append(f"{path}: {runtime_id} checks must be a non-empty list")
            continue
        if status == "pass":
            if source != "android-device":
                errors.append(f"{path}: {runtime_id} pass requires android-device evidence")
            for check in checks:
                if (not isinstance(check, dict) or not check.get("id") or check.get("status") != "pass"
                        or not str(check.get("output", "")).strip() or "reason" in check):
                    errors.append(f"{path}: {runtime_id} pass checks are incomplete")
                    break
            requirement = runtime_requirements.get(runtime_id)
            if requirement:
                combined_output = "\n".join(str(check.get("output", "")) for check in checks if isinstance(check, dict))
                expected = requirement.get("expected_version")
                if f"command={requirement.get('command')};" not in combined_output:
                    errors.append(f"{path}: {runtime_id} pass lacks contracted command evidence")
                if expected is not None and f"actual={expected};" not in combined_output:
                    errors.append(f"{path}: {runtime_id} pass lacks expected version evidence")
                constraint = requirement.get("version_constraint")
                if constraint is not None:
                    actual = re.search(r"(?:^|;)actual=([^;]+);", combined_output)
                    if actual is None or re.fullmatch(constraint, actual.group(1)) is None:
                        errors.append(f"{path}: {runtime_id} pass violates version constraint")
        else:
            if source not in ("none", "external-unverified", "android-device"):
                errors.append(f"{path}: {runtime_id} blocked evidence source is invalid")
            for check in checks:
                if (not isinstance(check, dict) or not check.get("id") or check.get("status") != "blocked"
                        or not str(check.get("reason", "")).strip() or "output" in check):
                    errors.append(f"{path}: {runtime_id} blocked checks are incomplete")
                    break
    return errors


def validate_device_runtime(payload, path, runtime_requirements):
    errors = []
    helper = payload.get("runtime")
    if not isinstance(helper, dict):
        return [f"{path}: device helper evidence is missing"]
    core = helper.get("core", {})
    if (core.get("phase") != "ready" or core.get("core_version") != "kotlin-local-fallback"
            or core.get("fallback_mode") != "full" or core.get("scheduler_host_state") != "active"
            or core.get("scheduler_guarantee_state") != "active" or core.get("instance_id") != "kotlin-local-fallback"):
        errors.append(f"{path}: Kotlin fallback core identity or health is invalid")
    steps = helper.get("steps")
    by_id = {step.get("id"): step for step in steps if isinstance(step, dict)} if isinstance(steps, list) else {}
    for runtime_id, requirement in runtime_requirements.items():
        evidence = by_id.get(requirement.get("step_id"), {}).get("evidence", {})
        output = str(evidence.get("output", "")).strip()
        if evidence.get("command") != requirement.get("command") or evidence.get("exit_code") != 0 or not output:
            errors.append(f"{path}: {runtime_id} helper command evidence is invalid")
            continue
        pattern = requirement.get("output_pattern")
        match = re.fullmatch(pattern, output) if pattern else None
        actual = match.group(1) if match else output
        if pattern and match is None:
            errors.append(f"{path}: {runtime_id} helper output is invalid")
            continue
        if requirement.get("expected_version") is not None and actual != requirement["expected_version"]:
            errors.append(f"{path}: {runtime_id} helper version does not match contract")
        constraint = requirement.get("version_constraint")
        if constraint is not None and re.fullmatch(constraint, actual) is None:
            errors.append(f"{path}: {runtime_id} helper version constraint failed")
    return errors


def validate(contract, evidence_root, channel, app_apk=None, test_apk=None):
    errors = []
    matrices = contract.get("device_smoke", {}).get("matrix", [])
    matrix_ids = [item.get("id") for item in matrices if isinstance(item, dict)]
    runtime_ids = contract.get("runtime_ids", [])
    if contract.get("schema_version") != 1 or len(matrix_ids) != len(matrices) or not matrix_ids or any(not item for item in matrix_ids):
        return ["release runtime contract is invalid"]
    if len(matrix_ids) != len(set(matrix_ids)):
        errors.append("release runtime contract contains duplicate matrix IDs")
    if not isinstance(runtime_ids, list) or not runtime_ids or len(runtime_ids) != len(set(runtime_ids)) or any(not isinstance(item, str) or not item for item in runtime_ids):
        errors.append("runtime_ids must be a non-empty unique canonical list")
        return errors
    stable_ids = contract.get("stable_required_runtime_ids", [])
    if not isinstance(stable_ids, list) or not stable_ids or len(stable_ids) != len(set(stable_ids)) or not set(stable_ids).issubset(runtime_ids):
        errors.append("stable_required_runtime_ids must be a non-empty unique runtime_ids subset")
        return errors
    runtime_requirements = contract.get("runtime_evidence")
    if not isinstance(runtime_requirements, dict) or not set(stable_ids).issubset(runtime_requirements):
        errors.append("runtime_evidence must define every stable required runtime")
        return errors
    runtime_entries = contract.get("runtime_entries")
    if not isinstance(runtime_entries, dict) or set(runtime_entries) != set(runtime_ids):
        errors.append("runtime_entries must define every canonical runtime")
        return errors
    for runtime_id, entry in runtime_entries.items():
        if (not isinstance(entry, dict) or entry.get("entry_type") not in ("apk_elf", "rootfs_command")
                or not isinstance(entry.get("entrypoint"), str) or not entry["entrypoint"]):
            errors.append(f"runtime_entries contains an invalid entry for {runtime_id}")
    if errors:
        return errors
    gate_scope = contract.get("release_gate_scope", {})
    required_gates = gate_scope.get("required") if isinstance(gate_scope, dict) else None
    optional_gates = gate_scope.get("optional") if isinstance(gate_scope, dict) else None
    if not isinstance(required_gates, list) or not required_gates or not isinstance(optional_gates, list) or not optional_gates:
        errors.append("release_gate_scope must define non-empty required and optional gates")
        return errors

    expected_artifacts = {}
    if channel == "stable":
        if app_apk is None or test_apk is None:
            errors.append("stable evidence validation requires app and test APK candidates")
            return errors
        try:
            expected_artifacts = {
                "app_apk": {"name": app_apk.name, "size": app_apk.stat().st_size, "sha256": sha256(app_apk)},
                "test_apk": {"name": test_apk.name, "size": test_apk.stat().st_size, "sha256": sha256(test_apk)},
            }
        except OSError as error:
            errors.append(f"candidate APK cannot be read: {error}")
            return errors

    for matrix_id in matrix_ids:
        runtime_paths = list(evidence_root.rglob(f"{matrix_id}.json"))
        if len(runtime_paths) != 1:
            errors.append(f"{matrix_id}: expected exactly one runtime evidence file, got {len(runtime_paths)}")
        else:
            runtime = load_json(runtime_paths[0], errors)
            if runtime is not None:
                errors.extend(validate_runtime_payload(runtime, runtime_paths[0], matrix_ids, runtime_ids, stable_ids, runtime_requirements, runtime_entries, channel))
                if channel == "stable":
                    errors.extend(validate_artifacts(runtime, runtime_paths[0], expected_artifacts))

        device_paths = list(evidence_root.rglob(f"{matrix_id}.device.json"))
        if channel == "stable":
            if len(device_paths) != 1:
                errors.append(f"{matrix_id}: expected exactly one device evidence file, got {len(device_paths)}")
                continue
            device = load_json(device_paths[0], errors)
            if device is None:
                continue
            errors.extend(validate_artifacts(device, device_paths[0], expected_artifacts))
            errors.extend(validate_device_runtime(device, device_paths[0], runtime_requirements))
            expected = next(item for item in matrices if item["id"] == matrix_id)
            actual_device = device.get("device", {})
            if device.get("status") != "verified":
                errors.append(f"{matrix_id}: device status must be verified")
            if actual_device.get("api") != expected["api"]:
                errors.append(f"{matrix_id}: device API does not match contract")
            if actual_device.get("page_size_bytes") != expected["page_size_bytes"]:
                errors.append(f"{matrix_id}: device page size does not match contract")
            if actual_device.get("abi") != expected["abi"]:
                errors.append(f"{matrix_id}: device ABI does not match contract")

        elif device_paths:
            errors.append(f"{matrix_id}: prerelease runtime artifact must not contain device evidence")
    if channel not in ("stable", "prerelease"):
        errors.append(f"unsupported release channel: {channel}")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", type=Path, default=Path("scripts/release-runtime-contract.json"))
    parser.add_argument("--evidence-dir", type=Path, required=True)
    parser.add_argument("--channel", choices=("stable", "prerelease"), required=True)
    parser.add_argument("--app-apk", type=Path)
    parser.add_argument("--test-apk", type=Path)
    args = parser.parse_args()
    errors = validate(read_json(args.contract), args.evidence_dir, args.channel, args.app_apk, args.test_apk)
    if errors:
        for error in errors:
            print(error)
        return 1
    print(f"release runtime evidence ok: {args.channel}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
