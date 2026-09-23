#!/usr/bin/env python3
import argparse
import json
import re
import sys
from pathlib import Path


SEMVER_PATTERN = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
PUBSPEC_VERSION_PATTERN = re.compile(r"(?m)^version:[ \t]*([^\s]+)[ \t]*$")
GO_VERSION_PATTERN = re.compile(r'(?m)^var Version = "([^"]+)"[ \t]*$')


class VersionError(Exception):
    pass


def android_version_code(version):
    match = SEMVER_PATTERN.fullmatch(version)
    if match is None:
        raise VersionError(f"version must be stable SemVer MAJOR.MINOR.PATCH: {version!r}")
    major, minor, patch = (int(part) for part in match.groups())
    if minor > 99 or patch > 99:
        raise VersionError("minor and patch versions must each be at most 99")
    code = major * 1_000_000 + minor * 10_000 + patch * 10
    if code < 1 or code > 2_100_000_000:
        raise VersionError(f"derived Android versionCode is out of range: {code}")
    return code


def read_json(path):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise VersionError(f"cannot read {path}: {error}") from error


def write_json(path, value):
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def read_source(root):
    path = root / "VERSION.json"
    source = read_json(path)
    if not isinstance(source, dict) or source.get("schemaVersion") != 1:
        raise VersionError("VERSION.json schemaVersion must be 1")
    version = source.get("version")
    version_code = source.get("androidVersionCode")
    if not isinstance(version, str):
        raise VersionError("VERSION.json version must be a string")
    expected_code = android_version_code(version)
    if type(version_code) is not int or version_code != expected_code:
        raise VersionError(
            "VERSION.json androidVersionCode must be "
            f"{expected_code} for version {version}"
        )
    return version, version_code


def expected_files(root, manifest_paths):
    return [
        ("pubspec", root / "app" / "pubspec.yaml"),
        ("package", root / "panel" / "web" / "package.json"),
        ("package_lock", root / "panel" / "web" / "package-lock.json"),
        ("go_version", root / "panel" / "server" / "handler" / "version.go"),
        *[("manifest", path) for path in manifest_paths],
    ]


def resolve_manifest_paths(root, values):
    paths = []
    for value in values:
        path = Path(value)
        paths.append(path if path.is_absolute() else root / path)
    return paths


def sync_pubspec(path, version, version_code):
    try:
        content = path.read_text(encoding="utf-8")
    except OSError as error:
        raise VersionError(f"cannot read {path}: {error}") from error
    replacement = f"version: {version}+{version_code}"
    updated, count = PUBSPEC_VERSION_PATTERN.subn(replacement, content)
    if count != 1:
        raise VersionError(f"expected exactly one version field in {path}")
    path.write_text(updated, encoding="utf-8")


def sync_package(path, version):
    package = read_json(path)
    if not isinstance(package, dict) or "version" not in package:
        raise VersionError(f"missing version field in {path}")
    package["version"] = version
    write_json(path, package)


def sync_package_lock(path, version):
    package_lock = read_json(path)
    if not isinstance(package_lock, dict) or "version" not in package_lock:
        raise VersionError(f"missing version field in {path}")
    root_package = package_lock.get("packages", {}).get("")
    if not isinstance(root_package, dict) or "version" not in root_package:
        raise VersionError(f"missing root package version field in {path}")
    package_lock["version"] = version
    root_package["version"] = version
    write_json(path, package_lock)


def sync_go_version(path, version):
    try:
        content = path.read_text(encoding="utf-8")
    except OSError as error:
        raise VersionError(f"cannot read {path}: {error}") from error
    updated, count = GO_VERSION_PATTERN.subn(f'var Version = "{version}"', content)
    if count != 1:
        raise VersionError(f"expected exactly one Go Version variable in {path}")
    path.write_text(updated, encoding="utf-8")


def sync_manifest(path, version, version_code):
    manifest = read_json(path)
    if not isinstance(manifest, dict):
        raise VersionError(f"update manifest must be a JSON object: {path}")
    if "version" not in manifest or "versionCode" not in manifest:
        raise VersionError(f"missing version or versionCode field in {path}")
    manifest["version"] = version
    manifest["versionCode"] = version_code
    write_json(path, manifest)


def check_files(root, manifest_paths, version, version_code):
    errors = []
    for kind, path in expected_files(root, manifest_paths):
        try:
            if kind == "pubspec":
                content = path.read_text(encoding="utf-8")
                match = PUBSPEC_VERSION_PATTERN.search(content)
                actual = match.group(1) if match else "<missing>"
                expected = f"{version}+{version_code}"
                if actual != expected:
                    errors.append(f"{path}: expected version {expected}, found {actual}")
            elif kind == "go_version":
                content = path.read_text(encoding="utf-8")
                match = GO_VERSION_PATTERN.search(content)
                actual = match.group(1) if match else "<missing>"
                if actual != version:
                    errors.append(f"{path}: expected Version {version}, found {actual}")
            else:
                value = read_json(path)
                expected = {"version": version}
                if kind == "manifest":
                    expected["versionCode"] = version_code
                for field, expected_value in expected.items():
                    actual = value.get(field) if isinstance(value, dict) else None
                    if actual != expected_value:
                        errors.append(
                            f"{path}: expected {field} {expected_value!r}, found {actual!r}"
                        )
                if kind == "package_lock":
                    root_package = value.get("packages", {}).get("") if isinstance(value, dict) else None
                    actual = root_package.get("version") if isinstance(root_package, dict) else None
                    if actual != version:
                        errors.append(
                            f"{path}: expected root package version {version!r}, found {actual!r}"
                        )
        except (OSError, VersionError) as error:
            errors.append(str(error))
    if errors:
        raise VersionError("version drift detected:\n" + "\n".join(errors))


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Synchronize and validate release versions from VERSION.json."
    )
    parser.add_argument("action", choices=("check", "sync", "show"))
    parser.add_argument(
        "--root",
        type=Path,
        default=Path(__file__).resolve().parents[1],
        help=argparse.SUPPRESS,
    )
    parser.add_argument(
        "--manifest",
        action="append",
        default=[],
        metavar="PATH",
        help="also synchronize or validate an APK update manifest; repeatable",
    )
    parser.add_argument(
        "--format",
        choices=("json", "shell"),
        default="json",
        help="output format for show",
    )
    args = parser.parse_args(argv)

    try:
        root = args.root.resolve()
        version, version_code = read_source(root)
        manifests = resolve_manifest_paths(root, args.manifest)
        if args.action == "sync":
            sync_pubspec(root / "app" / "pubspec.yaml", version, version_code)
            sync_package(root / "panel" / "web" / "package.json", version)
            sync_package_lock(root / "panel" / "web" / "package-lock.json", version)
            sync_go_version(root / "panel" / "server" / "handler" / "version.go", version)
            for path in manifests:
                sync_manifest(path, version, version_code)
            check_files(root, manifests, version, version_code)
            print(f"synchronized version {version} ({version_code})")
        elif args.action == "check":
            check_files(root, manifests, version, version_code)
            print(f"version {version} ({version_code}) is synchronized")
        elif args.format == "shell":
            print(f"VERSION={version}")
            print(f"ANDROID_VERSION_CODE={version_code}")
            print(f"FLUTTER_BUILD_NAME={version}")
            print(f"FLUTTER_BUILD_NUMBER={version_code}")
        else:
            print(
                json.dumps(
                    {
                        "version": version,
                        "androidVersionCode": version_code,
                        "flutterBuildName": version,
                        "flutterBuildNumber": version_code,
                    },
                    separators=(",", ":"),
                )
            )
    except VersionError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
