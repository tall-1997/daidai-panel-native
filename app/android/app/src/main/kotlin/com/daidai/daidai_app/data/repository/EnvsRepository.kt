package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.model.parseEnvVars
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 环境变量数据源契约（阶段 3-2）。
 *
 * 与既有 `PanelHttpClient` 认证约定保持一致：
 *  - 远程面板使用 `Authorization: Bearer <accessToken>`
 *  - 托管本地面板使用 `X-Daidai-Local-Token: <localToken>`
 * baseUrl 应已去掉尾部斜杠。
 */
interface EnvsRepository {
    /** 拉取全部环境变量。 */
    suspend fun list(): List<EnvVar>

    /** 新建环境变量；返回新记录 ID。 */
    suspend fun create(name: String, value: String, remark: String): Long

    /** 更新指定环境变量；返回更新后的记录。 */
    suspend fun update(
        id: Long,
        name: String,
        value: String,
        remark: String,
        enabled: Boolean,
    ): EnvVar

    /** 删除指定环境变量。 */
    suspend fun delete(id: Long)

    /** 启用 / 禁用指定环境变量。 */
    suspend fun setEnabled(id: Long, enabled: Boolean): EnvVar
}

/**
 * 读写 `GET/POST /api/envs`、`PUT/DELETE /api/envs/:id` 与
 * `PUT /api/envs/:id/enable|disable` 的默认实现（自包含 OkHttp 客户端）。
 *
 * 与后端 `serveEnvs` 契约一致：
 *   GET    /api/envs                -> { data:[...], total, page, page_size }
 *   POST   /api/envs                -> 201 { data:{ id } }
 *   PUT    /api/envs/:id            -> { data: envRow }
 *   DELETE /api/envs/:id            -> { data:{ id } }
 *   PUT    /api/envs/:id/enable|disable -> { data:{ id, enabled } }
 *
 * 字段映射：请求体使用后端 `remarks` 键；`secret` 为纯 UI 掩码提示，不下发。
 */
class PanelEnvsRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : EnvsRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun list(): List<EnvVar> = withContext(Dispatchers.IO) {
        // 服务端默认 page_size=20 且上限 100；循环翻页拉全量，对齐 Dart 参考实现。
        val rows = mutableListOf<EnvVar>()
        var page = 1
        while (page <= MAX_PAGES) {
            val body = execute("GET", "$baseUrl/api/envs?page=$page&page_size=$PAGE_SIZE")
            val parsed = parseEnvVars(body)
            rows += parsed
            val total = runCatching { JSONObject(body).optInt("total") }.getOrDefault(parsed.size)
            if (parsed.isEmpty() || rows.size >= total) break
            page += 1
        }
        rows
    }

    override suspend fun create(name: String, value: String, remark: String): Long =
        withContext(Dispatchers.IO) {
            val json = JSONObject()
                .put("name", name)
                .put("value", value)
                .put("remarks", remark)
                .put("enabled", true)
            val body = execute("POST", "$baseUrl/api/envs", json.toString())
            JSONObject(body).optJSONObject("data")?.optLong("id") ?: 0L
        }

    override suspend fun update(
        id: Long,
        name: String,
        value: String,
        remark: String,
        enabled: Boolean,
    ): EnvVar = withContext(Dispatchers.IO) {
        val json = JSONObject()
            .put("name", name)
            .put("value", value)
            .put("remarks", remark)
            .put("enabled", enabled)
        val body = execute("PUT", "$baseUrl/api/envs/$id", json.toString())
        val data = JSONObject(body).optJSONObject("data")
        if (data != null) EnvVar.fromJson(data) else EnvVar(id = id)
    }

    override suspend fun delete(id: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "$baseUrl/api/envs/$id")
        Unit
    }

    override suspend fun setEnabled(id: Long, enabled: Boolean): EnvVar =
        withContext(Dispatchers.IO) {
            val action = if (enabled) "enable" else "disable"
            val body = execute("PUT", "$baseUrl/api/envs/$id/$action")
            val data = JSONObject(body).optJSONObject("data")
            data?.let { EnvVar.fromJson(it) } ?: EnvVar(id = id, enabled = enabled)
        }

    private fun execute(method: String, url: String, json: String? = null): String =
        PanelRequests.execute(method, url, json, accessToken = accessToken, localToken = localToken)

    private companion object {
        private const val PAGE_SIZE = 100
        private const val MAX_PAGES = 100
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败诊断载体。 */
class EnvsApiException(
    val statusCode: Int,
    val responseBody: String,
) : RuntimeException("Envs request failed with HTTP $statusCode")