package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * Open API 应用（令牌主体）。映射自 Flutter `open_api_page.dart` 的应用卡片字段。
 *
 * GET /api/open-api/apps 返回的应用列表项，只读展示使用的最小字段集：
 * id / name / app_key / scopes / enabled / rate_limit / created_at。
 * fromJson 对所有字段做容错解析，缺失或不合法时回退到安全默认值。
 */
data class OpenApiApp(
    val id: Long = 0L,
    val name: String = "",
    val appKey: String = "",
    val scopes: List<String> = emptyList(),
    val enabled: Boolean = true,
    val rateLimit: Int = 0,
    val createdAt: String = "",
) {
    /** 密钥前缀：展示用截断（前 8 位），避免全文泄漏到列表界面。 */
    val appKeyPrefix: String
        get() = appKey.take(8)

    companion object {
        /** 从单个应用 JSON 对象解析。scopes 为逗号分隔字符串。 */
        fun fromJson(json: JSONObject): OpenApiApp = OpenApiApp(
            id = json.optLong("id"),
            name = json.optString("name"),
            appKey = json.optString("app_key"),
            scopes = json.optString("scopes")
                .split(',')
                .map { it.trim() }
                .filter { it.isNotEmpty() },
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            rateLimit = json.optInt("rate_limit"),
            createdAt = json.optString("created_at"),
        )
    }
}

/**
 * 把 GET /api/open-api/apps 的响应体解析为应用列表。
 *
 * 兼容多种服务端返回形状：顶层裸数组，或包了一层
 * `items` / `apps` / `data` 字段的对象。无法解析时返回空列表。
 */
fun parseOpenApiApps(rawBody: String): List<OpenApiApp> {
    if (rawBody.isBlank()) return emptyList()
    val array: JSONArray = try {
        val json = JSONObject(rawBody)
        val nested = json.optJSONArray("items")
            ?: json.optJSONArray("apps")
            ?: json.optJSONArray("data")
        if (nested != null) nested else JSONArray(rawBody)
    } catch (_: Exception) {
        try {
            JSONArray(rawBody)
        } catch (_: Exception) {
            return emptyList()
        }
    }
    val result = ArrayList<OpenApiApp>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(OpenApiApp.fromJson(element))
    }
    return result
}