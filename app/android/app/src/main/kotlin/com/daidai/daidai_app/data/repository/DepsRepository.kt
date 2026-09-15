package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.DepInstallRequest
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.model.DepManager
import com.daidai.daidai_app.data.model.DepStatus
import com.daidai.daidai_app.data.model.DepMirrors
import com.daidai.daidai_app.data.model.parseDepItems
import com.daidai.daidai_app.data.remote.PanelRequests
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import org.json.JSONArray
import org.json.JSONObject
import java.io.OutputStream

/**
 * 阶段 3-4 依赖包的数据源（自包含 OkHttp 实现，不改动既有网络层）。
 *
 * 与 `PanelHttpClient` 认证约定保持一致：远程面板使用
 * `Authorization: Bearer <accessToken>`，托管本地面板使用
 * `X-Daidai-Local-Token: <localToken>`。每请求重新解析连接（本地模式启动后
 * URL/token 可能变化），与 [SubscriptionsRepository] 约定一致。
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
    private val context: Context,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) {
    private val configRepo = AppServices.configRepository(context)

    /** 单次请求的连接描述：baseUrl + 认证头参数。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    private suspend fun resolveConnection(): Connection {
        val config = configRepo.getConfig()
        return when (config.mode) {
            PanelConnectionMode.REMOTE -> {
                val baseUrl = config.serverUrl.trim().trimEnd('/')
                if (baseUrl.isBlank()) {
                    throw IllegalStateException("尚未配置远程服务地址，请先在“配置服务器”页面填写")
                }
                Connection(baseUrl, config.accessToken, null)
            }
            PanelConnectionMode.MANAGED_LOCAL -> {
                val status = PanelCoreController.ensureStarted()
                val baseUrl = status.baseUrl?.trim()?.trimEnd('/')
                if (baseUrl.isNullOrBlank()) {
                    throw IllegalStateException("本地服务未能启动：${status.message ?: "未知原因"}")
                }
                Connection(baseUrl, null, status.localToken)
            }
        }
    }

    suspend fun status(id: Long): DepStatus = DepStatus.parse(execute("GET", "/api/deps/$id/status"))

    internal fun logStream(id: Long): Flow<CapabilityEvent> = flow {
        val conn = resolveConnection()
        emitAll(
            CapabilityRequests(httpClient, conn.accessToken, conn.localToken)
                .events(conn.baseUrl + "/api/deps/$id/log-stream"),
        )
    }

    suspend fun cancel(id: Long) = execute("PUT", "/api/deps/$id/cancel")

    suspend fun batchReinstall(ids: Set<Long>) = batch("batch-reinstall", ids)
    suspend fun batchDelete(ids: Set<Long>) = batch("batch-delete", ids)

    private suspend fun batch(action: String, ids: Set<Long>): String {
        require(ids.isNotEmpty() && ids.all { it > 0 }) { "请选择有效依赖" }
        return execute("POST", "/api/deps/$action", JSONObject().put("ids", JSONArray(ids.toList())).toString())
    }

    suspend fun mirrors(): DepMirrors = DepMirrors.parse(execute("GET", "/api/deps/mirrors"))
    suspend fun saveMirrors(mirrors: DepMirrors) = execute("PUT", "/api/deps/mirrors", mirrors.payload())

    suspend fun export(type: String, pythonVersion: String, output: OutputStream) {
        require(type in setOf("python", "nodejs", "linux"))
        val query = buildString {
            append("?type=").append(type)
            if (type == "python" && pythonVersion.isNotBlank()) {
                append("&python_version=").append(java.net.URLEncoder.encode(pythonVersion.trim(), "UTF-8"))
            }
        }
        val conn = resolveConnection()
        CapabilityRequests(httpClient, conn.accessToken, conn.localToken).download(conn.baseUrl + "/api/deps/export$query", output)
    }

    suspend fun list(): List<DepItem> = withContext(Dispatchers.IO) {
        val byId = LinkedHashMap<Long, DepItem>()
        for (backendType in listOf("python", "nodejs", "linux")) {
            val body = execute("GET", "/api/deps?type=$backendType")
            for (item in parseDepItems(body)) {
                byId[item.id] = item
            }
        }
        byId.values.toList()
    }

    suspend fun install(request: DepInstallRequest) = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("type", request.manager.backendType)
            .put("names", JSONArray().put(request.packageName.trim()))
        if (request.manager == DepManager.Pip && request.version.isNotBlank()) {
            payload.put("python_version", request.version.trim())
        }
        execute("POST", "/api/deps", payload.toString())
    }

    suspend fun uninstall(id: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "/api/deps/$id")
    }

    suspend fun reinstall(id: Long) = withContext(Dispatchers.IO) {
        execute("PUT", "/api/deps/$id/reinstall")
    }

    private suspend fun execute(method: String, path: String, json: String? = null): String {
        val conn = resolveConnection()
        return CapabilityRequests(httpClient, conn.accessToken, conn.localToken).text(method, conn.baseUrl + path, json)
    }

    private companion object {
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败：保留状态码、原始响应与可读错误描述。 */
class DepsApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException("Deps request failed with HTTP $statusCode")
