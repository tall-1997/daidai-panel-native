package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.Subscription
import com.daidai.daidai_app.data.model.SubscriptionWritePayload
import com.daidai.daidai_app.data.model.parseSubscriptions
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.TimeUnit
import com.daidai.daidai_app.data.model.SubscriptionLogPage
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.emitAll
import okhttp3.HttpUrl.Companion.toHttpUrl

/**
 * 订阅模块（阶段 3-5）数据源仓库。
 *
 * 读写 `GET/POST /api/subscriptions` 与 `PUT/DELETE /api/subscriptions/:id`，
 * 以及 `PUT /api/subscriptions/:id/pull`（触发一次拉取更新）。契约对齐 Go handler
 * 与 Kotlin fallback `serveSubscriptions`，两套后端均把成功响应包在 `{"data": ...}` 中。
 *
 * 认证与连接方式复用阶段 1/2 约定（参考 [DashboardRepository]）：
 *  - REMOTE：baseUrl = serverUrl，`Authorization: Bearer <accessToken>`
 *  - MANAGED_LOCAL：baseUrl / localToken 由 [PanelCoreController] 提供，
 *    `x-daidai-local-token` 头。
 *
 * 网络层用 OkHttp 直连，keep-alive 复用同一实例。每次请求前重新解析连接
 * （本地模式启动后 URL/token 可能变化）。
 */
class SubscriptionsRepository(
    context: Context,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) {
    private val appContext = context.applicationContext
    private val configRepo = AppServices.configRepository(context)

    /** 单次请求的连接描述：baseUrl + 认证头参数。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    // ---------------------------------------------------------------- CRUD API

    suspend fun list(): List<Subscription> = withContext(Dispatchers.IO) {
        val all = mutableListOf<Subscription>()
        var page = 1
        while (true) {
            val body = execute("GET", "/api/subscriptions?page=$page&page_size=100", conn = resolveConnection())
            val batch = parseSubscriptions(body)
            if (batch.isEmpty()) break
            all.addAll(batch)
            if (batch.size < 100) break
            page++
        }
        all
    }

    /** 新建订阅，返回后端返回的订阅 id（不可解析时回退 0）。 */
    suspend fun create(payload: SubscriptionWritePayload): Long = withContext(Dispatchers.IO) {
        val body = execute(
            "POST", "/api/subscriptions", payload.toJson().toString(), resolveConnection(),
        )
        try {
            JSONObject(body).optJSONObject("data")?.optLong("id") ?: 0L
        } catch (_: Exception) {
            0L
        }
    }

    /** 更新指定订阅（仅发送请求包含的关键字段）。 */
    suspend fun update(id: Long, payload: SubscriptionWritePayload) = withContext(Dispatchers.IO) {
        execute(
            "PUT", "/api/subscriptions/$id", payload.toJson().toString(), resolveConnection(),
        )
    }

    /** 删除指定订阅。 */
    suspend fun delete(id: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "/api/subscriptions/$id", conn = resolveConnection())
    }

    /** 触发指定订阅的拉取更新（PUT /api/subscriptions/:id/pull）。 */
    suspend fun pull(id: Long) = withContext(Dispatchers.IO) {
        execute("PUT", "/api/subscriptions/$id/pull", conn = resolveConnection())
    }

    suspend fun stopPull(id: Long) = execute("PUT", "/api/subscriptions/$id/pull/stop", conn = resolveConnection())

    suspend fun logs(id: Long, page: Int = 1): SubscriptionLogPage {
        require(page > 0)
        return SubscriptionLogPage.parse(execute("GET", "/api/subscriptions/$id/logs?page=$page&page_size=20",
            conn = resolveConnection()), page)
    }

    internal fun pullStream(id: Long, cursor: String = "", operationId: String = "") = flow {
        val conn = resolveConnection()
        val url = (conn.baseUrl + "/api/subscriptions/$id/pull-stream").toHttpUrl().newBuilder()
        if (cursor.isNotBlank()) url.addQueryParameter("cursor", cursor)
        if (operationId.isNotBlank()) url.addQueryParameter("operation_id", operationId)
        emitAll(CapabilityRequests(httpClient, conn.accessToken, conn.localToken).events(url.build().toString()))
    }

    // ---------------------------------------------------------------- Connection

    private suspend fun resolveConnection(): Connection {
        val config = configRepo.getConfig() // AppServices 幂等初始化，只读安全
        return when (config.mode) {
            PanelConnectionMode.REMOTE -> {
                val baseUrl = config.serverUrl.trim().trimEnd('/')
                if (baseUrl.isBlank()) {
                    throw IllegalStateException("尚未配置远程服务地址，请先在“配置服务器”页面填写")
                }
                Connection(baseUrl, config.accessToken, null)
            }
            PanelConnectionMode.MANAGED_LOCAL -> {
                val status = PanelCoreController.ensureStarted()
                val baseUrl = status.baseUrl?.trim()?.trimEnd('/')
                if (baseUrl.isNullOrBlank()) {
                    throw IllegalStateException("本地服务未能启动：${status.message ?: "未知原因"}")
                }
                Connection(baseUrl, null, status.localToken)
            }
        }
    }

    // ---------------------------------------------------------------- HTTP helper

    /** 执行 HTTP 请求并返回响应体字符串；非 2xx 抛 [SubscriptionsApiException]。 */
    private suspend fun execute(
        method: String,
        path: String,
        json: String?,
        conn: Connection,
    ): String = CapabilityRequests(httpClient, conn.accessToken, conn.localToken).text(method, conn.baseUrl + path, json)

    private suspend fun execute(method: String, path: String, conn: Connection) =
        execute(method, path, null, conn)

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败：保留状态码与原始响应体（供 UI 展示错误详情）。 */
class SubscriptionsApiException(
    val statusCode: Int,
    val responseBody: String = "",
) : RuntimeException(
    "订阅请求失败（HTTP $statusCode${if (responseBody.isBlank()) "" else ": $responseBody"}）",
)
