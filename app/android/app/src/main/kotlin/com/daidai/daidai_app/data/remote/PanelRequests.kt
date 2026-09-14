package com.daidai.daidai_app.data.remote

import java.util.concurrent.TimeUnit
import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * 进程级共享的面板 HTTP 会话。
 *
 * AppServices 启动时装配 provider/coordinator；未装配时 provider 返回 null，
 * 调用方需自行显式传参（与旧行为兼容）。
 */
object PanelSession {
    @Volatile var accessTokenProvider: () -> String? = { null }
    @Volatile var localTokenProvider: () -> String? = { null }
    @Volatile var refreshTokenProvider: () -> String? = { null }

    /** 执行 POST /api/auth/refresh 并持久化新 token；成功返回 true。 */
    @Volatile var refreshCoordinator: (suspend () -> Boolean)? = null

    private val refreshLock = Any()
    @Volatile private var lastRefreshAtMs = 0L

    fun accessToken(): String? = accessTokenProvider()
    fun localToken(): String? = localTokenProvider()

    /**
     * 单飞刷新：短窗口内的并发 401 只触发一次网络刷新，其余直接复用结果重试。
     * 阻塞调用（在 IO 线程执行），内部经 [refreshCoordinator] 完成网络与持久化。
     */
    fun tryRefresh(): Boolean {
        val coordinator = refreshCoordinator ?: return false
        synchronized(refreshLock) {
            if (System.currentTimeMillis() - lastRefreshAtMs < 1_500L) return true
            val ok = runBlocking { runCatching { coordinator() }.getOrDefault(false) }
            if (ok) lastRefreshAtMs = System.currentTimeMillis()
            return ok
        }
    }
}

/**
 * 自包含仓库共享的请求执行入口：
 * - 复用单个 OkHttpClient（连接池/线程池全进程共享）。
 * - 统一携带 Origin 头（对齐 Dart managed_local_session 与 Go 安全中间件契约）。
 * - 401 时经 [PanelSession.tryRefresh] 单飞刷新后重试一次。
 */
object PanelRequests {

    val sharedClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()

    /** 从 URL 提取 Origin（scheme://host[:port]），解析失败返回 null。 */
    fun originOf(url: String): String? = url.toHttpUrlOrNull()?.let { u ->
        val defaultPort = if (u.scheme == "https") 443 else 80
        buildString {
            append(u.scheme).append("://").append(u.host)
            if (u.port > 0 && u.port != defaultPort) append(':').append(u.port)
        }
    }

    /**
     * 执行面板请求。token 默认取自 [PanelSession]（登录/刷新后自动生效），
     * 也可显式覆盖；[allowRefresh] 控制是否允许 401 触发刷新重试。
     */
    fun execute(
        method: String,
        url: String,
        json: String? = null,
        accessToken: String? = PanelSession.accessToken(),
        localToken: String? = PanelSession.localToken(),
        allowRefresh: Boolean = true,
    ): String {
        val first = doExecute(method, url, json, accessToken, localToken)
        if (!first.is401 || !allowRefresh) return first.unwrap()
        if (!PanelSession.tryRefresh()) return first.unwrap()
        return doExecute(method, url, json, PanelSession.accessToken(), localToken).unwrap()
    }

    private class RawResponse(val code: Int, val body: String) {
        val is401: Boolean get() = code == 401
        fun unwrap(): String = body.takeIf { code in 200..299 }
            ?: throw PanelApiException(code, body)
    }

    private fun doExecute(
        method: String,
        url: String,
        json: String?,
        accessToken: String?,
        localToken: String?,
    ): RawResponse {
        val builder = Request.Builder().url(url)
        originOf(url)?.let { builder.header("Origin", it) }
        accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        localToken?.takeIf { it.isNotBlank() }?.let { builder.header("x-daidai-local-token", it) }
        when (method.uppercase()) {
            "GET" -> builder.get()
            "DELETE" -> builder.delete()
            "POST" -> builder.post((json ?: "{}").toRequestBody(JSON_MEDIA_TYPE))
            "PUT" -> builder.put((json ?: "{}").toRequestBody(JSON_MEDIA_TYPE))
            "PATCH" -> builder.patch((json ?: "{}").toRequestBody(JSON_MEDIA_TYPE))
            else -> throw IllegalArgumentException("Unsupported method: $method")
        }
        sharedClient.newCall(builder.build()).execute().use { response ->
            return RawResponse(response.code, response.body?.string().orEmpty())
        }
    }

    /** 刷新 access_token（Authorization: Bearer <refresh_token>），成功持久化并返回 true。 */
    suspend fun refreshAccessToken(baseUrl: String, refreshToken: String, onRefreshed: (String, String?) -> Unit): Boolean {
        val body = runCatching {
            execute(
                method = "POST",
                url = "${baseUrl.trim().trimEnd('/')}/api/auth/refresh",
                json = JSONObject().put("refresh_token", refreshToken).toString(),
                accessToken = refreshToken,
                allowRefresh = false,
            )
        }.getOrElse { failure ->
            // 401 时直接抛 PanelApiException，其余网络错误包装为失败。
            throw failure
        }
        val json = runCatching { JSONObject(body) }.getOrNull() ?: return false
        val payload = json.optJSONObject("data") ?: json
        val access = payload.optString("access_token").takeIf { it.isNotEmpty() } ?: return false
        val refreshed = payload.optString("refresh_token").takeIf { it.isNotEmpty() }
        onRefreshed(access, refreshed)
        return true
    }
}
