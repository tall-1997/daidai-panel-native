package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.ScriptContent
import com.daidai.daidai_app.data.model.ScriptFile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 脚本模块数据源契约（阶段 3-3，Compose 原生）。
 *
 * 与既有 `PanelHttpClient` 认证约定保持一致：
 *  - 远程面板使用 `Authorization: Bearer <accessToken>`
 *  - 托管本地面板使用 `X-Daidai-Local-Token: <localToken>`
 * 二者均未提供时按未认证请求发出。baseUrl 应已去掉尾部斜杠。
 */
interface ScriptsRepository {
    /** 拉取脚本文件树（目录 + 脚本条目的嵌套列表）。 */
    suspend fun listTree(): List<ScriptFile>

    /** 查看单个脚本内容。 */
    suspend fun getContent(path: String): ScriptContent

    /** 保存脚本内容（新建亦用同一端点，content 为空即建空文件）。 */
    suspend fun saveContent(path: String, content: String, message: String = "保存")

    /** 运行指定脚本，返回 runId（用于后续随日志查询 / 停止）。 */
    suspend fun runScript(path: String): String?

    /** 停止已运行脚本。 */
    suspend fun stopScript(runId: String)

    /** 删除文件或目录。 */
    suspend fun deleteScript(path: String, isDirectory: Boolean)
}

/**
 * 读写 `/api/scripts` 的默认实现。
 *
 * 独立于 [com.daidai.daidai_app.data.remote.PanelHttpClient] 的自包含客户端：
 * 仅负责本模块的拉取 / 变更，不改动既有网络层（与 OpenApiRepository / LogsRepository
 * 同一风格，keep-alive 复用同一实例）。
 *
 * 后端契约（对齐 Flutter `script_list_page.dart`）：
 *   GET    /api/scripts/tree             -> 文件树（ScriptFile 列表）
 *   GET    /api/scripts/content?path=    -> { content, binary|is_binary }
 *   PUT    /api/scripts/content          -> body {path, content, message}
 *   POST   /api/scripts/run              -> { run_id }
 *   PUT    /api/scripts/run/{runId}/stop -> 停止
 *   DELETE /api/scripts?path=            -> 删除文件 / 目录
 */
class PanelScriptsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : ScriptsRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun listTree(): List<ScriptFile> = withContext(Dispatchers.IO) {
        val body = execute("GET", "$baseUrl/api/scripts/tree", null)
        val raw = dataOrBody(body)
        when (raw) {
            is JSONArray -> {
                val result = ArrayList<ScriptFile>(raw.length())
                for (i in 0 until raw.length()) {
                    val obj = raw.optJSONObject(i) ?: continue
                    result.add(ScriptFile.fromJson(obj))
                }
                result
            }
            is JSONObject -> {
                // 兜底：可能直接返回单对象列表或包装空。
                raw.optJSONArray("children")
                    ?.let { arr ->
                        val result = ArrayList<ScriptFile>(arr.length())
                        for (i in 0 until arr.length()) {
                            val obj = arr.optJSONObject(i) ?: continue
                            result.add(ScriptFile.fromJson(obj))
                        }
                        result
                    }
                    ?: emptyList()
            }
            else -> emptyList()
        }
    }

    override suspend fun getContent(path: String): ScriptContent = withContext(Dispatchers.IO) {
        val url = ("$baseUrl/api/scripts/content")
            .toHttpUrlOrNull()
            ?.newBuilder()
            ?.addQueryParameter("path", path)
            ?.build()
            ?.toString()
            ?: "$baseUrl/api/scripts/content?path=${encodePath(path)}"
        val body = execute("GET", url, null)
        val raw = dataOrBody(body)
        if (raw is JSONObject) {
            ScriptContent.fromData(raw, path)
        } else {
            // 后端可能直接返回内容字符串。
            ScriptContent(path = path, content = body, isBinary = false)
        }
    }

    override suspend fun saveContent(
        path: String,
        content: String,
        message: String,
    ) {
        withContext(Dispatchers.IO) {
            val payload = ScriptContent.toSaveBody(path, content, message)
            execute("PUT", "$baseUrl/api/scripts/content", payload)
        }
    }

    override suspend fun runScript(path: String): String? = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("path", path)
        val body = execute("POST", "$baseUrl/api/scripts/run", payload)
        val raw = dataOrBody(body) as? JSONObject
        raw
            ?.opt("run_id")
            ?.toString()
            ?.takeIf { it.isNotBlank() }
    }

    override suspend fun stopScript(runId: String) {
        withContext(Dispatchers.IO) {
            execute("PUT", "$baseUrl/api/scripts/run/${encodePath(runId)}/stop", null)
        }
    }

    override suspend fun deleteScript(
        path: String,
        isDirectory: Boolean,
    ) {
        withContext(Dispatchers.IO) {
            val url = ("$baseUrl/api/scripts")
                .toHttpUrlOrNull()
                ?.newBuilder()
                ?.addQueryParameter("path", path)
                ?.build()
                ?.toString()
                ?: "$baseUrl/api/scripts?path=${encodePath(path)}"
            execute("DELETE", url, null)
        }
    }

    // ------------------------------------------------------------------
    // 内部工具
    // ------------------------------------------------------------------

    /** 统一请求执行：注入认证头、校验状态码、返回响应体字符串。 */
    private fun execute(method: String, url: String, body: JSONObject?): String =
        PanelRequests.execute(method, url, body?.toString(), accessToken = accessToken, localToken = localToken)

    /** 取响应体，兼容直接列表 / 内容，也兼容 { data: ... } 包装。 */
    private fun dataOrBody(body: String): Any {
        val trimmed = body.trim()
        if (trimmed.isNotEmpty() && trimmed.startsWith("[")) {
            return try {
                JSONArray(trimmed)
            } catch (_: Exception) {
                body
            }
        }
        val json = try {
            JSONObject(trimmed)
        } catch (_: Exception) {
            return body
        }
        if (json.has("data")) {
            return json.opt("data") ?: body
        }
        return json
    }

    private fun encodePath(raw: String): String =
        raw.toHttpUrlOrNull()?.toString()
            ?: java.net.URLEncoder.encode(raw, "UTF-8")

    private companion object {
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败：保留状态码与原始响应（供 UI 展示错误详情）。 */
class ScriptsApiException(
    val statusCode: Int,
    val responseBody: String = "",
) : RuntimeException("Scripts request failed with HTTP $statusCode")
