package com.daidai.daidai_app.data.model

import org.json.JSONObject

/**
 * 阶段 2-1 仪表盘数据模型（DTO）。
 *
 * 字段名与后端保持一致：Go server（panel/server/handler）与 Kotlin fallback
 * （LocalPanelHttpServer / LocalPanelStore）两个端点都返回 `{"data": {...}}` 包装，
 * 取其中的 data 对象解析。字段校验均做容错（optXxx + 默认值），后端任一字段缺失
 * 都不应导致页面崩溃。
 */

/** /api/system/info 的资源快照。字节类字段保持原始 uint64 语义，由 UI 层换算可读单位。 */
data class SystemInfo(
    val hostname: String = "-",
    val machineCode: String = "",
    val cpuUsage: Double = 0.0,
    val memoryTotal: Long = 0,
    val memoryUsed: Long = 0,
    val memoryFree: Long = 0,
    val memoryUsage: Double = 0.0,
    val diskTotal: Long = 0,
    val diskUsed: Long = 0,
    val diskFree: Long = 0,
    val diskUsage: Double = 0.0,
    val uptime: String = "-",
    val goRoutines: Int = 0,
    val goVersion: String = "",
    val os: String = "-",
    val arch: String = "",
    val numCpu: Int = 0,
    val netRxBytes: Long = 0,
    val netTxBytes: Long = 0,
) {
    companion object {
        fun fromData(json: JSONObject): SystemInfo = SystemInfo(
            hostname = json.optString("hostname").takeIf { it.isNotEmpty() } ?: json.optString("host_name"),
            machineCode = json.optString("machine_code"),
            cpuUsage = json.optDouble("cpu_usage", 0.0),
            memoryTotal = json.optLong("memory_total", 0),
            memoryUsed = json.optLong("memory_used", 0),
            memoryFree = json.optLong("memory_free", 0),
            memoryUsage = json.optDouble("memory_usage", 0.0),
            diskTotal = json.optLong("disk_total", 0),
            diskUsed = json.optLong("disk_used", 0),
            diskFree = json.optLong("disk_free", 0),
            diskUsage = json.optDouble("disk_usage", 0.0),
            uptime = json.optString("uptime").takeIf { it.isNotEmpty() } ?: "-",
            goRoutines = json.optInt("goroutines", 0),
            goVersion = json.optString("go_version"),
            os = json.optString("os").takeIf { it.isNotEmpty() } ?: "-",
            arch = json.optString("arch"),
            numCpu = json.optInt("num_cpu", 0),
            netRxBytes = json.optLong("net_rx_bytes", 0),
            netTxBytes = json.optLong("net_tx_bytes", 0),
        )
    }
}

/** 7 日趋势中的单日执行统计。 */
data class DailyStat(
    val date: String = "",
    val success: Long = 0,
    val failed: Long = 0,
    val aborted: Long = 0,
)

/** /api/system/dashboard 的任务统计与 7 日趋势。 */
data class DashboardStats(
    val taskCount: Long = 0,
    val enabledTasks: Long = 0,
    val runningTasks: Long = 0,
    val todayLogs: Long = 0,
    val todaySuccess: Long = 0,
    val todayFailed: Long = 0,
    val todayAborted: Long = 0,
    val envCount: Long = 0,
    val subCount: Long = 0,
    val prevTaskCount: Long = 0,
    val dailyStats: List<DailyStat> = emptyList(),
) {
    /** 禁用任务数：总数 − 启用数。
     * 后端若已直接给出 disabled 字段则以其为准。 */
    val disabledTasks: Long
        get() = if (enabledTasks in 0..taskCount) taskCount - enabledTasks else 0

    companion object {
        fun fromData(json: JSONObject): DashboardStats {
            val rawDaily: List<DailyStat> = runCatching {
                val array = json.optJSONArray("daily_stats")
                if (array == null) {
                    emptyList()
                } else {
                    buildList {
                        for (i in 0 until array.length()) {
                            val item = array.optJSONObject(i) ?: continue
                            add(
                                DailyStat(
                                    date = item.optString("date"),
                                    success = item.optLong("success", 0),
                                    failed = item.optLong("failed", 0),
                                    aborted = item.optLong("aborted", 0),
                                )
                            )
                        }
                    }
                }
            }.getOrElse { emptyList() }

            return DashboardStats(
                taskCount = json.optLong("task_count", 0),
                enabledTasks = json.optLong("enabled_tasks", 0),
                runningTasks = json.optLong("running_tasks", 0),
                todayLogs = json.optLong("today_logs", 0),
                todaySuccess = json.optLong("success_logs", 0),
                todayFailed = json.optLong("failed_logs", 0),
                todayAborted = json.optLong("aborted_logs", 0),
                envCount = json.optLong("env_count", 0),
                subCount = json.optLong("sub_count", 0),
                prevTaskCount = json.optLong("prev_task_count", 0),
                dailyStats = rawDaily,
            )
        }
    }
}
