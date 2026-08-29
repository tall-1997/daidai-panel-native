package com.daidai.daidai_app

import java.nio.file.Files
import java.nio.file.LinkOption
import java.io.IOException
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
        assertTrue(flags.contains("--kill-on-exit"))
        assertTrue(flags.contains("-0"))
        assertTrue(flags.windowed(2).any { it[0] == "-k" && it[1] == "4.14.0" })
    }

    @Test
    fun `proot command receives guest PATH for env shebangs`() {
        val environment = mutableMapOf("PATH" to "/system/bin")
        AndroidLinuxRuntime.applyGuestEnvironment(listOf("/native/libdaidai_proot.so", "/usr/bin/npm"), environment)
        assertEquals(AndroidLinuxRuntime.GUEST_PATH, environment["PATH"])
        assertEquals("/host-files", environment["HOME"])
        assertEquals("/tmp/host-cache", environment["TMPDIR"])
        assertEquals("/workspace", environment["PWD"])
    }

    @Test
    fun `direct Android command preserves host PATH`() {
        val environment = mutableMapOf(
            "PATH" to "/system/bin",
            "HOME" to "/data/user/0/app/files",
            "TMPDIR" to "/data/user/0/app/cache",
            "PWD" to "/data/user/0/app/files/workspace",
        )
        AndroidLinuxRuntime.applyGuestEnvironment(listOf("/system/bin/sh", "node-wrapper.sh"), environment)
        assertEquals("/system/bin", environment["PATH"])
        assertEquals("/data/user/0/app/files", environment["HOME"])
        assertEquals("/data/user/0/app/cache", environment["TMPDIR"])
        assertEquals("/data/user/0/app/files/workspace", environment["PWD"])
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

        val environment = AndroidLinuxRuntime.prootEnvironment(loader, cache)
        assertEquals(loader.absolutePath, environment["PROOT_LOADER"])
        assertEquals(cache.absolutePath, environment["PROOT_TMP_DIR"])
        assertEquals("0", environment["PROOT_VERBOSE"])
        assertFalse(environment.containsKey("PROOT_NO_SECCOMP"))
    }

    @Test
    fun `x86 node runtime patches native UTF-8 slicing while arm64 stays native`() {
        val options = AndroidLinuxRuntime.nodeRuntimeOptions("x86_64")
        assertEquals(AndroidLinuxRuntime.X86_NODE_UTF8_COMPAT, options)
        assertTrue(options!!.startsWith("--import=data:text/javascript,"))
        assertTrue(options.contains("Buffer.prototype.utf8Slice"))
        assertTrue(options.contains("TextDecoder"))
        assertEquals(null, AndroidLinuxRuntime.nodeRuntimeOptions("arm64-v8a"))
    }

    @Test
    fun `mirror defaults use Aliyun Ubuntu and npmmirror`() {
        val mirrors = AndroidLinuxRuntime.MirrorConfig()

        assertEquals("https://mirrors.aliyun.com/ubuntu-ports", mirrors.linuxMirror)
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
            AndroidLinuxRuntime.UBUNTU_APT_DEFAULT_MIRROR,
            AndroidLinuxRuntime.resolveMirrorValue(null, null, AndroidLinuxRuntime.UBUNTU_APT_DEFAULT_MIRROR),
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
        root.resolve("etc/os-release").apply { parentFile.mkdirs(); writeText("NAME=\"Ubuntu\"\n") }
        root.resolve("etc/lsb-release").apply { writeText("DISTRIB_CODENAME=noble\n") }
        val mirrors = AndroidLinuxRuntime.MirrorConfig(
            pipMirror = "https://pypi.org/simple",
            npmMirror = "https://registry.npmjs.org",
            linuxMirror = "https://mirrors.aliyun.com/ubuntu",
        )

        AndroidLinuxRuntime.configureRootfsMirrors(root, mirrors)

        assertEquals(
            "deb https://mirrors.aliyun.com/ubuntu/ noble main restricted universe multiverse\n" +
                "deb https://mirrors.aliyun.com/ubuntu/ noble-updates main restricted universe multiverse\n" +
                "deb https://mirrors.aliyun.com/ubuntu/ noble-security main restricted universe multiverse\n",
            root.resolve("etc/apt/sources.list").readText(),
        )
        assertTrue(root.resolve("etc/pip.conf").readText().contains("index-url = https://pypi.org/simple"))
        assertTrue(root.resolve("etc/npmrc").readText().contains("registry=https://registry.npmjs.org"))

        AndroidLinuxRuntime.configureRootfsMirrors(root, mirrors.copy(linuxMirror = "https://ports.ubuntu.com/ubuntu-ports"))
        assertTrue(root.resolve("etc/apt/sources.list").readText().contains("https://ports.ubuntu.com/ubuntu-ports/"))
    }

    @Test
    fun `arm64 defaults to ubuntu ports while amd64 uses ubuntu archive`() {
        assertEquals(AndroidLinuxRuntime.UBUNTU_PORTS_APT_DEFAULT_MIRROR, AndroidLinuxRuntime.defaultLinuxMirror("ubuntu", "arm64-v8a"))
        assertEquals(AndroidLinuxRuntime.UBUNTU_APT_DEFAULT_MIRROR, AndroidLinuxRuntime.defaultLinuxMirror("ubuntu", "x86_64"))
    }

    @Test
    fun `runtime ABI follows device preference within packaged ABIs`() {
        assertEquals("x86_64", AndroidLinuxRuntime.selectRuntimeAbi(listOf("x86_64", "arm64-v8a"), listOf("arm64-v8a", "x86_64")))
        assertEquals("arm64-v8a", AndroidLinuxRuntime.selectRuntimeAbi(listOf("armeabi-v7a", "arm64-v8a"), listOf("arm64-v8a")))
        assertEquals("unknown", AndroidLinuxRuntime.selectRuntimeAbi(listOf("x86"), listOf("arm64-v8a", "x86_64")))
    }

    @Test
    fun `Ubuntu architecture comes from generated ABI contract`() {
        assertEquals("arm64", AndroidLinuxRuntime.ubuntuArch("arm64-v8a"))
        assertEquals("amd64", AndroidLinuxRuntime.ubuntuArch("x86_64"))
    }

    @Test
    fun `runtime capabilities only report executable artifacts that exist`() {
        val root = Files.createTempDirectory("linux-runtime-capabilities").toFile()
        root.resolve("bin/sh").apply { parentFile.mkdirs(); writeText("sh"); setExecutable(true) }
        root.resolve("usr/bin/python3").apply { parentFile.mkdirs(); writeText("python"); setExecutable(true) }
        root.resolve("usr/bin/node").apply { writeText("node"); setExecutable(true) }

        val capabilities = AndroidLinuxRuntime.runtimeCapabilities(root)

        assertTrue(capabilities.containsAll(listOf("shell", "python", "node", "commonjs", "esm")))
        assertFalse(capabilities.contains("crypto"))
        assertFalse(capabilities.contains("typescript"))
    }

    @Test
    fun `DNS formatter validates addresses deduplicates and preserves scoped IPv6`() {
        assertEquals(
            "nameserver 10.0.0.2\nnameserver fe80::53%wlan0\n",
            AndroidLinuxRuntime.formatResolvConf(listOf(
                " 10.0.0.2 ", "fe80::53%wlan0", "10.0.0.2", "resolver.example", "8.8.8.8\nnameserver 6.6.6.6", "999.1.1.1",
            )),
        )
        assertEquals("", AndroidLinuxRuntime.formatResolvConf(emptyList()))
        assertEquals("fe80::1%42", AndroidLinuxRuntime.normalizeDnsServer("fe80::1%42"))
        assertEquals(null, AndroidLinuxRuntime.normalizeDnsServer("fe80::1%bad zone"))
        assertEquals(null, AndroidLinuxRuntime.normalizeDnsServer("2001:db8::1%zone%other"))
        assertEquals(null, AndroidLinuxRuntime.normalizeDnsServer("1a.2.3.4"))
    }

    @Test
    fun `DNS atomic replacement replaces symlink without touching host target`() {
        val root = Files.createTempDirectory("linux-runtime-dns-root").toFile()
        val etc = root.resolve("etc").apply { mkdirs() }
        val host = Files.createTempFile("linux-runtime-host-resolv", ".conf").toFile().apply { writeText("host-original\n") }
        listOf(host.toPath(), etc.toPath().relativize(host.toPath())).forEachIndexed { index, linkTarget ->
            val resolv = etc.resolve("resolv.conf")
            Files.deleteIfExists(resolv.toPath())
            Files.createSymbolicLink(resolv.toPath(), linkTarget)

            AndroidLinuxRuntime.atomicWriteResolvConf(root, listOf("10.0.0.${53 + index}")) { }

            assertFalse(Files.isSymbolicLink(resolv.toPath()))
            assertTrue(Files.isRegularFile(resolv.toPath(), LinkOption.NOFOLLOW_LINKS))
            assertEquals("nameserver 10.0.0.${53 + index}\n", resolv.readText())
            assertEquals("host-original\n", host.readText())
        }
    }

    @Test
    fun `DNS status records successful durable write`() {
        val root = Files.createTempDirectory("linux-runtime-dns-success").toFile()

        val status = AndroidLinuxRuntime.persistDnsConfig(
            root = root,
            source = "active_network",
            servers = listOf("1.1.1.1"),
            updatedAt = "2026-08-28T00:00:00Z",
        ) { target, servers -> AndroidLinuxRuntime.atomicWriteResolvConf(target, servers) { } }

        assertTrue(status.writeSuccess)
        assertEquals("2026-08-28T00:00:00Z", status.updatedAt)
        assertEquals("", status.error)
        assertEquals("nameserver 1.1.1.1\n", root.resolve("etc/resolv.conf").readText())
    }

    @Test
    fun `DNS status replaces old success when directory fsync fails`() {
        val root = Files.createTempDirectory("linux-runtime-dns-fsync-failure").toFile()
        AndroidLinuxRuntime.persistDnsConfig(root, "fallback", listOf("1.1.1.1")) { _, _ -> }

        val status = AndroidLinuxRuntime.persistDnsConfig(
            root = root,
            source = "active_network",
            servers = listOf("fe80::53%wlan0"),
            updatedAt = "2026-08-28T00:00:01Z",
        ) { target, servers ->
            AndroidLinuxRuntime.atomicWriteResolvConf(target, servers) { throw IOException("directory fsync failed") }
        }

        assertFalse(status.writeSuccess)
        assertEquals("active_network", status.source)
        assertEquals(listOf("fe80::53%wlan0"), status.servers)
        assertEquals("2026-08-28T00:00:01Z", status.updatedAt)
        assertTrue(status.error.contains("directory fsync failed"))
    }

    @Test
    fun `DNS status rejects an all-invalid server refresh`() {
        val root = Files.createTempDirectory("linux-runtime-dns-invalid").toFile()

        val status = AndroidLinuxRuntime.persistDnsConfig(root, "active_network", listOf("resolver.test", "1.1.1.1\nsearch bad"))

        assertFalse(status.writeSuccess)
        assertTrue(status.servers.isEmpty())
        assertTrue(status.error.contains("no valid DNS servers"))
    }

    @Test
    fun `bind candidate filtering reports enabled and skipped classes`() {
        val candidates = listOf(
            AndroidLinuxRuntime.BindCandidate("/proc", core = true),
            AndroidLinuxRuntime.BindCandidate("/linkerconfig"),
            AndroidLinuxRuntime.BindCandidate("/system"),
        )

        val selection = AndroidLinuxRuntime.filterBindCandidates(candidates) { it != "/linkerconfig" }

        assertEquals(listOf("/proc", "/system"), selection.enabled.map { it.path })
        assertEquals(listOf("/linkerconfig"), selection.skipped.map { it.path })
        assertTrue(selection.enabled.first().core)
        assertFalse(selection.skipped.first().core)
    }
}
