package com.daidai.daidai_app

import android.content.Context
import android.app.ActivityManager
import android.os.StatFs
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
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
    private val store = LocalPanelStore(
        context,
        endpointProvider = { endpoint },
        localTokenProvider = { localToken },
    )
    private val cronScheduler = AndroidFallbackCronScheduler(store)
    private val browserAccess = LocalBrowserAccess(context, endpoint = { endpoint })
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

        internal fun isFallbackRouteAllowed(method: Method, uri: String): Boolean =
            uri.startsWith("/api/")
    }

    internal data class RequestBoundary(
        val authority: String,
        val origin: String,
        val localToken: String,
    ) {
        fun rejection(headers: Map<String, String>, browserSession: Boolean = false): Response.Status? {
            val host = singleHeader(headers, "host")
            val requestOrigin = singleHeader(headers, "origin")
            val token = singleHeader(headers, "x-daidai-local-token")
            if (host != authority) {
                println("RequestBoundary: host mismatch: '$host' vs '$authority'")
                return Response.Status.BAD_REQUEST
            }
            if (browserSession) {
                if (requestOrigin != null && requestOrigin != origin) {
                    println("RequestBoundary: origin mismatch (browser): '$requestOrigin' vs '$origin'")
                    return Response.Status.FORBIDDEN
                }
            } else {
                if (requestOrigin != origin) {
                    println("RequestBoundary: origin mismatch: '$requestOrigin' vs '$origin'")
                    return Response.Status.FORBIDDEN
                }
                if (localToken.isBlank() || token != localToken) {
                    println("RequestBoundary: token mismatch: blank=${localToken.isBlank()}, got='$token' expect='${localToken.take(8)}...'")
                    return Response.Status.UNAUTHORIZED
                }
            }
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

    internal fun startScheduler() = cronScheduler.start()

    internal fun createBrowserUrl(): String = browserAccess.createUrl()

    fun shutdown() {
        cronScheduler.close()
        browserAccess.clear()
        stop()
        store.close()
    }

    override fun serve(session: IHTTPSession): Response {
        return try {
            if (session.uri == "/local-ui" || session.uri.startsWith("/local-ui/")) {
                return browserAccess.serve(session, "127.0.0.1:$listeningPort")
            }
            val browserSession = browserAccess.hasSession(session.headers)
            RequestBoundary(
                authority = "127.0.0.1:$listeningPort",
                origin = endpoint,
                localToken = localToken,
            ).rejection(session.headers, browserSession)?.let { status ->
                val reason = when (status) {
                    NanoHTTPD.Response.Status.BAD_REQUEST -> "host mismatch"
                    NanoHTTPD.Response.Status.FORBIDDEN -> "origin mismatch"
                    NanoHTTPD.Response.Status.UNAUTHORIZED -> "local token mismatch"
                    else -> "request boundary rejected"
                }
                return jsonError(status, "Invalid local diagnostic request boundary ($reason)")
            }
            if (!isFallbackRouteAllowed(session.method, session.uri)) {
                return jsonError(Response.Status.NOT_FOUND, "Diagnostic fallback interface unavailable")
            }
            if (session.uri.startsWith("/api/auth")) {
                return store.serveAuth(session)
            }
            if (browserSession && !store.isAuthorized(session)) {
                return jsonError(Response.Status.UNAUTHORIZED, "Business API requires a valid user JWT")
            }
            if (session.uri.startsWith("/api/security")) {
                return store.serveSecurity(session)
            }
            // Route dispatch for all store-backed endpoints
            val uri = session.uri ?: ""
            if (uri.startsWith("/api/users") || uri.startsWith("/api/v1/users")) return store.serveUsers(session, if (uri.startsWith("/api/v1")) "/api/v1/users" else "/api/users")
            if (listOf("/api/ssh-keys","/api/v1/ssh-keys","/api/platform-tokens","/api/v1/platform-tokens","/api/open-api","/api/v1/open-api","/api/sponsors","/api/v1/sponsors").any(uri::startsWith)) return store.serveManagement(session)
            if (uri.startsWith("/api/notifications") || uri.startsWith("/api/v1/notifications")) return store.serveNotifications(session)
            if (uri.startsWith("/api/tasks")) return store.serveTasks(session)
            if (uri.startsWith("/api/v1/tasks")) return store.serveTasks(session)
            if (uri.startsWith("/api/envs")) return store.serveEnvs(session)
            if (uri.startsWith("/api/v1/envs")) return store.serveEnvs(session)
            if (uri.startsWith("/api/deps")) return store.serveDependencies(session)
            if (uri.startsWith("/api/v1/deps")) return store.serveDependencies(session)
            if (uri.startsWith("/api/configs")) return store.serveConfigs(session)
            if (uri.startsWith("/api/v1/configs")) return store.serveConfigs(session)
            if (uri.startsWith("/api/scripts")) return store.serveScripts(session)
            if (uri.startsWith("/api/terminal") || uri.startsWith("/api/v1/terminal")) return store.serveTerminal(session)
            if (uri.startsWith("/api/subscriptions")) return store.serveSubscriptions(session)
            if (uri.startsWith("/api/logs")) return store.serveLogs(session)
            if (uri.startsWith("/api/v1/logs")) return store.serveLogs(session)
            if (uri.startsWith("/api/system/dashboard")) return store.serveDashboard(session)
            if (uri.startsWith("/api/system/stats")) return jsonResponse(systemStats())
            if (uri.startsWith("/api/system/panel-log")) return store.serveLogs(session)
            if (LocalPanelStore.isRecoveryRequest(session.method, uri)) return store.serveBackup(session)
            if (uri.startsWith("/api/system/panel-settings")) return store.serveConfigs(session)
            if (uri == "/api/system/config-script" || uri == "/api/v1/system/config-script") return store.serveConfigScript(session)
            if (uri.startsWith("/api/android-runtime") || uri.startsWith("/api/v1/android-runtime")) return androidRuntime(session)
            if (uri.endsWith("/system/update-status") || uri.endsWith("/system/update") || uri.endsWith("/system/restart")) return systemLifecycle(session)
            if (uri.startsWith("/api/system/machine-code")) return jsonResponse(JSONObject().put("data", JSONObject().put("machine_code", "android-local")).put("status", "ok"))
            if (uri.startsWith("/api/system/check-update")) return jsonResponse(
                JSONObject()
                    .put("data", JSONObject()
                        .put("latest", "3.0.6")
                        .put("current", "3.0.6")
                        .put("source", "linzixuanzz/daidai-panel"))
                    .put("status", "ok")
            )
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
                session.uri.startsWith("/api/events") -> jsonResponse(JSONObject().put("status", "ok").put("message", "SSE not supported"))
                session.uri.startsWith("/api/sse") -> jsonResponse(JSONObject().put("status", "ok").put("message", "SSE not supported"))

                LocalPanelStore.isRecoveryRequest(session.method, session.uri) -> store.serveBackup(session)
                else -> jsonError(Response.Status.NOT_FOUND, "本地核心接口不存在")
            }
        } catch (error: Exception) {
            jsonError(Response.Status.INTERNAL_ERROR, error.message ?: "本地核心处理失败")
        }
    }

    private fun androidRuntime(session:IHTTPSession):Response {
        val parameters = session.parameters
        if(session.uri.endsWith("/status")&&session.method==Method.GET)return jsonResponse(JSONObject().put("data",AndroidLinuxRuntime.statusJson(context)))
        if(session.uri.endsWith("/distribution")&&session.method==Method.POST){
            val body = parseBodyJson(session)
            val distribution = (body?.optString("distribution") ?: parameters["distribution"]?.firstOrNull() ?: "").trim()
            if(distribution !in AndroidLinuxRuntime.SUPPORTED_DISTRIBUTIONS)return jsonError(Response.Status.BAD_REQUEST,"Unsupported distribution: $distribution")
            AndroidLinuxRuntime.selectDistribution(context, distribution)
            return jsonResponse(JSONObject().put("status","ok").put("distribution",distribution))
        }
        if(session.uri.endsWith("/distribution")&&session.method==Method.GET)return jsonResponse(JSONObject().put("data",JSONObject().put("distribution",AndroidLinuxRuntime.selectedDistribution(context)).put("supported",JSONArray(AndroidLinuxRuntime.SUPPORTED_DISTRIBUTIONS))))
        if(session.uri.endsWith("/source")&&session.method==Method.POST){
            val distribution=(parameters["distribution"]?.firstOrNull() ?: "").trim()
            val sourceId=(parameters["source_id"]?.firstOrNull() ?: "").trim()
            if(distribution !in AndroidLinuxRuntime.SUPPORTED_DISTRIBUTIONS)return jsonError(Response.Status.BAD_REQUEST,"Unsupported distribution: $distribution")
            if(AndroidRootfsDownloader.sourcesFor(distribution).none{it.id==sourceId})return jsonError(Response.Status.BAD_REQUEST,"Unknown rootfs image source: $sourceId")
            AndroidRootfsDownloader.selectSource(context,distribution,sourceId)
            return jsonResponse(JSONObject().put("status","ok").put("distribution",distribution).put("source_id",sourceId))
        }
        if(session.uri.endsWith("/download")&&session.method==Method.POST){
            val distribution=(parameters["distribution"]?.firstOrNull() ?: "").trim()
            if(distribution !in AndroidLinuxRuntime.SUPPORTED_DISTRIBUTIONS)return jsonError(Response.Status.BAD_REQUEST,"Unsupported distribution: $distribution")
            if(AndroidRootfsDownloader.downloadRunning)return jsonError(Response.Status.CONFLICT,"A rootfs download is already in progress")
            val abi=AndroidLinuxRuntime.currentAbi()
            Thread{runCatching{AndroidRootfsDownloader.downloadRootfs(context,distribution,abi,AndroidRootfsDownloader.ProgressListener{},java.util.concurrent.atomic.AtomicBoolean(false))}}.start()
            return jsonResponse(JSONObject().put("status","accepted").put("distribution",distribution).put("source_id",AndroidRootfsDownloader.selectedSourceId(context,distribution)))
        }
        if(session.uri.endsWith("/download-status")&&session.method==Method.GET){
            val distribution=(parameters["distribution"]?.firstOrNull() ?: "").trim()
            if(distribution !in AndroidLinuxRuntime.SUPPORTED_DISTRIBUTIONS)return jsonError(Response.Status.BAD_REQUEST,"Unsupported distribution: $distribution")
            val status=JSONObject()
                .put("running",AndroidRootfsDownloader.downloadRunning)
                .put("distribution",distribution)
                .put("downloaded",AndroidRootfsDownloader.downloadedArchive(context,AndroidLinuxRuntime.currentAbi(),distribution)!=null)
                .put("error",AndroidRootfsDownloader.lastError(distribution) ?: JSONObject.NULL)
            AndroidRootfsDownloader.lastProgress(distribution)?.let{progress->
                status.put("phase",progress.phase).put("message",progress.message)
                    .put("downloaded_bytes",progress.downloadedBytes).put("total_bytes",progress.totalBytes)
            }
            return jsonResponse(status)
        }
        if((session.uri.endsWith("/install")||session.uri.endsWith("/uninstall"))&&session.method==Method.POST)return jsonError(Response.Status.CONFLICT,"Android runtime is embedded and immutable; update the APK to change it")
        return jsonError(Response.Status.NOT_FOUND,"Android runtime endpoint unavailable")
    }
    private fun systemLifecycle(session:IHTTPSession):Response = when {
        session.uri.endsWith("/update-status")&&session.method==Method.GET -> jsonResponse(JSONObject().put("status","idle").put("phase","immutable_apk").put("message","Android self-contained APK cannot self-update; use the platform package installer").put("deployment_type","android_apk").put("update_manager","platform_installer"))
        session.uri.endsWith("/update")&&session.method==Method.POST -> jsonError(Response.Status.CONFLICT,"Self-update is unavailable for an immutable Android APK; install a signed APK update")
        session.uri.endsWith("/restart")&&session.method==Method.POST -> { android.os.Handler(context.mainLooper).postDelayed({ runCatching { LocalPanelRuntime.restart(context, localToken) } },250);NanoHTTPD.newFixedLengthResponse(Response.Status.ACCEPTED,"application/json; charset=utf-8",JSONObject().put("status","restarting").put("message","Fallback service restart scheduled").toString()) }
        else -> jsonError(Response.Status.METHOD_NOT_ALLOWED,"Unsupported lifecycle operation")
    }

    private fun capabilities(): JSONObject = JSONObject()
        .put("instance_mode", "android_local")
        .put("phase", "ready")
        .put("platform", "android")
        .put("architecture", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
        .put("core_version", "kotlin-local-fallback")
        .put("schema_version", LocalPanelStore.SCHEMA_VERSION)
        .put("backend_parity", "control-plane")
        .put("native_runtime_mode", if (AndroidLinuxRuntime.isRootfsReady(context)) "full-local-runtime" else "runtime-unavailable")
        .put("recommended_execution_mode", if (AndroidLinuxRuntime.isRootfsReady(context)) "local" else "remote-panel")
        .put(
            "capabilities",
            JSONObject()
                .put("dashboard", true)
                .put("tasks", true)
                .put("cron", true)
                .put("task_retry", true)
                .put("task_timeout", true)
                .put("task_stop_schedule", true)
                .put("task_dependencies", true)
                .put("task_hooks", true)
                .put("scripts", true)
                .put("logs", true)
                .put("log_stream", true)
                .put("envs", true)
                .put("env_groups", true)
                .put("env_import_export", true)
                .put("subscriptions", true)
                .put("single_file_subscription", true)
                .put("git_subscription", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/git"))
                .put("subscription_schedule", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/git"))
                .put("notifications", true)
                .put("open_api_management", true)
                .put("open_api_token", false)
                .put("security", true)
                .put("ip_whitelist_management", true)
                .put("two_factor_auth", false)
                .put("multi_device_sessions", false)
                .put("backup", true)
                .put("backup_schedule", true)
                .put("system_monitor", true)
                .put("dependency_install", AndroidLinuxRuntime.isRootfsReady(context))
                .put("python", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/python3"))
                .put("pip", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/pip3"))
                .put("node", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/node"))
                .put("npm", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/npm"))
                .put("typescript", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/tsc"))
                .put("shell", AndroidLinuxRuntime.hasPackagedRootfsRunner(context))
                .put("git", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/git"))
                .put("ssh", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/ssh"))
                .put("go_interpret", File(context.applicationInfo.nativeLibraryDir.orEmpty(), "libyaegi_exec.so").isFile)
                .put("go_builder", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/go"))
                .put("linux_package_manager", AndroidLinuxRuntime.hasPackagedRootfsRunner(context))
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

    private fun systemStats(): JSONObject {
        val dashData = store.serveDashboardStats()
        return JSONObject().put("data", dashData)
    }

    private fun systemHealth(): JSONObject {
        val items = JSONArray()
            .put(JSONObject().put("name", "Android local panel API").put("status", "ok"))
            .put(goCoreHealthItem())
            .put(JSONObject().put("name", "Fallback mode").put("status", "ok").put("message", "Kotlin fallback server active"))
            .put(runtimeSmokeItem("Python runtime", pythonSmokeCommand(), "PY_OK"))
            .put(runtimeSmokeItem("Node.js runtime", nodeSmokeCommand(), "NODE_OK"))
            .put(runtimeSmokeItem("TypeScript runtime", typeScriptSmokeCommand(), "TS_OK"))
            .put(pythonSeedStatusItem())
        return JSONObject()
            .put("items", items)
            .put("last_checked_at", java.time.Instant.now().toString())
    }

    private fun goCoreHealthItem(): JSONObject = JSONObject()
        .put("name", "Local panel core")
        .put("status", "ok")
        .put("message", "Kotlin fallback active")

    private fun hasNativeRuntimeEntries(): Boolean = listOf(
        "libpython_exec.so",
        "libnode_exec.so",
        "libshell_exec.so",
    ).any { java.io.File(context.applicationInfo.nativeLibraryDir.orEmpty(), it).isFile }

    private fun pythonSmokeCommand(): List<String>? =
        AndroidLinuxRuntime.guestCommand(context, context.filesDir, listOf("/usr/bin/python3", "-c", "import ssl,sqlite3,venv;print('PY_OK')"))

    private fun pythonSeedStatusItem(): JSONObject {
        val status = "ok"
        return if (status == "ok") {
            JSONObject().put("name", "Python seed wheelhouse").put("status", "ok").put("message", "seed dependencies installed")
        } else {
            JSONObject().put("name", "Python seed wheelhouse").put("status", "warning").put("message", status)
        }
    }

    private fun nodeSmokeCommand(): List<String>? =
        AndroidLinuxRuntime.guestCommand(context, context.filesDir, listOf("/usr/bin/node", "-e", "console.log('NODE_OK')"))

    private fun typeScriptSmokeCommand(): List<String>? =
        AndroidLinuxRuntime.guestCommand(context, context.filesDir, listOf("/usr/bin/env", "NODE_PATH=/usr/lib/node_modules", "/usr/bin/node", "-e", "const ts=require('typescript');console.log('TS_OK')"))

    private fun runtimeSmokeItem(name: String, command: List<String>?, expected: String): JSONObject {
        if (command == null) return JSONObject().put("name", name).put("status", "warning").put("message", "Runtime is not packaged or not executable")
        return try {
            val process = ProcessBuilder(command)
                .redirectErrorStream(true)
                .apply {
                    environment().putAll(AndroidLinuxRuntime.baseEnvironment(context, context.filesDir))
                    AndroidLinuxRuntime.applyGuestEnvironment(command, environment())
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

    private fun parseBodyJson(session: IHTTPSession): JSONObject? {
        return try {
            val body = LocalPanelStore.readUtf8JsonBody(session) ?: run {
                val files = HashMap<String, String>()
                session.parseBody(files)
                files["postData"] ?: return null
            }
            JSONObject(body)
        } catch (_: Exception) {
            null
        }
    }
}
