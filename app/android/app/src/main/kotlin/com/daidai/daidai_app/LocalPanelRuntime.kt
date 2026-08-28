package com.daidai.daidai_app

import android.content.Context
import java.net.HttpURLConnection
import java.net.URL

object LocalPanelRuntime {
    private var fallbackServer: LocalPanelHttpServer? = null
    private val kotlinFallbackReason = "kotlin_local_fallback"

    @Volatile
    private var cachedResult: Map<String, Any>? = null
    @Volatile
    private var initializing = false

    fun tryEnsureStarted(context: Context, localToken: String): Map<String, Any> {
        val current = status(localToken)
        if (current["phase"] == "ready") return current
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
            if (!isHealthy(server)) {
                stopFallback()
                cachedResult = null
                return failedStatus(localToken, "health_check", "Kotlin fallback listener is unavailable")
            }
            server.updateBoundary(kotlinFallbackReason, localToken)
            fallbackStatus(server, localToken, kotlinFallbackReason)
        } ?: (cachedResult ?: stoppedStatus(localToken))
    }

    fun createBrowserUrl(context: Context, localToken: String): String {
        val fallback = fallbackServer
        return if (fallback != null) fallback.createBrowserUrl() else ""
    }

    @Synchronized
    fun triggerCronTick(context: Context, localToken: String) {
        tryEnsureStarted(context, localToken)
        fallbackServer?.triggerCronTick()
    }

    fun isCronIdle(): Boolean = cronCanStop(initializing, fallbackServer?.isCronIdle())

    private fun stopFallback() {
        fallbackServer?.shutdown()
        fallbackServer = null
    }

    private fun ensureFallbackStarted(context: Context, localToken: String): Map<String, Any> {
        val existing = fallbackServer
        if (existing != null) {
            if (listenerRecoveryAction(isHealthy(existing)) == ListenerRecoveryAction.REUSE) {
                existing.updateBoundary(kotlinFallbackReason, localToken)
                return fallbackStatus(existing, localToken, kotlinFallbackReason)
            }
            stopFallback()
        }
        val server = LocalPanelHttpServer(context, kotlinFallbackReason, localToken)
        try {
            server.start(60_000, false)
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

    private fun isHealthy(server: LocalPanelHttpServer): Boolean {
        if (!server.isAlive) return false
        if (probeHealth(server)) return true
        try {
            Thread.sleep(100)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            return false
        }
        return server.isAlive && probeHealth(server)
    }

    private fun probeHealth(server: LocalPanelHttpServer): Boolean {
        val connection = runCatching {
            (URL("${server.endpoint}/api/health").openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                connectTimeout = 750
                readTimeout = 750
                useCaches = false
            }
        }.getOrNull() ?: return false
        return try {
            isListenerHealthy(server.isAlive, connection.responseCode)
        } catch (_: Exception) {
            false
        } finally {
            connection.disconnect()
        }
    }

    internal fun isListenerHealthy(nanoHttpdAlive: Boolean, healthResponseCode: Int?): Boolean =
        nanoHttpdAlive && healthResponseCode != null && healthResponseCode in 200..299

    internal fun failedStatus(localToken: String, stage: String, message: String): Map<String, Any> = mapOf(
        "phase" to "failed",
        "base_url" to "",
        "instance_id" to "kotlin-local-fallback",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to LocalPanelStore.SCHEMA_VERSION,
        "failure_stage" to stage,
        "message" to message,
        "local_token" to localToken,
    )

    private fun stoppedStatus(localToken: String): Map<String, Any> = mapOf(
        "phase" to "stopped",
        "base_url" to "",
        "instance_id" to "kotlin-local-fallback",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to LocalPanelStore.SCHEMA_VERSION,
        "message" to "Kotlin fallback is not running",
        "local_token" to localToken,
    )

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

internal fun cronCanStop(initializing: Boolean, cronIdle: Boolean?): Boolean =
    !initializing && cronIdle != false

internal enum class ListenerRecoveryAction { REUSE, REBUILD }

internal fun listenerRecoveryAction(healthy: Boolean): ListenerRecoveryAction =
    if (healthy) ListenerRecoveryAction.REUSE else ListenerRecoveryAction.REBUILD
