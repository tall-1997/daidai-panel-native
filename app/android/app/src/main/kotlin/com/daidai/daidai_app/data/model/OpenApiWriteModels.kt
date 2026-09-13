package com.daidai.daidai_app.data.model

import org.json.JSONObject

/**
 * Open API 应用写操作的数据模型（Compose 原生，补齐-2）。
 *
 * 仅承载 OpenApi Create / Detail 两页的写请求与响应载荷，不触碰既有只读
 * [OpenApiApp] / [parseOpenApiApps]（保持阶段 2 只读文件不动）。
 * 映射自 Flutter `open_api_page.dart` 的写操作：
 *  - 创建：POST /api/open-api/apps   body {name, scopes, rate_limit}
 *  - 更新：PUT  /api/open-api/apps/:id body {enabled, scopes, rate_limit}
 *  - 重置密钥：POST /api/open-api/apps/:id/reset-secret
 *  - 删除：DELETE /api/open-api/apps/:id
 *
 * 密钥（app_secret）只会在创建 / 重置密钥成功后由后端一次性返回，回调给 UI
 * 展示后丢弃，不持久化（与 Flutter `_showSecretDialog` 的语义一致）。
 */
data class CreateAppPayload(
    val name: String = "",
    val scopes: List<String> = emptyList(),
    val rateLimit: Int = 100,
) {
    /** 序列化为后端契约的 JSON 请求体。scopes 以逗号分隔拼接。 */
    fun toJson(): JSONObject = JSONObject()
        .put("name", name)
        .put("scopes", scopes.joinToString(","))
        .put("rate_limit", rateLimit)
}

/** 更新单个应用：启停 + 权限范围 + 限流（PUT /api/open-api/apps/:id）。 */
data class UpdateAppPayload(
    val enabled: Boolean = true,
    val scopes: List<String> = emptyList(),
    val rateLimit: Int = 0,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("enabled", enabled)
        .put("scopes", scopes.joinToString(","))
        .put("rate_limit", rateLimit)
}

/**
 * 创建 / 重置密钥接口返回的凭据。secret 只在响应中出现一次，UI 成功展示后即弃。
 * fromJson 容错解析：app_secret 缺失时不抛异常，退回空字符串由调用方判断。
 */
data class ResetSecretResult(
    val appKey: String = "",
    val secret: String = "",
) {
    /** 是否携带了可展示的密钥（后端每次生成都会返回 app_secret）。 */
    val hasSecret: Boolean get() = secret.isNotBlank()

    companion object {
        fun fromJson(json: JSONObject): ResetSecretResult = ResetSecretResult(
            appKey = json.optString("app_key"),
            secret = json.optString("app_secret"),
        )
    }
}

/**
 * 把接口响应逐步解包到 JSON 对象：兼容「{ app_key, app_secret }」直接返回，
 * 也兼容标准 `{ code, message, data: { ... } }` 包装（data 内再解析）。
 * 返回 null 表示拿不到可解析对象。
 */
internal fun dataObjectOrNull(body: String): JSONObject? {
    val trimmed = body.trim()
    if (trimmed.isEmpty()) return null
    return try {
        val root = JSONObject(trimmed)
        val candidate = if (root.has("data")) root.opt("data") else root
        when (candidate) {
            is JSONObject -> candidate
            is String -> {
                val text = candidate.trim()
                if (text.isEmpty()) null
                else try {
                    JSONObject(text)
                } catch (_: Exception) {
                    null
                }
            }
            else -> null
        }
    } catch (_: Exception) {
        null
    }
}
