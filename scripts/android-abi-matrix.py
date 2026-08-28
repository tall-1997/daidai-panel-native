#!/usr/bin/env python3
"""Query and validate the Android runtime ABI contract."""

import argparse
import json
import pathlib

DEFAULT_MATRIX = pathlib.Path(__file__).resolve().parents[1] / "runtime" / "android-abi-matrix.json"
BOOLEAN_CAPABILITIES = ("package", "native", "rootfs", "yaegi", "smoke", "release")
REQUIRED_FIELDS = (
    *BOOLEAN_CAPABILITIES,
    "flutter_target",
    "release_suffix",
    "ndk_triple",
    "goarch",
    "elf_machine",
    "ubuntu_arch",
    "ubuntu_mirror",
    "qemu_static",
    "minimum_load_alignment",
)


def load_matrix(path: pathlib.Path) -> dict:
    matrix = json.loads(path.read_text(encoding="utf-8"))
    assert matrix.get("schema_version") == 1, "ABI matrix schema_version must be 1"
    abis = matrix.get("abis")
    assert isinstance(abis, dict) and abis, "ABI matrix must contain ABIs"
    assert matrix.get("default_abi") in abis, "default ABI is missing from matrix"
    suffixes = set()
    targets = set()
    for abi, config in abis.items():
        assert isinstance(config, dict), f"invalid ABI configuration: {abi}"
        missing = set(REQUIRED_FIELDS) - set(config)
        assert not missing, f"ABI {abi} is missing fields: {sorted(missing)}"
        assert all(isinstance(config[name], bool) for name in BOOLEAN_CAPABILITIES), f"ABI {abi} has invalid capabilities"
        assert config["minimum_load_alignment"] >= 16384, f"ABI {abi} alignment is below 16 KB"
        assert config["elf_machine"] > 0, f"ABI {abi} has invalid ELF machine"
        assert config["release_suffix"] not in suffixes, f"duplicate release suffix: {config['release_suffix']}"
        assert config["flutter_target"] not in targets, f"duplicate Flutter target: {config['flutter_target']}"
        assert not config["release"] or all(config[name] for name in ("package", "native", "rootfs", "yaegi")), f"release ABI {abi} is incomplete"
        suffixes.add(config["release_suffix"])
        targets.add(config["flutter_target"])
    return matrix


def resolve_abis(matrix: dict, capability: str, requested: str | None = None) -> list[str]:
    assert capability in BOOLEAN_CAPABILITIES, f"unknown ABI capability: {capability}"
    candidates = requested.replace(",", " ").split() if requested else matrix["abis"].keys()
    result = []
    for abi in candidates:
        assert abi in matrix["abis"], f"unknown ABI: {abi}"
        assert matrix["abis"][abi][capability], f"ABI {abi} does not support {capability}"
        if abi not in result:
            result.append(abi)
    assert result, f"no ABI supports {capability}"
    return result


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--matrix", type=pathlib.Path, default=DEFAULT_MATRIX)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("validate")
    list_parser = subparsers.add_parser("list")
    list_parser.add_argument("capability", choices=BOOLEAN_CAPABILITIES)
    list_parser.add_argument("--requested")
    get_parser = subparsers.add_parser("get")
    get_parser.add_argument("abi")
    get_parser.add_argument("field", choices=REQUIRED_FIELDS)
    args = parser.parse_args()
    matrix = load_matrix(args.matrix)
    if args.command == "validate":
        return
    if args.command == "list":
        print(" ".join(resolve_abis(matrix, args.capability, args.requested)))
        return
    assert args.abi in matrix["abis"], f"unknown ABI: {args.abi}"
    value = matrix["abis"][args.abi][args.field]
    print(str(value).lower() if isinstance(value, bool) else value)


if __name__ == "__main__":
    main()
