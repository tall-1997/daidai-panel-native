#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
Daidai Android App smoke test script.

Usage in the Android app:
1. Upload this file as android_app_smoke_test.py from the scripts page.
2. Run it from the scripts page or create a task command: task android_app_smoke_test.py
3. Check script logs, task logs, dependency status, and panel logs.

Optional environment variables:
- DAIDAI_TEST_AUTO_INSTALL=1
  Try to install missing packages with python -m pip install.

- DAIDAI_TEST_PACKAGES=requests,beautifulsoup4,pyyaml,cryptography
  Override the default dependency test list.

- DAIDAI_TEST_NETWORK=1
  Enable a small HTTPS request test.

Exit codes:
- 0: Base runtime passed and all requested dependency checks passed.
- 2: Runtime passed, but dependencies are missing.
- 3: Dependency auto-install failed.
- 4: File I/O failed.
- 5: Network test failed.
"""

import importlib.util
import json
import os
import pathlib
import platform
import subprocess
import sys
import time
import traceback
import urllib.request


DEFAULT_PACKAGES = [
    "requests",
    "beautifulsoup4",
    "pyyaml",
    "cryptography",
]

IMPORT_NAMES = {
    "beautifulsoup4": "bs4",
    "pyyaml": "yaml",
}


def log(message):
    print(f"[INFO] {message}", flush=True)


def warn(message):
    print(f"[WARN] {message}", flush=True)


def err(message):
    print(f"[ERROR] {message}", file=sys.stderr, flush=True)


def section(title):
    print("", flush=True)
    print(f"========== {title} ==========", flush=True)


def env_bool(name):
    return os.environ.get(name, "").strip().lower() in {"1", "true", "yes", "on"}


def get_packages():
    raw = os.environ.get("DAIDAI_TEST_PACKAGES", "").strip()
    if not raw:
        return DEFAULT_PACKAGES
    return [item.strip() for item in raw.split(",") if item.strip()]


def import_name(package_name):
    return IMPORT_NAMES.get(package_name, package_name.replace("-", "_"))


def package_available(package_name):
    name = import_name(package_name)
    return importlib.util.find_spec(name) is not None


def run_command(command, timeout=120):
    log(f"Running command: {' '.join(command)}")
    proc = subprocess.run(
        command,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=timeout,
    )
    output = proc.stdout or ""
    for line in output.splitlines():
        print(f"[CMD] {line}", flush=True)
    return proc.returncode, output


def install_missing_packages(packages):
    missing = [pkg for pkg in packages if not package_available(pkg)]
    if not missing:
        log("No missing packages to install")
        return True, []

    section("Dependency Install")
    log(f"Missing packages: {', '.join(missing)}")

    code, _ = run_command(
        [sys.executable, "-m", "pip", "install", "--no-input", *missing],
        timeout=300,
    )

    still_missing = [pkg for pkg in packages if not package_available(pkg)]
    if code != 0 or still_missing:
        err(f"Dependency install failed, still missing: {', '.join(still_missing)}")
        return False, still_missing

    log("Dependency install completed")
    return True, []


def test_runtime_info():
    section("Runtime Info")
    info = {
        "python_executable": sys.executable,
        "python_version": sys.version,
        "platform": platform.platform(),
        "machine": platform.machine(),
        "cwd": os.getcwd(),
        "argv": sys.argv,
        "DAIDAI_ANDROID_LOCAL": os.environ.get("DAIDAI_ANDROID_LOCAL", ""),
        "HOME": os.environ.get("HOME", ""),
        "TMPDIR": os.environ.get("TMPDIR", ""),
    }
    print(json.dumps(info, ensure_ascii=False, indent=2), flush=True)


def test_logs():
    section("Log Output")
    for index in range(1, 6):
        log(f"stdout log line {index}")
        if index == 3:
            err("stderr log line for log capture test")
        time.sleep(0.2)
    log("Log output test completed")


def test_file_io():
    section("File I/O")
    base = pathlib.Path(os.environ.get("TMPDIR") or os.getcwd())
    target = base / "daidai_app_smoke_test_output.json"
    payload = {
        "created_at": time.time(),
        "message": "Daidai Android local file I/O test",
        "pid": os.getpid(),
    }

    try:
        target.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        loaded = json.loads(target.read_text(encoding="utf-8"))
        if loaded.get("message") != payload["message"]:
            raise RuntimeError("Read content does not match written content")
        log(f"File write/read ok: {target}")
        return True
    except Exception as exc:
        err(f"File I/O failed: {exc}")
        traceback.print_exc()
        return False


def read_first_line(path):
    try:
        with open(path, "r", encoding="utf-8", errors="ignore") as file:
            return file.readline().strip()
    except Exception as exc:
        return f"unavailable: {exc}"


def test_proc_stats():
    section("Proc Stats")
    cpu_line = read_first_line("/proc/stat")
    meminfo = []
    try:
        with open("/proc/meminfo", "r", encoding="utf-8", errors="ignore") as file:
            for _ in range(5):
                line = file.readline()
                if not line:
                    break
                meminfo.append(line.strip())
    except Exception as exc:
        meminfo.append(f"unavailable: {exc}")

    log(f"/proc/stat first line: {cpu_line}")
    for line in meminfo:
        log(f"/proc/meminfo: {line}")


def test_dependencies(packages):
    section("Dependency Import")
    missing = []
    loaded = []

    for package in packages:
        name = import_name(package)
        if package_available(package):
            loaded.append(package)
            log(f"Dependency available: {package} import={name}")
        else:
            missing.append(package)
            warn(f"Dependency missing: {package} import={name}")

    summary = {
        "requested": packages,
        "loaded": loaded,
        "missing": missing,
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2), flush=True)
    return missing


def test_dependency_usage():
    section("Dependency Usage")
    if package_available("requests"):
        try:
            import requests

            log(f"requests version: {getattr(requests, '__version__', 'unknown')}")
        except Exception as exc:
            err(f"requests usage failed: {exc}")

    if package_available("pyyaml"):
        try:
            import yaml

            parsed = yaml.safe_load("name: daidai\nmode: android\n")
            log(f"pyyaml parse ok: {parsed}")
        except Exception as exc:
            err(f"pyyaml usage failed: {exc}")

    if package_available("beautifulsoup4"):
        try:
            from bs4 import BeautifulSoup

            soup = BeautifulSoup("<html><title>Daidai</title></html>", "html.parser")
            log(f"beautifulsoup4 parse ok: {soup.title.string}")
        except Exception as exc:
            err(f"beautifulsoup4 usage failed: {exc}")

    if package_available("cryptography"):
        try:
            import cryptography

            log(f"cryptography version: {getattr(cryptography, '__version__', 'unknown')}")
        except Exception as exc:
            err(f"cryptography usage failed: {exc}")


def test_network():
    section("Network")
    if not env_bool("DAIDAI_TEST_NETWORK"):
        warn("Network test skipped. Set DAIDAI_TEST_NETWORK=1 to enable it.")
        return True

    try:
        with urllib.request.urlopen("https://example.com", timeout=10) as response:
            status = getattr(response, "status", None)
            data = response.read(128)
        log(f"Network request ok: status={status}, bytes={len(data)}")
        return True
    except Exception as exc:
        err(f"Network request failed: {exc}")
        traceback.print_exc()
        return False


def main():
    started = time.time()
    exit_code = 0

    section("Daidai Android App Smoke Test Start")
    test_runtime_info()
    test_logs()

    if not test_file_io():
        exit_code = max(exit_code, 4)

    test_proc_stats()

    packages = get_packages()

    if env_bool("DAIDAI_TEST_AUTO_INSTALL"):
        ok, _ = install_missing_packages(packages)
        if not ok:
            exit_code = max(exit_code, 3)

    missing = test_dependencies(packages)
    test_dependency_usage()

    if missing:
        exit_code = max(exit_code, 2)

    if not test_network():
        exit_code = max(exit_code, 5)

    elapsed = time.time() - started
    section("Summary")
    summary = {
        "status": "success" if exit_code == 0 else "failed",
        "exit_code": exit_code,
        "elapsed_seconds": round(elapsed, 3),
        "missing_dependencies": missing,
        "auto_install": env_bool("DAIDAI_TEST_AUTO_INSTALL"),
        "network_test": env_bool("DAIDAI_TEST_NETWORK"),
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2), flush=True)

    if exit_code == 0:
        log("Smoke test passed")
    else:
        err("Smoke test failed. Check logs above for missing runtime, dependency, file I/O, or network details.")

    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
