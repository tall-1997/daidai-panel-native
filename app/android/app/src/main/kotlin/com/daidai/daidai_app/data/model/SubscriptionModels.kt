package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 订阅（Subscription）模块数据模型（阶段 3-5，Compose 原生）。
 *
 * 迁移自 Flutter `app/lib/shared/models/subscription.dart` 的字段子集，仅保留
 * 列表 / 详情 / 编辑所需的最小字段：id / name / url / targetPath（目标路径）/
 * enabled / lastPullAt（最近一次拉取时间）。
 *
 * JSON 键与后端契约对齐：
 *  - `save_dir`：目标保存路径（服务端对“目标路径”的字段名）。
 *  - `last_pull_at`：最近拉取时间；托管本地面板返回 `last_sync`，亦作兼容解析。
 * 其余字段缺省时回退到安全默认值。
 */
data class Subscription(
    val id: Long = 0L,
    val name: String = "",
    val url: String = "",
    /** 目标保存路径（对后端 `save_dir` / `target_path`）。 */
    val targetPath: String = "",
    val enabled: Boolean = true,
    /** 最近一次拉取时间；可能为空字符串（尚未拉取）。 */
    val lastPullAt: String = "",
) {
    companion object {
        /** 从单个订阅 JSON 对象容错解析。targetPath 优先 `save_dir`，其次 `target_path`。 */
        fun fromJson(json: JSONObject): Subscription = Subscription(
            id = json.optLong("id"),
            name = json.optString("name"),
            url = json.optString("url"),
            targetPath = json.optString("save_dir")
                .takeIf { it.isNotBlank() }
                ?: json.optString("target_path"),
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            lastPullAt = json.optString("last_pull_at").takeIf { it.isNotEmpty() && it != "null" } ?: ""
                .takeIf { it.isNotBlank() }
                ?: json.optString("last_sync"),
        )
    }
}

/**
 * 新建 / 更新订阅的写负载。
 *
 * 仅承载关键可编辑字段（名称 / 地址 / 目标路径 / 启用），toJson 产出后端
 * 期望的 JSON（目标路径以 `save_dir` 传输），name / url 必填由调用方保证。
 */
data class SubscriptionWritePayload(
    val name: String,
    val url: String,
    val targetPath: String = "",
    val enabled: Boolean = true,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("name", name)
        put("url", url)
        put("enabled", enabled)
        if (targetPath.isNotBlank()) put("save_dir", targetPath)
    }
}

/**
 * 把 GET /api/subscriptions 的响应体解析为订阅列表。
 *
 * 兼容多种服务端返回形状：顶层裸数组，或包了一层 `items` / `subscriptions`
 * / `data` 字段的对象。无法解析时返回空列表。
 */
fun parseSubscriptions(rawBody: String): List<Subscription> {
    if (rawBody.isBlank()) return emptyList()
    val array: JSONArray = try {
        val json = JSONObject(rawBody)
        val nested = json.optJSONArray("items")
            ?: json.optJSONArray("subscriptions")
            ?: json.optJSONArray("data")
        if (nested != null) nested else JSONArray(rawBody)
    } catch (_: Exception) {
        try {
            JSONArray(rawBody)
        } catch (_: Exception) {
            return emptyList()
        }
    }
    val result = ArrayList<Subscription>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(Subscription.fromJson(element))
    }
    return result
}
