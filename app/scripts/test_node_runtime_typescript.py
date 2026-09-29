import json
import pathlib
import unittest
import zipfile

ARCHIVE = pathlib.Path(__file__).resolve().parents[1] / "android/app/src/main/assets/node-runtime/18.20.4/node-runtime.zip"
PREFIX = "lib/node_modules/typescript/"


class NodeRuntimeTypeScriptTest(unittest.TestCase):
    def test_packaged_typescript_supports_cjs_transpile(self):
        with zipfile.ZipFile(ARCHIVE) as archive:
            package = json.loads(archive.read(PREFIX + "package.json"))
            compiler = archive.read(PREFIX + "lib/typescript.js").decode("utf-8")
            self.assertTrue(any(name.startswith("lib/node_modules/npm/") for name in archive.namelist()))
        self.assertEqual(package["version"], "5.6.3")
        self.assertEqual(package["main"], "./lib/typescript.js")
        self.assertIn("transpileModule", compiler)
        self.assertIn("ModuleKind", compiler)
