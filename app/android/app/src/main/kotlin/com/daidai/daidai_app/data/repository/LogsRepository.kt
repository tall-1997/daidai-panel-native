package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.model.LogPage
import com.daidai.daidai_app.data.model.LogStatus
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/** 日志列表筛选条件，与后端 GET /api/logs 支持的查询参数对齐。 */
data class LogFilter(
    val keyword: String = "",
    val taskId: String = "",
    val status: LogStatus? = null,
)

/**
 * 运行日志数据源（自包含 Http 实现，不改动既有网络层）。
 *
 * 远程面板使用 `Authorization: Bearer <accessToken>`，托管本地面板使用
 * `X-Daidai-Local-Token: <localToken>`，与 `PanelHttpClient` 认证约定保持一致。
 * baseUrl 应已去掉尾部斜杠。契约与后端一致：
 *   GET    /api/logs?page=&page_size=&keyword=&task_id=&status=
 *           响应 {data:[...], total:N, page:N, page_size:N}
 *   DELETE /api/logs/:id
 */
class LogsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) {
    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    suspend fun getLogs(
        filter: LogFilter = LogFilter(),
        page: Int = 1,
        pageSize: Int = 20,
    ): LogPage = withContext(Dispatchers.IO) {
        val url = buildListUrl(filter, page, pageSize)
        val body = execute("GET", url)
        val json = JSONObject(body)
        val total = json.optInt("total", -1)
        val items = json.opt("data").let { raw ->
            when (raw) {
                is JSONArray -> (0 until raw.length()).map { LogEntry.fromJson(raw.getJSONObject(it)) }
                is JSONObject -> raw.optJSONArray("data")
                    ?.let { arr -> (0 until arr.length()).map { LogEntry.fromJson(arr.getJSONObject(it)) } }
                    ?: emptyList()
                else -> emptyList()
            }
        }
        LogPage(
            items = items,
            total = if (total >= 0) total else items.size,
            page = json.optInt("page", page),
            pageSize = json.optInt("page_size", pageSize),
        )
    }

    /** 删除单条日志。后端 DELETE /api/logs/:id。 */
    suspend fun deleteLog(id: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "$baseUrl/api/logs/$id")
    }

    private fun buildListUrl(filter: LogFilter, page: Int, pageSize: Int): String {
        val builder = ("$baseUrl/api/logs").toHttpUrlOrNull()?.newBuilder()
            ?: defaultUrl().newBuilder()
        builder
            .addQueryParameter("page", page.toString())
            .addQueryParameter("page_size", pageSize.toString())
        filter.keyword.takeIf { it.isNotBlank() }?.let { builder.addQueryParameter("keyword", it) }
        filter.taskId.takeIf { it.isNotBlank() }?.let { builder.addQueryParameter("task_id", it) }
        filter.status?.let { builder.addQueryParameter("status", it.code.toString()) }
        return builder.build().toString()
    }

    private fun defaultUrl(): HttpUrl = HttpUrl.Builder()
        .scheme("https")
        .host("localhost")
        .addPathSegment("api")
        .addPathSegment("logs")
        .build()

    private fun execute(method: String, url: String, json: String? = null): String {
        val builder = Request.Builder().url(url)
        accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        localToken?.takeIf { it.isNotBlank() }?.let { builder.header("X-Daidai-Local-Token", it) }
        if (json != null) {
            builder.method(method, json.toRequestBody(JSON_MEDIA_TYPE))
        } else {
            builder.method(method, null)
        }
        httpClient.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = try {
                    val parsed = JSONObject(body)
                    parsed.optString("error").takeIf(String::isNotEmpty)
                        ?: parsed.optString("message")
                } catch (_: Exception) {
                    null
                }
                throw LogsApiException(response.code, body, message)
            }
            return body
        }
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun defaultHttpClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }
}

/** HTTP 失败：保留状态码、原始响应与可读错误描述。 */
class LogsApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException("Logs request failed with HTTP $statusCode")
