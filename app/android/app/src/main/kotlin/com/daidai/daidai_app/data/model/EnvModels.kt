package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 环境变量（阶段 3-2）。
 *
 * 映射自 Flutter `env_list_page.dart` 的 `EnvVar` 与后端 `serveEnvs` 的字段。
 * 列表卡片与编辑表单使用同一 DTO：
 *   id / name / value / enabled / remark / createTime
 *
 * 字段命名约定：
 *  - 后端 JSON 使用 `remarks`（复数）与 `created_at`，本模型按任务契约命名为
 *    [remark] 与 [createTime]，fromJson 自动做映射。
 *  - [secret] 表示「是否加密展示」的 UI 提示：后端 env 表不持久化该字段，仅用于
 *    编辑表单中对 value 输入框做掩码处理，缺失时回退 false。
 */
data class EnvVar(
    val id: Long = 0L,
    val name: String = "",
    val value: String = "",
    val secret: Boolean = false,
    val enabled: Boolean = true,
    val remark: String = "",
    val createTime: String = "",
) {
    companion object {
        /** 从单个环境变量 JSON 对象解析，全部字段容错回退。 */
        fun fromJson(json: JSONObject): EnvVar = EnvVar(
            id = json.optLong("id"),
            name = json.optString("name"),
            value = json.optString("value"),
            secret = if (json.has("secret")) json.optBoolean("secret") else false,
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            remark = json.optString("remarks"),
            createTime = json.optString("created_at"),
        )
    }
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