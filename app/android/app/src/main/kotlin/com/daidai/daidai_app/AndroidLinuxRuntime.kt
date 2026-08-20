package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.net.URI
import java.nio.file.Files
import java.nio.file.Paths
import java.util.zip.GZIPInputStream
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.json.JSONArray
import org.json.JSONObject

object AndroidLinuxRuntime {
    private const val ROOTFS_ASSET_PREFIX = "android-runtime"
    private const val ROOTFS_READY_MARKER = ".daidai-rootfs-ready"
    private const val ROOTFS_ASSET_NAME = "rootfs.tar.gz.bin"
    private const val ROOTFS_SHA256_ASSET_NAME = "rootfs.tar.gz.bin.sha256"
    const val ALPINE_APK_HUAWEI_MIRROR = "https://repo.huaweicloud.com/alpine"
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
        val linuxMirror: String = ALPINE_APK_HUAWEI_MIRROR,
    )

    data class RootfsPaths(
        val root: File,
        val proot: File,
        val busybox: File?,
        val packageManager: String,
    )

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

    fun baseEnvironment(context: Context, workingDir: File): MutableMap<String, String> {
        val mirrors = mirrorConfig(context)
        return mutableMapOf(
            "HOME" to context.filesDir.absolutePath,
            "TMPDIR" to context.cacheDir.absolutePath,
            "PWD" to workingDir.absolutePath,
            "DAIDAI_ANDROID_LOCAL" to "1",
            "DAIDAI_RUNTIME_ISOLATION" to "android-app-sandbox",
            "DAIDAI_RUNTIME_ABI" to currentAbi(),
            "LD_LIBRARY_PATH" to nativeLibraryDir(context).absolutePath,
            "PROOT_NO_SECCOMP" to "1",
            "PROOT_TMP_DIR" to context.cacheDir.absolutePath,
            "PROOT_VERBOSE" to "0",
        ).apply { putAll(mirrorEnvironment(mirrors)) }
    }

    internal fun mirrorEnvironment(mirrors: MirrorConfig): Map<String, String> = mapOf(
        "PIP_INDEX_URL" to mirrors.pipMirror,
        "NPM_CONFIG_REGISTRY" to mirrors.npmMirror,
        "npm_config_registry" to mirrors.npmMirror,
        "DAIDAI_LINUX_MIRROR" to mirrors.linuxMirror,
    )

    internal fun pipInstallArguments(mirror: String, target: String, packageSpec: String): List<String> =
        listOf("install", "--no-input", "--only-binary=:all:", "-i", mirror, "--target", target, packageSpec)

    internal fun npmInstallArguments(mirror: String, prefix: String, packageSpec: String): List<String> =
        listOf("install", "--ignore-scripts", "--registry", mirror, "--prefix", prefix, packageSpec)

    fun mirrorConfig(context: Context): MirrorConfig = synchronized(mirrorConfigLock) {
        val preferences = context.getSharedPreferences(MIRROR_PREFERENCES, Context.MODE_PRIVATE)
        MirrorConfig(
            pipMirror = normalizeMirrorUrl(preferences.getString(PIP_MIRROR_KEY, null).orEmpty()) ?: PYTHON_PIP_ALIBABA_INDEX,
            npmMirror = normalizeMirrorUrl(preferences.getString(NPM_MIRROR_KEY, null).orEmpty()) ?: NODE_NPM_NPMMIRROR_REGISTRY,
            linuxMirror = normalizeMirrorUrl(preferences.getString(LINUX_MIRROR_KEY, null).orEmpty()) ?: ALPINE_APK_HUAWEI_MIRROR,
        )
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

    fun ensureRootfsReady(context: Context, mirrors: MirrorConfig = mirrorConfig(context)): RootfsPaths? = synchronized(mirrorConfigLock) {
        val abi = currentAbi()
        val root = File(context.filesDir, "runtimes/linux-rootfs/$abi")
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so", "liboperit_proot.so")) ?: return@synchronized null
        val busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so", "liboperit_busybox.so"))
        if (!File(root, ROOTFS_READY_MARKER).isFile) {
            installRootfsAsset(context, root, mirrors) ?: return@synchronized null
        }
        prepareRuntimeDirectories(root, mirrors)
        RootfsPaths(root = root, proot = proot, busybox = busybox, packageManager = detectPackageManager(root))
    }

    fun shellCommand(context: Context, hostScript: File, workingDir: File): List<String>? {
        val rootfs = ensureRootfsReady(context) ?: return null
        val guestScript = "/workspace/${hostScript.name}"
        return prootCommand(context, rootfs, workingDir, "/workspace", listOf("/bin/sh", guestScript))
    }

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
        return assetExists(context, "$ROOTFS_ASSET_PREFIX/$abi/$ROOTFS_ASSET_NAME") &&
            resolveNativeTool(context, listOf("libdaidai_proot.so", "liboperit_proot.so")) != null
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
            "--kill-on-exit",
            "-k", "4.14.0",
            "-r", rootfs.root.absolutePath,
            "-w", guestWorkingDir,
            "-b", "${hostWorkingDir.absolutePath}:$guestWorkingDir",
            "-b", "${context.filesDir.absolutePath}:/host-files",
            "-b", "${context.cacheDir.absolutePath}:/tmp/host-cache",
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
        "--kill-on-exit",
        "-k", "4.14.0",
        "-0",
    )

    private fun installRootfsAsset(context: Context, root: File, mirrors: MirrorConfig): RootfsPaths? {
        val abi = currentAbi()
        val assetName = "$ROOTFS_ASSET_PREFIX/$abi/$ROOTFS_ASSET_NAME"
        if (!assetExists(context, assetName)) return null
        val proot = resolveNativeTool(context, listOf("libdaidai_proot.so", "liboperit_proot.so")) ?: return null
        root.deleteRecursively()
        root.mkdirs()
        try {
            context.assets.open(assetName).use { raw ->
                GZIPInputStream(raw).use { gzip ->
                    TarArchiveInputStream(gzip).use { tar ->
                        extractTar(root, tar)
                    }
                }
            }
            prepareRuntimeDirectories(root, mirrors)
            File(root, ROOTFS_READY_MARKER).writeText("ready:$abi:${detectPackageManager(root)}")
        } catch (_: Exception) {
            root.deleteRecursively()
            return null
        }
        return RootfsPaths(root = root, proot = proot, busybox = resolveNativeTool(context, listOf("libdaidai_busybox.so", "liboperit_busybox.so")), packageManager = detectPackageManager(root))
    }

    private fun extractTar(root: File, tar: TarArchiveInputStream) {
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
                entry.isFile -> {
                    output.parentFile?.mkdirs()
                    output.outputStream().use { tar.copyTo(it) }
                    output.setReadable(entry.mode and 0b100000000 != 0, false)
                    output.setWritable(entry.mode and 0b010000000 != 0, false)
                    output.setExecutable(entry.mode and 0b001000000 != 0, false)
                }
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

    internal fun configureRootfsMirrors(root: File, mirrors: MirrorConfig) {
        if (File(root, "etc/alpine-release").isFile || File(root, "sbin/apk").isFile || File(root, "usr/sbin/apk").isFile) {
            val release = File(root, "etc/alpine-release").readTextOrNull()?.trim()?.substringBeforeLast('.')?.takeIf { it.startsWith("3") } ?: "latest-stable"
            File(root, "etc/apk/repositories").apply {
                parentFile?.mkdirs()
                writeText("${mirrors.linuxMirror}/v$release/main\n${mirrors.linuxMirror}/v$release/community\n")
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
        val python = AndroidPythonRuntime.ensureReady(context)
        val node = AndroidNodeRuntime.ensureReady(context)
        val rootfs = rootfsStatus(context)
        val runtimes = JSONArray()
            .put(descriptorJson(RuntimeDescriptor(
                id = "python-3.14-android-arm64",
                language = "python",
                version = if (python != null) "3.14" else "",
                installed = python != null,
                executable = python?.wrapperScript.orEmpty(),
                home = python?.home.orEmpty(),
                isolation = "android-app-sandbox",
                capabilities = listOf("python", "pip", "venv", "ssl", "sqlite"),
            )))
            .put(descriptorJson(RuntimeDescriptor(
                id = "node-lts-android-arm64",
                language = "node",
                version = if (node != null) "18.20.4" else "",
                installed = node != null,
                executable = AndroidNodeRuntime.wrapperPath,
                home = node?.home.orEmpty(),
                isolation = "android-app-sandbox",
                capabilities = listOf("node", "npm", "typescript", "commonjs", "esm"),
            )))
        return JSONObject()
            .put("supported", true)
            .put("arch", currentAbi())
            .put("bin_dir", nativeLibraryDir(context).absolutePath)
            .put("termux_detected", false)
            .put("container_model", "layered-linux-runtime")
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
        val rootfsAsset = "$ROOTFS_ASSET_PREFIX/$abi/$ROOTFS_ASSET_NAME"
        val checksumAsset = "$ROOTFS_ASSET_PREFIX/$abi/$ROOTFS_SHA256_ASSET_NAME"
        val rootfsPackaged = assetExists(context, rootfsAsset)
        val rootfsDir = File(context.filesDir, "runtimes/linux-rootfs/$abi")
        return JSONObject()
            .put("id", "alpine-rootfs-android-arm64")
            .put("distribution", rootfsDistribution(rootfsDir))
            .put("installed", File(rootfsDir, ROOTFS_READY_MARKER).isFile)
            .put("packaged", rootfsPackaged)
            .put("asset", if (rootfsPackaged) rootfsAsset else "")
            .put("sha256_asset", if (assetExists(context, checksumAsset)) checksumAsset else "")
            .put("runner", prootRunnerStatus(context))
            .put("package_manager", detectPackageManager(rootfsDir))
            .put("compatibility", JSONObject()
                .put("no_seccomp", true)
                .put("link2symlink", true)
                .put("kill_on_exit", true)
                .put("fake_kernel_release", "4.14.0")
                .put("binds", JSONArray(listOf("/proc", "/dev", "/sys", "/sdcard", "/storage", "/host-files", "/tmp/host-cache"))))
            .put("mirrors", JSONObject()
                .put("apk", mirrors.linuxMirror)
                .put("pip", mirrors.pipMirror)
                .put("npm", mirrors.npmMirror))
    }

    private fun prootRunnerStatus(context: Context): JSONObject {
        val nativeDir = nativeLibraryDir(context)
        return JSONObject()
            .put("proot", File(nativeDir, "libdaidai_proot.so").isFile || File(nativeDir, "liboperit_proot.so").isFile)
            .put("proot_executable", listOf("libdaidai_proot.so", "liboperit_proot.so").map { File(nativeDir, it) }.any { it.isFile && it.canExecute() })
            .put("busybox", File(nativeDir, "libdaidai_busybox.so").isFile || File(nativeDir, "liboperit_busybox.so").isFile)
            .put("busybox_executable", listOf("libdaidai_busybox.so", "liboperit_busybox.so").map { File(nativeDir, it) }.any { it.isFile && it.canExecute() })
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
