package com.daidai.daidai_app.data.model

import org.json.JSONObject

/**
 * 运行日志状态：0=成功 1=失败 2=运行中 3=已终止。
 * 与 Flutter `app/lib/shared/models/task_log.dart` 的状态约定一致。
 */
enum class LogStatus(val code: Int, val label: String) {
    Success(0, "成功"),
    Failed(1, "失败"),
    Running(2, "运行中"),
    Aborted(3, "已终止");

    companion object {
        fun fromCode(code: Int?): LogStatus? =
            if (code == null) null else values().firstOrNull { it.code == code }
    }
}

/** 单条任务日志，字段与后端 GET /api/logs 列表返回项对齐。 */
data class LogEntry(
    val id: Long,
    val taskId: Long,
    val taskName: String?,
    val content: String,
    val status: LogStatus?,
    val duration: Double?,
    val logPath: String?,
    val createdAt: String?,
) {
    companion object {
        fun fromJson(json: JSONObject): LogEntry = LogEntry(
            id = json.optLong("id"),
            taskId = json.optLong("task_id"),
            taskName = json.optString("task_name").takeIf { it.isNotEmpty() },
            content = json.optString("content"),
            status = LogStatus.fromCode(
                if (json.has("status") && !json.isNull("status")) json.optInt("status", -1) else null
            ),
            duration = if (json.has("duration") && !json.isNull("duration")) json.optDouble("duration") else null,
            logPath = json.optString("log_path").takeIf { it.isNotEmpty() },
            createdAt = json.optString("created_at").takeIf { it.isNotEmpty() },
        )
    }
}

/** 分页响应，兼容后端 response.Paginated() 格式 {data:[...], total:N, page:N, page_size:N}。 */
data class LogPage(
    val items: List<LogEntry>,
    val total: Int,
    val page: Int,
    val pageSize: Int,
) {
    val hasMore: Boolean get() = items.size < total
}
