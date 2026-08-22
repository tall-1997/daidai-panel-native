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
        assertTrue(DependencyStorage.satisfies("nodejs", "got@11", "11.8.6"))
        assertFalse(DependencyStorage.satisfies("nodejs", "got@11", "12.0.0"))
    }

    @Test
    fun `python extras share canonical package identity and options are rejected`() {
        assertEquals("requests", DependencyStorage.normalizedName("python", "Requests[security]==2.32.0"))
        assertEquals("@scope/pkg", DependencyStorage.normalizedName("nodejs", "@scope/pkg@1.2.3"))
        assertThrows(IllegalArgumentException::class.java) {
            DependencyStorage.normalizedName("python", "--pre")
        }
    }

    @Test
    fun `node install specs pin commonjs compatible defaults`() {
        assertEquals("got@11.8.6", DependencyStorage.nodeInstallPackageSpec("got"))
        assertEquals("dotenv@16.4.5", DependencyStorage.nodeInstallPackageSpec("dotenv"))
        assertEquals("iconv-lite@0.6.3", DependencyStorage.nodeInstallPackageSpec("iconv-lite"))
        assertEquals("tough-cookie@4.1.4", DependencyStorage.nodeInstallPackageSpec("tough-cookie"))
        assertEquals("got@11", DependencyStorage.nodeInstallPackageSpec("got@11"))
        assertEquals("@scope/pkg@1.2.3", DependencyStorage.nodeInstallPackageSpec("@scope/pkg@1.2.3"))
    }

    @Test
    fun `node dependency manifest is created and broken lock is quarantined`() {
        val dir = Files.createTempDirectory("dependency-storage-node").toFile()
        dir.resolve("package-lock.json").writeText("}")

        DependencyStorage.ensureNodePackageManifest(dir)

        assertTrue(dir.resolve("package.json").readText().contains("daidai-android-node-deps"))
        assertFalse(dir.resolve("package-lock.json").exists())
        assertTrue(dir.listFiles().orEmpty().any { it.name.startsWith("package-lock.json.broken-") })
    }
}
