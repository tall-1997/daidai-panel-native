package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 阶段 3-1 任务模块数据模型（DTO）。
 *
 * 字段名与后端保持一致（Go server 与 Kotlin fallback 均返回 `{"data": ...}` 或
 * `{"data":[...], "total":N}` 包装），解析均做容错（optXxx + 默认值），任一字段缺失
 * 都不应导致页面崩溃。
 *
 * 与 Flutter `app/lib/shared/models/task.dart` 的字段约定对齐：
 *  - `command`：脚本可执行路径（本模块记为 scriptPath）
 *  - `cron_expression` / `cron_expressions`：调度表达式
 *  - `task_type`：cron / manual / startup
 *  - `status`：0=已禁用, 0.5=排队中, 1=已启用, 2=运行中
 */
data class Task(
    val id: Long = 0,
    val name: String = "",
    val scriptPath: String = "",
    val schedule: String = "",
    val schedules: List<String> = emptyList(),
    val type: String = "cron",
    val status: Double = 0.0,
    val createdAt: String = "",
    val updatedAt: String = "",
) {
    /** status == 1 视为已启用（与 Flutter Task.isEnabled 一致）。 */
    val enabled: Boolean get() = status == 1.0

    /** 运行中（status == 2）。 */
    val running: Boolean get() = status == 2.0

    /** 排队中（status == 0.5）。 */
    val queued: Boolean get() = status == 0.5

    /** 对用户可读的状态文案。 */
    val statusText: String
        get() = when {
            running -> "运行中"
            queued -> "排队中"
            enabled -> "已启用"
            else -> "已禁用"
        }

    val typeText: String
        get() = when (type) {
            "cron" -> "常规定时"
            "manual" -> "手动运行"
            "startup" -> "开机运行"
            else -> "类型 $type"
        }

    companion object {
        fun fromJson(json: JSONObject): Task = Task(
            id = json.optLong("id"),
            name = json.optString("name"),
            scriptPath = json.optString("command"),
            schedule = json.optString("cron_expression"),
            schedules = run {
                val raw = json.opt("cron_expressions")
                if (raw is JSONArray) {
                    (0 until raw.length())
                        .mapNotNull { raw.optString(it).takeIf(String::isNotEmpty) }
                } else {
                    emptyList()
                }
            },
            type = json.optString("task_type").takeIf { it.isNotEmpty() } ?: "cron",
            status = json.optDouble("status", 0.0),
            createdAt = json.optString("created_at").takeIf { it.isNotEmpty() && it != "null" } ?: "",
            updatedAt = json.optString("updated_at").takeIf { it.isNotEmpty() && it != "null" } ?: "",
        )

        /** 解析后端 Paginated() 列表响应 {data:[...], total:N} 或直接 {data:[...]}。 */
        fun parseList(raw: String): List<Task> {
            val json = JSONObject(raw)
            val rawData = json.opt("data")
            val items = when (rawData) {
                is JSONArray -> (0 until rawData.length()).map { rawData.getJSONObject(it) }
                is JSONObject -> {
                    val inner = rawData.optJSONArray("data") ?: return emptyList()
                    (0 until inner.length()).map { inner.getJSONObject(it) }
                }
                else -> return emptyList()
            }
            return items.map { fromJson(it) }
        }
    }
}

/**
 * 任务创建 / 更新的请求载荷。仅写入精简字段（名称/脚本路径/调度/类型），
 * 未启用的任务以 `status=0` 落地（后端默认新任务启用）。
 */
data class TaskWritePayload(
    val name: String,
    val scriptPath: String,
    val schedule: String,
    val type: String = "cron",
    val status: Int = 1,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("name", name)
        put("command", scriptPath)
        when {
            schedule.isBlank() -> {
                put("cron_expression", "")
                put("cron_expressions", JSONArray())
            }
            else -> {
                val expressions = schedule.split('\n').map { it.trim() }.filter { it.isNotEmpty() }
                put("cron_expression", expressions.joinToString("\n"))
                put("cron_expressions", JSONArray(expressions))
            }
        }
        put("task_type", type)
        put("status", status)
    }
}