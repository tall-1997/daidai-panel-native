package com.daidai.daidai_app

import android.content.Context
import fi.iki.elonen.NanoHTTPD

object LocalPanelRuntime {
    private var fallbackServer: LocalPanelHttpServer? = null
    private var lastFallbackReason: String = ""

    @Volatile
    private var cachedResult: Map<String, Any>? = null
    @Volatile
    private var initializing = false

    fun tryEnsureStarted(context: Context, localToken: String): Map<String, Any> {
        cachedResult?.let { return it }
        return ensureStarted(context, localToken)
    }

    @Synchronized
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> {
        cachedResult?.let { return it }
        initializing = true
        try {
            val coreStatus = GoCoreBridge.ensureStarted(context.applicationContext, localToken)
            val result = if (!requiresFallback(coreStatus)) {
                stopFallback()
                coreStatus
            } else {
                ensureFallbackStarted(context.applicationContext, localToken, fallbackReason(coreStatus))
            }
            cachedResult = result
            return result
        } finally {
            initializing = false
        }
    }

    @Synchronized
    fun stop(localToken: String): Map<String, Any> {
        stopFallback()
        return GoCoreBridge.stop(localToken)
    }

    @Synchronized
    fun restart(context: Context, localToken: String): Map<String, Any> =
        stop(localToken).let { ensureStarted(context.applicationContext, localToken) }

    @Synchronized
    fun status(localToken: String): Map<String, Any> {
        cachedResult?.let { return it }
        val coreStatus = GoCoreBridge.status(localToken)
        if (!requiresFallback(coreStatus)) {
            stopFallback()
            return coreStatus
        }
        return fallbackServer?.let { server ->
            val reason = lastFallbackReason.ifBlank { fallbackReason(coreStatus) }
            server.updateBoundary(reason, localToken)
            fallbackStatus(server, localToken, reason)
        } ?: coreStatus
    }

    private fun stopFallback() {
        fallbackServer?.shutdown()
        fallbackServer = null
        lastFallbackReason = ""
    }

    private fun ensureFallbackStarted(context: Context, localToken: String, reason: String): Map<String, Any> {
        val normalizedReason = reason.ifBlank { "go_core_unavailable" }
        lastFallbackReason = normalizedReason
        val existing = fallbackServer
        if (existing != null) {
            existing.updateBoundary(normalizedReason, localToken)
            return fallbackStatus(existing, localToken, normalizedReason)
        }
        val server = LocalPanelHttpServer(context, normalizedReason, localToken)
        try {
            server.start(NanoHTTPD.SOCKET_READ_TIMEOUT, false)
            server.startScheduler()
        } catch (error: Exception) {
            server.shutdown()
            throw error
        }
        fallbackServer = server
        return fallbackStatus(server, localToken, normalizedReason)
    }

    private fun fallbackReason(status: Map<String, Any>): String {
        val stage = status["failure_stage"]?.toString().orEmpty().ifBlank { "go_core_unavailable" }
        val errorType = status["go_core_error_type"]?.toString().orEmpty()
        val rootType = status["go_core_root_error_type"]?.toString().orEmpty()
        return listOf(stage, errorType, rootType).filter(String::isNotBlank).joinToString(":")
    }

    internal fun requiresFallback(status: Map<String, Any>): Boolean = status["phase"] != "ready"

    private fun fallbackStatus(server: LocalPanelHttpServer, localToken: String, reason: String): Map<String, Any> =
        fallbackStatus(server.endpoint, localToken, reason)

    internal fun fallbackStatus(endpoint: String, localToken: String, reason: String): Map<String, Any> = mapOf(
        "phase" to "degraded",
        "base_url" to endpoint,
        "instance_id" to "kotlin-local-fallback",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to LocalPanelStore.SCHEMA_VERSION,
        "failure_stage" to reason,
        "fallback_stage" to reason,
        "fallback_mode" to "diagnostic",
        "message" to "Kotlin fallback server ready",
        "foreground_service_enabled" to true,
        "scheduler_host_state" to "active",
        "scheduler_guarantee_state" to "active",
        "scheduler_guarantee_reason" to "kotlin_fallback",
        "scheduler_intervention" to "none",
        "local_token" to localToken,
    )
}
