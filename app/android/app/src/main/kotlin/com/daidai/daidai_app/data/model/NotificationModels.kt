package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 通知渠道。映射自 Flutter `shared/models/notify_channel.dart`（NotifyChannel）。
 *
 * 只读展示使用的最小字段集：id / name / type / enabled / pushScope。
 * fromJson 对所有字段做容错解析，缺失或不合法时回退到安全默认值。
 */
data class NotificationChannel(
    val id: Long = 0L,
    val name: String = "",
    val type: String = "",
    val enabled: Boolean = true,
    val pushScope: String = "default",
) {
    companion object {
        /** 从单个渠道 JSON 对象解析。 */
        fun fromJson(json: JSONObject): NotificationChannel = NotificationChannel(
            id = json.optLong("id"),
            name = json.optString("name"),
            type = json.optString("type"),
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            pushScope = json.optString("push_scope", "default"),
        )
    }
}

/**
 * 把 GET /api/notifications/types 的响应体解析为渠道类型列表。
 *
 * 兼容多种服务端返回形状：顶层裸数组、或者包了一层
 * `items` / `channels` / `data` 字段的对象。无法解析时返回空列表。
 */
fun parseNotificationChannelTypes(rawBody: String): List<NotificationChannelType> {
    if (rawBody.isBlank()) return emptyList()
    val array: JSONArray = try {
        val root = JSONObject(rawBody)
        val items = root.optJSONArray("items")
            ?: root.optJSONArray("channels")
            ?: root.optJSONArray("data")
        if (items != null) {
            items
        } else {
            return emptyList()
        }
    } catch (_: Exception) {
        // 顶层不是对象则尝试直接当作数组解析
        try {
            JSONArray(rawBody)
        } catch (_: Exception) {
            return emptyList()
        }
    }
    val result = ArrayList<NotificationChannelType>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(NotificationChannelType.fromJson(element))
    }
    return result
}

/**
 * 把 GET /api/tasks/notification-channels 的响应体解析为渠道列表。
 *
 * 兼容多种服务端返回形状：顶层裸数组、或者包了一层
 * `items` / `channels` / `data` 字段的对象。无法解析时返回空列表。
 */
fun parseNotificationChannels(rawBody: String): List<NotificationChannel> {
    if (rawBody.isBlank()) return emptyList()
    val array: JSONArray = try {
        val root = JSONObject(rawBody)
        val items = root.optJSONArray("items")
            ?: root.optJSONArray("channels")
            ?: root.optJSONArray("data")
        if (items != null) {
            items
        } else {
            // 后端有时会返回空对象或空数据
            return emptyList()
        }
    } catch (_: Exception) {
        // 顶层不是对象则尝试直接当作数组解析
        try {
            JSONArray(rawBody)
        } catch (_: Exception) {
            return emptyList()
        }
    }
    val result = ArrayList<NotificationChannel>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(NotificationChannel.fromJson(element))
    }
    return result
}
