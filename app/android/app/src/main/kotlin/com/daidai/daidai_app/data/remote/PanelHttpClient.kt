package com.daidai.daidai_app.data.remote

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/** Small OkHttp adapter kept independent from the Compose screens. */
class PanelHttpClient(
    baseUrl: String,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = OkHttpClient(),
) : PanelApi {
    var baseUrl: String = normalizeBaseUrl(baseUrl)
        set(value) { field = normalizeBaseUrl(value) }

    override suspend fun health(): HealthResponse = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/health")
        HealthResponse(body)
    }

    override suspend fun checkInit(): CheckInitResponse = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/auth/check-init")
        CheckInitResponse(JSONObject(body).optBoolean("need_init"), body)
    }

    override suspend fun init(request: AuthInitRequest): AuthResponse = withContext(Dispatchers.IO) {
        parseAuth(execute("POST", "/api/auth/init", JSONObject().apply {
            put("username", request.username)
            put("password", request.password)
        }.toString()))
    }

    override suspend fun login(request: AuthLoginRequest): AuthResponse = withContext(Dispatchers.IO) {
        parseAuth(execute("POST", "/api/auth/login", JSONObject().apply {
            put("username", request.username)
            put("password", request.password)
            request.totpCode?.let { put("totp_code", it) }
        }.toString()))
    }

    private fun execute(method: String, path: String, json: String? = null): String {
        val builder = Request.Builder().url(baseUrl + path)
        localToken?.takeIf { it.isNotBlank() }?.let { builder.header("x-daidai-local-token", it) }
        if (json != null) {
            builder.post(json.toRequestBody(JSON_MEDIA_TYPE))
        } else {
            builder.get()
        }
        httpClient.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw PanelApiException(response.code, body)
            return body
        }
    }

    private fun parseAuth(body: String): AuthResponse {
        val json = JSONObject(body)
        val userJson = json.optJSONObject("user")
        return AuthResponse(
            message = json.optString("message").takeIf { it.isNotEmpty() },
            accessToken = json.optString("access_token").takeIf { it.isNotEmpty() },
            refreshToken = json.optString("refresh_token").takeIf { it.isNotEmpty() },
            user = userJson?.let { AuthUser(it.optLong("id").takeIf { id -> id != 0L }, it.optString("username").takeIf { name -> name.isNotEmpty() }, it.toString()) },
            rawBody = body,
        )
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun normalizeBaseUrl(value: String): String = value.trim().trimEnd('/')
    }
}
