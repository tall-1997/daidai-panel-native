package com.daidai.daidai_app.data.model

import org.json.JSONObject

/**
 * 个人中心（Profile）模块数据模型（DTO），阶段迁移自
 * Flutter `app/lib/features/profile/views/profile_page.dart`。
 *
 * 后端（Go handler）目前没有 `/api/profile` 或 `/api/auth/me` 专门端点，
 * 但 `auth.go` 提供了等价的当前登录用户端点：
 *   GET `/api/v1/auth/user`（JWT 鉴权）→ `{"user": {...User.ToDict()}}`
 * 返回字段含 `id / username / role / enabled / avatar_url / created_at` 等。
 * 本 ProfileInfo 即从该响应的 user 对象解析；版本号取自公开版本端点
 * `GET /api/v1/system/public-version`。
 *
 * 解析一律容错（optXxx + 默认值），任一字段缺失都不应导致页面崩溃，
 * 与 OpenApi / Tasks / Users 模块约定一致。
 */
data class ProfileInfo(
    val id: Long = 0L,
    val username: String = "",
    val role: String = "viewer",
    val enabled: Boolean = true,
    val avatarUrl: String = "",
    /** 面板版本号（来自 /api/v1/system/public-version）。 */
    val version: String = "",
) {
    /** 对用户可读的角色文案。 */
    val roleText: String
        get() = when (role.lowercase()) {
            "admin" -> "管理员"
            "viewer" -> "访客"
            "operator" -> "操作员"
            "developer" -> "开发者"
            else -> role
        }

    /** 用户信息是否可用（至少存在登录用户名）。 */
    val hasIdentity: Boolean get() = username.isNotBlank()

    companion object {
        /** 从头像 URL 中用到的临时占位 / 空值归一。 */
        fun normalizeAvatarUrl(raw: String?): String =
            raw?.trim()?.takeIf(String::isNotEmpty).orEmpty()

        /** 从 GET /api/v1/auth/user 的 user 对象解析，全部字段容错。 */
        fun fromAuthUser(userJson: JSONObject?, version: String = ""): ProfileInfo =
            if (userJson == null) {
                ProfileInfo(version = version)
            } else {
                ProfileInfo(
                    id = userJson.optLong("id"),
                    username = userJson.optString("username").trim(),
                    role = userJson.optString("role").takeIf { it.isNotBlank() } ?: "viewer",
                    enabled = if (userJson.has("enabled")) userJson.optBoolean("enabled") else true,
                    avatarUrl = normalizeAvatarUrl(userJson.optString("avatar_url")),
                    version = version,
                )
            }

        /** 从公开版本响应 `{"version": ..., "data": {"version": ...}}` 提取版本号。 */
        fun extractVersion(body: String): String =
            runCatching {
                val json = JSONObject(body)
                val raw = json.optString("version").takeIf { it.isNotBlank() }
                    ?: json.optJSONObject("data")?.optString("version").orEmpty()
                raw.ifBlank { "2.0.0" }
            }.getOrElse { "2.0.0" }
    }
}
