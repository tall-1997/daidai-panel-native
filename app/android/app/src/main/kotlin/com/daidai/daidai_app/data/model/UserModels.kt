package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 阶段 4-4 用户管理模块数据模型（DTO）。
 *
 * 字段名与后端保持一致（Go `model.User.ToDict()` 返回
 * `id / username / role / enabled / avatar_url / created_at / last_login_at`），
 * 列表接口返回 `{"data": [...]}` 包装。解析均做容错（optXxx + 默认值），
 * 任一字段缺失都不应导致页面崩溃（与 OpenApi / Tasks 模块约定一致）。
 *
 * 与 Flutter `app/lib/features/users/views/user_list_page.dart`
 * `UserListItem.fromJson` 的字段语义对齐。
 */
data class User(
    val id: Long = 0L,
    val username: String = "",
    val role: String = "viewer",
    val enabled: Boolean = true,
    val createdAt: String = "",
    val avatar: String = "",
    val lastLoginAt: String = "",
) {
    /** 对用户可读的状态文案。 */
    val enabledText: String get() = if (enabled) "已启用" else "已禁用"

    companion object {
        /** 从单个用户 JSON 对象解析，所有字段容错。 */
        fun fromJson(json: JSONObject): User = User(
            id = json.optLong("id"),
            username = json.optString("username"),
            role = json.optString("role").takeIf { it.isNotBlank() } ?: "viewer",
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            createdAt = json.optString("created_at"),
            avatar = json.optString("avatar_url"),
            lastLoginAt = if (json.isNull("last_login_at")) "" else json.optString("last_login_at"),
        )

        /**
         * 解析列表响应体。兼容以下两种形态，均做容错：
         *  - 直接数组 `[...]`
         *  - 标准包装 `{"data": [...]}`
         */
        fun parseList(raw: String): List<User> {
            val rawTrimmed = raw.trim()
            if (rawTrimmed.isEmpty()) return emptyList()
            val items = try {
                when (rawTrimmed.first()) {
                    '[' -> JSONArray(rawTrimmed)
                    '{' -> {
                        val rawData = JSONObject(rawTrimmed).opt("data")
                        when (rawData) {
                            is JSONArray -> rawData
                            is JSONObject -> rawData.optJSONArray("data") ?: return emptyList()
                            else -> return emptyList()
                        }
                    }
                    else -> return emptyList()
                }
            } catch (_: Exception) {
                return emptyList()
            }
            val result = ArrayList<User>(items.length())
            for (i in 0 until items.length()) {
                val element = items.optJSONObject(i) ?: continue
                result.add(fromJson(element))
            }
            return result
        }
    }
}

/**
 * 重置密码请求载荷。
 *
 * 对应 `PUT /api/users/:id/reset-password`，后端仅写入 `password` 字段
 * （校验 6-128 位）。
 */
data class ResetPasswordPayload(
    val password: String,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("password", password)
    }
}
