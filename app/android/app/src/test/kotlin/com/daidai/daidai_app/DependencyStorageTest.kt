package com.daidai.daidai_app

import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class DependencyStorageTest {
    @Test
    fun `same normalized dependency installs once across callers`() {
        val installs = AtomicInteger()
        val installed = AtomicInteger()
        val start = CountDownLatch(1)
        val pool = Executors.newFixedThreadPool(6)
        val futures = (0 until 6).map { index ->
            pool.submit<Boolean> {
                start.await()
                DependencyStorage.withInstallLock("python", if (index % 2 == 0) "Requests" else "requests", "3.14") {
                    if (installed.get() == 1) true else {
                        installs.incrementAndGet()
                        installed.set(1)
                        true
                    }
                }
            }
        }
        start.countDown()
        futures.forEach { assertTrue(it.get()) }
        pool.shutdownNow()

        assertEquals(1, installs.get())
    }

    @Test
    fun `python dependencies are shared outside reconstructed runtime`() {
        val files = Files.createTempDirectory("dependency-storage-runtime").toFile()
        val dependency = DependencyStorage.pythonSitePackages(files).resolve("shared.py").apply {
            parentFile.mkdirs()
            writeText("shared")
        }
        files.resolve("runtimes/python-3.14").apply { mkdirs(); deleteRecursively(); mkdirs() }

        assertTrue(dependency.isFile)
        assertFalse(dependency.path.contains("/runtimes/"))
        assertEquals(dependency.parentFile, DependencyStorage.pythonSitePackages(files))
    }

    @Test
    fun `cache trimming keeps newest files within byte boundary`() {
        val cache = Files.createTempDirectory("dependency-storage-cache").toFile()
        val old = cache.resolve("old.bin").apply { writeBytes(ByteArray(8)); setLastModified(1) }
        val recent = cache.resolve("recent.bin").apply { writeBytes(ByteArray(8)); setLastModified(2) }

        assertEquals(8, DependencyStorage.trimDirectory(cache, 8))
        assertFalse(old.exists())
        assertTrue(recent.exists())
    }

    @Test
    fun `explicit versions only skip exact installed version`() {
        assertTrue(DependencyStorage.satisfies("python", "requests", "2.32.0"))
        assertTrue(DependencyStorage.satisfies("python", "requests==2.32.0", "2.32.0"))
        assertFalse(DependencyStorage.satisfies("python", "requests==2.31.0", "2.32.0"))
        assertFalse(DependencyStorage.satisfies("python", "requests>=2.31.0", "2.32.0"))
        assertTrue(DependencyStorage.satisfies("nodejs", "lodash@4.17.21", "4.17.21"))
    }

    @Test
    fun `python extras share canonical package identity and options are rejected`() {
        assertEquals("requests", DependencyStorage.normalizedName("python", "Requests[security]==2.32.0"))
        assertEquals("@scope/pkg", DependencyStorage.normalizedName("nodejs", "@scope/pkg@1.2.3"))
        assertThrows(IllegalArgumentException::class.java) {
            DependencyStorage.normalizedName("python", "--pre")
        }
    }
}
