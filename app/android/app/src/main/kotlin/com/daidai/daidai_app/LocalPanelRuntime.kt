package com.daidai.daidai_app

import android.content.Context
import fi.iki.elonen.NanoHTTPD

object LocalPanelRuntime {
    private var fallbackServer: LocalPanelHttpServer? = null
    private val kotlinFallbackReason = "kotlin_local_fallback"

    @Volatile
    private var cachedResult: Map<String, Any>? = null
    @Volatile
    private var initializing = false

    fun tryEnsureStarted(context: Context, localToken: String): Map<String, Any> {
        val current = status(localToken)
        if (fallbackServer != null) return current
        return ensureStarted(context, localToken)
    }

    @Synchronized
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> {
        initializing = true
        try {
            val result = ensureFallbackStarted(context.applicationContext, localToken)
            cachedResult = result
            return result
        } finally {
            initializing = false
        }
    }

    @Synchronized
    fun stop(localToken: String): Map<String, Any> {
        cachedResult = null
        stopFallback()
        return mapOf(
            "phase" to "stopped",
            "base_url" to "",
            "instance_id" to "kotlin-local-fallback",
            "core_version" to "kotlin-local-fallback",
            "schema_version" to LocalPanelStore.SCHEMA_VERSION,
            "message" to "Kotlin fallback stopped",
            "local_token" to localToken,
        ).also { cachedResult = null }
    }

    @Synchronized
    fun restart(context: Context, localToken: String): Map<String, Any> =
        stop(localToken).let { ensureStarted(context.applicationContext, localToken) }

    @Synchronized
    fun status(localToken: String): Map<String, Any> {
        return fallbackServer?.let { server ->
            server.updateBoundary(kotlinFallbackReason, localToken)
            fallbackStatus(server, localToken, kotlinFallbackReason)
        } ?: (cachedResult ?: fallbackStatus("", localToken, "not_started"))
    }

    fun createBrowserUrl(context: Context, localToken: String): String {
        val fallback = fallbackServer
        return if (fallback != null) fallback.createBrowserUrl() else ""
    }

    private fun stopFallback() {
        fallbackServer?.shutdown()
        fallbackServer = null
    }

    private fun ensureFallbackStarted(context: Context, localToken: String): Map<String, Any> {
        val existing = fallbackServer
        if (existing != null) {
            existing.updateBoundary(kotlinFallbackReason, localToken)
            return fallbackStatus(existing, localToken, kotlinFallbackReason)
        }
        val server = LocalPanelHttpServer(context, kotlinFallbackReason, localToken)
        try {
            server.start(NanoHTTPD.SOCKET_READ_TIMEOUT, false)
            android.util.Log.i("daidai-panel", "local server started on 0.0.0.0:${server.listeningPort}")
            server.startScheduler()
        } catch (error: Exception) {
            android.util.Log.e("daidai-panel", "local server start FAILED: ${error.message}", error)
            server.shutdown()
            throw error
        }
        fallbackServer = server
        return fallbackStatus(server, localToken, kotlinFallbackReason)
    }

    private fun fallbackStatus(server: LocalPanelHttpServer, localToken: String, reason: String): Map<String, Any> =
        fallbackStatus(server.endpoint, localToken, reason)

    internal fun fallbackStatus(endpoint: String, localToken: String, reason: String): Map<String, Any> = mapOf(
        "phase" to "ready",
        "base_url" to endpoint,
        "instance_id" to "kotlin-local-fallback",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to LocalPanelStore.SCHEMA_VERSION,
        "failure_stage" to reason,
        "fallback_stage" to reason,
        "fallback_mode" to "full",
        "message" to "Kotlin fallback server ready",
        "foreground_service_enabled" to true,
        "scheduler_host_state" to "active",
        "scheduler_guarantee_state" to "active",
        "scheduler_guarantee_reason" to "kotlin_fallback",
        "scheduler_intervention" to "none",
        "local_token" to localToken,
    )
}
