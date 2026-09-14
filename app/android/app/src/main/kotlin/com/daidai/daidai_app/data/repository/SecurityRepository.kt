package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.model.parseAuditLogs
import com.daidai.daidai_app.data.model.parseLoginLogs
import com.daidai.daidai_app.data.model.parseSecurityOverview
import com.daidai.daidai_app.data.model.parseSessions
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.TimeUnit

/**
 * 阶段 4-1 安全模块只读数据仓库（结合阶段 2-1 DashboardRepository 的连接解析
 * 与阶段 2-4 OpenApiRepository 的认证约定）。
 *
 * 拉取四个只读端点并以 DTO 返回：
 *   GET /api/security/login-logs   -> 登录日志（分页数组）
 *   GET /api/security/sessions     -> 在线会话（数组）
 *   GET /api/security/audit-logs   -> 审计日志（分页数组）
 *   GET /api/security/login-stats  -> 登录统计概览 + 派生风险提示
 *
 * 连接方式经 [AppServices]（只读）解析：
 *   - REMOTE：baseUrl = serverUrl，Authorization: Bearer <access_token>
 *   - MANAGED_LOCAL：baseUrl/token 由 [PanelCoreController] 提供，
 *     x-daidai-local-token 头（与既有 PanelHttpClient 一致）。
 * 不修改网络层/存储层，网络层用 OkHttp 直连，keep-alive 复用同一实例。
 */
class SecurityRepository(
    context: Context,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) {
    private val appContext = context.applicationContext
    private val configRepo = AppServices.configRepository(context)

    /** endpoint 连接描述：每次请求前解析一次（本地模式启动后 URL/token 可能变化）。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    /** 拉取登录日志列表。 */
    suspend fun getLoginLogs(): List<LoginLog> = withContext(Dispatchers.IO) {
        val raw = get(resolveConnection(), "/api/security/login-logs")
        parseLoginLogs(raw)
    }

    /** 拉取在线会话列表。 */
    suspend fun getSessions(): List<Session> = withContext(Dispatchers.IO) {
        val raw = get(resolveConnection(), "/api/security/sessions")
        parseSessions(raw)
    }

    /** 拉取审计日志列表。 */
    suspend fun getAuditLogs(): List<AuditLog> = withContext(Dispatchers.IO) {
        val raw = get(resolveConnection(), "/api/security/audit-logs")
        parseAuditLogs(raw)
    }

    /** 拉取登录统计概览（默认近 7 天）。 */
    suspend fun getSecurityOverview(): SecurityOverview = withContext(Dispatchers.IO) {
        val raw = get(resolveConnection(), "/api/security/login-stats?days=7")
        parseSecurityOverview(raw)
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

    private fun get(conn: Connection, path: String): String =
        PanelRequests.execute(
            "GET",
            conn.baseUrl + path,
            accessToken = conn.accessToken,
            localToken = conn.localToken,
        )

    private companion object {
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败诊断载体。 */
class SecurityApiException(
    val code: Int,
    val rawBody: String = "",
) : Exception("安全数据请求失败（HTTP $code${if (rawBody.isBlank()) "" else ": $rawBody"}）")
