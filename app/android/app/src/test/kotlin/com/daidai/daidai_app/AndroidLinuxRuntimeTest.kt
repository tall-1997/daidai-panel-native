package com.daidai.daidai_app

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AndroidLinuxRuntimeTest {
    @Test
    fun `copyVersionedLibraries creates missing compatibility aliases`() {
        val dir = Files.createTempDirectory("linux-runtime-test").toFile()
        val nativeDir = dir.resolve("native").apply { mkdirs() }
        val compatDir = dir.resolve("compat")
        nativeDir.resolve("libssl_v3.so").writeText("ssl")

        AndroidLinuxRuntime.copyVersionedLibraries(
            nativeDir = nativeDir,
            compatLibDir = compatDir,
            links = mapOf("libssl_v3.so" to listOf("libssl.so.3")),
        )

        assertEquals("ssl", compatDir.resolve("libssl.so.3").readText())
    }

    @Test
    fun `copyVersionedLibraries preserves existing aliases`() {
        val dir = Files.createTempDirectory("linux-runtime-preserve-test").toFile()
        val nativeDir = dir.resolve("native").apply { mkdirs() }
        val compatDir = dir.resolve("compat").apply { mkdirs() }
        nativeDir.resolve("libcrypto_v3.so").writeText("new")
        compatDir.resolve("libcrypto.so.3").writeText("existing")

        AndroidLinuxRuntime.copyVersionedLibraries(
            nativeDir = nativeDir,
            compatLibDir = compatDir,
            links = mapOf("libcrypto_v3.so" to listOf("libcrypto.so.3")),
        )

        assertEquals("existing", compatDir.resolve("libcrypto.so.3").readText())
    }

    @Test
    fun `writeShellWrapper exports runtime environment and execs launcher`() {
        val dir = Files.createTempDirectory("linux-runtime-wrapper-test").toFile()
        val launcher = dir.resolve("launcher.so")
        val wrapper = dir.resolve("wrapper.sh")

        AndroidLinuxRuntime.writeShellWrapper(
            output = wrapper,
            env = mapOf("HOME" to "/data/home", "DAIDAI_RUNTIME_LANGUAGE" to "node"),
            executable = launcher,
        )

        val text = wrapper.readText()
        assertTrue(text.contains("export HOME=\"/data/home\""))
        assertTrue(text.contains("export DAIDAI_RUNTIME_LANGUAGE=\"node\""))
        assertTrue(text.contains("exec \"${launcher.absolutePath}\" \"\$@\""))
        assertFalse(text.contains("RUNTIME_STUB_OK"))
    }

    @Test
    fun `system package script supports apk apt yum and dnf`() {
        assertEquals("apk update; apk add --no-cache curl", AndroidLinuxRuntime.packageInstallScript("apk", "curl"))
        assertEquals("export DEBIAN_FRONTEND=noninteractive; apt-get update; apt-get install -y curl", AndroidLinuxRuntime.packageInstallScript("apt", "curl"))
        assertEquals("yum install -y curl", AndroidLinuxRuntime.packageInstallScript("yum", "curl"))
        assertEquals("dnf install -y curl", AndroidLinuxRuntime.packageInstallScript("dnf", "curl"))
    }

    @Test
    fun `system package spec rejects shell metacharacters`() {
        assertTrue(AndroidLinuxRuntime.isSafeSystemPackageSpec("python3-dev"))
        assertTrue(AndroidLinuxRuntime.isSafeSystemPackageSpec("libssl3.0"))
        assertFalse(AndroidLinuxRuntime.isSafeSystemPackageSpec("curl;reboot"))
        assertFalse(AndroidLinuxRuntime.isSafeSystemPackageSpec("../escape"))
        assertFalse(AndroidLinuxRuntime.isSafeSystemPackageSpec("$(id)"))
    }

    @Test
    fun `proot compatibility flags include android safe defaults`() {
        val flags = AndroidLinuxRuntime.prootCompatibilityFlags()
        assertTrue(flags.contains("--link2symlink"))
        assertTrue(flags.contains("--kill-on-exit"))
        assertTrue(flags.contains("-0"))
        assertTrue(flags.windowed(2).any { it[0] == "-k" && it[1] == "4.14.0" })
    }
}
