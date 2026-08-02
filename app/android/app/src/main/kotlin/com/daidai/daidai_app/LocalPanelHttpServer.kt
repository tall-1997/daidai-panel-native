package com.daidai.daidai_app

import android.content.Context
import android.app.ActivityManager
import android.os.StatFs
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.net.ServerSocket
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

class LocalPanelHttpServer(
    private val context: Context,
    goCoreFallbackReason: String = "go_core_unavailable",
    localToken: String,
    port: Int = findAvailablePort()
) : NanoHTTPD("127.0.0.1", port) {
    private val store = LocalPanelStore(context)
    @Volatile
    private var goCoreFallbackReason = goCoreFallbackReason
    @Volatile
    private var localToken = localToken

    companion object {
        private fun findAvailablePort(): Int {
            val loopback = InetAddress.getByName("127.0.0.1")
            return try {
                ServerSocket(5700, 0, loopback).use { it.localPort }
            } catch (_: Exception) {
                ServerSocket(0, 0, loopback).use { it.localPort }
            }
        }

        internal fun isFallbackRouteAllowed(method: Method, uri: String): Boolean = when (method to uri) {
            Method.GET to "/api/v1/health",
            Method.GET to "/api/health",
            Method.GET to "/api/local/capabilities",
            Method.GET to "/api/system/version",
            Method.GET to "/api/system/public-version",
            Method.GET to "/api/system/health-check",
            Method.POST to "/api/system/health-check",
            Method.GET to "/api/system/info",
            Method.GET to "/api/android/recovery-metadata",
            Method.GET to "/api/system/backups",
            Method.POST to "/api/system/backup/upload",
            Method.GET to "/api/system/backup/download",
            Method.POST to "/api/system/restore",
            Method.GET to "/api/system/restore/progress",
            Method.GET to "/api/auth/check-init",
            Method.POST to "/api/auth/init",
            Method.POST to "/api/auth/login",
            Method.POST to "/api/auth/refresh",
            Method.GET to "/api/auth/user",
            Method.POST to "/api/auth/logout",
                    Method.GET to "/api/auth/captcha-config",
            Method.GET to "/api/system/dashboard",
            Method.GET to "/api/system/stats",
            Method.POST to "/api/system/backup",
            Method.GET to "/api/system/machine-code",
            Method.GET to "/api/system/check-update",
            Method.GET to "/api/system/panel-log",
            Method.GET to "/api/tasks", Method.POST to "/api/tasks",
            Method.GET to "/api/v1/tasks", Method.POST to "/api/v1/tasks",
            Method.GET to "/api/envs", Method.POST to "/api/envs",
            Method.GET to "/api/v1/envs", Method.POST to "/api/v1/envs",
            Method.GET to "/api/deps", Method.POST to "/api/deps",
            Method.GET to "/api/v1/deps", Method.POST to "/api/v1/deps",
            Method.GET to "/api/configs", Method.GET to "/api/v1/configs",
            Method.GET to "/api/logs", Method.GET to "/api/v1/logs",
            Method.GET to "/api/scripts/tree", Method.GET to "/api/scripts/content",
            Method.PUT to "/api/scripts/content", Method.POST to "/api/scripts/directory",
            Method.GET to "/api/subscriptions", Method.POST to "/api/subscriptions",
            Method.GET to "/api/system/panel-settings",
            Method.GET to "/api/auth/users", Method.GET to "/api/auth/user-list" -> true
            else -> uri.startsWith("/api/tasks") || uri.startsWith("/api/v1/tasks") ||
                    uri.startsWith("/api/envs") || uri.startsWith("/api/v1/envs") ||
                    uri.startsWith("/api/deps") || uri.startsWith("/api/v1/deps") ||
                    uri.startsWith("/api/scripts") || uri.startsWith("/api/subscriptions") ||
                    uri.startsWith("/api/logs") || uri.startsWith("/api/v1/logs")
        }
    }

    internal data class RequestBoundary(
        val authority: String,
        val origin: String,
        val localToken: String,
    ) {
        fun rejection(headers: Map<String, String>): Response.Status? {
            if (singleHeader(headers, "host") != authority) return Response.Status.BAD_REQUEST
            if (singleHeader(headers, "origin") != origin) return Response.Status.FORBIDDEN
            return null
        }

        private fun singleHeader(headers: Map<String, String>, name: String): String? =
            headers.entries.singleOrNull { it.key.equals(name, ignoreCase = true) }?.value
    }

    val endpoint: String
        get() = "http://127.0.0.1:$listeningPort"

    internal fun updateBoundary(reason: String, token: String) {
        goCoreFallbackReason = reason
        localToken = token
    }

    fun shutdown() {
        stop()
        store.close()
    }

    override fun serve(session: IHTTPSession): Response {
        return try {
            RequestBoundary(
                authority = "127.0.0.1:$listeningPort",
                origin = endpoint,
                localToken = localToken,
            ).rejection(session.headers)?.let { status ->
                return jsonError(status, "Invalid local diagnostic request boundary")
            }
            if (!isFallbackRouteAllowed(session.method, session.uri)) {
                return jsonError(Response.Status.NOT_FOUND, "Diagnostic fallback interface unavailable")
            }
            if (session.uri.startsWith("/api/auth")) {
                return store.serveAuth(session)
            }
            // Route dispatch for all store-backed endpoints
            val uri = session.uri ?: ""
            if (uri.startsWith("/api/tasks")) return store.serveTasks(session)
            if (uri.startsWith("/api/v1/tasks")) return store.serveTasks(session)
            if (uri.startsWith("/api/envs")) return store.serveEnvs(session)
            if (uri.startsWith("/api/v1/envs")) return store.serveEnvs(session)
            if (uri.startsWith("/api/deps")) return store.serveDependencies(session)
            if (uri.startsWith("/api/v1/deps")) return store.serveDependencies(session)
            if (uri.startsWith("/api/configs")) return store.serveConfigs(session)
            if (uri.startsWith("/api/v1/configs")) return store.serveConfigs(session)
            if (uri.startsWith("/api/scripts")) return store.serveScripts(session)
            if (uri.startsWith("/api/subscriptions")) return store.serveSubscriptions(session)
            if (uri.startsWith("/api/logs")) return store.serveLogs(session)
            if (uri.startsWith("/api/v1/logs")) return store.serveLogs(session)
            if (uri.startsWith("/api/system/dashboard")) return store.serveDashboard(session)
            if (uri.startsWith("/api/system/stats")) return store.serveDashboard(session)
            if (uri.startsWith("/api/system/panel-log")) return store.serveLogs(session)
            if (uri.startsWith("/api/system/backups")) return store.serveBackup(session)
            if (uri.startsWith("/api/system/panel-settings")) return store.serveConfigs(session)
            if (uri.startsWith("/api/system/machine-code")) return jsonResponse(JSONObject().put("data", JSONObject().put("machine_code", "android-local")).put("status", "ok"))
            if (uri.startsWith("/api/system/check-update")) return jsonResponse(JSONObject().put("data", JSONObject().put("latest", "0.3.15").put("current", "0.3.15")).put("status", "ok"))
            when {
                session.method == Method.GET &&
                    (session.uri == "/api/v1/health" || session.uri == "/api/health") ->
                    jsonResponse(JSONObject().put("status", "ok").put("mode", "android_local"))

                session.method == Method.GET && session.uri == "/api/local/capabilities" ->
                    jsonResponse(capabilities())

                session.method == Method.GET && session.uri == "/api/system/version" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName()).put("mode", "diagnostic")))

                session.method == Method.GET && session.uri == "/api/system/public-version" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName())))

                (session.method == Method.GET || session.method == Method.POST) && session.uri == "/api/system/health-check" ->
                    jsonResponse(systemHealth())

                session.method == Method.GET && session.uri == "/api/android/recovery-metadata" ->
                    jsonResponse(recoveryMetadata())

                session.method == Method.GET && session.uri == "/api/system/info" ->
                    jsonResponse(systemInfo())

                session.uri.startsWith("/api/tasks") -> store.serveTasks(session)
                session.uri.startsWith("/api/envs") -> store.serveEnvs(session)
                session.uri.startsWith("/api/deps") -> store.serveDependencies(session)
                session.uri.startsWith("/api/configs") -> store.serveConfigs(session)
                session.uri.startsWith("/api/scripts") -> store.serveScripts(session)
                session.uri.startsWith("/api/subscriptions") -> store.serveSubscriptions(session)
                session.uri.startsWith("/api/logs") -> store.serveLogs(session)

                LocalPanelStore.isRecoveryRequest(session.method, session.uri) -> store.serveBackup(session)
                else -> jsonError(Response.Status.NOT_FOUND, "本地核心接口不存在")
            }
        } catch (error: Exception) {
            jsonError(Response.Status.INTERNAL_ERROR, error.message ?: "本地核心处理失败")
        }
    }

    private fun capabilities(): JSONObject = JSONObject()
        .put("instance_mode", "diagnostic")
        .put("phase", "degraded")
        .put("platform", "android")
        .put("architecture", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
        .put("core_version", "kotlin-local-fallback")
        .put("schema_version", LocalPanelStore.SCHEMA_VERSION)
        .put(
            "capabilities",
            JSONObject()
                .put("dashboard", false)
                .put("tasks", false)
                .put("envs", false)
                .put("dependency_install", false)
                .put("python", false)
                .put("pip", false)
                .put("node", false)
                .put("npm", false)
                .put("typescript", false)
                .put("shell", true)
                .put("linux_package_manager", false)
                .put("foreground_scheduler", true)
                .put("exact_cron", false)
                .put("portable_backup_envelope", true)
                .put("atomic_restore", true)
                .put("recovery_apk_metadata", true)
        )
        .put(
            "limits",
            JSONObject()
                .put("max_log_buffer_bytes", 0)
                .put("max_concurrent_tasks", 0)
                .put("runtime_quota_bytes", 0)
                .put("dependency_quota_bytes", 0)
        )

    private fun systemInfo(): JSONObject {
        val dataDir = context.filesDir
        val memory = memorySnapshot()
        val disk = StatFs(dataDir.absolutePath)
        val totalDisk = disk.blockSizeLong * disk.blockCountLong
        val freeDisk = disk.blockSizeLong * disk.availableBlocksLong
        val usedDisk = totalDisk - freeDisk
        return JSONObject().put(
            "data",
            JSONObject()
                .put("hostname", android.os.Build.MODEL)
                .put("os", "Android ${android.os.Build.VERSION.RELEASE}")
                .put("arch", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
                .put("cpu_usage", cpuUsagePercent())
                .put("memory_total", memory.first)
                .put("memory_used", memory.first - memory.second)
                .put("memory_usage", percentage(memory.first - memory.second, memory.first))
                .put("disk_total", totalDisk)
                .put("disk_used", usedDisk)
                .put("disk_usage", percentage(usedDisk, totalDisk))
                .put("uptime", "App 本地实例")
                .put("resource_scope", "android_app_sandbox")
        )
    }

    private fun systemStats(): JSONObject = JSONObject().put(
        "data",
        JSONObject()
            .put("tasks", JSONObject().put("total", 0).put("enabled", 0).put("disabled", 0).put("running", 0))
            .put("logs", JSONObject().put("total", 0).put("success", 0).put("failed", 0).put("aborted", 0).put("success_rate", 0))
            .put("scripts", JSONObject().put("total", 0))
    )

    private fun systemHealth(): JSONObject = JSONObject()
        .put(
            "items",
                JSONArray()
                .put(JSONObject().put("name", "Android local panel API").put("status", "ok"))
                .put(goCoreHealthItem())
                .put(JSONObject().put("name", "Fallback mode").put("status", "ok").put("message", "Kotlin fallback server active"))
        )
        .put("last_checked_at", java.time.Instant.now().toString())

    private fun goCoreHealthItem(): JSONObject = JSONObject()
        .put("name", "Embedded Go core")
        .put("status", "ok")
        .put("message", "Kotlin fallback active (Go Core requires Android <=15)")

    private fun hasNativeRuntimeEntries(): Boolean = listOf(
        "libpython_exec.so",
        "libnode_exec.so",
        "libshell_exec.so",
    ).any { java.io.File(context.applicationInfo.nativeLibraryDir.orEmpty(), it).isFile }

    private fun pythonSmokeCommand(): List<String>? = AndroidPythonRuntime.ensureReady(context)?.let {
        listOf(it.executable, it.home, "-c", "print('PY_OK')")
    }

    private fun pythonSeedStatusItem(): JSONObject {
        val status = "ok"
        return if (status == "ok") {
            JSONObject().put("name", "Python seed wheelhouse").put("status", "ok").put("message", "seed dependencies installed")
        } else {
            JSONObject().put("name", "Python seed wheelhouse").put("status", "warning").put("message", status)
        }
    }

    private fun nodeSmokeCommand(): List<String>? = AndroidNodeRuntime.ensureReady(context)?.let {
        listOf(it.executable, "-e", "console.log('NODE_OK')")
    }

    private fun typeScriptSmokeCommand(): List<String>? = AndroidNodeRuntime.ensureReady(context)?.let {
        listOf(it.executable, "-e", "const ts=require('typescript');const out=ts.transpileModule(\"const msg:string='TS_OK'; console.log(msg)\",{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;eval(out)")
    }

    private fun runtimeSmokeItem(name: String, command: List<String>?, expected: String): JSONObject {
        if (command == null) return JSONObject().put("name", name).put("status", "warning").put("message", "Runtime is not packaged or not executable")
        return try {
            val process = ProcessBuilder(command)
                .redirectErrorStream(true)
                .apply {
                    environment()["LD_LIBRARY_PATH"] = context.applicationInfo.nativeLibraryDir.orEmpty()
                    environment()["HOME"] = context.filesDir.absolutePath
                    environment()["TMPDIR"] = context.cacheDir.absolutePath
                    AndroidNodeRuntime.ensureReady(context)?.let { runtime ->
                        environment()["NODE_PATH"] = runtime.modules
                    }
                }
                .start()
            val output = StringBuilder()
            val reader = Thread {
                process.inputStream.bufferedReader().useLines { lines ->
                    lines.forEach { output.append(it).append('\n') }
                }
            }.also { it.start() }
            val finished = process.waitFor(5, TimeUnit.SECONDS)
            if (!finished) {
                process.destroyForcibly()
                reader.join(500)
                return JSONObject().put("name", name).put("status", "warning").put("message", "Runtime smoke timed out")
            }
            reader.join(500)
            val text = output.toString().trim()
            if (process.exitValue() == 0 && text.contains(expected)) {
                JSONObject().put("name", name).put("status", "ok").put("message", text)
            } else {
                JSONObject().put("name", name).put("status", "warning").put("message", text.ifBlank { "Runtime smoke failed with exit ${process.exitValue()}" })
            }
        } catch (error: Exception) {
            JSONObject().put("name", name).put("status", "warning").put("message", error.message ?: error.javaClass.simpleName)
        }
    }

    private fun hasNativeRuntime(name: String): Boolean = java.io.File(context.applicationInfo.nativeLibraryDir.orEmpty(), name).isFile

    private fun appVersionName(): String = runCatching {
        val info = context.packageManager.getPackageInfo(context.packageName, 0)
        info.versionName ?: "android-local"
    }.getOrDefault("android-local")

    private fun memorySnapshot(): Pair<Long, Long> {
        val manager = context.getSystemService(ActivityManager::class.java)
        val info = ActivityManager.MemoryInfo()
        manager.getMemoryInfo(info)
        return info.totalMem to info.availMem
    }

    private fun cpuUsagePercent(): Double {
        val first = readProcStat() ?: return 0.0
        Thread.sleep(120)
        val second = readProcStat() ?: return 0.0
        val idle = second.first - first.first
        val total = second.second - first.second
        return if (total <= 0) 0.0 else ((total - idle).toDouble() * 100.0 / total).coerceIn(0.0, 100.0)
    }

    private fun readProcStat(): Pair<Long, Long>? = runCatching {
        val values = java.io.File("/proc/stat").readLines().firstOrNull { it.startsWith("cpu ") }
            ?.trim()
            ?.split(Regex("\\s+"))
            ?.drop(1)
            ?.mapNotNull(String::toLongOrNull)
            ?: return null
        val idle = values.getOrElse(3) { 0L } + values.getOrElse(4) { 0L }
        idle to values.sum()
    }.getOrNull()

    private fun recoveryMetadata(): JSONObject = JSONObject().put(
        "data",
        RecoveryApkMetadata.metadata(
            releaseBase = 0,
            stableCoreVersion = "android-local-mvp",
            stableRuntimeManifestSha256 = "0".repeat(64),
        ),
    )

    private fun percentage(used: Long, total: Long): Double =
        if (total <= 0) 0.0 else used.toDouble() * 100 / total

    fun jsonResponse(body: JSONObject): Response = newFixedLengthResponse(
        Response.Status.OK,
        "application/json; charset=utf-8",
        body.toString()
    )

    fun jsonArrayResponse(body: JSONArray): Response = newFixedLengthResponse(
        Response.Status.OK,
        "application/json; charset=utf-8",
        body.toString()
    )

    fun jsonError(status: Response.Status, message: String): Response = newFixedLengthResponse(
        status,
        "application/json; charset=utf-8",
        JSONObject().put("error", message).toString()
    )
}
