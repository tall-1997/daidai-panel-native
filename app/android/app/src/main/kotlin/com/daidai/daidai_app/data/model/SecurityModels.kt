package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 阶段 4-1 安全模块数据模型（DTO）。
 *
 * 字段名与后端对齐：Go server（panel/server/handler/security.go）的
 * `GET /api/security/{login-logs,sessions,audit-logs,login-stats}` 端点
 * 返回 `{"data": ...}` 包装，其中 login-logs / audit-logs 为分页数组
 * （`{data:[...], total, page, page_size}`），sessions 为纯数组，login-stats
 * 为对象。字段解析一律用 optXxx + 安全默认值，任一字段缺失都不应导致崩溃。
 */

/** 登录日志单条。status：0=成功，非 0=失败（后端约定）。 */
data class LoginLog(
    val id: Long = 0L,
    val username: String = "",
    val ip: String = "",
    val status: Int = 0,
    val message: String = "",
    val createdAt: String = "",
    val method: String = "",
    val clientName: String = "",
) {
    /** 登录是否成功。 */
    val success: Boolean get() = status == 0

    companion object {
        fun fromJson(json: JSONObject): LoginLog = LoginLog(
            id = json.optLong("id"),
            username = json.optString("username"),
            ip = json.optString("ip"),
            status = json.optInt("status"),
            message = json.optString("message"),
            createdAt = json.optString("created_at"),
            method = json.optString("method"),
            clientName = json.optString("client_name"),
        )
    }
}

/** 在线会话单条。 */
data class Session(
    val id: Long = 0L,
    val username: String = "",
    val ip: String = "",
    val createdAt: String = "",
    val expiresAt: String = "",
    val clientName: String = "",
    val clientType: String = "",
) {
    /** 最近活跃时间：后端仅有 created_at，就近活跃语义映射为创建时间。 */
    val lastActive: String get() = createdAt

    companion object {
        fun fromJson(json: JSONObject): Session = Session(
            id = json.optLong("id"),
            username = json.optString("username"),
            ip = json.optString("ip"),
            createdAt = json.optString("created_at"),
            expiresAt = json.optString("expires_at"),
            clientName = json.optString("client_name"),
            clientType = json.optString("client_type"),
        )
    }
}

/** 审计日志单条。 */
data class AuditLog(
    val id: Long = 0L,
    val username: String = "",
    val action: String = "",
    val detail: String = "",
    val ip: String = "",
    val createdAt: String = "",
) {
    companion object {
        fun fromJson(json: JSONObject): AuditLog = AuditLog(
            id = json.optLong("id"),
            username = json.optString("username"),
            action = json.optString("action"),
            detail = json.optString("detail"),
            ip = json.optString("ip"),
            createdAt = json.optString("created_at"),
        )
    }
}

/** 登录统计概览（GET /api/security/login-stats?days=N 的 data 对象）。 */
data class SecurityOverview(
    val totalLogins: Long = 0L,
    val successLogins: Long = 0L,
    val failedLogins: Long = 0L,
    val activeSessions: Long = 0L,
    val lockedAccounts: Long = 0L,
    val periodDays: Int = 7,
) {
    /** 由统计数据推导的轻量安全风险提示；空表示暂时没有明显风险。 */
    val riskHints: List<String>
        get() {
            val hints = ArrayList<String>()
            if (failedLogins >= 5) {
                hints.add("近 $periodDays 天登录失败 $failedLogins 次，可能存在暴力破解尝试")
            }
            if (lockedAccounts > 0) {
                hints.add("有 $lockedAccounts 个账号因多次失败被临时锁定")
            }
            return hints
        }

    companion object {
        fun fromJson(json: JSONObject): SecurityOverview = SecurityOverview(
            totalLogins = json.optLong("total_logins"),
            successLogins = json.optLong("success_logins"),
            failedLogins = json.optLong("failed_logins"),
            activeSessions = json.optLong("active_sessions"),
            lockedAccounts = json.optLong("locked_accounts"),
            periodDays = json.optInt("period_days", 7),
        )
    }
}

/** 解析 `{data:[...]}` 或 `{data:{...}}` 包裹中的数组体的通用工具。 */
internal object SecurityJson {
    fun dataArray(raw: String): JSONArray {
        val root = JSONObject(raw)
        val node = root.opt("data") ?: return JSONArray()
        return when (node) {
            is JSONArray -> node
            else -> JSONArray()
        }
    }
}

/** 解析登录日志数组（容忍空/非法）。 */
fun parseLoginLogs(raw: String): List<LoginLog> {
    val array = SecurityJson.dataArray(raw)
    val result = ArrayList<LoginLog>(array.length())
    for (i in 0 until array.length()) {
        array.optJSONObject(i)?.let { result.add(LoginLog.fromJson(it)) }
    }
    return result
}

/** 解析会话数组（容忍空/非法）。 */
fun parseSessions(raw: String): List<Session> {
    val array = SecurityJson.dataArray(raw)
    val result = ArrayList<Session>(array.length())
    for (i in 0 until array.length()) {
        array.optJSONObject(i)?.let { result.add(Session.fromJson(it)) }
    }
    return result
}

/** 解析审计日志数组（容忍空/非法）。 */
fun parseAuditLogs(raw: String): List<AuditLog> {
    val array = SecurityJson.dataArray(raw)
    val result = ArrayList<AuditLog>(array.length())
    for (i in 0 until array.length()) {
        array.optJSONObject(i)?.let { result.add(AuditLog.fromJson(it)) }
    }
    return result
}

/** 解析登录统计概览（data 为对象，缺失时给空概览）。 */
fun parseSecurityOverview(raw: String): SecurityOverview {
    val root = JSONObject(raw)
    val node = root.optJSONObject("data") ?: return SecurityOverview()
    return SecurityOverview.fromJson(node)
}
