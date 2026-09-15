package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * A1 平台/令牌管理数据模型（对齐 panel/server/model/platform_token.go 与
 * panel/server/handler/platform_token.go 的响应契约）。
 *
 * 关键语义（两端一致）：
 *   - 列表接口的 token 字段永远是掩码 [PLATFORM_TOKEN_MASK]，明文令牌只在创建时
 *     由用户输入，服务端密封存储（Go: AES-GCM；local: SQLite 明文但响应同样打码）。
 *   - 更新时若把掩码原样发回，服务端会跳过 token 字段（isMaskedToken 守卫），
 *     客户端同样在 payload 构造时剔除，双重防御。
 *   - Go 端 `sealed` 为布尔；本地端不提供该键，默认 false。
 */

/** 列表回显用的固定掩码值，与服务端 `common.MaskedTokenValue` 保持一致。 */
const val PLATFORM_TOKEN_MASK: String = "********"

/** token 值是否为服务端掩码哨兵（与 Go isMaskedToken / LocalPanelStore 判定一致）。 */
fun isMaskedPlatformToken(value: String?): Boolean =
    value != null && value.trim() == PLATFORM_TOKEN_MASK

/** 平台定义（GET /api/platform-tokens/platforms）。 */
data class PlatformInfo(
    val id: Long = 0,
    val name: String = "",
    val label: String = "",
    val icon: String = "",
) {
    /** label 为空时退回 name 展示。 */
    val displayName: String get() = label.ifBlank { name }

    companion object {
        fun fromJson(json: JSONObject): PlatformInfo = PlatformInfo(
            id = json.optLong("id"),
            name = json.optString("name"),
            label = json.optString("label"),
            icon = json.optString("icon"),
        )

        /** 解析 `{"data":[...]}` 顶层包装；缺失 data 时返回空列表。 */
        fun listFromResponse(body: String): List<PlatformInfo> {
            val data = JSONObject(body).optJSONArray("data") ?: JSONArray()
            return buildList {
                for (i in 0 until data.length()) {
                    data.optJSONObject(i)?.let { add(fromJson(it)) }
                }
            }
        }
    }
}

/** 平台令牌（GET /api/platform-tokens），token 字段仅承载掩码，永不持有明文。 */
data class PlatformTokenInfo(
    val id: Long = 0,
    val platformId: Long = 0,
    val name: String = "",
    /** 服务端回显的掩码值；本页从不需要明文（明文只在创建请求里由用户输入一次）。 */
    val maskedToken: String = PLATFORM_TOKEN_MASK,
    val sealed: Boolean = false,
    val service: String = "",
    val serviceUser: String = "",
    val remarks: String = "",
    val enabled: Boolean = true,
    val expiresAt: String = "",
    val createdAt: String = "",
    val updatedAt: String = "",
    /** 平台展示名：Go 的 platform_name（=label）或本地端 platform 对象。 */
    val platformDisplayName: String = "",
    val displayToken: String = maskedToken,
) {
    companion object {
        fun fromJson(json: JSONObject): PlatformTokenInfo {
            val platformLabel = json.optString("platform_name")
            val platformObj = json.optJSONObject("platform")
            val platformName = when {
                platformLabel.isNotBlank() -> platformLabel
                platformObj != null ->
                    platformObj.optString("label").ifBlank { platformObj.optString("name") }
                else -> ""
            }
            return PlatformTokenInfo(
                id = json.optLong("id"),
                platformId = json.optLong("platform_id"),
                name = json.optString("name"),
                maskedToken = json.optString("token").ifBlank { PLATFORM_TOKEN_MASK },
                sealed = json.optBoolean("sealed", false),
                service = json.optString("service"),
                serviceUser = json.optString("service_user"),
                remarks = json.optString("remarks"),
                enabled = json.optBoolean("enabled", true),
                expiresAt = json.optString("expires_at"),
                createdAt = json.optString("created_at"),
                updatedAt = json.optString("updated_at"),
                platformDisplayName = platformName,
            )
        }

        /** 解析 `{"data":[...]}`；缺失 data 时返回空列表。 */
        fun listFromResponse(body: String): List<PlatformTokenInfo> {
            val data = JSONObject(body).optJSONArray("data") ?: JSONArray()
            return buildList {
                for (i in 0 until data.length()) {
                    data.optJSONObject(i)?.let { add(fromJson(it)) }
                }
            }
        }
    }
}

/**
 * POST /api/platform-tokens 请求体。platform_id/name/token 必填校验在调用方，
 * 空 name/token 直接抛 [IllegalArgumentException]（对齐服务端 required 绑定）。
 */
internal fun platformTokenCreatePayload(
    platformId: Long,
    name: String,
    token: String,
    service: String,
    serviceUser: String,
    remarks: String,
): JSONObject {
    require(platformId > 0) { "必须选择所属平台" }
    require(name.isNotBlank()) { "令牌名称不能为空" }
    require(token.isNotBlank()) { "令牌内容不能为空" }
    return JSONObject()
        .put("platform_id", platformId)
        .put("name", name.trim())
        .put("token", token.trim())
        .put("service", service.trim())
        .put("service_user", serviceUser.trim())
        .put("remarks", remarks.trim())
}

/**
 * PUT /api/platform-tokens/:id 请求体。仅携带被显式修改的键；
 * token 为空或等于掩码时剔除（服务端同样会拒绝掩码写入）。
 */
internal fun platformTokenUpdatePayload(
    name: String? = null,
    token: String? = null,
    service: String? = null,
    serviceUser: String? = null,
    remarks: String? = null,
): JSONObject = JSONObject().apply {
    name?.trim()?.takeIf { it.isNotEmpty() }?.let { put("name", it) }
    token?.trim()?.takeIf { it.isNotEmpty() && !isMaskedPlatformToken(it) }?.let { put("token", it) }
    service?.trim()?.let { put("service", it) }
    serviceUser?.trim()?.let { put("service_user", it) }
    remarks?.trim()?.let { put("remarks", it) }
}

/** POST /api/platform-tokens/platforms 请求体（name 必填）。 */
internal fun platformCreatePayload(name: String, label: String, icon: String): JSONObject {
    require(name.isNotBlank()) { "平台标识不能为空" }
    return JSONObject()
        .put("name", name.trim())
        .put("label", label.trim().ifEmpty { name.trim() })
        .put("icon", icon.trim())
}
