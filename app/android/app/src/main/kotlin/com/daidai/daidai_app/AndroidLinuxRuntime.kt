package com.daidai.daidai_app

import android.content.Context
import android.net.ConnectivityManager
import android.os.Process
import android.system.Os
import android.system.OsConstants
import java.io.BufferedInputStream
import java.io.File
import java.io.InputStream
import java.io.IOException
import java.net.Inet6Address
import java.net.InetAddress
import java.net.URI
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.LinkOption
import java.nio.file.Paths
import java.nio.file.StandardCopyOption
import java.nio.file.StandardOpenOption
import java.security.MessageDigest
import java.time.Instant
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
    internal val SUPPORTED_DISTRIBUTIONS = listOf("ubuntu", "alpine")
    // x86_64 must default to musl: Ubuntu glibc calls set_robust_list/rseq which the
    // app-domain seccomp filter kills with SIGSYS, taking down the PRoot tracee.
    // musl (Alpine) never issues those syscalls. arm64 keeps Ubuntu.
    private val DEFAULT_DISTRIBUTIONS_BY_ABI = mapOf("x86_64" to "alpine")
    private const val DEFAULT_DISTRIBUTION_FALLBACK = "ubuntu"
    private val REQUIRED_COMMANDS_UBUNTU = linkedMapOf(
        "bash" to listOf("/bin/bash", "/usr/bin/bash"),
        "python3" to listOf("/usr/bin/python3"),
        "pip" to listOf("/usr/bin/pip3", "/usr/bin/pip"),
        "node" to listOf("/usr/bin/node"),
        "npm" to listOf("/usr/bin/npm"),
        "pnpm" to listOf("/usr/local/bin/pnpm"),
    )
    private val REQUIRED_COMMANDS_ALPINE = linkedMapOf(
        "bash" to listOf("/bin/bash", "/usr/bin/bash"),
        "python3" to listOf("/usr/bin/python3"),
        "pip" to listOf("/usr/bin/pip3", "/usr/bin/pip"),
        "node" to listOf("/usr/bin/node"),
        "npm" to listOf("/usr/bin/npm"),
        "pnpm" to listOf("/usr/local/bin/pnpm"),
    )
    private val REQUIRED_PACKAGE_MANAGERS = mapOf(
        "apt" to listOf("/usr/bin/apt-get"),
        "apk" to listOf("/sbin/apk", "/usr/sbin/apk"),
    )

    private fun defaultDistributionFor(abi: String): String =
        DEFAULT_DISTRIBUTIONS_BY_ABI[abi] ?: DEFAULT_DISTRIBUTION_FALLBACK

    private fun requiredCommandsFor(packageManager: String): Map<String, List<String>> =
        if (packageManager == "apk") REQUIRED_COMMANDS_ALPINE else REQUIRED_COMMANDS_UBUNTU
    const val UBUNTU_APT_DEFAULT_MIRROR = "https://mirrors.aliyun.com/ubuntu"
    const val UBUNTU_PORTS_APT_DEFAULT_MIRROR = "https://mirrors.aliyun.com/ubuntu-ports"
    const val ALPINE_APK_DEFAULT_MIRROR = "https://mirrors.aliyun.com/alpine"
    const val PYTHON_PIP_ALIBABA_INDEX = "https://mirrors.aliyun.com/pypi/simple"
    const val NODE_NPM_NPMMIRROR_REGISTRY = "https://registry.npmmirror.com"
    internal const val X86_NODE_UTF8_COMPAT = "--import=data:text/javascript,Buffer.prototype.utf8Slice%3Dfunction%28s%2Ce%29%7Bif%28s%3C0%29s%3D0%3Bif%28e%3C0%29e%3D0%3Bif%28e%3Ethis.length%29e%3Dthis.length%3Bif%28s%3Ee%29return%20%27%27%3Bvar%20o%3D%5B%5D%2Cb%2Ci%3Ds%3Bwhile%28i%3Ce%29%7Bb%3Dthis%5Bi%5D%3Bif%28b%3C128%29%7Bo.push%28String.fromCharCode%28b%29%29%3Bi%2B%3D1%7Delse%20if%28b%3C224%26%26i%2B1%3Ce%29%7Bo.push%28String.fromCharCode%28%28b%2631%29%3C%3C6%7Cthis%5Bi%2B1%5D%2663%29%29%3Bi%2B%3D2%7Delse%20if%28b%3C240%26%26i%2B2%3Ce%29%7Bo.push%28String.fromCharCode%28%28b%2615%29%3C%3C12%7C%28this%5Bi%2B1%5D%2663%29%3C%3C6%7Cthis%5Bi%2B2%5D%2663%29%29%3Bi%2B%3D3%7Delse%20if%28b%3C248%26%26i%2B3%3Ce%29%7Bvar%20c%3D%28%28b%267%29%3C%3C18%7C%28this%5Bi%2B1%5D%2663%29%3C%3C12%7C%28this%5Bi%2B2%5D%2663%29%3C%3C6%7Cthis%5Bi%2B3%5D%2663%29-65536%3Bo.push%28String.fromCharCode%2855296%2B%28c%3E%3E10%29%2C56320%2B%28c%261023%29%29%29%3Bi%2B%3D4%7Delse%7Bo.push%28%27%EF%BF%BD%27%29%3Bi%2B%3D1%7D%7Dreturn%20o.join%28%27%27%29%7D"
    const val MIRROR_PREFERENCES = "daidai-local-configs"
    const val PIP_MIRROR_KEY = "pip_mirror"
    const val NPM_MIRROR_KEY = "npm_mirror"
    const val LINUX_MIRROR_KEY = "linux_mirror"
    private val DNS_FALLBACK_SERVERS = listOf("1.1.1.1", "8.8.8.8")

    data class DnsConfig(
        val source: String,
        val servers: List<String>,
        val writeSuccess: Boolean,
        val updatedAt: String,
        val error: String,
    )

    internal data class BindCandidate(val path: String, val core: Boolean = false)

    internal data class BindSelection(
        val enabled: List<BindCandidate>,
        val skipped: List<BindCandidate>,
    )

    private val HOST_BIND_CANDIDATES = listOf(
        BindCandidate("/proc", core = true),
        BindCandidate("/dev", core = true),
        BindCandidate("/sys", core = true),
        BindCandidate("/sdcard"),
        BindCandidate("/storage"),
        BindCandidate("/apex"),
        BindCandidate("/odm"),
        BindCandidate("/product"),
        BindCandidate("/system"),
        BindCandidate("/system_ext"),
        BindCandidate("/vendor"),
        BindCandidate("/linkerconfig"),
    )

    @Volatile private var lastDnsConfig = DnsConfig(
        source = "uninitialized",
        servers = emptyList(),
        writeSuccess = false,
        updatedAt = Instant.EPOCH.toString(),
        error = "rootfs DNS has not been refreshed",
    )

    internal val mirrorConfigLock = Any()

    data class MirrorConfig(
        val pipMirror: String = PYTHON_PIP_ALIBABA_INDEX,
        val npmMirror: String = NODE_NPM_NPMMIRROR_REGISTRY,
        val linuxMirror: String = UBUNTU_PORTS_APT_DEFAULT_MIRROR,
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

    internal fun selectRuntimeAbi(deviceAbis: List<String>, packagedAbis: List<String>): String =
        deviceAbis.firstOrNull { it in packagedAbis }
            ?: packagedAbis.singleOrNull()
            ?: "unknown"

    private fun buildConfigMap(value: String): Map<String, String> = value.split(',')
        .mapNotNull { entry ->
            val separator = entry.indexOf('=')
            if (separator <= 0) null else entry.substring(0, separator) to entry.substring(separator + 1)
        }
        .toMap()

    internal fun ubuntuArch(abi: String): String =
        buildConfigMap(BuildConfig.RUNTIME_UBUNTU_ARCHES)[abi]
            ?: throw IOException("不支持的架构：$abi")

    fun currentAbi(): String = selectRuntimeAbi(
        android.os.Build.SUPPORTED_ABIS.toList(),
        BuildConfig.PACKAGED_RUNTIME_ABIS.split(',').filter(String::isNotBlank),
    )

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
            nodeRuntimeOptions(currentAbi())?.let { put("NODE_OPTIONS", it) }
            putAll(mirrorEnvironment(mirrors))
        }
    }

    internal fun nodeRuntimeOptions(abi: String): String? =
        X86_NODE_UTF8_COMPAT.takeIf { abi == "x86_64" }

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
            environment["HOME"] = "/host-files"
            environment["TMPDIR"] = "/tmp/host-cache"
            environment["PWD"] = "/workspace"
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
        val normalized = if (distribution.trim().lowercase() in SUPPORTED_DISTRIBUTIONS) distribution.trim().lowercase() else defaultDistribution()
        context.getSharedPreferences(DISTRO_PREFERENCES, Context.MODE_PRIVATE).edit().putString(DISTRO_KEY, normalized).apply()
        synchronized(mirrorConfigLock) { cachedLinuxRootfs = null }
        return normalized
    }

    private fun defaultDistribution(): String = defaultDistributionFor(currentAbi())

    fun selectedDistribution(context: Context): String {
        val abi = currentAbi()
        val stored = context.getSharedPreferences(DISTRO_PREFERENCES, Context.MODE_PRIVATE)
            .getString(DISTRO_KEY, null)?.trim()?.lowercase()?.takeIf { it in SUPPORTED_DISTRIBUTIONS }
            ?: return defaultDistributionFor(abi)
        // 升级可能改变资产布局（x86_64 从 Ubuntu glibc 切到 Alpine musl）。存储的发行版资产
        // 缺失时回退到该 ABI 的默认发行版，避免卡死在缺失资产上；资产存在时严格尊重用户选择
        // （arm64 默认 ubuntu 且资产始终存在，行为不变）。
        if (assetExists(context, "$ROOTFS_ASSET_PREFIX/$abi/$stored/$ROOTFS_ASSET_NAME")) return stored
        return defaultDistributionFor(abi)
    }

    internal fun defaultLinuxMirror(distribution: String, abi: String = currentAbi()): String =
        if (distribution == "alpine") ALPINE_APK_DEFAULT_MIRROR
        else buildConfigMap(BuildConfig.RUNTIME_LINUX_MIRRORS)[abi] ?: UBUNTU_APT_DEFAULT_MIRROR

    fun distributionPackageManager(distribution: String): String =
        if (distribution == "alpine") "apk" else "apt"

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
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so"))
        if (proot == null) {
            android.util.Log.w("daidai-panel", "ensureRootfsReady: missing packaged proot native tool")
            return@synchronized null
        }
        val prootLoader = resolveNativeTool(context, listOf(PROOT_LOADER_LIBRARY_NAME))
        if (prootLoader == null) {
            android.util.Log.w("daidai-panel", "ensureRootfsReady: missing packaged proot loader native tool")
            return@synchronized null
        }
        val busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so"))
        if (!rootfsMarkerMatchesAsset(context, root)) {
            if (!rootfsMarkerMatchesDownloaded(context, root)) {
                val installed = installRootfsAsset(context, root, mirrors)
                if (installed == null) {
                    android.util.Log.w("daidai-panel", "ensureRootfsReady: packaged rootfs asset install failed for abi=$abi distro=${selectedDistribution(context)}")
                    val downloaded = installDownloadedRootfs(context, root, mirrors)
                    if (downloaded == null) {
                        android.util.Log.w("daidai-panel", "ensureRootfsReady: downloaded rootfs install also failed for abi=$abi distro=${selectedDistribution(context)}")
                        return@synchronized null
                    }
                }
            }
        }
        prepareRuntimeDirectories(root, mirrors)
        val commands = detectCommands(root)
        val packageManager = detectPackageManager(root)
        if (!rootfsFirstClass(commands, packageManager)) {
            android.util.Log.w("daidai-panel", "ensureRootfsReady: rootfs not first-class for pm=$packageManager required=${requiredCommandsFor(packageManager).keys} found=${commands.keys} root=$root")
            return@synchronized null
        }
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
        return File(rootfs.root, executable.trimStart('/')).let { it.isFile && it.canExecute() }
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
        refreshRootfsDns(context, rootfs.root)
        hostWorkingDir.mkdirs()
        File(rootfs.root, guestWorkingDir.trimStart('/')).mkdirs()
        File(rootfs.root, "host-files").mkdirs()
        File(rootfs.root, "tmp/host-cache").mkdirs()
        val binds = selectHostBinds()
        binds.enabled.forEach { prepareRootfsBindTarget(rootfs.root, it.path) }
        val command = mutableListOf(
            rootfs.proot.absolutePath,
            "--kill-on-exit",
            "-k", "4.14.0",
            "-r", rootfs.root.absolutePath,
            "-w", guestWorkingDir,
            "-b", "${hostWorkingDir.absolutePath}:$guestWorkingDir",
            "-b", "${context.filesDir.absolutePath}:/host-files",
            "-b", "${context.cacheDir.absolutePath}:/tmp/host-cache",
            "-0",
        )
        binds.enabled.forEach { bind ->
            command.addAll(listOf("-b", "${bind.path}:${bind.path}"))
        }
        command += guestCommand
        return command
    }

    internal fun prootCompatibilityFlags(): List<String> = listOf(
        "--kill-on-exit",
        "-k", "4.14.0",
        "-0",
    )

    internal fun prootLoaderLibraryName(): String = PROOT_LOADER_LIBRARY_NAME

    private fun installRootfsAsset(context: Context, root: File, mirrors: MirrorConfig): RootfsPaths? {
        val abi = currentAbi()
        val distribution = selectedDistribution(context)
        val assetName = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_ASSET_NAME"
        val checksumName = "$ROOTFS_ASSET_PREFIX/$abi/$distribution/$ROOTFS_SHA256_ASSET_NAME"
        if (!assetExists(context, assetName)) {
            android.util.Log.w("daidai-panel", "installRootfsAsset: packaged asset missing: $assetName")
            return null
        }
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so"))
        if (proot == null) {
            android.util.Log.w("daidai-panel", "installRootfsAsset: missing packaged proot native tool")
            return null
        }
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
            require(expected.length == 64 && expected.equals(actual, true)) { "rootfs checksum mismatch: expected=$expected actual=$actual" }
            context.assets.open(assetName).use { raw ->
                openRootfsTar(raw).use { tar ->
                    val entries = extractTar(root, tar)
                    require(entries > 0) { "rootfs archive extracted zero entries" }
                }
            }
            prepareRuntimeDirectories(root, mirrors)
            val commands = detectCommands(root)
            val packageManager = detectPackageManager(root)
            require(rootfsFirstClass(commands, packageManager)) {
                "rootfs missing required commands: ${requiredCommandsFor(packageManager).keys - commands.keys}"
            }
            require(verifyRootfsLibraries(root)) { "rootfs missing dynamic linker" }
            File(root, ROOTFS_READY_MARKER).writeText("ready:$abi:$distribution:${assetChecksum(context, checksumName)}")
        } catch (failure: Exception) {
            android.util.Log.e("daidai-panel", "installRootfsAsset: packaged rootfs install failed for abi=$abi distro=$distribution asset=$assetName: ${failure.message}", failure)
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
        root.deleteRecursively()
        root.mkdirs()
        try {
            archive.inputStream().use { raw ->
                openRootfsTar(raw).use { tar ->
                    val entries = extractTar(root, tar)
                    require(entries > 0) { "rootfs archive extracted zero entries" }
                }
            }
            prepareRuntimeDirectories(root, mirrors)
            val commands = detectCommands(root)
            val packageManager = detectPackageManager(root)
            require(rootfsFirstClass(commands, packageManager)) {
                "rootfs missing required commands: ${requiredCommandsFor(packageManager).keys - commands.keys}"
            }
            require(verifyRootfsLibraries(root)) { "rootfs missing dynamic linker" }
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

    private fun extractTar(root: File, tar: TarArchiveInputStream): Int {
        var extractedEntries = 0
        val hardLinks = mutableListOf<Pair<File, String>>()
        while (true) {
            val entry = tar.nextTarEntry ?: break
            val output = File(root, entry.name).canonicalFile
            if (!output.path.startsWith(root.canonicalPath + File.separator)) continue
            when {
                entry.isDirectory -> output.mkdirs()
                entry.isSymbolicLink -> {
                    output.parentFile?.mkdirs()
                    // GNU tar replaces a pre-existing entry at the same path. The alpine
                    // rootfs tar legitimately ships duplicate bin symlinks (nodejs and npm
                    // packages both provide usr/bin/node-gyp), so drop any existing file,
                    // symlink, or empty directory before linking.
                    if (Files.exists(output.toPath(), LinkOption.NOFOLLOW_LINKS)) {
                        output.deleteRecursively()
                    }
                    // Never swallow symlink failures: missing lib symlinks (e.g. libnode.so.109)
                    // previously produced half-installed rootfs images with a ready marker.
                    try {
                        Files.createSymbolicLink(output.toPath(), Paths.get(entry.linkName))
                    } catch (exception: Exception) {
                        throw IOException("failed to create rootfs symlink ${entry.name} -> ${entry.linkName}", exception)
                    }
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
            extractedEntries++
        }
        hardLinks.forEach { (output, linkName) ->
            val target = File(root, linkName).canonicalFile
            if (target.path.startsWith(root.canonicalPath + File.separator) && target.isFile) {
                output.parentFile?.mkdirs()
                try {
                    Files.createLink(output.toPath(), target.toPath())
                } catch (exception: Exception) {
                    try {
                        target.copyTo(output, overwrite = true)
                    } catch (copyException: Exception) {
                        throw IOException("failed to materialize rootfs hard link ${output.name}", copyException)
                    }
                }
            }
        }
        return extractedEntries
    }

    // A rootfs that extracted "successfully" but lost its dynamic linker (or every
    // interpreter) cannot run any guest binary; installing it would previously mark
    // the rootfs ready and fail later with a misleading "command not found" (exit 127).
    // Match loosely across lib/lib64 and glibc/musl naming (ld-linux-*, ld-musl-*, ...).
    internal fun verifyRootfsLibraries(root: File): Boolean {
        val libRoots = listOf("lib", "lib64", "usr/lib", "usr/lib64")
        val linker = libRoots.any { prefix ->
            val dir = File(root, prefix)
            dir.isDirectory && dir.list()?.any { name -> name.startsWith("ld-") && name.contains(".so") } == true
        }
        return linker
    }

    private fun prepareRuntimeDirectories(root: File, mirrors: MirrorConfig) {
        listOf("tmp", "workspace", "host", "proc", "sys", "dev", "sdcard", "storage").forEach {
            File(root, it).mkdirs()
        }
        configureRootfsMirrors(root, mirrors)
        File(root, "tmp").setWritable(true, false)
    }

    internal fun normalizeDnsServer(value: String): String? {
        val candidate = value.trim()
        if (candidate.isEmpty() || candidate.length > 255 || candidate.any { it.isWhitespace() || it.isISOControl() }) return null
        if (':' !in candidate) {
            val octets = candidate.split('.')
            if (octets.size != 4 || octets.any { it.isEmpty() || it.length > 3 || it.any { character -> !character.isDigit() } }) return null
            if (octets.any { it.toIntOrNull() !in 0..255 }) return null
            return candidate
        }

        val zoneSeparator = candidate.indexOf('%')
        val address = if (zoneSeparator >= 0) candidate.substring(0, zoneSeparator) else candidate
        val zone = if (zoneSeparator >= 0) candidate.substring(zoneSeparator + 1) else ""
        if (candidate.indexOf('%', zoneSeparator + 1) >= 0) return null
        if (zoneSeparator >= 0 && !zone.matches(Regex("[A-Za-z0-9_.-]{1,64}"))) return null
        if (runCatching { InetAddress.getByName(address) }.getOrNull() !is Inet6Address) return null
        return candidate
    }

    internal fun normalizeDnsServers(servers: List<String>): List<String> =
        servers.mapNotNull(::normalizeDnsServer).distinct()

    internal fun formatResolvConf(servers: List<String>): String {
        val unique = LinkedHashSet<String>()
        normalizeDnsServers(servers).forEach(unique::add)
        return unique.joinToString(separator = "\n") { "nameserver $it" }.let { if (it.isEmpty()) it else "$it\n" }
    }

    internal fun atomicWriteResolvConf(
        root: File,
        servers: List<String>,
        syncDirectory: (java.nio.file.Path) -> Unit = { directory ->
            FileChannel.open(directory, StandardOpenOption.READ).use { it.force(true) }
        },
    ) {
        val normalizedServers = normalizeDnsServers(servers)
        require(normalizedServers.isNotEmpty()) { "no valid DNS servers" }
        val rootPath = root.toPath().toRealPath()
        val etcPath = rootPath.resolve("etc")
        if (!Files.exists(etcPath, LinkOption.NOFOLLOW_LINKS)) Files.createDirectory(etcPath)
        require(!Files.isSymbolicLink(etcPath)) { "rootfs etc must be a real directory" }
        val realEtc = etcPath.toRealPath()
        require(realEtc.startsWith(rootPath) && Files.isDirectory(realEtc)) { "rootfs etc escapes rootfs" }

        val target = realEtc.resolve("resolv.conf")
        val temp = Files.createTempFile(realEtc, ".resolv.conf.", ".tmp")
        try {
            FileChannel.open(temp, StandardOpenOption.WRITE).use { channel ->
                val bytes = formatResolvConf(normalizedServers).toByteArray(Charsets.UTF_8)
                val buffer = ByteBuffer.wrap(bytes)
                while (buffer.hasRemaining()) channel.write(buffer)
                channel.force(true)
            }
            try {
                Files.move(temp, target, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            } catch (_: AtomicMoveNotSupportedException) {
                Files.move(temp, target, StandardCopyOption.REPLACE_EXISTING)
            } catch (_: IOException) {
                Files.move(temp, target, StandardCopyOption.REPLACE_EXISTING)
            }
            syncDirectory(realEtc)
        } finally {
            Files.deleteIfExists(temp)
        }
    }

    internal fun filterBindCandidates(
        candidates: List<BindCandidate>,
        accessible: (String) -> Boolean,
    ): BindSelection {
        val enabled = candidates.filter { accessible(it.path) }
        return BindSelection(enabled, candidates - enabled.toSet())
    }

    private fun prepareRootfsBindTarget(root: File, guestPath: String) {
        val rootPath = root.toPath().toRealPath()
        val target = rootPath.resolve(guestPath.trimStart('/'))
        require(target.parent == rootPath) { "bind target must be a rootfs top-level directory" }
        if (!Files.exists(target, LinkOption.NOFOLLOW_LINKS)) Files.createDirectory(target)
        require(!Files.isSymbolicLink(target) && Files.isDirectory(target, LinkOption.NOFOLLOW_LINKS)) {
            "bind target must be a real rootfs directory: $guestPath"
        }
    }

    private fun selectHostBinds(): BindSelection = filterBindCandidates(HOST_BIND_CANDIDATES) { path ->
        runCatching {
            val stat = Os.stat(path)
            OsConstants.S_ISDIR(stat.st_mode) && Os.access(path, OsConstants.R_OK or OsConstants.X_OK)
        }.getOrDefault(false)
    }

    internal fun persistDnsConfig(
        root: File,
        source: String,
        servers: List<String>,
        updatedAt: String = Instant.now().toString(),
        writer: (File, List<String>) -> Unit = { target, values -> atomicWriteResolvConf(target, values) },
    ): DnsConfig {
        val normalizedServers = normalizeDnsServers(servers)
        val config = try {
            require(normalizedServers.isNotEmpty()) { "no valid DNS servers" }
            writer(root, normalizedServers)
            DnsConfig(source, normalizedServers, true, updatedAt, "")
        } catch (error: Exception) {
            val detail = error.message.orEmpty().replace(Regex("[\\r\\n]+"), " ").take(500)
            DnsConfig(source, normalizedServers, false, updatedAt, "${error.javaClass.simpleName}: $detail".trim())
        }
        lastDnsConfig = config
        return config
    }

    private fun refreshRootfsDns(context: Context, root: File): DnsConfig {
        val systemServers = runCatching {
            val manager = context.getSystemService(ConnectivityManager::class.java)
            manager.getLinkProperties(manager.activeNetwork)?.dnsServers.orEmpty()
                .map { it.hostAddress }
        }.getOrDefault(emptyList())
        val validSystemServers = normalizeDnsServers(systemServers)
        return if (validSystemServers.isEmpty()) {
            persistDnsConfig(root, "fallback", DNS_FALLBACK_SERVERS)
        } else {
            persistDnsConfig(root, "active_network", validSystemServers)
        }
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
        return marker == "downloaded:$abi:$distribution:$expected" &&
            AndroidRootfsDownloader.downloadedArchive(context, abi, distribution) != null
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
            if (release.isNotEmpty()) {
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
        val rootfsDir = File(context.filesDir, "runtimes/linux-rootfs/${currentAbi()}")
        val available = runtimeCapabilities(rootfsDir)
        val runtimes = JSONArray()
            .put(descriptorJson(RuntimeDescriptor(
                id = "linux-runtime-${currentAbi()}",
                language = "linux",
                version = rootfs.optString("distribution"),
                installed = rootfs.optBoolean("installed"),
                executable = rootfs.optJSONObject("runner")?.optString("path").orEmpty(),
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = available,
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "python-rootfs-android-${currentAbi()}",
                language = "python",
                version = if (pythonAvailable) rootfs.optString("distribution") else "",
                installed = pythonAvailable,
                executable = "/usr/bin/python3",
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = available.filter { it in setOf("python", "pip", "venv", "ssl", "sqlite", "crypto") },
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "node-rootfs-android-${currentAbi()}",
                language = "node",
                version = if (nodeAvailable) rootfs.optString("distribution") else "",
                installed = nodeAvailable,
                executable = "/usr/bin/node",
                home = rootfsHome,
                isolation = "layered-rootfs",
                capabilities = available.filter { it in setOf("node", "npm", "typescript", "commonjs", "esm") },
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

    internal fun runtimeCapabilities(root: File): List<String> = buildList {
        fun executable(vararg paths: String) = paths.any { File(root, it.trimStart('/')).let { file -> file.isFile && file.canExecute() } }
        if (executable("/bin/sh", "/bin/bash")) add("shell")
        if (executable("/usr/bin/python3")) {
            add("python")
            if (root.walkTopDown().maxDepth(8).any { it.isDirectory && it.name == "venv" && File(it, "__init__.py").isFile }) add("venv")
            if (root.walkTopDown().maxDepth(8).any { it.isFile && it.name.startsWith("_ssl.") }) add("ssl")
            if (root.walkTopDown().maxDepth(8).any { it.isFile && it.name.startsWith("_sqlite3.") }) add("sqlite")
            if (root.walkTopDown().maxDepth(10).any {
                    it.isDirectory && it.name == "Cipher" && it.parentFile?.name == "Crypto" && File(it, "AES.py").isFile
                }) add("crypto")
        }
        if (executable("/usr/bin/pip3", "/usr/bin/pip")) add("pip")
        if (executable("/usr/bin/node")) addAll(listOf("node", "commonjs", "esm"))
        if (executable("/usr/bin/npm")) add("npm")
        if (executable("/usr/bin/tsc", "/usr/local/bin/tsc")) add("typescript")
        if (executable("/usr/bin/git")) add("git")
        if (executable("/usr/bin/ssh")) add("ssh")
        if (executable("/usr/bin/go")) add("go-build")
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
        val dns = if (rootfsDir.isDirectory) {
            refreshRootfsDns(context, rootfsDir)
        } else {
            DnsConfig("unavailable", emptyList(), false, Instant.now().toString(), "rootfs directory is unavailable")
        }
        val binds = selectHostBinds()
        val builtInBinds = listOf(
            JSONObject().put("path", "/host-files").put("class", "core"),
            JSONObject().put("path", "/tmp/host-cache").put("class", "core"),
        )
        val enabledBinds = builtInBinds + binds.enabled.map { bind ->
            JSONObject().put("path", bind.path).put("class", if (bind.core) "core" else "optional")
        }
        val skippedBinds = binds.skipped.map { bind ->
            JSONObject().put("path", bind.path).put("class", if (bind.core) "core" else "optional")
        }
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
                .put("no_seccomp", false)
                .put("link2symlink", true)
                .put("kill_on_exit", true)
                .put("fake_kernel_release", "4.14.0")
                .put("host_uid", Process.myUid())
                .put("guest_uid", 0)
                .put("privilege_model", "proot-root-id-mapping")
                .put("binds", JSONArray(enabledBinds.map { it.getString("path") }))
                .put("enabled_binds", JSONArray(enabledBinds))
                .put("skipped_binds", JSONArray(skippedBinds))
                .put("dns_source", dns.source)
                .put("dns_servers", JSONArray(dns.servers))
                .put("dns", JSONObject()
                    .put("source", dns.source)
                    .put("servers", JSONArray(dns.servers))
                    .put("write_success", dns.writeSuccess)
                    .put("updated_at", dns.updatedAt)
                    .put("error", dns.error)))
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
