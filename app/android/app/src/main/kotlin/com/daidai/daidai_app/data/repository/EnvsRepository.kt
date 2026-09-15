package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.model.QlEnvItem
import com.daidai.daidai_app.data.model.parseEnvVars
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 环境变量数据源契约（阶段 3-2 + F5 增强）。
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

    /** 新建环境变量并指定分组；返回新记录 ID。 */
    suspend fun createWithGroups(name: String, value: String, remark: String, groups: List<String>): Long

    /** 更新指定环境变量；返回更新后的记录。 */
    suspend fun update(
        id: Long,
        name: String,
        value: String,
        remark: String,
        enabled: Boolean,
        groups: List<String>,
    ): EnvVar

    /** 删除指定环境变量。 */
    suspend fun delete(id: Long)

    /** 启用 / 禁用指定环境变量。 */
    suspend fun setEnabled(id: Long, enabled: Boolean): EnvVar

    /** 拉取分组列表（含默认组「默认分组」，未分组 env 归入其中）。 */
    suspend fun listGroups(): List<String>

    /** 批量导入环境变量；返回导入结果（成功数/错误信息）。 */
    suspend fun import(envs: List<QlEnvItem>, mode: String): EnvImportResult

    /** 调整排序：调用 /envs/sort 将 sourceId 的 sort_order 设为 targetId（目标排序值）。 */
    suspend fun sort(sourceId: Long, targetId: Long)

    /** 置顶环境变量。 */
    suspend fun moveTop(id: Long)

    /** 更新指定环境变量的分组（全量覆盖）。 */
    suspend fun setGroups(id: Long, groups: List<String>)
}

/** 批量导入结果。 */
data class EnvImportResult(
    val imported: Int,
    val skipped: Int,
    val errors: List<String>,
)

/**
 * 基于 OkHttp 的默认实现：全部请求走 [PanelRequests.execute]，
 * 复用进程级 token 会话与 401 刷新重试。
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
        createWithGroups(name = name, value = value, remark = remark, groups = emptyList())

    override suspend fun createWithGroups(
        name: String,
        value: String,
        remark: String,
        groups: List<String>,
    ): Long = withContext(Dispatchers.IO) {
        val json = JSONObject()
            .put("name", name)
            .put("value", value)
            .put("remarks", remark)
            .put("enabled", true)
            .put("groups", JSONArray(groups))
        val body = execute("POST", "$baseUrl/api/envs", json.toString())
        JSONObject(body).optJSONObject("data")?.optLong("id") ?: 0L
    }

    override suspend fun update(
        id: Long,
        name: String,
        value: String,
        remark: String,
        enabled: Boolean,
        groups: List<String>,
    ): EnvVar = withContext(Dispatchers.IO) {
        val json = JSONObject()
            .put("name", name)
            .put("value", value)
            .put("remarks", remark)
            .put("enabled", enabled)
            .put("groups", JSONArray(groups))
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

    override suspend fun listGroups(): List<String> = withContext(Dispatchers.IO) {
        val body = execute("GET", "$baseUrl/api/envs/groups")
        val raw = runCatching { JSONArray(body) }.getOrNull()
        if (raw != null) return@withContext normalizeGroupNames(raw)
        val data = runCatching { JSONObject(body).opt("data") }.getOrNull()
        when (data) {
            is JSONArray -> normalizeGroupNames(data)
            is JSONObject -> data.optJSONArray("groups")?.let { normalizeGroupNames(it) }.orEmpty()
            else -> emptyList()
        }
    }

    override suspend fun import(envs: List<QlEnvItem>, mode: String): EnvImportResult =
        withContext(Dispatchers.IO) {
            val array = JSONArray()
            for (item in envs) {
                array.put(
                    JSONObject()
                        .put("name", item.name)
                        .put("value", item.value)
                        .put("remarks", item.remark)
                        .put("enabled", true)
                )
            }
            val json = JSONObject()
                .put("envs", array)
                .put("mode", mode)
            val body = execute("POST", "$baseUrl/api/envs/import", json.toString())
            val parsed = runCatching { JSONObject(body) }.getOrNull()
            val imported = parsed?.optInt("imported")
                ?: parsed?.optJSONObject("data")?.optInt("imported")
                ?: envs.size
            val errors = mutableListOf<String>()
            parsed?.optJSONArray("errors")?.let { arr ->
                for (i in 0 until arr.length()) errors += arr.optString(i)
            }
            parsed?.optJSONObject("data")?.optJSONArray("errors")?.let { arr ->
                if (errors.isEmpty()) for (i in 0 until arr.length()) errors += arr.optString(i)
            }
            EnvImportResult(
                imported = imported,
                skipped = (envs.size - imported).coerceAtLeast(0),
                errors = errors,
            )
        }

    override suspend fun sort(sourceId: Long, targetId: Long) = withContext(Dispatchers.IO) {
        val json = JSONObject()
            .put("source_id", sourceId)
            .put("target_id", targetId)
        execute("PUT", "$baseUrl/api/envs/sort", json.toString())
        Unit
    }

    override suspend fun moveTop(id: Long) = withContext(Dispatchers.IO) {
        execute("PUT", "$baseUrl/api/envs/$id/move-top")
        Unit
    }

    override suspend fun setGroups(id: Long, groups: List<String>) = withContext(Dispatchers.IO) {
        val json = JSONObject()
            .put("id", id)
            .put("groups", JSONArray(groups))
        execute("PUT", "$baseUrl/api/envs/batch/group", json.toString())
        Unit
    }

    private fun normalizeGroupNames(raw: JSONArray): List<String> {
        val result = linkedSetOf<String>()
        for (i in 0 until raw.length()) {
            val value = raw.optString(i).trim()
            if (value.isNotEmpty()) result += value
        }
        return result.toList()
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
