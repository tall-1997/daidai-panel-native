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
 *
 * 高级执行字段（F4 补齐，JSON key 与 Go 后端 task_mutate.go / task.go ToDict 以及
 * 本地 LocalPanelStore.kt createTask/updateTask/taskDetailData 对齐）：
 *  - `timeout`：单次运行超时秒数（0=不限，上限 604800）
 *  - `max_retries` / `retry_interval`：失败重试次数 / 重试间隔秒
 *  - `stop_schedule`：定时停止 cron（到点停止任务）
 *  - `depends_on`：前置依赖任务 id（单值，与两端一致）
 *  - `task_before` / `task_after`：前后置钩子（脚本路径或命令）
 *  - `allow_multiple_instances`：是否允许多实例并发
 *  - `schedule_policy`：调度并发策略 skip / queue / parallel
 *  - `notify_on_failure/success/abort` + `notification_channel_id`：运行通知
 *  - `python_version`：脚本运行所需 Python 版本
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
    val timeout: Int = 0,
    val maxRetries: Int = 0,
    val retryInterval: Int = 60,
    val stopSchedule: String = "",
    val dependsOn: Long? = null,
    val taskBefore: String = "",
    val taskAfter: String = "",
    val allowMultipleInstances: Boolean = false,
    val schedulePolicy: String = "skip",
    val pythonVersion: String = "",
    val notifyOnFailure: Boolean = false,
    val notifyOnSuccess: Boolean = false,
    val notifyOnAbort: Boolean = false,
    val successExitCodes: String = "0",
    val randomDelaySeconds: Int? = null,
    val labels: List<String> = emptyList(),
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
            timeout = json.optInt("timeout", 0).coerceIn(0, 604800),
            maxRetries = json.optInt("max_retries", 0).coerceIn(0, 20),
            retryInterval = json.optInt("retry_interval", 60).coerceIn(0, 86400),
            stopSchedule = json.optString("stop_schedule"),
            dependsOn = if (json.has("depends_on") && !json.isNull("depends_on")) {
                json.optLong("depends_on").takeIf { it > 0 }
            } else null,
            taskBefore = json.optString("task_before"),
            taskAfter = json.optString("task_after"),
            allowMultipleInstances = json.optBoolean("allow_multiple_instances"),
            schedulePolicy = json.optString("schedule_policy").takeIf { it.isNotEmpty() } ?: "skip",
            pythonVersion = json.optString("python_version"),
            notifyOnFailure = json.optBoolean("notify_on_failure"),
            notifyOnSuccess = json.optBoolean("notify_on_success"),
            notifyOnAbort = json.optBoolean("notify_on_abort"),
            successExitCodes = json.optString("success_exit_codes").takeIf { it.isNotEmpty() } ?: "0",
            randomDelaySeconds = if (json.has("random_delay_seconds") && !json.isNull("random_delay_seconds")) {
                json.optInt("random_delay_seconds").takeIf { it >= 0 }
            } else null,
            labels = run {
                val raw = json.opt("labels")
                if (raw is JSONArray) {
                    (0 until raw.length()).mapNotNull { raw.optString(it).takeIf(String::isNotEmpty) }
                } else if (raw is String && raw.isNotBlank()) {
                    raw.split(',').map { it.trim() }.filter(String::isNotEmpty)
                } else {
                    emptyList()
                }
            },
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
 * 任务创建 / 更新的请求载荷。写入精简字段（名称/脚本路径/调度/类型）+ 高级执行字段
 * （F4 补齐：超时/重试/定时停止/依赖/前后置钩子/并发控制/通知/Python 版本）。
 *
 * JSON key 与 Go 后端 `task_mutate.go` Create/Update 请求体及本地
 * `LocalPanelStore.kt` createTask/updateTask 的读取 key 对齐：
 *  - name / command / cron_expression / cron_expressions / task_type / status
 *  - timeout / max_retries / retry_interval / stop_schedule / depends_on
 *  - task_before / task_after / allow_multiple_instances / schedule_policy
 *  - python_version / notify_on_failure / notify_on_success / notify_on_abort
 *  - success_exit_codes / random_delay_seconds / labels
 */
data class TaskWritePayload(
    val name: String,
    val scriptPath: String,
    val schedule: String,
    val type: String = "cron",
    val status: Int = 1,
    val timeout: Int = 0,
    val maxRetries: Int = 0,
    val retryInterval: Int = 60,
    val stopSchedule: String = "",
    val dependsOn: Long? = null,
    val taskBefore: String = "",
    val taskAfter: String = "",
    val allowMultipleInstances: Boolean = false,
    val schedulePolicy: String = "skip",
    val pythonVersion: String = "",
    val notifyOnFailure: Boolean = false,
    val notifyOnSuccess: Boolean = false,
    val notifyOnAbort: Boolean = false,
    val successExitCodes: String = "0",
    val randomDelaySeconds: Int? = null,
    val labels: List<String> = emptyList(),
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
        // 高级执行字段：始终写入（两端均以缺省/指针语义容错，冗余写入保证更新完整）。
        put("timeout", timeout.coerceIn(0, 604800))
        put("max_retries", maxRetries.coerceIn(0, 20))
        put("retry_interval", retryInterval.coerceIn(0, 86400))
        put("stop_schedule", stopSchedule.trim())
        if (dependsOn != null && dependsOn > 0) put("depends_on", dependsOn) else put("depends_on", JSONObject.NULL)
        put("task_before", taskBefore)
        put("task_after", taskAfter)
        put("allow_multiple_instances", allowMultipleInstances)
        put("schedule_policy", schedulePolicy.takeIf { it.isNotBlank() } ?: "skip")
        put("python_version", pythonVersion)
        put("notify_on_failure", notifyOnFailure)
        put("notify_on_success", notifyOnSuccess)
        put("notify_on_abort", notifyOnAbort)
        put("success_exit_codes", successExitCodes.ifBlank { "0" })
        if (randomDelaySeconds != null) put("random_delay_seconds", randomDelaySeconds.coerceIn(0, 86400)) else put("random_delay_seconds", JSONObject.NULL)
        put("labels", JSONArray(labels))
    }
}
