package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.DashboardStats
import com.daidai.daidai_app.data.model.SystemInfo
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject

/**
 * 阶段 2-1 仪表盘数据仓库。
 *
 * 拉取两个只读端点并以 DTO 返回：
 *   GET /api/system/info       -> SystemInfo（CPU/内存/磁盘/主机）
 *   GET /api/system/dashboard  -> DashboardStats（任务统计 + 7 日趋势）
 *
 * 不修改网络层/存储层，仅通过 [AppServices]（只读）读当前配置来决定连接方式：
 *   - REMOTE：baseUrl = serverUrl，Authorization: Bearer <access_token>
 *   - MANAGED_LOCAL：baseUrl/token 由 [PanelCoreController] 提供，
 *     x-daidai-local-token 头（与现有 PanelHttpClient 一致）
 *
 * 网络层用 OkHttp 直连，keep-alive 复用同一实例。
 */
class DashboardRepository(
    context: Context,
    private val httpClient: OkHttpClient = OkHttpClient(),
) {
    private val appContext = context.applicationContext
    private val configRepo = AppServices.configRepository(context)

    /** endpoint 连接描述：每次请求前解析一次（本地模式启动后 URL/token 可能变化）。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    /** 拉取系统信息 + 仪表盘统计。 */
    suspend fun loadDashboard(): DashboardDataBundle = withContext(Dispatchers.IO) {
        val conn = resolveConnection()
        val infoRaw = get(conn, "/api/system/info")
        val dashRaw = get(conn, "/api/system/dashboard")
        DashboardDataBundle(
            systemInfo = SystemInfo.fromData(withData(infoRaw)),
            stats = DashboardStats.fromData(withData(dashRaw)),
        )
    }

    private suspend fun resolveConnection(): Connection {
        val config = configRepo.getConfig() // AppServices 幂等初始化，只读安全
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

    /** 取响应 JSON 的 data 对象；响应非 2xx 或 data 缺失时抛异常。 */
    private fun withData(raw: String): JSONObject {
        val json = JSONObject(raw)
        val data = json.optJSONObject("data")
            ?: throw IllegalStateException("响应缺少 data 字段")
        return data
    }

    private fun get(conn: Connection, path: String): String {
        val builder = Request.Builder().url(conn.baseUrl + path)
        conn.accessToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("Authorization", "Bearer $it") }
        conn.localToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("x-daidai-local-token", it) }
        builder.get()
        httpClient.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw DashboardRepositoryException(response.code, body)
            }
            return body
        }
    }
}

/** 阶段 2-1 仓库的聚合结果。 */
data class DashboardDataBundle(
    val systemInfo: SystemInfo,
    val stats: DashboardStats,
)

/** 网络/解析失败的诊断载体。 */
class DashboardRepositoryException(
    val code: Int,
    val rawBody: String = "",
) : Exception("仪表盘请求失败（HTTP $code${if (rawBody.isBlank()) "" else ": $rawBody"}）")
