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

    @Test
    fun `proot command receives guest PATH for env shebangs`() {
        val environment = mutableMapOf("PATH" to "/system/bin")
        AndroidLinuxRuntime.applyGuestEnvironment(listOf("/native/libdaidai_proot.so", "/usr/bin/npm"), environment)
        assertEquals(AndroidLinuxRuntime.GUEST_PATH, environment["PATH"])
    }

    @Test
    fun `direct Android command preserves host PATH`() {
        val environment = mutableMapOf("PATH" to "/system/bin")
        AndroidLinuxRuntime.applyGuestEnvironment(listOf("/system/bin/sh", "node-wrapper.sh"), environment)
        assertEquals("/system/bin", environment["PATH"])
    }

    @Test
    fun `base runtime contract uses packaged proot loader name`() {
        assertEquals("libproot_loader.so", AndroidLinuxRuntime.prootLoaderLibraryName())
    }

    @Test
    fun `proot environment overrides Termux loader path with packaged ELF`() {
        val dir = Files.createTempDirectory("proot-environment-test").toFile()
        val loader = dir.resolve("libproot_loader.so")
        val cache = dir.resolve("cache")

        assertEquals(
            mapOf(
                "PROOT_LOADER" to loader.absolutePath,
                "PROOT_NO_SECCOMP" to "1",
                "PROOT_TMP_DIR" to cache.absolutePath,
                "PROOT_VERBOSE" to "0",
            ),
            AndroidLinuxRuntime.prootEnvironment(loader, cache),
        )
    }

    @Test
    fun `mirror defaults use Tsinghua Aliyun and npmmirror`() {
        val mirrors = AndroidLinuxRuntime.MirrorConfig()

        assertEquals("https://mirrors.tuna.tsinghua.edu.cn/alpine", mirrors.linuxMirror)
        assertEquals("https://mirrors.aliyun.com/pypi/simple", mirrors.pipMirror)
        assertEquals("https://registry.npmmirror.com", mirrors.npmMirror)
    }

    @Test
    fun `mirror URL validation accepts custom and official HTTP sources`() {
        assertEquals("https://pypi.org/simple", AndroidLinuxRuntime.normalizeMirrorUrl(" https://pypi.org/simple/ "))
        assertEquals("http://mirror.example.test:8080/npm", AndroidLinuxRuntime.normalizeMirrorUrl("http://mirror.example.test:8080/npm"))
    }

    @Test
    fun `mirror URL validation rejects unsafe or ambiguous values`() {
        assertEquals(null, AndroidLinuxRuntime.normalizeMirrorUrl("file:///tmp/mirror"))
        assertEquals(null, AndroidLinuxRuntime.normalizeMirrorUrl("https://user:secret@example.test/repo"))
        assertEquals(null, AndroidLinuxRuntime.normalizeMirrorUrl("https://example.test/repo#fragment"))
        assertEquals(null, AndroidLinuxRuntime.normalizeMirrorUrl("https://example.test:70000/repo"))
        assertEquals(null, AndroidLinuxRuntime.normalizeMirrorUrl("https://example.test/repo\nnext"))
    }

    @Test
    fun `mirror initialization preserves saved values and only fills missing values`() {
        assertEquals(
            "https://pypi.org/simple",
            AndroidLinuxRuntime.resolveMirrorValue(
                persisted = "https://pypi.org/simple",
                imported = "https://mirrors.aliyun.com/pypi/simple",
                defaultValue = AndroidLinuxRuntime.PYTHON_PIP_ALIBABA_INDEX,
            ),
        )
        assertEquals(
            AndroidLinuxRuntime.ALPINE_APK_DEFAULT_MIRROR,
            AndroidLinuxRuntime.resolveMirrorValue(null, null, AndroidLinuxRuntime.ALPINE_APK_DEFAULT_MIRROR),
        )
    }

    @Test
    fun `task environment uses one supplied mirror configuration`() {
        val mirrors = AndroidLinuxRuntime.MirrorConfig(
            pipMirror = "https://pypi.org/simple",
            npmMirror = "https://registry.npmjs.org",
            linuxMirror = "https://dl-cdn.alpinelinux.org/alpine",
        )

        assertEquals(
            mapOf(
                "PIP_INDEX_URL" to mirrors.pipMirror,
                "NPM_CONFIG_REGISTRY" to mirrors.npmMirror,
                "npm_config_registry" to mirrors.npmMirror,
                "DAIDAI_LINUX_MIRROR" to mirrors.linuxMirror,
            ),
            AndroidLinuxRuntime.mirrorEnvironment(mirrors),
        )
    }

    @Test
    fun `fallback installers receive configured mirrors as structured arguments`() {
        assertEquals(
            listOf("install", "--no-input", "--no-cache-dir", "-i", "https://pypi.org/simple", "--target", "/deps/python", "--", "requests"),
            AndroidLinuxRuntime.pipInstallArguments("https://pypi.org/simple", "/deps/python", "requests"),
        )
        assertEquals(
            listOf("install", "--no-audit", "--no-fund", "--update-notifier=false", "--registry", "https://registry.npmjs.org", "--cache", "/deps/cache", "--prefix", "/deps/node", "--", "lodash"),
            AndroidLinuxRuntime.npmInstallArguments("https://registry.npmjs.org", "/deps/node", "/deps/cache", "lodash"),
        )
    }

    @Test
    fun `rootfs mirror files use one supplied configuration snapshot`() {
        val root = Files.createTempDirectory("linux-runtime-mirror-test").toFile()
        root.resolve("etc/alpine-release").apply { parentFile.mkdirs(); writeText("3.21.2") }
        val mirrors = AndroidLinuxRuntime.MirrorConfig(
            pipMirror = "https://pypi.org/simple",
            npmMirror = "https://registry.npmjs.org",
            linuxMirror = "https://dl-cdn.alpinelinux.org/alpine",
        )

        AndroidLinuxRuntime.configureRootfsMirrors(root, mirrors)

        assertEquals(
            "https://dl-cdn.alpinelinux.org/alpine/v3.21/main\nhttps://dl-cdn.alpinelinux.org/alpine/v3.21/community\n",
            root.resolve("etc/apk/repositories").readText(),
        )
        assertTrue(root.resolve("etc/pip.conf").readText().contains("index-url = https://pypi.org/simple"))
        assertTrue(root.resolve("etc/npmrc").readText().contains("registry=https://registry.npmjs.org"))
    }
}
