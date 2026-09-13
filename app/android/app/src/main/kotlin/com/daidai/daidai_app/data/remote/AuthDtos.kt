package com.daidai.daidai_app.data.remote

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
) : RuntimeException("Panel request failed with HTTP $statusCode")
