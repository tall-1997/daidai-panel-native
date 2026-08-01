package com.daidai.daidai_app

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test

class AndroidRuntimeMetadataBridgeTest {
    @Test
    fun `metadata replacement publishes complete content and removes staging file`() {
        val directory = Files.createTempDirectory("runtime-metadata-test").toFile()
        val output = directory.resolve("manifest.json").apply { writeText("old") }

        AndroidRuntimeMetadataBridge.copyAtomically(
            input = "new-complete-content".byteInputStream(),
            output = output,
        )

        assertEquals("new-complete-content", output.readText())
        assertFalse(directory.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
    }

    @Test
    fun `failed metadata copy preserves published content`() {
        val directory = Files.createTempDirectory("runtime-metadata-failure-test").toFile()
        val output = directory.resolve("manifest.json").apply { writeText("published") }
        val failingInput = object : java.io.InputStream() {
            override fun read(): Int = throw java.io.IOException("copy failed")
        }

        assertThrows(java.io.IOException::class.java) {
            AndroidRuntimeMetadataBridge.copyAtomically(failingInput, output)
        }

        assertEquals("published", output.readText())
        assertFalse(directory.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
    }
}
