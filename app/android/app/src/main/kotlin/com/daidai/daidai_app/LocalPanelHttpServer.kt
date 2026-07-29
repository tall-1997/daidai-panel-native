package com.daidai.daidai_app

import android.content.Context
import android.app.ActivityManager
import android.os.StatFs
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.TimeUnit

class LocalPanelHttpServer(
    private val context: Context,
    private val goCoreFallbackReason: String = "go_core_unavailable",
    port: Int = findAvailablePort()
) : NanoHTTPD("127.0.0.1", port) {
    private val store = LocalPanelStore(context)

    companion object {
        private fun findAvailablePort(): Int {
            val loopback = InetAddress.getByName("127.0.0.1")
            return try {
                ServerSocket(5700, 0, loopback).use { it.localPort }
            } catch (_: Exception) {
                ServerSocket(0, 0, loopback).use { it.localPort }
            }
        }
    }

    val endpoint: String
        get() = "http://127.0.0.1:$listeningPort"

    override fun serve(session: IHTTPSession): Response {
        return try {
            if (session.uri.startsWith("/api/auth")) {
                return store.serveAuth(session)
            }
            if (!store.isPublicRequest(session) && !store.isAuthorized(session)) {
                return jsonError(Response.Status.UNAUTHORIZED, "本地会话已失效")
            }
            when {
                session.method == Method.GET &&
                    (session.uri == "/api/v1/health" || session.uri == "/api/health") ->
                    jsonResponse(JSONObject().put("status", "ok").put("mode", "android_local"))

                session.method == Method.GET && session.uri == "/api/local/capabilities" ->
                    jsonResponse(capabilities())

                session.method == Method.GET && session.uri == "/api/system/version" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName()).put("mode", "android_local")))

                session.method == Method.GET && session.uri == "/api/system/public-version" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName())))

                session.method == Method.GET && session.uri == "/api/system/machine-code" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("machine_code", "android-${android.provider.Settings.Secure.getString(context.contentResolver, android.provider.Settings.Secure.ANDROID_ID).orEmpty()}")))

                session.method == Method.GET && session.uri == "/api/system/check-update" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("has_update", false).put("current_version", appVersionName())))

                session.method == Method.GET && session.uri == "/api/system/update-status" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("status", "idle").put("deployment_type", "android_apk")))

                session.method == Method.POST && session.uri == "/api/system/update" ->
                    jsonResponse(JSONObject().put("message", "Android APK 更新请安装新版 Release 包"))

                session.method == Method.POST && session.uri == "/api/system/restart" ->
                    jsonResponse(JSONObject().put("message", "Android 本地面板服务保持运行"))

                (session.method == Method.GET || session.method == Method.POST) && session.uri == "/api/system/health-check" ->
                    jsonResponse(systemHealth())

                session.method == Method.GET && session.uri == "/api/android/recovery-metadata" ->
                    jsonResponse(recoveryMetadata())

                session.method == Method.GET && session.uri == "/api/system/info" ->
                    jsonResponse(systemInfo())

                session.method == Method.GET && session.uri == "/api/system/stats" ->
                    jsonResponse(systemStats())

                session.uri.startsWith("/api/system/backup") || session.uri.startsWith("/api/system/backups") ||
                    session.uri.startsWith("/api/system/restore") -> store.serveBackup(session)

                session.uri.startsWith("/api/logs") || session.uri.startsWith("/api/v1/logs") ->
                    store.serveLogs(session)

                session.method == Method.GET && session.uri == "/api/system/dashboard" ->
                    jsonResponse(store.dashboard())

                session.method == Method.GET && session.uri == "/api/system/panel-log" ->
                    store.panelLog(session)

                session.method == Method.GET && session.uri == "/api/system/panel-settings" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("panel_title", "呆呆本地面板")))

                session.uri.startsWith("/api/tasks") || session.uri.startsWith("/api/v1/tasks") -> store.serveTasks(session)
                session.uri.startsWith("/api/scripts") || session.uri.startsWith("/api/v1/scripts") -> store.serveScripts(session)
                session.uri.startsWith("/api/envs") || session.uri.startsWith("/api/v1/envs") -> store.serveEnvs(session)
                session.uri.startsWith("/api/deps") || session.uri.startsWith("/api/v1/deps") ->
                    store.serveDependencies(session)
                session.uri.startsWith("/api/configs") || session.uri.startsWith("/api/v1/configs") ->
                    store.serveConfigs(session)
                else -> jsonError(Response.Status.NOT_FOUND, "本地核心接口不存在")
            }
        } catch (error: Exception) {
            jsonError(Response.Status.INTERNAL_ERROR, error.message ?: "本地核心处理失败")
        }
    }

    private fun capabilities(): JSONObject = JSONObject()
        .put("instance_mode", "android_local")
        .put("platform", "android")
        .put("architecture", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
        .put("core_version", "android-local-mvp")
        .put("schema_version", LocalPanelStore.SCHEMA_VERSION)
        .put(
            "capabilities",
            JSONObject()
                .put("dashboard", true)
                .put("tasks", true)
                .put("envs", true)
                .put("dependency_install", true)
                .put("python", true)
                .put("pip", true)
                .put("node", hasNativeRuntime("libnode_exec.so"))
                .put("npm", hasNativeRuntime("libnode_exec.so"))
                .put("typescript", hasNativeRuntime("libnode_exec.so"))
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
                .put("max_log_buffer_bytes", 1024 * 1024)
                .put("max_concurrent_tasks", 1)
                .put("runtime_quota_bytes", 1024L * 1024 * 1024)
                .put("dependency_quota_bytes", 1024L * 1024 * 1024)
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
                .put(JSONObject().put("name", "Android local HTTP API").put("status", "ok"))
                .put(goCoreHealthItem())
                .put(JSONObject().put("name", "Local management core").put("status", "ok").put("message", "Kotlin fallback is serving local management APIs"))
                .put(runtimeSmokeItem("Python runtime", pythonSmokeCommand(), "PY_OK"))
                .put(pythonSeedStatusItem())
                .put(runtimeSmokeItem("Node runtime", nodeSmokeCommand(), "NODE_OK"))
                .put(runtimeSmokeItem("TypeScript runtime", typeScriptSmokeCommand(), "TS_OK"))
        )
        .put("last_checked_at", java.time.Instant.now().toString())

    private fun goCoreHealthItem(): JSONObject = JSONObject()
        .put("name", "Embedded Go core")
        .put("status", "warning")
        .put("message", goCoreFallbackReason.ifBlank { "go_core_unavailable" })

    private fun hasNativeRuntimeEntries(): Boolean = listOf(
        "libpython_exec.so",
        "libnode_exec.so",
        "libshell_exec.so",
    ).any { java.io.File(context.applicationInfo.nativeLibraryDir.orEmpty(), it).isFile }

    private fun pythonSmokeCommand(): List<String>? = AndroidPythonRuntime.ensureReady(context)?.let {
        listOf(it.executable, it.home, "-c", "print('PY_OK')")
    }

    private fun pythonSeedStatusItem(): JSONObject {
        val status = AndroidPythonRuntime.seedStatus(context)
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
