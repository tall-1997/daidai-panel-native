package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 脚本文件树 / 内容模型（Compose 原生阶段 3-3）。
 *
 * 迁移自 Flutter `app/lib/features/scripts/`。映射后端脚本 API 的核心字段：
 *   GET    /api/scripts/tree   -> ScriptFile 列表
 *   GET    /api/scripts/content?path= -> ScriptContent
 *
 * 所有 fromJson 均对字段做容错解析：键名同时兼容后端驼峰/下划线两种风格，
 * 缺失或不合法时回退到安全默认值（与既有 OpenApiModels / DashboardModels 约定一致）。
 */
data class ScriptFile(
    val path: String = "",
    val name: String = "",
    val type: String = "",        // "file" / "directory"；也兼容 "dir"、"folder"
    val size: Long = 0L,
    val updateTime: String = "",
    val children: List<ScriptFile> = emptyList(),
) {
    /** 目录判断：优先看 type，其次兼容 isLeaf 反转 / is_directory 布尔字段。 */
    val isDirectory: Boolean
        get() = when {
            type.equals("directory", ignoreCase = true) ||
                type.equals("dir", ignoreCase = true) ||
                type.equals("folder", ignoreCase = true) -> true
            type.equals("file", ignoreCase = true) -> false
            else -> false // 未知 type 统一按文件处理，避免误展开空目录
        }

    companion object {
        /**
         * 从单个 JSON 节点解析。同时兼容两套键名：
         *   title/name -> name；key/path -> path；type/is_directory/isLeaf -> 目录判定；
         *   size -> size；updateTime/update_time/mtime -> updateTime。
         * children 递归解析子节点。
         */
        fun fromJson(json: JSONObject): ScriptFile {
            val rawType = json.optString("type")
            var resolvedType = rawType
            if (resolvedType.isBlank()) {
                resolvedType = when {
                    json.optBoolean("is_directory", false) -> "directory"
                    json.has("isLeaf") && !json.optBoolean("isLeaf", true) -> "directory"
                    else -> "file"
                }
            }
            return ScriptFile(
                path = json.optString("key").takeIf(String::isNotBlank)
                    ?: json.optString("path"),
                name = json.optString("title").takeIf(String::isNotBlank)
                    ?: json.optString("name"),
                type = resolvedType,
                size = json.optLong("size"),
                updateTime = firstNonBlank(
                    json.optString("updateTime"),
                    json.optString("update_time"),
                    json.optString("mtime"),
                    json.optString("modified"),
                ),
                children = json.optJSONArray("children")
                    ?.let { arr -> parseChildren(arr) }
                    ?: emptyList(),
            )
        }

        private fun parseChildren(array: JSONArray): List<ScriptFile> {
            val result = ArrayList<ScriptFile>(array.length())
            for (i in 0 until array.length()) {
                val obj = array.optJSONObject(i) ?: continue
                result.add(ScriptFile.fromJson(obj))
            }
            return result
        }

        private fun firstNonBlank(vararg values: String): String = values.firstOrNull { it.isNotBlank() } ?: ""
    }
}

/**
 * 单个脚本文件的内容（查看 / 保存共用）。
 *
 * GET  /api/scripts/content?path= 返回 content 文本与二进制标记；
 * PUT  /api/scripts/content 保存时用 path + content + message（message 用于版本记录）。
 */
data class ScriptContent(
    val path: String = "",
    val content: String = "",
    val isBinary: Boolean = false,
) {
    companion object {
        /** 容错解析：优先 content 字段文本；兼容下划线二进制标记；缺失回退空串。 */
        fun fromData(data: JSONObject, requestedPath: String = ""): ScriptContent {
            val isBinary = data.optBoolean("binary", false) ||
                data.optBoolean("is_binary", false)
            val rawContent = data.opt("content")
            return ScriptContent(
                path = requestedPath,
                content = rawContent?.toString() ?: "",
                isBinary = isBinary,
            )
        }

        /** 保存请求体（自包含构造，供 repository 复用）。 */
        fun toSaveBody(path: String, content: String, message: String): JSONObject =
            JSONObject()
                .put("path", path)
                .put("content", content)
                .put("message", message)
    }
}

/**
 * 脚本版本记录（C2 版本查看 / 回滚）。
 *
 * 对应 Go model.ScriptVersion.ToDict / 本地 LocalPanelStore.listScriptVersions：
 *   GET    /api/scripts/versions?path=     -> {data: [ScriptVersion]}（不含 content）
 *   GET    /api/scripts/versions/:id       -> {data: {..., content}}
 *   PUT    /api/scripts/versions/:id/rollback -> {message, version}
 *   DELETE /api/scripts/versions?path=     -> {message, cleared_count}
 */
data class ScriptVersion(
    val id: Long = 0,
    val scriptPath: String = "",
    val version: Int = 0,
    val message: String = "",
    val createdAt: String = "",
    val contentLength: Int = 0,
    /** 仅详情接口返回；列表条目为 null。 */
    val content: String? = null,
) {
    companion object {
        fun fromJson(json: JSONObject): ScriptVersion {
            val rawContent = if (json.has("content") && !json.isNull("content")) {
                json.optString("content")
            } else {
                null
            }
            return ScriptVersion(
                id = json.optLong("id"),
                scriptPath = json.optString("script_path").takeIf { it.isNotBlank() }
                    ?: json.optString("scriptPath"),
                version = json.optInt("version"),
                message = json.optString("message"),
                createdAt = json.optString("created_at").takeIf { it.isNotBlank() }
                    ?: json.optString("createdAt"),
                contentLength = json.optInt("content_length", -1)
                    .takeIf { it >= 0 }
                    ?: (rawContent?.length ?: 0),
                content = rawContent,
            )
        }
    }
}

/**
 * 运行日志一段增量结果（GET /api/scripts/run/:run_id/logs?cursor=N）。
 *
 * Go 端返回扁平对象，本地面板返回 {data: {...}} 包装——repository 已抹平差异。
 * cursor 语义：本次返回后日志总行数（下次增量请求传该值）。
 */
data class ScriptRunLogPage(
    val logs: List<String> = emptyList(),
    val cursor: Long = 0,
    val totalLines: Int = 0,
    val done: Boolean = false,
    val status: String = "",
    val exitCode: Int? = null,
    val error: String? = null,
) {
    val isFinished: Boolean
        get() = done || status == "exited" || status == "failed" || status == "stopped" || status == "completed"

    companion object {
        fun fromData(data: JSONObject): ScriptRunLogPage {
            val logs = ArrayList<String>()
            data.optJSONArray("logs")?.let { arr ->
                for (i in 0 until arr.length()) logs += arr.optString(i)
            }
            return ScriptRunLogPage(
                logs = logs,
                cursor = data.optLong("cursor"),
                totalLines = data.optInt("log_count", logs.size),
                done = data.optBoolean("done"),
                status = data.optString("status"),
                exitCode = if (data.has("exit_code") && !data.isNull("exit_code")) {
                    data.optInt("exit_code")
                } else {
                    null
                },
                error = data.optString("error").takeIf { it.isNotBlank() },
            )
        }
    }
}

/**
 * 客户端脚本运行历史条目（C2）。
 *
 * Go / 本地面板均无「列出全部 run」端点（Go 存内存 map、本地存 SQLite 但不提供 list），
 * 因此 run_id 由本设备发起运行时记录到本地（ScriptRunHistoryStore），
 * 状态 / 日志通过 GET /api/scripts/run/:id/logs 实时拉取；
 * 「删除记录」= DELETE /api/scripts/run/:id（服务端清日志与进程）+ 移除本地条目。
 */
data class ScriptRunRecord(
    val runId: String = "",
    val path: String = "",
    val startedAt: String = "",
) {
    fun toJson(): JSONObject = JSONObject()
        .put("run_id", runId)
        .put("path", path)
        .put("started_at", startedAt)

    companion object {
        fun fromJson(json: JSONObject): ScriptRunRecord = ScriptRunRecord(
            runId = json.optString("run_id"),
            path = json.optString("path"),
            startedAt = json.optString("started_at"),
        )
    }
}
