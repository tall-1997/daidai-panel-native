package com.daidai.daidai_app.data.model

import org.json.JSONObject

data class DepStatus(val id: Long, val status: String, val log: String) {
    val active: Boolean get() = status in setOf("queued", "installing", "removing")
    companion object {
        fun parse(raw: String): DepStatus {
            val root = JSONObject(raw)
            val json = root.optJSONObject("data") ?: root
            return DepStatus(json.optLong("id"), json.optString("status"), json.optString("log").takeLast(128 * 1024))
        }
    }
}

data class DepMirrors(
    val pip: String, val npm: String, val linux: String,
    val linuxSupported: Boolean, val linuxLabel: String, val message: String,
) {
    fun payload(): String {
        fun validate(value: String) {
            require(value.isBlank() || value.startsWith("https://") || value.startsWith("http://")) {
                "镜像源须使用 HTTP(S) 地址"
            }
        }
        validate(pip); validate(npm)
        val result = JSONObject().put("pip_mirror", pip.trim()).put("npm_mirror", npm.trim())
        if (linuxSupported) { validate(linux); result.put("linux_mirror", linux.trim()) }
        return result.toString()
    }
    companion object {
        fun parse(raw: String): DepMirrors {
            val root = JSONObject(raw)
            val j = root.optJSONObject("data") ?: root
            return DepMirrors(j.optString("pip_mirror"), j.optString("npm_mirror"), j.optString("linux_mirror"),
                j.optBoolean("linux_mirror_supported"), j.optString("linux_mirror_label", "Linux"),
                j.optString("linux_mirror_message"))
        }
    }
}

data class SubscriptionLogEntry(val id: Long, val content: String, val createdAt: String, val status: String, val operationId: String)
data class SubscriptionLogPage(val entries: List<SubscriptionLogEntry>, val total: Int, val page: Int, val paginated: Boolean) {
    val hasNext: Boolean get() = paginated && page * 20 < total
    companion object {
        fun parse(raw: String, page: Int): SubscriptionLogPage {
            val root = JSONObject(raw)
            val array = root.optJSONArray("data") ?: error("日志响应缺少 data 数组")
            val paginated = root.has("total")
            val start = if (paginated) 0 else (page - 1) * 20
            val entries = (start until minOf(start + 20, array.length())).map { index ->
                val j = array.getJSONObject(index)
                SubscriptionLogEntry(j.optLong("id"), j.optString("content", j.optString("message")).takeLast(32768),
                    j.optString("created_at"), j.optString("level", j.optString("status")), j.optString("operation_id"))
            }
            return SubscriptionLogPage(entries, root.optInt("total", array.length()), page, true)
        }
    }
}
