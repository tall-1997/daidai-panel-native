package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 系统管理（C8/C9）相关数据模型及通用解析工具。
 * 解析器全部做容错：
 *   - Go 端以 {"data":...} 包装；本地端以顶层返回为主；
 *   - value 字段本地可能为 {value:...} 对象，Go 端为纯字符串；
 *   - panel-log 本地为任务日志列表（对象），Go 端为字符串数组；
 *   - restore-progress 两端字段不完全一致，属性缺失时采用默认值。
 */

/** 服务器返回的错误抽取（统一用于 Repository 异常包装）。 */
internal fun panelErrorDetail(code: Int, body: String, fallback: String = "系统操作失败"): String {
    val json = runCatching { JSONObject(body) }.getOrNull()
    val detail = json?.optString("error")?.takeIf { it.isNotBlank() }
        ?: json?.optJSONObject("data")?.optString("error")?.takeIf { it.isNotBlank() }
        ?: json?.optString("message")?.takeIf { it.isNotBlank() }
    return "$fallback（HTTP $code${detail?.let { ": $it" } ?: ""}）"
}

/** 面板更新状态（/api/system/update-status），用于 About 页诚实用于展示 Android 不可自更新语义。 */
data class PanelUpdateStatus(
    val status: String = "idle",
    val phase: String = "",
    val message: String = "",
    val error: String = "",
    val deploymentType: String = "",
    val updateManager: String = "",
) {
    companion object {
        fun fromJson(body: String): PanelUpdateStatus {
            val root = JSONObject(body)
            val data = root.optJSONObject("data") ?: root
            return PanelUpdateStatus(
                status = data.optString("status"),
                phase = data.optString("phase"),
                message = data.optString("message"),
                error = data.optString("error"),
                deploymentType = data.optString("deployment_type")
                    .ifBlank { data.optString("deployment_type") },
                updateManager = data.optString("update_manager"),
            )
        }
    }

    /** Android 自身 APK 免疫自更新时 phase=immutable_apk 或 manager=platform_installer。 */
    val isPlatformInstallerManaged: Boolean
        get() = phase.equals("immutable_apk", true) ||
                deploymentType.equals("android_apk", true) ||
                updateManager.equals("platform_installer", true)
}

/** 面板机器码（/api/system/machine-code）。 */
data class MachineCode(val code: String) {
    companion object {
        fun fromJson(body: String): MachineCode {
            val root = JSONObject(body)
            val data = root.optJSONObject("data") ?: root
            return MachineCode(data.optString("machine_code"))
        }
    }
}

/** 面板设置（/api/system/panel-settings），本地返回 {key:{value,default_value}} 需要展平。 */
data class PanelSettingsSnapshot(
    val panelTitle: String = "",
    val panelIcon: String = "",
    val editorBackgroundColor: String = "",
    val logBackgroundColor: String = "",
    val logBackgroundImage: String = "",
    val panelRuntimeMode: String = "",
    val panelServiceManager: String = "",
    val panelServiceName: String = "",
) {
    companion object {
        private fun stringFromAny(v: Any?): String = when (v) {
            is String -> v
            is JSONObject -> v.optString("value")
            else -> ""
        }
        fun fromJson(body: String): PanelSettingsSnapshot {
            val root = JSONObject(body)
            val data = root.optJSONObject("data") ?: JSONObject()
            fun opt(key: String) = stringFromAny(data.opt(key))
            return PanelSettingsSnapshot(
                panelTitle = opt("panel_title"),
                panelIcon = opt("panel_icon"),
                editorBackgroundColor = opt("editor_background_color"),
                logBackgroundColor = opt("log_background_color"),
                logBackgroundImage = opt("log_background_image"),
                panelRuntimeMode = opt("panel_runtime_mode"),
                panelServiceManager = opt("panel_service_manager"),
                panelServiceName = opt("panel_service_name"),
            )
        }
    }
}

/** config.sh 内容（/api/system/config-script）。Go 与本地皆为顶层 content/path。 */
data class ConfigScriptContent(val content: String = "", val path: String = "config.sh") {
    companion object {
        fun fromJson(body: String): ConfigScriptContent {
            val root = JSONObject(body)
            // 本地/Go 皆顶层返回；容错 data 包装
            val data = root.optJSONObject("data") ?: root
            return ConfigScriptContent(
                content = data.optString("content"),
                path = data.optString("path", "config.sh"),
            )
        }
    }
}

/**
 * panel-log 列表（/api/system/panel-log?lines=...）。
 * Go 端返回 data.logs: [String]；本地端返回 task-log 列表（JSONArray）。
 */
data class PanelLogPage(val lines: List<String>, val total: Int = 0, val level: String = "") {
    companion object {
        fun fromJson(body: String): PanelLogPage {
            val root = JSONObject(body)
            val data = root.optJSONObject("data") ?: JSONObject()
            val total = data.optInt("total", 0)
            val level = data.optString("level", "")
            val lines = buildList {
                when {
                    data.has("logs") -> {
                        val arr = data.optJSONArray("logs") ?: JSONArray()
                        for (i in 0 until arr.length()) {
                            arr.optString(i)?.let { add(it) }
                        }
                    }
                    data.has("data") -> {
                        // 本地误路由到任务日志时数组处理
                        val arr = data.optJSONArray("data") ?: JSONArray()
                        for (i in 0 until arr.length()) {
                            val obj = arr.optJSONObject(i) ?: continue
                            // 取常用字段拼成一行
                            val msg = obj.optString("content")
                                .ifBlank { obj.optString("message") }
                                .ifBlank { obj.optString("log") }
                            if (msg.isNotBlank()) {
                                val ts = obj.optString("created_at").ifBlank { obj.optString("started_at") }
                                add(if (ts.isNotBlank()) "[$ts] $msg" else msg)
                            } else {
                                add(obj.toString())
                            }
                        }
                    }
                    else -> {
                        val arr = data.optJSONArray("logs") ?: JSONArray()
                        for (i in 0 until arr.length()) {
                            arr.optString(i)?.let { add(it) }
                        }
                    }
                }
            }
            return PanelLogPage(lines, total, level)
        }
    }
}

/** 恢复进度（/api/system/restore/progress）。 */
data class RestoreProgress(
    val active: Boolean = false,
    val status: String = "idle",
    val filename: String = "",
    val source: String = "",
    val stage: String = "",
    val message: String = "",
    val percent: Int = 0,
    val error: String = "",
    val startedAt: String = "",
    val updatedAt: String = "",
    val activeTaskIds: List<Long> = emptyList(),
) {
    val running: Boolean get() = status.equals("running", true) || active
    val finished: Boolean get() = status.equals("completed", true) || status.equals("failed", true)
    companion object {
        fun fromJson(body: String): RestoreProgress {
            val root = JSONObject(body)
            val data = root.optJSONObject("data") ?: root
            val taskIds = buildList {
                val arr = data.optJSONArray("active_task_ids") ?: JSONArray()
                for (i in 0 until arr.length()) {
                    arr.optLong(i)?.let { add(it) }
                }
            }
            return RestoreProgress(
                active = data.optBoolean("active"),
                status = data.optString("status", if (data.optBoolean("active")) "running" else "idle"),
                filename = data.optString("filename"),
                source = data.optString("source"),
                stage = data.optString("stage"),
                message = data.optString("message"),
                percent = data.optInt("percent", 0),
                error = data.optString("error"),
                startedAt = data.optString("started_at"),
                updatedAt = data.optString("updated_at"),
                activeTaskIds = taskIds,
            )
        }
    }
}
