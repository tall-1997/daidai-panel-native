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
            Method.GET to "/api/health",
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
            Method.GET to "/api/system/panel-settings" -> true
            else -> false
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
