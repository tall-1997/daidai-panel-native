package com.daidai.daidai_app

import android.content.Context
import fi.iki.elonen.NanoHTTPD

object LocalPanelRuntime {
    private var fallbackServer: LocalPanelHttpServer? = null

    @Synchronized
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> {
        val coreStatus = GoCoreBridge.ensureStarted(context.applicationContext, localToken)
        if (coreStatus["phase"] == "ready") return coreStatus
        return ensureFallbackStarted(context.applicationContext, localToken, coreStatus["failure_stage"]?.toString().orEmpty())
    }

    @Synchronized
    fun stop(localToken: String): Map<String, Any> {
        fallbackServer?.stop()
        fallbackServer = null
        return GoCoreBridge.stop(localToken)
    }

    @Synchronized
    fun restart(context: Context, localToken: String): Map<String, Any> =
        stop(localToken).let { ensureStarted(context.applicationContext, localToken) }

    @Synchronized
    fun status(localToken: String): Map<String, Any> {
        val coreStatus = GoCoreBridge.status(localToken)
        if (coreStatus["phase"] == "ready") return coreStatus
        return fallbackServer?.let { fallbackStatus(it, localToken, "go_core_unavailable") } ?: coreStatus
    }

    private fun ensureFallbackStarted(context: Context, localToken: String, reason: String): Map<String, Any> {
        val existing = fallbackServer
        if (existing != null) return fallbackStatus(existing, localToken, reason.ifBlank { "go_core_unavailable" })
        val server = LocalPanelHttpServer(context)
        server.start(NanoHTTPD.SOCKET_READ_TIMEOUT, false)
        fallbackServer = server
        return fallbackStatus(server, localToken, reason.ifBlank { "go_core_unavailable" })
    }

    private fun fallbackStatus(server: LocalPanelHttpServer, localToken: String, reason: String): Map<String, Any> = mapOf(
        "phase" to "ready",
        "base_url" to server.endpoint,
        "instance_id" to "kotlin-local-fallback",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to LocalPanelStore.SCHEMA_VERSION,
        "failure_stage" to "",
        "fallback_stage" to reason,
        "message" to "",
            "foreground_service_enabled" to false,
            "scheduler_host_state" to "system_compensation",
            "scheduler_guarantee_state" to "system_compensation",
            "scheduler_guarantee_reason" to "kotlin_fallback",
            "scheduler_intervention" to "",
            "local_token" to localToken,
        )
}
