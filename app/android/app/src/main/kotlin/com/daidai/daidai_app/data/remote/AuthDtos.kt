package com.daidai.daidai_app.data.remote

import org.json.JSONObject

/** Request sent to the panel when creating the first administrator. */
data class AuthInitRequest(
    val username: String,
    val password: String,
)

/** Credentials used by the panel login endpoint. */
data class AuthLoginRequest(
    val username: String,
    val password: String,
    val totpCode: String? = null,
)

data class HealthResponse(
    val rawBody: String,
)

data class CheckInitResponse(
    val needInit: Boolean,
    val rawBody: String,
)

data class AuthUser(
    val id: Long? = null,
    val username: String? = null,
    val rawJson: String = "",
)

data class AuthResponse(
    val message: String? = null,
    val accessToken: String? = null,
    val refreshToken: String? = null,
    val user: AuthUser? = null,
    val rawBody: String,
)

/** HTTP failures deliberately keep both the status and the server response body. */
class PanelApiException(
    val statusCode: Int,
    val responseBody: String,
) : RuntimeException(formatMessage(statusCode, responseBody)) {

    /** 服务端错误体中的可读信息（message/error/msg 字段），缺失时为 null。 */
    val serverMessage: String? = extractServerMessage(responseBody)

    /** 服务端要求两步验证码（two_factor_required）。 */
    val twoFactorRequired: Boolean = runCatching {
        JSONObject(responseBody).optBoolean("two_factor_required")
    }.getOrDefault(false)

    private companion object {
        fun extractServerMessage(body: String): String? = runCatching {
            val json = JSONObject(body)
            val payload = json.optJSONObject("data") ?: json
            listOf("message", "error", "msg").firstNotNullOfOrNull { key ->
                payload.optString(key).takeIf { it.isNotEmpty() }
            }
        }.getOrNull()

        fun formatMessage(statusCode: Int, body: String): String =
            extractServerMessage(body)?.takeIf { it.isNotBlank() }
                ?: "Panel request failed with HTTP $statusCode"
    }
}
