package com.daidai.daidai_app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.file.Files

class PortableBackupEnvelopeTest {
    @Test
    fun `export writes encrypted manifest with hashes and restores files atomically`() {
        val workspace = Files.createTempDirectory("pbe-test").toFile()
        val source = File(workspace, "source").apply { mkdirs() }
        File(source, "db.sqlite").writeText("schema-v1")
        File(source, "nested/runtime.json").apply {
            parentFile.mkdirs()
            writeText("runtime-ok")
        }
        val envelope = PortableBackupEnvelope().exportDirectory(
            source,
            "correct horse battery staple".toCharArray(),
            JSONObject().put("schemaVersion", 1).put("runtimeManifest", "manifest.json"),
        )

        val manifest = PortableBackupEnvelope.readManifest(envelope)
        assertEquals("AES-256-GCM", manifest.getJSONObject("archive").getString("cipher"))
        assertEquals("PBKDF2-HMAC-SHA256", manifest.getJSONObject("keyWrap").getJSONObject("kdf").getString("name"))
        assertEquals(2, manifest.getJSONArray("files").length())
        assertFalse(String(envelope).contains("schema-v1"))

        val target = File(workspace, "target").apply { mkdirs() }
        File(target, "db.sqlite").writeText("old-data")
        PortableBackupEnvelope().restoreDirectory(envelope, "correct horse battery staple".toCharArray(), target)

        assertEquals("schema-v1", File(target, "db.sqlite").readText())
        assertEquals("runtime-ok", File(target, "nested/runtime.json").readText())
    }

    @Test
    fun `wrong password is rejected before target data changes`() {
        val workspace = Files.createTempDirectory("pbe-wrong-password").toFile()
        val source = File(workspace, "source").apply { mkdirs() }
        File(source, "db.sqlite").writeText("new-data")
        val envelope = PortableBackupEnvelope().exportDirectory(source, "right-password".toCharArray())
        val target = File(workspace, "target").apply { mkdirs() }
        File(target, "db.sqlite").writeText("old-data")

        try {
            PortableBackupEnvelope().restoreDirectory(envelope, "wrong-password".toCharArray(), target)
        } catch (error: WrongBackupPasswordException) {
            assertEquals("old-data", File(target, "db.sqlite").readText())
            return
        }
        throw AssertionError("wrong password should fail")
    }

    @Test
    fun `tampered archive is rejected by authenticated encryption`() {
        val workspace = Files.createTempDirectory("pbe-tamper").toFile()
        val source = File(workspace, "source").apply { mkdirs() }
        File(source, "db.sqlite").writeText("new-data")
        val envelope = PortableBackupEnvelope().exportDirectory(source, "password".toCharArray())
        envelope[envelope.lastIndex] = (envelope.last().toInt() xor 0x01).toByte()

        try {
            PortableBackupEnvelope().restoreDirectory(envelope, "password".toCharArray(), File(workspace, "target"))
        } catch (error: PortableBackupException) {
            assertNotEquals("", error.message.orEmpty())
            return
        }
        throw AssertionError("tampered archive should fail")
    }
}
