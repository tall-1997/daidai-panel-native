package com.daidai.daidai_app

import android.content.Context
import java.io.BufferedInputStream
import java.io.File
import java.io.InputStream
import java.net.URI
import java.nio.file.Files
import java.nio.file.Paths
import java.security.MessageDigest
import java.util.zip.GZIPInputStream
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.xz.XZCompressorInputStream
import org.json.JSONArray
import org.json.JSONObject

object AndroidLinuxRuntime {
    private const val ROOTFS_ASSET_PREFIX = "android-runtime"
    private const val ROOTFS_READY_MARKER = ".daidai-rootfs-ready"
    private const val ROOTFS_ASSET_NAME = "rootfs.tar.gz.bin"
    private const val ROOTFS_SHA256_ASSET_NAME = "rootfs.tar.gz.bin.sha256"
    private const val PROOT_LOADER_LIBRARY_NAME = "libproot_loader.so"
    internal const val GUEST_PATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    private const val DISTRO_PREFERENCES = "daidai-linux-distro"
    private const val DISTRO_KEY = "selected_distribution"
    private const val DEFAULT_DISTRIBUTION = "alpine"
    internal val SUPPORTED_DISTRIBUTIONS = listOf("alpine", "ubuntu")
    private val REQUIRED_COMMANDS_ALPINE = linkedMapOf(
        "bash" to listOf("/bin/bash", "/usr/bin/bash"),
        "python3" to listOf("/usr/bin/python3"),
        "pip" to listOf("/usr/bin/pip3", "/usr/bin/pip"),
        "node" to listOf("/usr/bin/node"),
        "npm" to listOf("/usr/bin/npm"),
        "pnpm" to listOf("/usr/bin/pnpm"),
        "uv" to listOf("/usr/bin/uv"),
    )
    private val REQUIRED_COMMANDS_UBUNTU = linkedMapOf(
        "bash" to listOf("/bin/bash", "/usr/bin/bash"),
        "python3" to listOf("/usr/bin/python3"),
        "pip" to listOf("/usr/bin/pip3", "/usr/bin/pip"),
        "node" to listOf("/usr/bin/node"),
        "npm" to listOf("/usr/bin/npm"),
        "pnpm" to listOf("/usr/local/bin/pnpm"),
    )
    private val REQUIRED_PACKAGE_MANAGERS = mapOf(
        "apk" to listOf("/sbin/apk", "/usr/sbin/apk"),
        "apt" to listOf("/usr/bin/apt-get"),
    )

    private fun requiredCommandsFor(packageManager: String): Map<String, List<String>> =
        if (packageManager == "apk") REQUIRED_COMMANDS_ALPINE else REQUIRED_COMMANDS_UBUNTU
    const val ALPINE_APK_DEFAULT_MIRROR = "https://repo.huaweicloud.com/alpine"
    const val UBUNTU_APT_DEFAULT_MIRROR = "https://mirrors.aliyun.com/ubuntu"
    const val PYTHON_PIP_ALIBABA_INDEX = "https://mirrors.aliyun.com/pypi/simple"
    const val NODE_NPM_NPMMIRROR_REGISTRY = "https://registry.npmmirror.com"
    const val MIRROR_PREFERENCES = "daidai-local-configs"
    const val PIP_MIRROR_KEY = "pip_mirror"
    const val NPM_MIRROR_KEY = "npm_mirror"
    const val LINUX_MIRROR_KEY = "linux_mirror"

    internal val mirrorConfigLock = Any()

    data class MirrorConfig(
        val pipMirror: String = PYTHON_PIP_ALIBABA_INDEX,
        val npmMirror: String = NODE_NPM_NPMMIRROR_REGISTRY,
        val linuxMirror: String = ALPINE_APK_DEFAULT_MIRROR,
    )

    data class RootfsPaths(
        val root: File,
        val proot: File,
        val prootLoader: File,
        val busybox: File?,
        val packageManager: String,
        val commands: Map<String, String>,
    )

    enum class GuestShell(val executable: String) {
        SH("/bin/sh"),
        BASH("/bin/bash"),
    }

    data class RuntimeDescriptor(
        val id: String,
        val language: String,
        val version: String,
        val installed: Boolean,
        val executable: String,
        val home: String,
        val isolation: String,
        val capabilities: List<String>,
    )

    fun currentAbi(): String = android.os.Build.SUPPORTED_ABIS.firstOrNull().orEmpty().ifBlank { "unknown" }

    fun nativeLibraryDir(context: Context): File = File(context.applicationInfo.nativeLibraryDir.orEmpty())

    private fun nativeCompatDir(context: Context): File {
        val compat = File(context.filesDir, "runtimes/native-compat/${currentAbi()}")
        copyVersionedLibraries(nativeLibraryDir(context), compat, mapOf(
            "libtalloc_v2.so" to listOf("libtalloc.so.2"),
            "libbusybox_v138.so" to listOf("libbusybox.so.1.38.0"),
        ))
        return compat
    }

    fun baseEnvironment(context: Context, workingDir: File): MutableMap<String, String> {
        val mirrors = mirrorConfig(context)
        val prootLoader = resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME))
        return mutableMapOf(
            "HOME" to context.filesDir.absolutePath,
            "TMPDIR" to context.cacheDir.absolutePath,
            "PWD" to workingDir.absolutePath,
            "DAIDAI_ANDROID_LOCAL" to "1",
            "DAIDAI_RUNTIME_ISOLATION" to "android-app-sandbox",
            "DAIDAI_RUNTIME_ABI" to currentAbi(),
            "PYTHONUNBUFFERED" to "1",
            "PYTHONIOENCODING" to "utf-8",
            "GLIBC_TUNABLES" to "glibc.pthread.rseq=0",
            "LD_LIBRARY_PATH" to "${nativeCompatDir(context).absolutePath}:${nativeLibraryDir(context).absolutePath}",
        ).apply {
            prootLoader?.let { putAll(prootEnvironment(it, context.cacheDir)) }
            putAll(mirrorEnvironment(mirrors))
        }
    }

    internal fun prootEnvironment(loader: File, cacheDir: File): Map<String, String> = mapOf(
        "PROOT_LOADER" to loader.absolutePath,
        "PROOT_TMP_DIR" to cacheDir.absolutePath,
        "PROOT_VERBOSE" to "0",
    )

    internal fun mirrorEnvironment(mirrors: MirrorConfig): Map<String, String> = mapOf(
        "PIP_INDEX_URL" to mirrors.pipMirror,
        "NPM_CONFIG_REGISTRY" to mirrors.npmMirror,
        "npm_config_registry" to mirrors.npmMirror,
        "DAIDAI_LINUX_MIRROR" to mirrors.linuxMirror,
    )

    internal fun pipInstallArguments(mirror: String, target: String, packageSpec: String): List<String> =
        listOf("install", "--no-input", "--no-cache-dir", "-i", mirror, "--target", target, "--", packageSpec)

    internal fun npmInstallArguments(mirror: String, prefix: String, cacheDir: String, packageSpec: String): List<String> =
        listOf("install", "--no-audit", "--no-fund", "--update-notifier=false", "--registry", mirror, "--cache", cacheDir, "--prefix", prefix, "--", packageSpec)

    internal fun nativeBuildToolchainCommand(context: Context): List<String> {
        val manager = ensureRootfsReady(context)?.packageManager.orEmpty()
        return when (manager) {
            "apk" -> listOf("/sbin/apk", "add", "--no-cache", "build-base", "python3-dev", "linux-headers", "cargo")
            "apt" -> listOf("/bin/sh", "-lc", "export DEBIAN_FRONTEND=noninteractive; apt-get update; apt-get install -y build-essential python3-dev linux-headers-generic cargo")
            else -> listOf("/bin/sh", "-lc", "echo 'no package manager available'; exit 1")
        }
    }

    internal fun applyGuestEnvironment(command: List<String>, environment: MutableMap<String, String>) {
        if (command.firstOrNull()?.substringAfterLast('/') == "libdaidai_proot.so") {
            environment["PATH"] = GUEST_PATH
        }
    }

    fun mirrorConfig(context: Context): MirrorConfig = synchronized(mirrorConfigLock) {
        val preferences = context.getSharedPreferences(MIRROR_PREFERENCES, Context.MODE_PRIVATE)
        MirrorConfig(
            pipMirror = normalizeMirrorUrl(preferences.getString(PIP_MIRROR_KEY, null).orEmpty()) ?: PYTHON_PIP_ALIBABA_INDEX,
            npmMirror = normalizeMirrorUrl(preferences.getString(NPM_MIRROR_KEY, null).orEmpty()) ?: NODE_NPM_NPMMIRROR_REGISTRY,
            linuxMirror = normalizeMirrorUrl(preferences.getString(LINUX_MIRROR_KEY, null).orEmpty()) ?: defaultLinuxMirror(selectedDistribution(context)),
        )
    }

    // 发行版选择：写入 SharedPreferences，下次启动生效。返回当前选中的发行版 id。
    fun selectDistribution(context: Context, distribution: String): String {
        val normalized = if (distribution.trim().lowercase() in SUPPORTED_DISTRIBUTIONS) distribution.trim().lowercase() else DEFAULT_DISTRIBUTION
        context.getSharedPreferences(DISTRO_PREFERENCES, Context.MODE_PRIVATE).edit().putString(DISTRO_KEY, normalized).apply()
        synchronized(mirrorConfigLock) { cachedLinuxRootfs = null }
        return normalized
    }

    fun selectedDistribution(context: Context): String =
        context.getSharedPreferences(DISTRO_PREFERENCES, Context.MODE_PRIVATE)
            .getString(DISTRO_KEY, null)?.trim()?.lowercase()?.takeIf { it in SUPPORTED_DISTRIBUTIONS }
            ?: DEFAULT_DISTRIBUTION

    internal fun defaultLinuxMirror(distribution: String): String =
        if (distribution == "ubuntu") UBUNTU_APT_DEFAULT_MIRROR else ALPINE_APK_DEFAULT_MIRROR

    fun distributionPackageManager(distribution: String): String =
        if (distribution == "ubuntu") "apt" else "apk"

    private fun rootfsAssetPrefix(context: Context): String {
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        return "$ROOTFS_ASSET_PREFIX/$abi/$distribution"
    }

    // 预加载：后台完成 rootfs 解包与就绪校验，避免首次任务时阻塞。
    fun preload(context: Context) {
        Thread { try { ensureRootfsReady(context) } catch (_: Exception) { } }.start()
    }

    internal fun normalizeMirrorUrl(value: String): String? {
        val normalized = value.trim().trimEnd('/')
        if (normalized.isEmpty() || normalized.any { it.isWhitespace() || it.isISOControl() }) return null
        val uri = runCatching { URI(normalized) }.getOrNull() ?: return null
        if (uri.scheme?.lowercase() !in setOf("http", "https") || uri.host.isNullOrBlank()) return null
        if (uri.userInfo != null || uri.fragment != null) return null
        if (uri.port !in -1..65535 || uri.port == 0) return null
        return normalized
    }

    internal fun resolveMirrorValue(persisted: String?, imported: String?, defaultValue: String): String =
        persisted?.let(::normalizeMirrorUrl)
            ?: imported?.let(::normalizeMirrorUrl)
            ?: defaultValue

    fun copyVersionedLibraries(nativeDir: File, compatLibDir: File, links: Map<String, List<String>>) {
        compatLibDir.mkdirs()
        for ((source, targets) in links) {
            val sourceFile = File(nativeDir, source)
            if (!sourceFile.isFile) continue
            for (target in targets) {
                val targetFile = File(compatLibDir, target)
                if (!targetFile.exists()) {
                    runCatching { sourceFile.copyTo(targetFile, overwrite = true) }
                }
            }
        }
    }

    @Volatile private var cachedLinuxRootfs: RootfsPaths? = null

    fun ensureRootfsReady(context: Context, mirrors: MirrorConfig = mirrorConfig(context)): RootfsPaths? = synchronized(mirrorConfigLock) {
        cachedLinuxRootfs?.let { return@synchronized it }
        val abi = currentAbi()
        val root = File(context.filesDir, "runtimes/linux-rootfs/$abi")
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so")) ?: return@synchronized null
        val prootLoader = resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME)) ?: return@synchronized null
        val busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so"))
        if (!rootfsMarkerMatchesAsset(context, root)) {
            if (!rootfsMarkerMatchesDownloaded(context, root)) {
                val installed = installRootfsAsset(context, root, mirrors) ?: installDownloadedRootfs(context, root, mirrors)
                if (installed == null) return@synchronized null
            }
        }
        prepareRuntimeDirectories(root, mirrors)
        val commands = detectCommands(root)
        val packageManager = detectPackageManager(root)
        if (!rootfsFirstClass(commands, packageManager)) return@synchronized null
        cachedLinuxRootfs = RootfsPaths(root = root, proot = proot, prootLoader = prootLoader, busybox = busybox, packageManager = packageManager, commands = commands)
        cachedLinuxRootfs
    }

    internal fun rootfsFirstClass(commands: Map<String, String>, packageManager: String): Boolean {
        if (!commands.keys.containsAll(requiredCommandsFor(packageManager).keys)) return false
        return commands[packageManager] != null
    }

    fun shellCommand(context: Context, hostScript: File, workingDir: File, shell: GuestShell = GuestShell.SH): List<String>? {
        val rootfs = ensureRootfsReady(context) ?: return null
        val guestScript = "/workspace/${hostScript.name}"
        return prootCommand(context, rootfs, workingDir, "/workspace", listOf(shell.executable, guestScript))
    }

    fun shellTextCommand(context: Context, workingDir: File, command: String, shell: GuestShell): List<String>? {
        val rootfs = ensureRootfsReady(context) ?: return null
        return prootCommand(context, rootfs, workingDir, "/workspace", listOf(shell.executable, "-lc", command))
    }

    fun guestCommand(context: Context, workingDir: File, command: List<String>): List<String>? {
        val rootfs = ensureRootfsReady(context) ?: return null
        return prootCommand(context, rootfs, workingDir, "/workspace", command)
    }

    fun guestRuntimeAvailable(context: Context, executable: String): Boolean {
        val rootfs = ensureRootfsReady(context) ?: return false
        return File(rootfs.root, executable.trimStart('/')).isFile
    }

    fun rootfsCapabilities(context: Context): Map<String, Boolean> {
        val rootfs = ensureRootfsReady(context)
        val commands = requiredCommandsFor(rootfs?.packageManager ?: "apt")
        return if (rootfs == null) commands.keys.associateWith { false }
        else commands.keys.associateWith { it in rootfs.commands }
    }

    fun guestRuntimeVersion(context: Context, executable: String): String =
        ensureRootfsReady(context)?.let { rootfs -> File(rootfs.root, executable.trimStart('/')).takeIf(File::isFile) }
            ?.let { executable.substringAfterLast('/') }.orEmpty()

    fun systemPackageInstallCommand(
        context: Context,
        packageSpec: String,
        preferredManager: String = "",
        mirrors: MirrorConfig = mirrorConfig(context),
    ): List<String>? {
        if (!isSafeSystemPackageSpec(packageSpec)) return null
        val rootfs = ensureRootfsReady(context, mirrors) ?: return null
        val manager = preferredManager.ifBlank { rootfs.packageManager }
        val script = packageInstallScript(manager, packageSpec) ?: return null
        return prootCommand(context, rootfs, context.filesDir, "/", listOf("/bin/sh", "-lc", script))
    }

    fun isRootfsReady(context: Context): Boolean = ensureRootfsReady(context) != null

    fun hasPackagedRootfsRunner(context: Context): Boolean {
        val abi = currentAbi()
        return SUPPORTED_DISTRIBUTIONS.any { distribution ->
            assetExists(context, "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_ASSET_NAME")
        } &&
            resolveNativeTool(context, listOf("libdaidai_proot.so")) != null &&
            resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME)) != null
    }

    internal fun isSafeSystemPackageSpec(value: String): Boolean =
        Regex("[A-Za-z0-9][A-Za-z0-9._+:-]{0,127}").matches(value.trim())

    internal fun packageInstallScript(manager: String, packageSpec: String): String? {
        val pkg = packageSpec.trim()
        if (!isSafeSystemPackageSpec(pkg)) return null
        return when (manager.trim().lowercase()) {
            "apk" -> "apk update; apk add --no-cache $pkg"
            "apt", "apt-get" -> "export DEBIAN_FRONTEND=noninteractive; apt-get update; apt-get install -y $pkg"
            "yum" -> "yum install -y $pkg"
            "dnf" -> "dnf install -y $pkg"
            else -> null
        }
    }

    private fun prootCommand(
        context: Context,
        rootfs: RootfsPaths,
        hostWorkingDir: File,
        guestWorkingDir: String,
        guestCommand: List<String>,
    ): List<String> {
        hostWorkingDir.mkdirs()
        File(rootfs.root, guestWorkingDir.trimStart('/')).mkdirs()
        val command = mutableListOf(
            rootfs.proot.absolutePath,
            "--link2symlink",
            "--sysvipc",
            "--kill-on-exit",
            "-k", "4.14.0",
            "-r", rootfs.root.absolutePath,
            "-w", guestWorkingDir,
            "-b", "${hostWorkingDir.absolutePath}:$guestWorkingDir",
            "-b", "${context.filesDir.absolutePath}:/host-files",
            "-b", "${context.cacheDir.absolutePath}:/tmp/host-cache",
            "-L",
            "-0",
        )
        listOf("/proc", "/dev", "/sys", "/sdcard", "/storage").forEach { path ->
            if (File(path).exists()) command.addAll(listOf("-b", "$path:$path"))
        }
        command += guestCommand
        return command
    }

    internal fun prootCompatibilityFlags(): List<String> = listOf(
        "--link2symlink",
        "--sysvipc",
        "--kill-on-exit",
        "-k", "4.14.0",
        "-L",
        "-0",
    )

    internal fun prootLoaderLibraryName(): String = PROOT_LOADER_LIBRARY_NAME

    private fun installRootfsAsset(context: Context, root: File, mirrors: MirrorConfig): RootfsPaths? {
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        val assetName = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_ASSET_NAME"
        val checksumName = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_SHA256_ASSET_NAME"
        if (!assetExists(context, assetName)) return null
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so")) ?: return null
        root.deleteRecursively()
        root.mkdirs()
        try {
            val expected = context.assets.open(checksumName).bufferedReader().use { it.readText().trim().substringBefore(' ') }
            val digest = MessageDigest.getInstance("SHA-256")
            context.assets.open(assetName).use { input ->
                val buffer = ByteArray(64 * 1024)
                while (true) { val count = input.read(buffer); if (count < 0) break; digest.update(buffer, 0, count) }
            }
            val actual = digest.digest().joinToString("") { "%02x".format(it) }
            require(expected.length == 64 && expected.equals(actual, true)) { "rootfs checksum mismatch" }
            context.assets.open(assetName).use { raw ->
                openRootfsTar(raw).use { tar ->
                    extractTar(root, tar)
                }
            }
            prepareRuntimeDirectories(root, mirrors)
            val commands = detectCommands(root)
            val packageManager = detectPackageManager(root)
            require(rootfsFirstClass(commands, packageManager)) {
                "rootfs missing required commands: ${requiredCommandsFor(packageManager).keys - commands.keys}"
            }
            File(root, ROOTFS_READY_MARKER).writeText("ready:$abi:$distribution:${assetChecksum(context, checksumName)}")
        } catch (_: Exception) {
            root.deleteRecursively()
            return null
        }
        val prootLoader = resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME)) ?: return null
        val commands = detectCommands(root)
        val packageManager = detectPackageManager(root)
        return RootfsPaths(root = root, proot = proot, prootLoader = prootLoader, busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so")), packageManager = packageManager, commands = commands)
    }

    private fun installDownloadedRootfs(context: Context, root: File, mirrors: MirrorConfig): RootfsPaths? {
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        val archive = AndroidRootfsDownloader.downloadedArchive(context, abi, distribution) ?: return null
        val expected = AndroidRootfsDownloader.downloadedChecksum(context, abi, distribution) ?: return null
        val actual = sha256File(archive)
        if (expected.length != 64 || !expected.equals(actual, true)) return null
        root.deleteRecursively()
        root.mkdirs()
        try {
            archive.inputStream().use { raw ->
                openRootfsTar(raw).use { tar ->
                    extractTar(root, tar)
                }
            }
            prepareRuntimeDirectories(root, mirrors)
            val commands = detectCommands(root)
            val packageManager = detectPackageManager(root)
            require(rootfsFirstClass(commands, packageManager)) {
                "rootfs missing required commands: ${requiredCommandsFor(packageManager).keys - commands.keys}"
            }
            File(root, ROOTFS_READY_MARKER).writeText("downloaded:$abi:$distribution:$expected")
        } catch (_: Exception) {
            root.deleteRecursively()
            return null
        }
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so")) ?: return null
        val prootLoader = resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME)) ?: return null
        val commands = detectCommands(root)
        val packageManager = detectPackageManager(root)
        return RootfsPaths(
            root = root,
            proot = proot,
            prootLoader = prootLoader,
            busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so")),
            packageManager = packageManager,
            commands = commands,
        )
    }

    private fun sha256File(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    private fun openRootfsTar(raw: InputStream): TarArchiveInputStream {
        val buffered = BufferedInputStream(raw)
        buffered.mark(6)
        val magic = ByteArray(6).also { buffered.read(it) }
        buffered.reset()
        val compressor: InputStream = when {
            magic[0] == 0x1f.toByte() && magic[1] == 0x8b.toByte() -> GZIPInputStream(buffered)
            magic[0] == 0xfd.toByte() && magic[1] == 0x37.toByte() && magic[2] == 0x7a.toByte() && magic[3] == 0x58.toByte() && magic[4] == 0x5a.toByte() && magic[5] == 0x00.toByte() -> XZCompressorInputStream(buffered)
            else -> throw IllegalArgumentException("unsupported rootfs compression format")
        }
        return TarArchiveInputStream(compressor)
    }

    private fun extractTar(root: File, tar: TarArchiveInputStream) {
        val hardLinks = mutableListOf<Pair<File, String>>()
        while (true) {
            val entry = tar.nextTarEntry ?: break
            val output = File(root, entry.name).canonicalFile
            if (!output.path.startsWith(root.canonicalPath + File.separator)) continue
            when {
                entry.isDirectory -> output.mkdirs()
                entry.isSymbolicLink -> {
                    output.parentFile?.mkdirs()
                    runCatching { Files.createSymbolicLink(output.toPath(), Paths.get(entry.linkName)) }
                }
                entry.isLink -> {
                    hardLinks += output to entry.linkName
                }
                entry.isFile -> {
                    output.parentFile?.mkdirs()
                    output.outputStream().use { tar.copyTo(it) }
                    output.setReadable(entry.mode and 0b100000000 != 0, false)
                    output.setWritable(entry.mode and 0b010000000 != 0, false)
                    output.setExecutable(entry.mode and 0b001000000 != 0, false)
                }
            }
        }
        hardLinks.forEach { (output, linkName) ->
            val target = File(root, linkName).canonicalFile
            if (target.path.startsWith(root.canonicalPath + File.separator) && target.isFile) {
                output.parentFile?.mkdirs()
                runCatching { Files.createLink(output.toPath(), target.toPath()) }
                    .recoverCatching { target.copyTo(output, overwrite = true) }
            }
        }
    }

    private fun prepareRuntimeDirectories(root: File, mirrors: MirrorConfig) {
        listOf("tmp", "workspace", "host", "proc", "sys", "dev", "sdcard", "storage").forEach {
            File(root, it).mkdirs()
        }
        configureRootfsMirrors(root, mirrors)
        val resolvConf = File(root, "etc/resolv.conf")
        if (!resolvConf.isFile) {
            resolvConf.parentFile?.mkdirs()
            resolvConf.writeText("nameserver 1.1.1.1\nnameserver 8.8.8.8\n")
        }
        File(root, "tmp").setWritable(true, false)
    }

    private fun detectPackageManager(root: File): String = when {
        File(root, "sbin/apk").isFile || File(root, "usr/sbin/apk").isFile -> "apk"
        File(root, "usr/bin/apt-get").isFile -> "apt"
        File(root, "usr/bin/dnf").isFile -> "dnf"
        File(root, "usr/bin/yum").isFile -> "yum"
        else -> ""
    }

    private fun detectCommands(root: File): Map<String, String> {
        val packageManager = detectPackageManager(root)
        val required = requiredCommandsFor(packageManager).mapNotNull { (name, candidates) ->
            candidates.firstOrNull { File(root, it.trimStart('/')).let { file -> file.isFile && file.canExecute() } }?.let { name to it }
        }.toMap()
        val managerPath = REQUIRED_PACKAGE_MANAGERS[packageManager].orEmpty()
            .firstOrNull { File(root, it.trimStart('/')).isFile }
        return if (managerPath != null) required + (packageManager to managerPath) else required
    }

    private fun rootfsMarkerMatchesAsset(context: Context, root: File): Boolean {
        val checksumName = "${rootfsAssetPrefix(context)}/$ROOTFS_SHA256_ASSET_NAME"
        val marker = File(root, ROOTFS_READY_MARKER).readTextOrNull()?.trim().orEmpty()
        val expected = runCatching { assetChecksum(context, checksumName) }.getOrNull() ?: return false
        return marker == "ready:${currentAbi()}:${selectedDistribution(context)}:$expected"
    }

    private fun rootfsMarkerMatchesDownloaded(context: Context, root: File): Boolean {
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        val marker = File(root, ROOTFS_READY_MARKER).readTextOrNull()?.trim().orEmpty()
        val expected = AndroidRootfsDownloader.downloadedChecksum(context, abi, distribution) ?: return false
        return marker == "downloaded:$abi:$distribution:$expected"
    }

    private fun assetChecksum(context: Context, name: String): String =
        context.assets.open(name).bufferedReader().use { it.readText().trim().substringBefore(' ') }

    internal fun configureRootfsMirrors(root: File, mirrors: MirrorConfig) {
        if (File(root, "etc/alpine-release").isFile || File(root, "sbin/apk").isFile || File(root, "usr/sbin/apk").isFile) {
            val release = File(root, "etc/alpine-release").readTextOrNull()?.trim()?.substringBeforeLast('.')?.takeIf { it.startsWith("3") } ?: "latest-stable"
            File(root, "etc/apk/repositories").apply {
                parentFile?.mkdirs()
                writeText("${mirrors.linuxMirror}/v$release/main\n${mirrors.linuxMirror}/v$release/community\n")
            }
        }
        if (File(root, "etc/os-release").readTextOrNull()?.contains("Ubuntu", ignoreCase = true) == true || File(root, "usr/bin/apt-get").isFile) {
            val release = File(root, "etc/lsb-release").readTextOrNull()
                ?.lineSequence()?.firstOrNull { it.startsWith("DISTRIB_CODENAME=") }?.substringAfter("=")?.trim().orEmpty()
            if (release.isNotEmpty() && File(root, "etc/apt/sources.list").let { !it.isFile || !it.readTextOrNull().orEmpty().contains("mirror") }) {
                File(root, "etc/apt/sources.list").apply {
                    parentFile?.mkdirs()
                    writeText("deb ${mirrors.linuxMirror}/ $release main restricted universe multiverse\n" +
                        "deb ${mirrors.linuxMirror}/ $release-updates main restricted universe multiverse\n" +
                        "deb ${mirrors.linuxMirror}/ $release-security main restricted universe multiverse\n")
                }
                listOf("etc/apt/sources.list.d").forEach { dir -> File(root, dir).let { if (it.isDirectory) it.listFiles()?.forEach { child -> child.delete() } } }
            }
        }
        val pipConfig = "[global]\nindex-url = ${mirrors.pipMirror}\ntimeout = 60\n"
        listOf(File(root, "etc/pip.conf"), File(root, "root/.pip/pip.conf")).forEach { file ->
            file.parentFile?.mkdirs()
            file.writeText(pipConfig)
        }
        File(root, "etc/npmrc").apply {
            parentFile?.mkdirs()
            writeText("registry=${mirrors.npmMirror}\nignore-scripts=true\n")
        }
    }

    fun writeShellWrapper(
        output: File,
        env: Map<String, String>,
        executable: File,
        extraExecArgs: List<String> = emptyList(),
    ): File {
        output.parentFile?.mkdirs()
        val body = buildString {
            append("#!/system/bin/sh\n")
            env.forEach { (key, value) -> append("export ").append(key).append("=\"").append(value).append("\"\n") }
            append("exec \"").append(executable.absolutePath).append("\"")
            extraExecArgs.forEach { append(" ").append(it) }
            append(" \"$@\"\n")
        }
        output.writeText(body)
        return output
    }

    fun statusJson(context: Context): JSONObject {
        val rootfs = rootfsStatus(context)
        val pythonAvailable = guestRuntimeAvailable(context, "/usr/bin/python3")
        val nodeAvailable = guestRuntimeAvailable(context, "/usr/bin/node")
        val rootfsHome = File(context.filesDir, "runtimes/linux-rootfs/${currentAbi()}").absolutePath
        val runtimes = JSONArray()
            .put(descriptorJson(RuntimeDescriptor(
                id = "linux-runtime-${currentAbi()}",
                language = "linux",
                version = rootfs.optString("distribution"),
                installed = rootfs.optBoolean("installed"),
                executable = rootfs.optJSONObject("runner")?.optString("path").orEmpty(),
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = listOf("shell", "python", "pip", "node", "npm", "typescript", "git", "ssh", "go-build"),
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "python-rootfs-android-${currentAbi()}",
                language = "python",
                version = if (pythonAvailable) rootfs.optString("distribution") else "",
                installed = pythonAvailable,
                executable = "/usr/bin/python3",
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = listOf("python", "pip", "venv", "ssl", "sqlite"),
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "node-rootfs-android-${currentAbi()}",
                language = "node",
                version = if (nodeAvailable) rootfs.optString("distribution") else "",
                installed = nodeAvailable,
                executable = "/usr/bin/node",
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = listOf("node", "npm", "typescript", "commonjs", "esm"),
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "yaegi-go-${currentAbi()}", language = "go", version = "0.16.1",
                installed = File(nativeLibraryDir(context), "libyaegi_exec.so").isFile,
                executable = File(nativeLibraryDir(context), "libyaegi_exec.so").absolutePath,
                home = "", isolation = "isolated-worker", capabilities = listOf("go-interpret"),
            )))
        return JSONObject()
            .put("supported", true)
            .put("arch", currentAbi())
            .put("bin_dir", nativeLibraryDir(context).absolutePath)
            .put("container_model", "layered-linux-runtime")
            .put("distribution", selectedDistribution(context))
            .put("rootfs", rootfs)
            .put("presets", JSONArray())
            .put("runtimes", runtimes)
    }

    private fun descriptorJson(descriptor: RuntimeDescriptor): JSONObject = JSONObject()
        .put("id", descriptor.id)
        .put("name", descriptor.language)
        .put("language", descriptor.language)
        .put("version", descriptor.version)
        .put("installed", descriptor.installed)
        .put("path", descriptor.executable)
        .put("home", descriptor.home)
        .put("isolation", descriptor.isolation)
        .put("capabilities", JSONArray(descriptor.capabilities))

    private fun rootfsStatus(context: Context): JSONObject {
        val mirrors = mirrorConfig(context)
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        val rootfsAsset = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_ASSET_NAME"
        val checksumAsset = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_SHA256_ASSET_NAME"
        val rootfsDir = File(context.filesDir, "runtimes/linux-rootfs/$abi")
        val commandPaths = detectCommands(rootfsDir)
        val commandStatus = JSONObject()
        requiredCommandsFor(detectPackageManager(rootfsDir)).keys.forEach { name -> commandStatus.put(name, commandPaths[name] ?: false) }
        val distributions = JSONObject()
        SUPPORTED_DISTRIBUTIONS.forEach { dist ->
            distributions.put(dist, assetExists(context, "$ROOTFS_ASSET_PREFIX/$abi/$dist/$ROOTFS_ASSET_NAME"))
        }
        return JSONObject()
            .put("id", "linux-rootfs-android-$abi")
            .put("distribution", rootfsDistribution(rootfsDir))
            .put("selected_distribution", distribution)
            .put("installed", rootfsMarkerMatchesAsset(context, rootfsDir) || rootfsMarkerMatchesDownloaded(context, rootfsDir))
            .put("packaged", distributions.optBoolean(distribution))
            .put("asset", if (distributions.optBoolean(distribution)) rootfsAsset else "")
            .put("sha256_asset", if (assetExists(context, checksumAsset)) checksumAsset else "")
            .put("distributions_available", distributions)
            .put("image", JSONObject()
                .put("sources", JSONArray(AndroidRootfsDownloader.sourcesFor(distribution).map { source ->
                    JSONObject()
                        .put("id", source.id)
                        .put("display_name", source.displayName)
                        .put("distribution", source.distribution)
                        .put("base_url", source.baseUrl)
                }))
                .put("selected_source", AndroidRootfsDownloader.selectedSourceId(context, distribution))
                .put("downloaded", AndroidRootfsDownloader.downloadedArchive(context, abi, distribution) != null))
            .put("runner", prootRunnerStatus(context))
            .put("package_manager", detectPackageManager(rootfsDir))
            .put("first_class", rootfsFirstClass(commandPaths, detectPackageManager(rootfsDir)))
            .put("commands", commandStatus)
            .put("compatibility", JSONObject()
                .put("no_seccomp", true)
                .put("link2symlink", true)
                .put("kill_on_exit", true)
                .put("fake_kernel_release", "4.14.0")
                .put("binds", JSONArray(listOf("/proc", "/dev", "/sys", "/sdcard", "/storage", "/host-files", "/tmp/host-cache"))))
            .put("mirrors", JSONObject()
                .put("linux", mirrors.linuxMirror)
                .put("pip", mirrors.pipMirror)
                .put("npm", mirrors.npmMirror))
    }

    private fun prootRunnerStatus(context: Context): JSONObject {
        val nativeDir = nativeLibraryDir(context)
        return JSONObject()
            .put("proot", File(nativeDir, "libdaidai_proot.so").isFile)
            .put("proot_executable", listOf("libdaidai_proot.so").map { File(nativeDir, it) }.any { it.isFile && it.canExecute() })
            .put("proot_loader", File(nativeDir, PROOT_LOADER_LIBRARY_NAME).isFile)
            .put("proot_loader_executable", File(nativeDir, PROOT_LOADER_LIBRARY_NAME).let { it.isFile && it.canExecute() })
            .put("busybox", File(nativeDir, "libdaidai_busybox.so").isFile)
            .put("busybox_executable", listOf("libdaidai_busybox.so").map { File(nativeDir, it) }.any { it.isFile && it.canExecute() })
    }

    private fun resolveNativeTool(context: Context, candidates: List<String>): File? {
        val nativeDir = nativeLibraryDir(context)
        return candidates.map { File(nativeDir, it) }.firstOrNull { it.isFile }
    }

    private fun rootfsDistribution(root: File): String = when {
        File(root, "etc/alpine-release").isFile -> "alpine"
        File(root, "etc/os-release").readTextOrNull()?.contains("Ubuntu", ignoreCase = true) == true -> "ubuntu"
        root.exists() -> "linux"
        else -> ""
    }

    private fun File.readTextOrNull(): String? = try { readText() } catch (_: Exception) { null }

    private fun assetExists(context: Context, name: String): Boolean = try {
        context.assets.open(name).close()
        true
    } catch (_: Exception) {
        false
    }
}
