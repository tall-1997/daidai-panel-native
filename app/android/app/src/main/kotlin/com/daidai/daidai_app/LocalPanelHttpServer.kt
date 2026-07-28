package com.daidai.daidai_app

import android.content.Context
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.net.ServerSocket

class LocalPanelHttpServer(
    private val context: Context,
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
                    jsonResponse(JSONObject().put("data", JSONObject().put("version", "android-local-mvp")))

                session.method == Method.GET && session.uri == "/api/system/info" ->
                    jsonResponse(systemInfo())

                session.method == Method.GET && session.uri == "/api/system/dashboard" ->
                    jsonResponse(store.dashboard())

                session.method == Method.GET && session.uri == "/api/system/panel-settings" ->
                    jsonResponse(JSONObject().put("data", JSONObject().put("panel_title", "呆呆本地面板")))

                session.uri.startsWith("/api/tasks") -> store.serveTasks(session)
                session.uri.startsWith("/api/envs") -> store.serveEnvs(session)
                session.uri.startsWith("/api/deps") || session.uri.startsWith("/api/v1/deps") ->
                    store.serveDependencies(session)
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
                .put("node", true)
                .put("npm", true)
                .put("linux_package_manager", true)
                .put("foreground_scheduler", true)
                .put("exact_cron", false)
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
        val runtime = Runtime.getRuntime()
        val dataDir = context.filesDir
        val totalMemory = runtime.totalMemory()
        val usedMemory = totalMemory - runtime.freeMemory()
        val totalDisk = dataDir.totalSpace
        val usedDisk = totalDisk - dataDir.usableSpace
        return JSONObject().put(
            "data",
            JSONObject()
                .put("hostname", android.os.Build.MODEL)
                .put("os", "Android ${android.os.Build.VERSION.RELEASE}")
                .put("arch", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
                .put("cpu_usage", 0)
                .put("memory_total", totalMemory)
                .put("memory_used", usedMemory)
                .put("memory_usage", percentage(usedMemory, totalMemory))
                .put("disk_total", totalDisk)
                .put("disk_used", usedDisk)
                .put("disk_usage", percentage(usedDisk, totalDisk))
                .put("uptime", "App 本地实例")
                .put("resource_scope", "android_app_sandbox")
        )
    }

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
