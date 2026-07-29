package com.daidai.daidai_app

import java.net.URI
import org.json.JSONObject

object GoCoreResultMapper {
    fun toStatus(raw: String, localToken: String): Map<String, Any> {
        val result = runCatching { JSONObject(raw) }.getOrNull()
            ?: return failed("invalid_result")
        val running = result.optBoolean("running") && result.optString("status") == "running"
        val endpoint = result.optString("endpoint")
        if (running && !isStrictLoopback(endpoint)) {
            return failed("invalid_endpoint")
        }
        if (!result.optBoolean("ok") || !running) {
            val stopped = result.optString("status") == "stopped" &&
                result.optString("errorCode") in setOf("", "NOT_RUNNING")
            return if (stopped) stopped() else failed(
                result.optString("failureStage").ifBlank { result.optString("errorCode", "core_failed") },
                result.optString("errorType"),
                result.optString("rootErrorType"),
            )
        }

        return baseStatus("ready").toMutableMap().apply {
            this["base_url"] = endpoint
            this["instance_id"] = result.optLong("id").toString()
            this["local_token"] = localToken
            result.optJSONObject("platformCapabilities")?.let {
                this["platform_capabilities"] = it.toString()
            }
            result.optJSONObject("schedulerGuarantee")?.let {
                this["scheduler_guarantee"] = it.toString()
            }
        }
    }

    fun encode(status: Map<String, Any>): String = JSONObject(status).toString()

    private fun isStrictLoopback(endpoint: String): Boolean = runCatching {
        val uri = URI(endpoint)
        uri.scheme == "http" && uri.host == "127.0.0.1" && uri.port in 1..65535 &&
            uri.userInfo == null && (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/") &&
            uri.rawQuery == null && uri.rawFragment == null
    }.getOrDefault(false)

    private fun stopped(): Map<String, Any> = baseStatus("stopped")

    private fun failed(stage: String, errorType: String = "", rootErrorType: String = ""): Map<String, Any> = baseStatus("failed").toMutableMap().apply {
        this["failure_stage"] = stage.takeIf { it.matches(Regex("[A-Za-z0-9_]{1,64}")) } ?: "core_failed"
        this["message"] = "Embedded core failed"
        if (errorType.matches(Regex("[A-Za-z0-9_.]{1,96}"))) this["go_core_error_type"] = errorType
        if (rootErrorType.matches(Regex("[A-Za-z0-9_.]{1,96}"))) this["go_core_root_error_type"] = rootErrorType
    }

    private fun baseStatus(phase: String): Map<String, Any> = mapOf(
        "phase" to phase,
        "base_url" to "",
        "instance_id" to "",
        "core_version" to "gomobile",
        "schema_version" to 0,
        "failure_stage" to "",
        "message" to "",
        "foreground_service_enabled" to false,
    )
}
