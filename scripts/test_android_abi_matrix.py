import importlib.util
import json
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("android-abi-matrix.py")
SPEC = importlib.util.spec_from_file_location("android_abi_matrix", SCRIPT)
abi_matrix = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(abi_matrix)


class AndroidAbiMatrixTest(unittest.TestCase):
    def test_release_matrix_contains_arm64_and_x86_64(self):
        matrix = abi_matrix.load_matrix(abi_matrix.DEFAULT_MATRIX)
        self.assertEqual(["arm64-v8a", "x86_64"], abi_matrix.resolve_abis(matrix, "release"))
        self.assertEqual("android-x64", matrix["abis"]["x86_64"]["flutter_target"])
        self.assertEqual(62, matrix["abis"]["x86_64"]["elf_machine"])

    def test_rejects_unknown_requested_abi(self):
        matrix = abi_matrix.load_matrix(abi_matrix.DEFAULT_MATRIX)
        with self.assertRaisesRegex(AssertionError, "unknown ABI"):
            abi_matrix.resolve_abis(matrix, "package", "x86")

    def test_rejects_incomplete_release_abi(self):
        matrix = json.loads(abi_matrix.DEFAULT_MATRIX.read_text(encoding="utf-8"))
        matrix["abis"]["x86_64"]["rootfs"] = False
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "matrix.json"
            path.write_text(json.dumps(matrix), encoding="utf-8")
            with self.assertRaisesRegex(AssertionError, "release ABI x86_64 is incomplete"):
                abi_matrix.load_matrix(path)


if __name__ == "__main__":
    unittest.main()
