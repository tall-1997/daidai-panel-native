package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.NotificationChannel
import com.daidai.daidai_app.data.model.NotificationChannelType
import com.daidai.daidai_app.data.model.parseNotificationChannels
import com.daidai.daidai_app.data.model.parseNotificationChannelTypes
import com.daidai.daidai_app.data.remote.PanelRequests
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

/**
 * 通知渠道仓库。
 *
 * 调用 `GET /api/tasks/notification-channels` 拉取渠道列表，
 * `GET /api/notifications/types` 拉取渠道类型 schema，
 * `POST /api/notifications/send` 发送测试通知。
 * 网络访问统一走 [PanelRequests]（进程级共享 OkHttp、Origin 头与 401 单飞刷新），
 * 与其余自包含仓库保持同一传输层。
 *
 * @param baseUrl 面板服务地址（不含结尾斜杠，会自动规整）
 * @param accessToken 登录后取得的访问令牌，用于 `Authorization: Bearer`
 */
class NotificationsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
) {
    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    /** 拉取全部通知渠道。HTTP 失败时抛出 [PanelApiException]。 */
    suspend fun getChannels(): List<NotificationChannel> = withContext(Dispatchers.IO) {
        val body = PanelRequests.execute(
            "GET",
            baseUrl + "/api/tasks/notification-channels",
            accessToken = accessToken,
        )
        parseNotificationChannels(body)
    }

    /**
     * 拉取通知渠道类型 schema（22 渠道，含字段定义）。
     * HTTP 失败时抛出 [PanelApiException]。
     */
    suspend fun getChannelTypes(): List<NotificationChannelType> = withContext(Dispatchers.IO) {
        val body = PanelRequests.execute(
            "GET",
            baseUrl + "/api/notifications/types",
            accessToken = accessToken,
        )
        parseNotificationChannelTypes(body)
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
        val body = PanelRequests.execute(
            "POST",
            baseUrl + "/api/notifications/send",
            json = payload.toString(),
            accessToken = accessToken,
        )
        runCatching {
            JSONObject(body).optString("message").takeIf { it.isNotBlank() }
        }.getOrNull() ?: "测试通知已发送"
    }

    suspend fun setChannelEnabled(id: Long, enabled: Boolean) = withContext(Dispatchers.IO) {
        PanelRequests.execute(
            "PUT", baseUrl + "/api/notifications/$id/" + if (enabled) "enable" else "disable",
            accessToken = accessToken,
        )
    }

    suspend fun deleteChannel(id: Long) = withContext(Dispatchers.IO) {
        PanelRequests.execute("DELETE", baseUrl + "/api/notifications/$id", accessToken = accessToken)
    }

    suspend fun testChannel(id: Long): String = withContext(Dispatchers.IO) {
        val body = PanelRequests.execute(
            "POST", baseUrl + "/api/notifications/$id/test", json = "{}", accessToken = accessToken,
        )
        runCatching { JSONObject(body).optString("message").takeIf { it.isNotBlank() } }.getOrNull() ?: "测试通知已发送"
    }
}
