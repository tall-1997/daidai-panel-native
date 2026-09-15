package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.LogCleanupConfig
import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.model.LogPage
import com.daidai.daidai_app.data.model.LogStatus
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
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

/** 日志列表筛选条件，与后端 GET /api/logs 支持的查询参数对齐。 */
data class LogFilter(
    val keyword: String = "",
    val taskId: String = "",
    val status: LogStatus? = null,
    val channel: String = "",
)

/**
 * 运行日志数据源（自包含 Http 实现，不改动既有网络层）。
 *
 * 远程面板使用 `Authorization: Bearer <accessToken>`，托管本地面板使用
 * `X-Daidai-Local-Token: <localToken>`，与 `PanelHttpClient` 认证约定保持一致。
 * baseUrl 应已去掉尾部斜杠。契约与后端一致：
 *   GET    /api/logs?page=&page_size=&keyword=&task_id=&status=&channel=
 *           响应 {data:[...], total:N, page:N, page_size:N}
 *   DELETE /api/logs/:id
 *   SSE    /api/logs/stream?channel=...
 *   DELETE /api/logs/cleanup?channel=...&older_than=...
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

    /**
     * 使用 SSE 实时流获取日志
     * @param channel 日志频道类型
     * @param signal 中止信号，用于取消流
     * @param startTime 开始时间过滤器（可选）
     * @return Flow 发射 LogEntry 日志条目
     */
    fun streamLogs(
        channel: String,
        signal: AbortSignal? = null,
        startTime: String? = null,
    ): Flow<LogEntry> = callbackFlow {
        try {
            val params = mutableListOf<String>()
            params.add("channel=$channel")
            startTime?.takeIf { it.isNotBlank() }?.let { params.add("start_time=$it") }

            val url = "$baseUrl/api/logs/stream?${params.joinToString("&")}"
            val request = Request.Builder()
                .url(url)
                .get()
                .build()

            val call = httpClient.newCall(request)
            call.enqueue(object : okhttp3.Callback {
                override fun onFailure(call: okhttp3.Call, e: java.io.IOException) {
                    close(e)
                }

                override fun onResponse(call: okhttp3.Call, response: okhttp3.Response) {
                    try {
                        if (!response.isSuccessful) {
                            close(java.io.IOException("HTTP ${response.code}"))
                            return
                        }
                        val body = response.body?.string() ?: return
                        // 解析 SSE 格式数据
                        body.lines().forEach { line ->
                            if (line.startsWith("data:")) {
                                val payload = line.removePrefix("data:").trim()
                                try {
                                    val json = JSONObject(payload)
                                    val entry = LogEntry.fromJson(json)
                                    trySend(entry)
                                } catch (_: Exception) {
                                    // 忽略解析错误
                                }
                            }
                        }
                        close()
                    } catch (e: Exception) {
                        close(e)
                    }
                }
            })

            awaitClose {
                call.cancel()
            }
        } catch (e: Exception) {
            close(e)
        }
    }

    /**
     * 触发日志清理
     * @param channel 日志频道类型
     * @param olderThan 大于指定时间的日志（ISO 格式）将被删除（可选）
     * @return 删除的日志数量
     */
    suspend fun cleanLogs(channel: String, olderThan: String? = null): Int = withContext(Dispatchers.IO) {
        val params = mutableListOf<String>()
        params.add("channel=$channel")
        olderThan?.takeIf { it.isNotBlank() }?.let { params.add("older_than=$it") }

        val url = "$baseUrl/api/logs/cleanup?${params.joinToString("&")}"
        try {
            val body = execute("DELETE", url)
            val json = JSONObject(body)
            json.optInt("deleted", 0)
        } catch (e: Exception) {
            0
        }
    }

    /**
     * 获取当前频道的清理配置
     */
    suspend fun getCleanupConfig(channel: String): LogCleanupConfig = withContext(Dispatchers.IO) {
        val url = "$baseUrl/api/logs/cleanup/config?channel=$channel"
        val body = execute("GET", url)
        val json = JSONObject(body)
        LogCleanupConfig(
            retentionDays = json.optInt("retention_days", 30),
            autoClean = json.optBoolean("auto_clean", false),
            channel = json.optString("channel", channel)
        )
    }

    /**
     * 更新清理配置
     */
    suspend fun updateCleanupConfig(config: LogCleanupConfig) = withContext(Dispatchers.IO) {
        val body = JSONObject().apply {
            put("channel", config.channel)
            put("retention_days", config.retentionDays)
            put("auto_clean", config.autoClean)
        }.toString()

        execute("POST", "$baseUrl/api/logs/cleanup/config", body)
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
        filter.channel.takeIf { it.isNotBlank() }?.let { builder.addQueryParameter("channel", it) }
        return builder.build().toString()
    }

    private fun defaultUrl(): HttpUrl = HttpUrl.Builder()
        .scheme("https")
        .host("localhost")
        .addPathSegment("api")
        .addPathSegment("logs")
        .build()

    private fun execute(method: String, url: String, json: String? = null): String =
        PanelRequests.execute(method, url, json, accessToken = accessToken, localToken = localToken)

    /** 中止信号类，用于取消 SSE 流 */
    class AbortSignal {
        private var _aborted = false
        val isAborted: Boolean get() = _aborted

        fun abort() {
            _aborted = true
        }

        fun check() {
            if (_aborted) throw kotlinx.coroutines.CancellationException("Stream aborted")
        }
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient.newBuilder()
            .readTimeout(5, TimeUnit.MINUTES)
            .build()
    }
}

/** HTTP 失败：保留状态码、原始响应与可读错误描述。 */
class LogsApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException("Logs request failed with HTTP $statusCode")
