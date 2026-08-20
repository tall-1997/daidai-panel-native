package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import kotlin.io.path.createTempDirectory

class ScriptCompatibilityTest {
    @Test fun mapsOnlyKnownTopLevelPythonImports() {
        val scan = ScriptCompatibility.scanPython("import os, yaml\nfrom bs4 import BeautifulSoup\nimport totally_unknown\n", File("."))
        assertEquals(setOf("pyyaml", "beautifulsoup4"), scan.pythonPackages)
    }

    @Test fun identifiesNodePackagesAndMissingRelativeFiles() {
        val dir = createTempDirectory("compat-").toFile()
        try {
            File(dir, "exists.js").writeText("module.exports={}")
            val scan = ScriptCompatibility.scanNode("require('fs'); require('axios'); require('./exists'); require('./missing')", dir)
            assertEquals(setOf("axios"), scan.nodePackages)
            assertEquals(setOf("./missing"), scan.missingCompanionFiles)
        } finally { dir.deleteRecursively() }
    }

    @Test fun ignoresDynamicAndUnknownRequires() {
        val scan = ScriptCompatibility.scanNode("require(name); require('not-an-allowlisted-package')", File("."))
        assertTrue(scan.nodePackages.isEmpty())
    }
}
