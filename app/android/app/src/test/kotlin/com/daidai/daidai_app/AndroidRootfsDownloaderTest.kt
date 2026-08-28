package com.daidai.daidai_app

import java.io.File
import java.nio.file.Files
import java.security.MessageDigest
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.zip.GZIPOutputStream
import org.apache.commons.compress.archivers.tar.TarArchiveEntry
import org.apache.commons.compress.archivers.tar.TarArchiveOutputStream
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.Assert.assertThrows

class AndroidRootfsDownloaderTest {
    @After
    fun clearValidationCache() {
        AndroidRootfsDownloader.archiveValidationObserver = null
        AndroidRootfsDownloader.clearTrustedArchiveValidationCache()
    }

    @Test
    fun `partial response uses complete Content-Range length`() {
        assertEquals(1_000L, AndroidRootfsDownloader.responseTotalBytes(206, 400, 600, "bytes 400-999/1000", -1))
    }

    @Test(expected = java.io.IOException::class)
    fun `partial response rejects a mismatched range start`() {
        AndroidRootfsDownloader.responseTotalBytes(206, 400, 600, "bytes 0-599/1000", -1)
    }

    @Test(expected = java.io.IOException::class)
    fun `partial response rejects body length inconsistent with range`() {
        AndroidRootfsDownloader.responseTotalBytes(206, 400, 599, "bytes 400-999/1000", -1)
    }

    @Test
    fun `download admission is atomic`() {
        AndroidRootfsDownloader.finishDownload()
        assertTrue(AndroidRootfsDownloader.tryStartDownload())
        assertFalse(AndroidRootfsDownloader.tryStartDownload())
        AndroidRootfsDownloader.finishDownload()
    }

    @Test
    fun `published checksum parser selects the exact Ubuntu image`() {
        val expected = "a".repeat(64)
        val checksum = AndroidRootfsDownloader.parsePublishedChecksum(
            sequenceOf("${"b".repeat(64)}  other.tar.gz", "$expected *ubuntu-base.tar.gz"),
            "ubuntu-base.tar.gz",
        )

        assertEquals(expected, checksum)
    }

    @Test
    fun `publisher checksum is mandatory and must match`() {
        AndroidRootfsDownloader.requirePublisherChecksum("a".repeat(64), "A".repeat(64))
        assertThrows(java.io.IOException::class.java) {
            AndroidRootfsDownloader.requirePublisherChecksum("a".repeat(64), "b".repeat(64))
        }
    }

    @Test
    fun `trusted archive requires matching checksum and rootfs tar structure`() {
        val archive = Files.createTempFile("rootfs", ".tar.gz").toFile()
        writeRootfsTar(archive)
        val digest = MessageDigest.getInstance("SHA-256").digest(archive.readBytes()).joinToString("") { "%02x".format(it) }

        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive, digest))
        archive.appendText("corrupt")
        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive, digest))
    }

    @Test
    fun `matching checksum cannot make a non-rootfs tar trusted`() {
        val archive = Files.createTempFile("invalid-rootfs", ".tar.gz").toFile()
        GZIPOutputStream(archive.outputStream()).use { gzip ->
            TarArchiveOutputStream(gzip).use { tar ->
                val data = "fixture".toByteArray()
                tar.putArchiveEntry(TarArchiveEntry("README").apply { size = data.size.toLong() })
                tar.write(data)
                tar.closeArchiveEntry()
                tar.finish()
            }
        }
        val digest = MessageDigest.getInstance("SHA-256").digest(archive.readBytes()).joinToString("") { "%02x".format(it) }

        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive, digest))
    }

    @Test
    fun `trusted archive validation reuses unchanged metadata`() {
        val archive = validArchive()
        val validations = AtomicInteger()
        AndroidRootfsDownloader.archiveValidationObserver = { validations.incrementAndGet() }

        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))
        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))

        assertEquals(1, validations.get())
    }

    @Test
    fun `trusted archive validation retries after metadata changes`() {
        val archive = validArchive()
        val validations = AtomicInteger()
        AndroidRootfsDownloader.archiveValidationObserver = { validations.incrementAndGet() }
        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))

        archive.file.appendText("corrupt")

        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))
        assertEquals(2, validations.get())
    }

    @Test
    fun `trusted archive validation retries after explicit invalidation`() {
        val archive = validArchive()
        val validations = AtomicInteger()
        AndroidRootfsDownloader.archiveValidationObserver = { validations.incrementAndGet() }
        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))

        AndroidRootfsDownloader.invalidateTrustedArchive(archive.file)

        assertTrue(AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest))
        assertEquals(2, validations.get())
    }

    @Test
    fun `concurrent trusted archive validation is single flight`() {
        val archive = validArchive()
        val validations = AtomicInteger()
        val validatorEntered = CountDownLatch(1)
        val releaseValidator = CountDownLatch(1)
        AndroidRootfsDownloader.archiveValidationObserver = {
            validations.incrementAndGet()
            validatorEntered.countDown()
            releaseValidator.await(5, TimeUnit.SECONDS)
        }
        val executor = Executors.newFixedThreadPool(8)
        try {
            val start = CountDownLatch(1)
            val futures = (1..8).map {
                executor.submit<Boolean> {
                    start.await()
                    AndroidRootfsDownloader.isTrustedArchive(archive.file, archive.digest)
                }
            }

            start.countDown()
            assertTrue(validatorEntered.await(5, TimeUnit.SECONDS))
            releaseValidator.countDown()

            assertTrue(futures.all { it.get(5, TimeUnit.SECONDS) })
            assertEquals(1, validations.get())
        } finally {
            releaseValidator.countDown()
            executor.shutdownNow()
        }
    }

    @Test
    fun `failed validation is cached until file metadata changes`() {
        val archive = Files.createTempFile("invalid-rootfs-cache", ".tar.gz").toFile().apply { writeText("invalid") }
        val validations = AtomicInteger()
        AndroidRootfsDownloader.archiveValidationObserver = { validations.incrementAndGet() }
        val digest = "a".repeat(64)

        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive, digest))
        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive, digest))
        archive.appendText("changed")
        assertFalse(AndroidRootfsDownloader.isTrustedArchive(archive, digest))

        assertEquals(2, validations.get())
    }

    private data class ValidArchive(val file: File, val digest: String)

    private fun validArchive(): ValidArchive {
        val file = Files.createTempFile("rootfs-cache", ".tar.gz").toFile()
        writeRootfsTar(file)
        val digest = MessageDigest.getInstance("SHA-256").digest(file.readBytes()).joinToString("") { "%02x".format(it) }
        return ValidArchive(file, digest)
    }

    private fun writeRootfsTar(output: File) {
        GZIPOutputStream(output.outputStream()).use { gzip ->
            TarArchiveOutputStream(gzip).use { tar ->
                listOf("etc/os-release", "bin/sh").forEach { name ->
                    val data = "fixture".toByteArray()
                    tar.putArchiveEntry(TarArchiveEntry(name).apply { size = data.size.toLong(); mode = 0b111101101 })
                    tar.write(data)
                    tar.closeArchiveEntry()
                }
                tar.finish()
            }
        }
    }
}
