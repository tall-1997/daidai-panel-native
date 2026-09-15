package com.daidai.daidai_app.data.model

/** 系统备份记录（由服务器返回的既有结构）。 */
data class BackupRecord(
    val name: String,
    val size: Long,
    val created: String,
    val info: String? = null,
)

/** 定时备份计划。 */
data class BackupSchedule(
    val enabled: Boolean = false,
    val frequency: String = "daily", // "daily" | "weekly" | "monthly"
    val time: String = "03:00",       // "HH:mm"
)
