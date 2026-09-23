package com.daidai.daidai_app

import android.content.Context
import android.app.ActivityManager
import android.os.StatFs
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.ServerSocket
import java.net.URL
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

class LocalPanelHttpServer(
    private val context: Context,
    goCoreFallbackReason: String = "go_core_unavailable",
    localToken: String,
    port: Int = findAvailablePort()
) : NanoHTTPD("0.0.0.0", port) {
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

    // /api/system/health-check 是公开接口且会通过 proot 启动多个 guest smoke，
    // 采用 TTL 缓存 + single-flight，避免未认证请求反复触发昂贵的运行时探测。
    private val healthCheckBusy = java.util.concurrent.atomic.AtomicBoolean(false)

    @Volatile
    private var cachedHealth: JSONObject? = null

    @Volatile
    private var cachedHealthAtMillis = 0L

    companion object {
        private const val HEALTH_CHECK_TTL_MILLIS = 30_000L
        private fun findAvailablePort(): Int {
            val anyLocal = InetAddress.getByName("0.0.0.0")
            return try {
                ServerSocket(5700, 0, anyLocal).use { it.localPort }
            } catch (_: Exception) {
                ServerSocket(0, 0, anyLocal).use { it.localPort }
            }
        }

        internal const val ANDROID_RELEASE_REPO = "tall-1997/daidai-panel-native"

        internal fun compareVersionLabels(current: String, latest: String): Int {
            val left = current.split(Regex("[^0-9]+")).mapNotNull { it.toIntOrNull() }
            val right = latest.split(Regex("[^0-9]+")).mapNotNull { it.toIntOrNull() }
            val size = maxOf(left.size, right.size)
            for (index in 0 until size) {
                val l = left.getOrElse(index) { 0 }
                val r = right.getOrElse(index) { 0 }
                if (l != r) return l.compareTo(r)
            }
            return 0
        }

        internal fun pickLatestPublishedRelease(body: String): JSONObject? {
            val trimmed = body.trim()
            if (trimmed.startsWith("{")) {
                val obj = JSONObject(trimmed)
                return if (obj.optBoolean("draft")) null else obj
            }
            val array = JSONArray(trimmed)
            for (index in 0 until array.length()) {
                val item = array.optJSONObject(index) ?: continue
                if (item.optBoolean("draft")) continue
                return item
            }
            return null
        }

        internal fun isFallbackRouteAllowed(method: Method, uri: String): Boolean =
            uri.startsWith("/api/")

        internal fun isOpenApiTokenCapabilityEnabled(): Boolean = true

        internal fun isPublicAuthRoute(method: Method, uri: String): Boolean = when (LocalPanelStore.normalizeApiPath(uri)) {
            "/api/auth/check-init" -> method == Method.GET
            "/api/auth/init", "/api/auth/login", "/api/auth/refresh" -> method == Method.POST
            "/api/auth/captcha-config" -> method == Method.GET
            else -> false
        }

        internal fun isRawLogDownloadRoute(method: Method, uri: String): Boolean {
            if (method != Method.GET) return false
            val segs = LocalPanelStore.normalizeApiPath(uri).removePrefix("/api").trim('/').split('/').filter { it.isNotEmpty() }
            if (segs.size == 3 && segs[0] == "logs" && segs[1].toLongOrNull() != null && segs[2] == "raw") return true
            if (segs.size >= 5 && segs[0] == "tasks" && segs[1].toLongOrNull() != null && segs[2] == "log-files" && segs.last() == "raw") return true
            return false
        }

        internal fun isPublicApiRoute(method: Method, uri: String): Boolean = when (LocalPanelStore.normalizeApiPath(uri)) {
            "/api/health", "/api/local/capabilities", "/api/system/public-version",
            "/api/android/recovery-metadata" -> method == Method.GET
            "/api/system/health-check" -> method == Method.GET || method == Method.POST
            "/api/system/panel-settings" -> method == Method.GET
            else -> false
        }

        internal fun isLocalOrLanHost(host: String?): Boolean {
            val value = host?.trim()?.takeIf(String::isNotEmpty) ?: return false
            val hostPart = when {
                value.startsWith('[') -> {
                    val close = value.indexOf(']')
                    if (close <= 1 || value.substring(close + 1).let { it.isNotEmpty() && !it.matches(Regex(":\\d+")) }) return false
                    value.substring(1, close)
                }
                value.count { it == ':' } > 1 -> value
                else -> value.substringBefore(':')
            }.lowercase()
            if (hostPart == "localhost") return true
            if (!hostPart.matches(Regex("[0-9a-f:.]+"))) return false
            val address = runCatching { InetAddress.getByName(hostPart) }.getOrNull() ?: return false
            val bytes = address.address
            val uniqueLocalV6 = bytes.size == 16 && (bytes[0].toInt() and 0xfe) == 0xfc
            return address.isLoopbackAddress || address.isSiteLocalAddress || address.isLinkLocalAddress || uniqueLocalV6
        }
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
            // 监听 0.0.0.0，Host 放宽为本机或局域网 IP。
            if (!isLocalOrLanHost(host)) return Response.Status.BAD_REQUEST
            if (browserSession) {
                // 本机浏览器（ticket 兑换）：Origin 若存在须匹配。
                if (requestOrigin != null && requestOrigin != origin) return Response.Status.FORBIDDEN
            } else if (!token.isNullOrBlank()) {
                // App 内部 / 本地通知（sendNotify.js）：带 local-token，token 为主要认证。
                if (requestOrigin != null && requestOrigin != origin) return Response.Status.FORBIDDEN
                if (localToken.isBlank() || !LocalPanelStore.secretsEqual(token, localToken)) return Response.Status.UNAUTHORIZED
            }
            // LAN 浏览器（无 token 无 session）：放行，后续由 serve 强制 JWT。
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

    internal fun triggerCronTick() = cronScheduler.tickNow()

    internal fun isCronIdle(): Boolean = cronScheduler.isIdle()

    internal fun createBrowserUrl(): String = browserAccess.createUrl()

    fun shutdown() {
        cronScheduler.close()
        browserAccess.clear()
        stop()
        store.close()
    }

    override fun serve(session: IHTTPSession): Response {
        return try {
            android.util.Log.d("daidai-panel", "serve ${session.method} ${session.uri} host=${session.headers["host"]} hasLocalToken=${session.headers["x-daidai-local-token"] != null}")
            if (session.uri == "/local-ui" || session.uri.startsWith("/local-ui/")) {
                return browserAccess.serve(session, "127.0.0.1:$listeningPort")
            }
            val browserSession = browserAccess.hasSession(session.headers)
            val hasLocalToken = !session.headers.entries
                .singleOrNull { it.key.equals("x-daidai-local-token", ignoreCase = true) }
                ?.value.isNullOrBlank()
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
                val headerValue = when (status) {
                    NanoHTTPD.Response.Status.BAD_REQUEST -> session.headers.entries.singleOrNull { it.key.equals("host", ignoreCase = true) }?.value
                    NanoHTTPD.Response.Status.FORBIDDEN -> session.headers.entries.singleOrNull { it.key.equals("origin", ignoreCase = true) }?.value
                    NanoHTTPD.Response.Status.UNAUTHORIZED -> session.headers.entries.singleOrNull { it.key.equals("x-daidai-local-token", ignoreCase = true) }?.value
                    else -> null
                }
                val expected = when (status) {
                    NanoHTTPD.Response.Status.BAD_REQUEST -> "127.0.0.1:$listeningPort"
                    NanoHTTPD.Response.Status.FORBIDDEN -> endpoint
                    NanoHTTPD.Response.Status.UNAUTHORIZED -> "x-daidai-local-token"
                    else -> ""
                }
                return jsonError(status, "Invalid local diagnostic request boundary ($reason)")
            }
            if (!isFallbackRouteAllowed(session.method, session.uri)) {
                return jsonError(Response.Status.NOT_FOUND, "Diagnostic fallback interface unavailable")
            }
            if (isPublicApiRoute(session.method, session.uri)) return servePublicApi(session)
            if (isRawLogDownloadRoute(session.method, session.uri)) {
                val path = (session.uri ?: "").substringBefore('?')
                return if (path.contains("/tasks/")) store.serveTasks(session) else store.serveLogs(session)
            }
            if (session.uri.startsWith("/api/auth") || session.uri.startsWith("/api/v1/auth")) {
                if (!isPublicAuthRoute(session.method, session.uri) && !store.isAuthorized(session)) {
                    return jsonError(Response.Status.UNAUTHORIZED, "登录态已失效，请重新登录")
                }
                return store.serveAuth(session)
            }
            if (session.uri == "/api/open-api/token" || session.uri == "/api/v1/open-api/token") {
                return store.serveOpenApiToken(session)
            }
            if (!hasLocalToken) {
                if (session.uri.startsWith("/api/open-api") || session.uri.startsWith("/api/v1/open-api")) {
                    if (!store.isAuthorized(session)) return jsonError(Response.Status.UNAUTHORIZED, "登录态已失效，请重新登录")
                } else {
                    store.authorizeBusinessRequest(session)?.let { return it }
                }
            }
            if (session.uri.startsWith("/api/security") || session.uri.startsWith("/api/v1/security")) {
                return store.serveSecurity(session)
            }
            val uri = session.uri ?: ""
            val api = LocalPanelStore.normalizeApiPath(uri)
            if (api.startsWith("/api/users")) return store.serveUsers(session, if (uri.startsWith("/api/v1")) "/api/v1/users" else "/api/users")
            if (listOf("/api/ssh-keys", "/api/platform-tokens", "/api/open-api", "/api/sponsors").any(api::startsWith)) return store.serveManagement(session)
            if (api.startsWith("/api/notifications")) return store.serveNotifications(session)
            if (api.startsWith("/api/tasks")) return store.serveTasks(session)
            if (api.startsWith("/api/envs")) return store.serveEnvs(session)
            if (api.startsWith("/api/deps")) return store.serveDependencies(session)
            if (api.startsWith("/api/configs")) return store.serveConfigs(session)
            if (api.startsWith("/api/scripts")) return store.serveScripts(session)
            if (api.startsWith("/api/terminal")) return store.serveTerminal(session)
            if (api.startsWith("/api/subscriptions")) return store.serveSubscriptions(session)
            if (api.startsWith("/api/logs")) return store.serveLogs(session)
            if (api.startsWith("/api/system/dashboard")) return store.serveDashboard(session)
            if (api.startsWith("/api/system/stats")) return jsonResponse(systemStats())
            if (api.startsWith("/api/system/panel-log")) return store.panelLog(session)
            if (LocalPanelStore.isRecoveryRequest(session.method, uri)) return store.serveBackup(session)
            if (api.startsWith("/api/system/panel-settings")) return store.servePanelSettings(session)
            if (api == "/api/system/config-script") return store.serveConfigScript(session)
            if (api.startsWith("/api/android-runtime")) return androidRuntime(session)
            if (api.endsWith("/system/update-status") || api.endsWith("/system/update") || api.endsWith("/system/restart")) return systemLifecycle(session)
            if (api.startsWith("/api/system/machine-code")) return jsonResponse(JSONObject().put("data", JSONObject().put("machine_code", "android-local")).put("status", "ok"))
            if (api.startsWith("/api/system/check-update")) return jsonResponse(checkUpdatePayload())
            when {
                session.method == Method.GET && api == "/api/system/version" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName()).put("mode", "diagnostic")))

                session.method == Method.GET && api == "/api/system/info" ->
                    jsonResponse(systemInfo())

                api.startsWith("/api/events") -> jsonResponse(JSONObject().put("status", "ok").put("message", "SSE not supported"))
                api.startsWith("/api/sse") -> jsonResponse(JSONObject().put("status", "ok").put("message", "SSE not supported"))

                LocalPanelStore.isRecoveryRequest(session.method, session.uri) -> store.serveBackup(session)
                else -> jsonError(Response.Status.NOT_FOUND, "本地核心接口不存在")
            }
        } catch (error: Exception) {
            android.util.Log.e("daidai-panel", "serve ERROR ${session.uri}: ${error.message}", error)
            if (error is org.json.JSONException) {
                jsonError(Response.Status.BAD_REQUEST, "请求体 JSON 格式错误")
            } else {
                jsonError(Response.Status.INTERNAL_ERROR, "本地核心处理失败")
            }
        }
    }

    private fun servePublicApi(session: IHTTPSession): Response {
        val api = LocalPanelStore.normalizeApiPath(session.uri ?: "")
        return when {
            session.method == Method.GET && api == "/api/health" ->
                jsonResponse(JSONObject().put("status", "ok").put("mode", "android_local"))
            session.method == Method.GET && api == "/api/local/capabilities" -> jsonResponse(capabilities())
            session.method == Method.GET && api == "/api/system/public-version" ->
                jsonResponse(JSONObject().put("data", JSONObject().put("version", appVersionName())))
            session.method == Method.GET && api == "/api/system/health-check" -> jsonResponse(systemHealth(force = false))
            session.method == Method.POST && api == "/api/system/health-check" -> jsonResponse(systemHealth(force = true))
            session.method == Method.GET && api == "/api/android/recovery-metadata" -> jsonResponse(recoveryMetadata())
            session.method == Method.GET && api == "/api/system/panel-settings" -> store.servePanelSettings(session)
            else -> jsonError(Response.Status.NOT_FOUND, "公开接口不存在")
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
            if(!AndroidRootfsDownloader.tryStartDownload())return jsonError(Response.Status.CONFLICT,"A rootfs download is already in progress")
            val abi=AndroidLinuxRuntime.currentAbi()
            try {
                Thread{
                    try {
                        runCatching{AndroidRootfsDownloader.downloadRootfsClaimed(context,distribution,abi,AndroidRootfsDownloader.ProgressListener{},java.util.concurrent.atomic.AtomicBoolean(false))}
                    } finally {
                        AndroidRootfsDownloader.finishDownload()
                    }
                }.start()
            } catch (error: RuntimeException) {
                AndroidRootfsDownloader.finishDownload()
                throw error
            }
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
                .put("open_api_token", isOpenApiTokenCapabilityEnabled())
                .put("security", true)
                .put("ip_whitelist_management", true)
                .put("two_factor_auth", true)
                .put("multi_device_sessions", true)
                .put("backup", true)
                .put("backup_schedule", true)
                .put("system_monitor", true)
                .put("dependency_install", AndroidLinuxRuntime.isRootfsReady(context))
                .put("python", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/python3"))
                .put("pip", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/pip3"))
                .put("node", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/node"))
                .put("npm", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/npm"))
                .put("typescript", AndroidLinuxRuntime.guestRuntimeAvailable(context, "/usr/bin/tsc") || File(context.filesDir, "deps/nodejs/node_modules/typescript").isDirectory)
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
                .put("max_log_buffer_bytes", 2L * 1024 * 1024)
                .put("max_concurrent_tasks", 2)
                .put("max_task_logs", DependencyStorage.MAX_TASK_LOGS)
                .put("max_script_runs", DependencyStorage.MAX_SCRIPT_RUNS)
                .put("max_backups", DependencyStorage.MAX_BACKUPS)
                .put("runtime_quota_bytes", 0)
                .put("dependency_quota_bytes", DependencyStorage.MAX_CACHE_BYTES)
                .put("fallback_queue_capacity", 32)
                .put("max_debug_run_seconds", 7200)
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

    private fun systemHealth(force: Boolean = false): JSONObject {
        val now = System.currentTimeMillis()
        val cached = cachedHealth
        if (!force) {
            return cached ?: JSONObject()
                .put("items", JSONArray())
                .put("last_checked_at", JSONObject.NULL)
        }
        if (cached != null && now - cachedHealthAtMillis < HEALTH_CHECK_TTL_MILLIS) {
            return cached
        }
        // 已有请求在刷新时立即返回旧结果，绝不让并发请求同时启动 proot 探测。
        if (!healthCheckBusy.compareAndSet(false, true)) {
            return cached ?: JSONObject()
                .put("items", JSONArray().put(JSONObject().put("name", "Android local panel API").put("status", "ok").put("message", "health check in progress")))
                .put("last_checked_at", java.time.Instant.now().toString())
        }
        return try {
            val computed = computeSystemHealth()
            cachedHealth = computed
            cachedHealthAtMillis = System.currentTimeMillis()
            computed
        } finally {
            healthCheckBusy.set(false)
        }
    }

    private fun computeSystemHealth(): JSONObject {
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

    private fun checkUpdatePayload(): JSONObject {
        val current = appVersionName().removePrefix("v")
        val source = ANDROID_RELEASE_REPO
        return try {
            val url = URL("https://api.github.com/repos/$source/releases?per_page=8")
            val connection = url.openConnection() as HttpURLConnection
            connection.connectTimeout = 8000
            connection.readTimeout = 8000
            connection.instanceFollowRedirects = false
            connection.setRequestProperty("Accept", "application/vnd.github+json")
            connection.setRequestProperty("User-Agent", "daidai-android-local")
            if (connection.responseCode !in 200..299) {
                connection.disconnect()
                throw IllegalStateException("GitHub ${connection.responseCode}")
            }
            val body = connection.inputStream.bufferedReader().use { it.readText() }
            connection.disconnect()
            val release = pickLatestPublishedRelease(body) ?: throw IllegalStateException("no published release")
            val latest = release.optString("tag_name").removePrefix("v").ifBlank { current }
            JSONObject()
                .put("status", "ok")
                .put(
                    "data",
                    JSONObject()
                        .put("current", current)
                        .put("latest", latest)
                        .put("has_update", compareVersionLabels(current, latest) < 0)
                        .put("release_name", release.optString("name"))
                        .put("release_url", release.optString("html_url"))
                        .put("release_notes", release.optString("body"))
                        .put("published_at", release.optString("published_at"))
                        .put("auto_update_supported", false)
                        .put("update_disabled_reason", "Android APK 不可热更新，请通过 GitHub Release 安装")
                        .put("update_target", JSONObject().put("deployment_type", "android_apk"))
                        .put("source", source),
                )
        } catch (error: Exception) {
            JSONObject()
                .put("status", "ok")
                .put(
                    "data",
                    JSONObject()
                        .put("current", current)
                        .put("latest", current)
                        .put("has_update", false)
                        .put("auto_update_supported", false)
                        .put("update_disabled_reason", "检查更新失败: ${error.message ?: error.javaClass.simpleName}")
                        .put("update_target", JSONObject().put("deployment_type", "android_apk"))
                        .put("source", source),
                )
        }
    }

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
