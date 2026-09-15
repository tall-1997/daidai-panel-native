package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 环境变量（阶段 3-2 + F5 增强）。
 *
 * 映射自 Flutter `env_list_page.dart` 的 `EnvVar` 与后端 `serveEnvs` 的字段。
 * 列表卡片与编辑表单使用同一 DTO：
 *   id / name / value / enabled / remark / groups / createTime
 *
 * 字段命名约定：
 *  - 后端 JSON 使用 `remarks`（复数）与 `created_at`，本模型按任务契约命名为
 *    [remark] 与 [createTime]，fromJson 自动做映射。
 *  - [secret] 表示「是否加密展示」的 UI 提示：后端 env 表不持久化该字段，仅用于
 *    编辑表单中对 value 输入框做掩码处理，缺失时回退 false。
 *  - [groups] 为环境变量所属分组（后端持久化在 `groups_json`，响应同时带
 *    `group` 逗号串与 `groups` 数组），缺省空列表。
 */
data class EnvVar(
    val id: Long = 0L,
    val name: String = "",
    val value: String = "",
    val secret: Boolean = false,
    val enabled: Boolean = true,
    val remark: String = "",
    val groups: List<String> = emptyList(),
    val sortOrder: Long = 0L,
    val createTime: String = "",
) {
    /** 主分组：取第一个分组，无分组时返回空串（默认组「全部」用空串表达）。 */
    fun primaryGroup(): String = groups.firstOrNull()?.trim().orEmpty()

    companion object {
        /** 从单个环境变量 JSON 对象解析，全部字段容错回退。 */
        fun fromJson(json: JSONObject): EnvVar = EnvVar(
            id = json.optLong("id"),
            name = json.optString("name"),
            value = json.optString("value"),
            secret = if (json.has("secret")) json.optBoolean("secret") else false,
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            remark = json.optString("remarks"),
            groups = parseGroups(json),
            sortOrder = json.optLong("sort_order"),
            createTime = json.optString("created_at"),
        )

        /** 解析分组字段：优先 `groups` 数组，回退 `group` 逗号分隔串。 */
        fun parseGroups(json: JSONObject): List<String> {
            json.optJSONArray("groups")?.let { array ->
                val result = ArrayList<String>(array.length())
                for (i in 0 until array.length()) {
                    val value = array.optString(i).trim()
                    if (value.isNotEmpty()) result += value
                }
                return result
            }
            return json.optString("group")
                .split(',')
                .map(String::trim)
                .filter(String::isNotEmpty)
        }
    }
}

/**
 * 青龙 ql 格式导出文本生成。
 *
 * 每行 `名称=值 备注`：名称按 ql 规则校验（字母/数字/下划线，首字符非数字），
 * 非法名称的行跳过并计入 [skippedNames]。值中的换行转义为 `\n`（与后端
 * `exportEnvFiles` 行为一致）；备注含空白时用双引号包裹。
 */
data class QlExportResult(
    val text: String,
    val skippedNames: List<String>,
)

fun toQlExportText(envs: List<EnvVar>): QlExportResult {
    val lines = ArrayList<String>(envs.size)
    val skipped = ArrayList<String>()
    for (env in envs) {
        val name = env.name.trim()
        if (!isQlEnvName(name)) {
            skipped += name
            continue
        }
        val value = env.value.replace("\n", "\\n")
        val remark = env.remark.trim()
        val remarkPart = when {
            remark.isEmpty() -> ""
            remark.contains(' ') || remark.contains('"') -> " \"${remark.replace("\"", "\\\"")}\""
            else -> " $remark"
        }
        lines += "$name=$value$remarkPart"
    }
    return QlExportResult(text = lines.joinToString("\n"), skippedNames = skipped)
}

/**
 * 青龙 ql 环境变量格式解析（F5 批量导入）。
 *
 * 兼容三行式：
 *  1. `名称 值 备注`                —— 空白分隔，值不允许含空白
 *  2. `名称="值" 备注` / `name="值" 备注`
 *  3. `名称=值 备注`                —— 等号分隔，与导出格式互通
 *
 * 名称支持英文/中文等任意非空白字符；解析失败的行放入 [errors]（带行号），
 * 不影响其余行导入。
 */
data class QlParseResult(
    val items: List<QlEnvItem>,
    val errors: List<String>,
)

data class QlEnvItem(
    val name: String,
    val value: String,
    val remark: String,
)

fun parseQlEnvText(text: String): QlParseResult {
    val items = ArrayList<QlEnvItem>()
    val errors = ArrayList<String>()
    val lines = text.split('\n', '\r')
    for ((index, rawLine) in lines.withIndex()) {
        val line = rawLine.trim()
        if (line.isEmpty() || line.startsWith("#")) continue
        val parsed = parseQlEnvLine(line)
        if (parsed != null) {
            items += parsed
        } else {
            errors += "第 ${index + 1} 行无法解析：$line"
        }
    }
    return QlParseResult(items, errors)
}

private val QL_NAME_REGEX = Regex("[A-Za-z_][A-Za-z0-9_]*")

/** 是否为合法的 ql 导出名称（字母/下划线开头，后续字母/数字/下划线）。 */
fun isQlEnvName(name: String): Boolean = name.isNotEmpty() && QL_NAME_REGEX.matches(name)

/**
 * 解析单行 ql 文本。策略：
 *  1) 先尝试等号形式 `name=value 备注`（同时兼容 `name="value" 备注`）。
 *  2) 再尝试空白分隔形式 `name value 备注`（名称或值带双引号时剥离引号）。
 * 备注可省略；值不能为空；名称不能为空。
 */
internal fun parseQlEnvLine(line: String): QlEnvItem? {
    val equals = findUnquotedEquals(line)
    if (equals >= 0) {
        val name = line.substring(0, equals).trim().trim('"').trim()
        val rest = line.substring(equals + 1).trim()
        if (name.isEmpty()) return null
        val (value, remark) = splitValueAndRemark(rest)
        if (value.isEmpty()) return null
        return QlEnvItem(name = name, value = value, remark = remark)
    }

    val tokens = tokenize(line)
    if (tokens.isEmpty()) return null
    val name = tokens[0].trim('"').trim()
    if (name.isEmpty() || tokens.size < 2) return null
    val value = tokens[1].trim('"').trim()
    if (value.isEmpty()) return null
    val remark = if (tokens.size > 2) tokens.drop(2).joinToString(" ").trim() else ""
    return QlEnvItem(name = name, value = value, remark = remark)
}

/** 查找未加引号的 `=` 位置；找不到返回 -1。 */
private fun findUnquotedEquals(line: String): Int {
    var inQuote = false
    for (i in line.indices) {
        val ch = line[i]
        when (ch) {
            '"' -> inQuote = !inQuote
            '=' -> if (!inQuote) return i
        }
    }
    return -1
}

/** 将 `值 备注` 剩余部分拆成值 + 备注：值优先带引号整体，否则取第一个空白词。 */
private fun splitValueAndRemark(rest: String): Pair<String, String> {
    if (rest.isEmpty()) return "" to ""
    if (rest.startsWith('"')) {
        val close = rest.indexOf('"', 1)
        if (close > 0) {
            val value = rest.substring(1, close)
            val remark = rest.substring(close + 1).trim()
            return value to remark
        }
    }
    val parts = rest.split(Regex("\\s+"), limit = 2)
    val value = parts[0].trim()
    val remark = if (parts.size > 1) parts[1].trim() else ""
    return value to remark
}

/** 按空白切分，但保留双引号包裹的整体（如 `name="a b" c` → [name, a b, c]）。 */
private fun tokenize(line: String): List<String> {
    val tokens = ArrayList<String>()
    val current = StringBuilder()
    var inQuote = false
    for (ch in line) {
        when {
            ch == '"' -> inQuote = !inQuote
            ch.isWhitespace() && !inQuote -> {
                if (current.isNotEmpty()) {
                    tokens += current.toString()
                    current.clear()
                }
            }
            else -> current.append(ch)
        }
    }
    if (current.isNotEmpty()) tokens += current.toString()
    return tokens
}

/** 解析环境变量列表响应中的 `data` 数组；空或非法时返回空列表。 */
fun parseEnvVars(rawBody: String): List<EnvVar> {
    val raw = runCatching { JSONArray(rawBody) }.getOrNull()
    if (raw != null) return jsonArrayToEnvVars(raw)
    val json = runCatching { JSONObject(rawBody) }.getOrNull() ?: return emptyList()
    val data = json.opt("data")
    return when (data) {
        is JSONArray -> jsonArrayToEnvVars(data)
        is JSONObject -> data.optJSONArray("data")
            ?.let { jsonArrayToEnvVars(it) }
            ?: emptyList()
        else -> emptyList()
    }
}

private fun jsonArrayToEnvVars(array: JSONArray): List<EnvVar> {
    val result = ArrayList<EnvVar>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(EnvVar.fromJson(element))
    }
    return result
}
