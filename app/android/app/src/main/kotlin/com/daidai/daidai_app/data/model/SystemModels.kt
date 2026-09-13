package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 阶段 4-5 系统管理数据模型（DTO）：
 *   - [BackupRecord]       备份列表记录（GET /api/system/backups）
 *   - [HealthCheckItem]    健康检查单项（组件名/状态/说明）
 *   - [HealthCheckResult]  健康检查整体结果（GET/POST /api/system/health-check）
 *
 * 字段与后端保持一致：Go server（panel/server/handler/system.go）返回 `{"data": [...]}`
 * 或 `{"data": {"items": [...], ...}}` 包装。字段校验均做容错（optXxx + 默认值），
 * 后端任一字段缺失都不应导致页面崩溃。
 */

/**
 * 一次备份文件记录。后端列表项仅有 name/size/created_at 三个字段；
 * id/status 为给页面展示补充的派生字段（id 复用文件名字符串，status 默认 available）。
 */
data class BackupRecord(
    val id: String = "",
    val name: String = "",
    val size: Long = 0,
    val createdAt: String = "",
    val status: String = "available",
) {
    companion object {
        fun fromJson(json: JSONObject): BackupRecord {
            val name = json.optString("name").takeIf { it.isNotEmpty() }
                ?: json.optString("filename").takeIf { it.isNotEmpty() }
                ?: "-"
            val size = json.optLong("size", 0)
            // 兼容字符串时间或 epoch 秒（后端返回 time.Time 序列化为毫秒整数或字符串）
            val createdAt = when (val raw = json.opt("created_at")) {
                is String -> raw
                is Number -> java.text.SimpleDateFormat(
                    "yyyy-MM-dd HH:mm:ss",
                    java.util.Locale.CHINA,
                ).format(java.util.Date(raw.toLong() * 1000))
                else -> ""
            }
            return BackupRecord(
                id = name,
                name = name,
                size = size,
                createdAt = createdAt,
                status = "available",
            )
        }
    }
}

/** 健康检查的单项结果。 */
data class HealthCheckItem(
    val name: String = "-",
    val status: String = "unknown",
    val message: String = "",
) {
    companion object {
        fun fromJson(json: JSONObject): HealthCheckItem = HealthCheckItem(
            name = json.optString("name").takeIf { it.isNotEmpty() } ?: "-",
            status = json.optString("status").takeIf { it.isNotEmpty() } ?: "unknown",
            message = json.optString("message"),
        )
    }
}

/** 健康检查整体结果（GET 读缓存快照 / POST 立即重新检查）。 */
data class HealthCheckResult(
    val items: List<HealthCheckItem> = emptyList(),
    val lastCheckedAt: String = "",
) {
    val passedCount: Int get() = items.count { it.status.equals("pass", ignoreCase = true) || it.status.equals("ok", ignoreCase = true) }

    companion object {
        fun fromData(json: JSONObject): HealthCheckResult {
            val rawItems = json.optJSONArray("items") ?: JSONArray()
            val items = buildList {
                for (i in 0 until rawItems.length()) {
                    rawItems.optJSONObject(i)?.let { add(HealthCheckItem.fromJson(it)) }
                }
            }
            return HealthCheckResult(
                items = items,
                lastCheckedAt = json.optString("last_checked_at"),
            )
        }
    }
}
