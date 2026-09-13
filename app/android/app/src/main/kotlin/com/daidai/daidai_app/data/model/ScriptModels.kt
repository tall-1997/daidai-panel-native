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
