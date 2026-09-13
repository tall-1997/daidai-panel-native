package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.DepInstallRequest
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.model.DepManager
import com.daidai.daidai_app.data.model.parseDepItems
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

/**
 * 阶段 3-4 依赖包的数据源（自包含 OkHttp 实现，不改动既有网络层）。
 *
 * 与 `PanelHttpClient` 认证约定保持一致：远程面板使用
 * `Authorization: Bearer <accessToken>`，托管本地面板使用
 * `X-Daidai-Local-Token: <localToken>`。baseUrl 应已去掉尾部斜杠。
 *
 * 契约与后端 Go handler 对齐：
 *   GET  /api/deps?type=python|nodejs|linux   -> {data:[...], total:N}
 *   POST /api/deps                            -> {type,names:[...],python_version?}
 *   DELETE /api/deps/:id                      -> {message}
 *   PUT  /api/deps/:id/reinstall               -> {message}
 *
 * 说明：后端列表按 type 过滤，本仓库合并拉取 python/nodejs/linux 三档并按 id 去重，
 * 以提供完整全量列表。
 */
class DepsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) {
    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    /** 拉取全量依赖列表（合并 python/nodejs/linux，按 id 去重）。 */
    suspend fun list(): List<DepItem> = withContext(Dispatchers.IO) {
        val backendTypes = listOf("python", "nodejs", "linux")
        val byId = LinkedHashMap<Long, DepItem>()
        for (backendType in backendTypes) {
            val url = buildListUrl(backendType)
            val body = execute("GET", url)
            for (item in parseDepItems(body)) {
                byId[item.id] = item
            }
        }
        byId.values.toList()
    }

    /**
     * 安装依赖。映射到后端 POST /api/deps：
     *  - manager pip -> type "python"，npm -> type "nodejs"
     *  - packageName -> names[0]
     *  - version     -> python_version（仅 Python 回传）
     */
    suspend fun install(request: DepInstallRequest) = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("type", request.manager.backendType)
            .put("names", JSONArray().put(request.packageName.trim()))
        if (request.manager == DepManager.Pip && request.version.isNotBlank()) {
            payload.put("python_version", request.version.trim())
        }
        execute("POST", "$baseUrl/api/deps", payload.toString())
    }

    /** 卸载依赖。后端 DELETE /api/deps/:id（异步执行卸载任务）。 */
    suspend fun uninstall(id: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "$baseUrl/api/deps/$id")
    }

    /** 重装依赖。后端 PUT /api/deps/:id/reinstall。 */
    suspend fun reinstall(id: Long) = withContext(Dispatchers.IO) {
        execute("PUT", "$baseUrl/api/deps/$id/reinstall")
    }

    private fun buildListUrl(backendType: String): String {
        val builder = ("$baseUrl/api/deps").toHttpUrlOrNull()?.newBuilder()
            ?: defaultUrl().newBuilder()
        builder.addQueryParameter("type", backendType)
        return builder.build().toString()
    }

    private fun defaultUrl(): HttpUrl = HttpUrl.Builder()
        .scheme("https")
        .host("localhost")
        .addPathSegment("api")
        .addPathSegment("deps")
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
                throw DepsApiException(response.code, body, message)
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
class DepsApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException("Deps request failed with HTTP $statusCode")
