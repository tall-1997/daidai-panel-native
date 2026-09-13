package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.NotificationChannel
import com.daidai.daidai_app.data.model.parseNotificationChannels
import com.daidai.daidai_app.data.remote.PanelApiException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request

/**
 * 通知渠道只读仓库。
 *
 * 调用 `GET /api/tasks/notification-channels` 拉取渠道列表。
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
}
