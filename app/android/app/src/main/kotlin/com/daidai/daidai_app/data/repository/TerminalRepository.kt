package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.TerminalSessionSnapshot
import com.daidai.daidai_app.data.remote.PanelRequests
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import org.json.JSONObject
import java.util.Base64

/**
 * PTY 终端会话仓库（A2）。
 *
 * 接口与 Go / 本地面板的 /terminal 端点保持一致：
 *  - POST /api/terminal/sessions {rows, columns}
 *  - GET  /api/terminal/sessions/:id?cursor=N
 *  - POST /api/terminal/sessions/:id/input {data, encoding}
 *  - PUT  /api/terminal/sessions/:id/resize {rows, columns}
 *  - PUT  /api/terminal/sessions/:id/stop
 *  - DELETE /api/terminal/sessions/:id
 * 均需 operator 角色与 JWT 认证；baseUrl 由 AppServices.configRepository 提供。
 */
interface TerminalRepository {
    suspend fun createSession(rows: Int, columns: Int): TerminalSessionSnapshot
    suspend fun getSession(id: String, cursor: Long = 0L): TerminalSessionSnapshot
    suspend fun sendInput(id: String, text: String)
    suspend fun resize(id: String, rows: Int, columns: Int)
    suspend fun stop(id: String): TerminalSessionSnapshot
    suspend fun close(id: String)
}

/**
 * OkHttp 直连 Go / 本地面板 `/api/terminal/...` 的默认实现。
 * 复用 [PanelRequests]，与 ScriptsRepository / LogsRepository 同一风格。
 */
class PanelTerminalRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    @Suppress("unused") private val httpClient: OkHttpClient = PanelRequests.sharedClient,
) : TerminalRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun createSession(rows: Int, columns: Int): TerminalSessionSnapshot =
        withContext(Dispatchers.IO) {
            val payload = JSONObject()
                .put("rows", rows.coerceIn(2, 200))
                .put("columns", columns.coerceIn(10, 400))
            val body = execute("POST", terminalPath("/sessions"), payload.toString())
            parseSnapshot(body)
        }

    override suspend fun getSession(id: String, cursor: Long): TerminalSessionSnapshot =
        withContext(Dispatchers.IO) {
            val body = execute("GET", terminalPath("/sessions/$id?cursor=$cursor"))
            parseSnapshot(body)
        }

    override suspend fun sendInput(id: String, text: String) {
        withContext(Dispatchers.IO) {
            val bytes = text.toByteArray(Charsets.UTF_8)
            val b64 = Base64.getEncoder().encodeToString(bytes)
            val payload = JSONObject()
                .put("data", b64)
                .put("encoding", "base64")
            execute("POST", terminalPath("/sessions/$id/input"), payload.toString())
        }
    }

    override suspend fun resize(id: String, rows: Int, columns: Int) {
        withContext(Dispatchers.IO) {
            val payload = JSONObject()
                .put("rows", rows.coerceIn(2, 200))
                .put("columns", columns.coerceIn(10, 400))
            execute("PUT", terminalPath("/sessions/$id/resize"), payload.toString())
        }
    }

    override suspend fun stop(id: String): TerminalSessionSnapshot = withContext(Dispatchers.IO) {
        val body = execute("PUT", terminalPath("/sessions/$id/stop"))
        parseSnapshot(body)
    }

    override suspend fun close(id: String) {
        withContext(Dispatchers.IO) {
            execute("DELETE", terminalPath("/sessions/$id"))
        }
    }

    // ------------------------------------------------------------------

    private fun terminalPath(tail: String): String = "$baseUrl/api/terminal$tail"

    private fun execute(method: String, url: String, json: String? = null): String =
        PanelRequests.execute(method, url, json, accessToken = accessToken, localToken = localToken)

    private fun parseSnapshot(body: String): TerminalSessionSnapshot {
        val trimmed = body.trim()
        if (trimmed.isEmpty()) return TerminalSessionSnapshot()
        val json = try {
            JSONObject(trimmed)
        } catch (_: Exception) {
            return TerminalSessionSnapshot()
        }
        val data = json.optJSONObject("data") ?: json
        return TerminalSessionSnapshot.fromJson(data)
    }
}
