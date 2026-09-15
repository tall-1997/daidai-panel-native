package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject
import java.util.Base64

/**
 * PTY 终端会话模型（A2）。
 *
 * 对齐 Go 服务端 /terminal 与本地面板 LocalPanelHttpServer.serveTerminal：
 *  - POST /api/terminal/sessions  → 创建，返回 {data: snapshot}
 *  - GET  /api/terminal/sessions/:id?cursor=N  → 轮询，返回 {data: snapshot}
 *  - snapshot 字段：id / status / shell / pid / created_at / exit_code / cursor / output[]
 */
data class TerminalSessionSnapshot(
    val id: String = "",
    val status: String = "",
    val shell: String = "",
    val pid: Int = 0,
    val createdAt: String = "",
    val exitCode: Int? = null,
    val cursor: Long = 0L,
    val output: List<TerminalOutputChunk> = emptyList(),
) {
    companion object {
        fun fromJson(data: JSONObject): TerminalSessionSnapshot {
            val output = mutableListOf<TerminalOutputChunk>()
            data.optJSONArray("output")?.let { arr ->
                for (i in 0 until arr.length()) {
                    val chunk = arr.optJSONObject(i)
                    if (chunk != null) output.add(TerminalOutputChunk.fromJson(chunk))
                }
            }
            return TerminalSessionSnapshot(
                id = data.optString("id"),
                status = data.optString("status"),
                shell = data.optString("shell"),
                pid = data.optInt("pid"),
                createdAt = data.optString("created_at"),
                exitCode = if (data.isNull("exit_code") || !data.has("exit_code")) null else data.optInt("exit_code"),
                cursor = data.optLong("cursor"),
                output = output,
            )
        }
    }
}

data class TerminalOutputChunk(
    val cursor: Long = 0L,
    val data: ByteArray = byteArrayOf(),
    val encoding: String = "base64",
) {
    fun decodeToString(): String = try {
        String(Base64.getDecoder().decode(data), Charsets.UTF_8)
    } catch (_: Exception) {
        String(data, Charsets.UTF_8)
    }

    companion object {
        fun fromJson(json: JSONObject): TerminalOutputChunk {
            val raw = json.optString("data")
            val encoding = json.optString("encoding", "base64")
            return when {
                raw.isBlank() -> TerminalOutputChunk(json.optLong("cursor"), byteArrayOf(), encoding)
                encoding.equals("base64", ignoreCase = true) ->
                    TerminalOutputChunk(json.optLong("cursor"), Base64.getDecoder().decode(raw), "base64")
                else ->
                    TerminalOutputChunk(json.optLong("cursor"), raw.toByteArray(Charsets.UTF_8), encoding)
            }
        }
    }

    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (javaClass != other?.javaClass) return false
        other as TerminalOutputChunk
        return cursor == other.cursor &&
            data.contentEquals(other.data) &&
            encoding == other.encoding
    }

    override fun hashCode(): Int = arrayOf(cursor, data.contentHashCode(), encoding).contentHashCode()
}
