package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.NotificationChannel
import com.daidai.daidai_app.data.model.NotificationChannelType
import com.daidai.daidai_app.data.model.parseNotificationChannels
import com.daidai.daidai_app.data.model.parseNotificationChannelTypes
import com.daidai.daidai_app.data.remote.PanelApiException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * 通知渠道仓库。
 *
 * 调用 `GET /api/tasks/notification-channels` 拉取渠道列表，
 * `GET /api/notifications/types` 拉取渠道类型 schema，
 * `POST /api/notifications/send` 发送测试通知。
 * 网络访问保持独立于 Compose 屏幕（对齐阶段 0 的 [OkHttpClient] 惯例），
 * 通过注入的 baseUrl + accessToken 拼装请求，不修改既有网络/存储模块。
 *
 * @param baseUrl 面板服务地址（不含结尾斜杠，会自动规整）
 * @param accessToken 登录后取得的访问令牌，用于 `Authorization: Bearer`
 * @param httpClient 可注入的 OkHttp 客户端，便于测试
 */
class NotificationsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val httpClient: OkHttpClient = OkHttpClient(),
) {
    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    /** 拉取全部通知渠道。HTTP 失败时抛出 [PanelApiException]。 */
    suspend fun getChannels(): List<NotificationChannel> = withContext(Dispatchers.IO) {
        val builder = Request.Builder().url(baseUrl + "/api/tasks/notification-channels")
        accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        httpClient.newCall(builder.get().build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw PanelApiException(response.code, body)
            parseNotificationChannels(body)
        }
    }

    /**
     * 拉取通知渠道类型 schema（22 渠道，含字段定义）。
     * HTTP 失败时抛出 [PanelApiException]。
     */
    suspend fun getChannelTypes(): List<NotificationChannelType> = withContext(Dispatchers.IO) {
        val builder = Request.Builder().url(baseUrl + "/api/notifications/types")
        accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        httpClient.newCall(builder.get().build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw PanelApiException(response.code, body)
            parseNotificationChannelTypes(body)
        }
    }

    /**
     * 发送测试通知（远程服务端 / 本地 fallback 的 send 接口）。
     *
     * @param type 渠道类型（如 webhook / bark / custom）
     * @param config 渠道配置键值（草稿）
     * @return 成功时的可读消息
     */
    suspend fun sendTestForType(type: String, config: Map<String, String>): String = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("title", "呆呆面板测试通知")
            .put("content", "通知渠道配置正常（类型：$type）")
            .put("type", type)
            .put("config", JSONObject(config))
        val builder = Request.Builder().url(baseUrl + "/api/notifications/send")
        accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        builder.post(payload.toString().toRequestBody(JSON_MEDIA_TYPE))
        httpClient.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw PanelApiException(response.code, body)
            runCatching {
                val json = JSONObject(body)
                json.optString("message").takeIf { it.isNotBlank() }
            }.getOrNull() ?: "测试通知已发送"
        }
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
