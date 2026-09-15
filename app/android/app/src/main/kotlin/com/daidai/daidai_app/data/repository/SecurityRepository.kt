package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.IpWhitelistEntry
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityLoadResult
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.model.SessionPolicy
import com.daidai.daidai_app.data.model.TwoFactorStatus
import com.daidai.daidai_app.data.model.TwoFactorSetupResult
import com.daidai.daidai_app.data.model.TwoFactorVerifyResult
import com.daidai.daidai_app.data.model.LoginLogFilter
import com.daidai.daidai_app.data.model.parseTwoFactorSetupResult
import com.daidai.daidai_app.data.model.parseTwoFactorVerifyResult
import com.daidai.daidai_app.data.model.parseAuditLogs
import com.daidai.daidai_app.data.model.parseConfigValue
import com.daidai.daidai_app.data.model.parseIpWhitelist
import com.daidai.daidai_app.data.model.parseLoginLogs
import com.daidai.daidai_app.data.model.parseSecurityOverview
import com.daidai.daidai_app.data.model.parseSessions
import com.daidai.daidai_app.data.model.parseTwoFactorStatus
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import kotlinx.coroutines.CancellationException
import okhttp3.OkHttpClient
import org.json.JSONObject

/**
 * 阶段 4-1 安全模块数据源契约。
 *
 * 所有方法承诺 **不向调用方抛出 HTTP 层异常**：非 2xx / 网络错误 / 解析失败
 * 一律包装为 [SecurityLoadResult.error]（带面向用户的可读消息），数据侧保持
 * 安全默认值。这保证 UI 层（SecurityScreen / 个人中心相关）渲染错误态而非
 * 主线程崩溃（历史缺陷：`PanelApiException: Panel request failed with HTTP 400`
 * 直接冒泡导致 FATAL）。
 *
 * 写操作（撤销会话 / 白名单增删 / 会话策略保存）同样不抛 HTTP 异常，成功与否
 * 通过 [SecurityLoadResult.isSuccess] 表达。
 */
interface SecurityDataSource {
    /** 拉取登录日志列表。 */
    suspend fun getLoginLogs(): SecurityLoadResult<List<LoginLog>>

    /** 拉取在线会话列表。 */
    suspend fun getSessions(): SecurityLoadResult<List<Session>>

    /** 拉取审计日志列表。 */
    suspend fun getAuditLogs(): SecurityLoadResult<List<AuditLog>>

    /** 拉取登录统计概览（默认近 7 天）。 */
    suspend fun getSecurityOverview(): SecurityLoadResult<SecurityOverview>

    /** 拉取 2FA/TOTP 状态（GET /api/security/2fa/status）。 */
    suspend fun getTwoFactorStatus(): SecurityLoadResult<TwoFactorStatus>

    /** 拉取 IP 白名单（GET /api/security/ip-whitelist）。 */
    suspend fun getIpWhitelist(): SecurityLoadResult<List<IpWhitelistEntry>>

    /** 拉取网页端 / APP 端最大会话数（GET /api/configs/max_{web,app}_sessions）。 */
    suspend fun getSessionPolicy(): SecurityLoadResult<SessionPolicy>

    /** 撤销单个会话（DELETE /api/security/sessions/{id}）。 */
    suspend fun revokeSession(id: Long): SecurityLoadResult<Unit>

    /** 撤销当前用户的其他会话（DELETE /api/security/sessions/others）。 */
    suspend fun revokeOtherSessions(): SecurityLoadResult<Unit>

    /** 新增 IP 白名单（POST /api/security/ip-whitelist）。 */
    suspend fun addIpWhitelist(ip: String, remarks: String): SecurityLoadResult<IpWhitelistEntry?>

    /** 删除 IP 白名单条目（DELETE /api/security/ip-whitelist/{id}）。 */
    suspend fun removeIpWhitelist(id: Long): SecurityLoadResult<Unit>

    /** 保存网页端 / APP 端最大会话数（POST /api/configs）。 */
    suspend fun setSessionPolicy(maxWebSessions: Int, maxAppSessions: Int): SecurityLoadResult<Unit>

    /** 启用 2FA/TOTP（POST /api/security/2fa）。 */
    suspend fun enableTwoFactor(): SecurityLoadResult<TwoFactorSetup>

    /** 禁用 2FA/TOTP（POST /api/security/2fa/disable）。 */
    suspend fun disableTwoFactor(code: String): SecurityLoadResult<Unit>

    /** 验证 2FA/TOTP（POST /api/security/2fa/verify）。 */
    suspend fun verifyTwoFactor(code: String): SecurityLoadResult<Unit>

    /** 强制下线会话（POST /api/security/sessions/{id}/force-logout）。 */
    suspend fun forceLogoutSession(sessionId: Long): SecurityLoadResult<Unit>
}

/**
 * 阶段 4-1 安全模块只读/写数据仓库（结合阶段 2-1 DashboardRepository 的连接解析
 * 与阶段 2-4 OpenApiRepository 的认证约定）。
 *
 * 拉取安全相关端点并以 DTO 返回：
 *   GET /api/security/login-logs   -> 登录日志（分页数组）
 *   GET /api/security/sessions     -> 在线会话（数组）
 *   GET /api/security/audit-logs   -> 审计日志（分页数组）
 *   GET /api/security/login-stats  -> 登录统计概览 + 派生风险提示
 *   GET /api/security/2fa/status   -> 2FA 状态
 *   GET /api/security/ip-whitelist -> IP 白名单
 *   GET /api/configs/:key          -> 会话策略配置
 * 写端点：
 *   DELETE /api/security/sessions/{id} / others
 *   POST /api/security/ip-whitelist，DELETE /api/security/ip-whitelist/{id}
 *   POST /api/configs
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
) : SecurityDataSource {
    private val appContext = context.applicationContext
    private val configRepo = AppServices.configRepository(context)

    /** endpoint 连接描述：每次请求前解析一次（本地模式启动后 URL/token 可能变化）。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    /** 拉取登录日志列表。 */
    override suspend fun getLoginLogs(filter: LoginLogFilter = LoginLogFilter()): SecurityLoadResult<List<LoginLog>> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/login-logs",
            parse = { parseLoginLogs(it) },
            fallback = emptyList(),
        )
    }

    /** 拉取在线会话列表。 */
    override suspend fun getSessions(): SecurityLoadResult<List<Session>> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/sessions",
            parse = { parseSessions(it) },
            fallback = emptyList(),
        )
    }

    /** 拉取审计日志列表。 */
    override suspend fun getAuditLogs(): SecurityLoadResult<List<AuditLog>> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/audit-logs",
            parse = { parseAuditLogs(it) },
            fallback = emptyList(),
        )
    }

    /** 拉取登录统计概览（默认近 7 天）。 */
    override suspend fun getSecurityOverview(): SecurityLoadResult<SecurityOverview> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/login-stats?days=7",
            parse = { parseSecurityOverview(it) },
            fallback = SecurityOverview(),
        )
    }

    /** 拉取 2FA/TOTP 状态。 */
    override suspend fun getTwoFactorStatus(): SecurityLoadResult<TwoFactorStatus> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/2fa/status",
            parse = { parseTwoFactorStatus(it) },
            fallback = TwoFactorStatus(),
        )
    }

    /** 启用 2FA/TOTP。 */
    override suspend fun enableTwoFactor(): SecurityLoadResult<TwoFactorSetup> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/2fa",
            parse = { parseTwoFactorSetup(it) },
            fallback = TwoFactorSetup(),
        )
    }

    /** 禁用 2FA/TOTP。 */
    override suspend fun disableTwoFactor(code: String): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/2fa/disable", method = "POST", body = """{"code":"$code"}""")
    }

    /** 验证 2FA/TOTP。 */
    override suspend fun verifyTwoFactor(code: String): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/2fa/verify", method = "POST", body = """{"code":"$code"}""")
    }

    /** 强制下线会话。 */
    override suspend fun forceLogoutSession(sessionId: Long): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/sessions/$sessionId/force-logout", method = "POST")
    }
    /** 拉取 IP 白名单。 */
    override suspend fun getIpWhitelist(): SecurityLoadResult<List<IpWhitelistEntry>> = withContext(Dispatchers.IO) {
        safeGet(
            path = "/api/security/ip-whitelist",
            parse = { parseIpWhitelist(it) },
            fallback = emptyList(),
        )
    }

    /** 拉取会话策略（网页端 / APP 端最大会话数）。 */
    override suspend fun getSessionPolicy(): SecurityLoadResult<SessionPolicy> = withContext(Dispatchers.IO) {
        val conn = resolveConnectionOrNull() ?: return@withContext SecurityLoadResult(
            SessionPolicy(),
            "安全后端未装配",
        )
        val web = safeGetWithConnection(
            conn = conn,
            path = "/api/configs/max_web_sessions",
            parse = { parseConfigValue(it)?.toIntOrNull() },
            fallback = null,
        )
        val app = safeGetWithConnection(
            conn = conn,
            path = "/api/configs/max_app_sessions",
            parse = { parseConfigValue(it)?.toIntOrNull() },
            fallback = null,
        )
        val error = listOfNotNull(web.error, app.error).firstOrNull()
        SecurityLoadResult(
            data = SessionPolicy(
                maxWebSessions = web.data,
                maxAppSessions = app.data,
            ),
            error = error,
        )
    }

    /** 撤销单个会话。 */
    override suspend fun revokeSession(id: Long): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/sessions/$id", method = "DELETE")
    }

    /** 撤销当前用户的其他会话。 */
    override suspend fun revokeOtherSessions(): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/sessions/others", method = "DELETE")
    }

    /** 新增 IP 白名单。 */
    override suspend fun addIpWhitelist(ip: String, remarks: String): SecurityLoadResult<IpWhitelistEntry?> =
        withContext(Dispatchers.IO) {
            val trimmed = ip.trim()
            if (trimmed.isEmpty()) {
                return@withContext SecurityLoadResult(null, "IP 地址不能为空")
            }
            try {
                val conn = resolveConnectionOrNull() ?: return@withContext SecurityLoadResult(null, "安全后端未装配")
                val json = JSONObject().put("ip", trimmed).put("remarks", remarks.trim()).toString()
                val raw = request(conn, "POST", "/api/security/ip-whitelist", json)
                val entry = parseIpWhitelist(raw).firstOrNull()
                    ?: runCatching { IpWhitelistEntry.fromJson(JSONObject(raw).optJSONObject("data")) }.getOrNull()
                SecurityLoadResult(entry)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult(null, friendlyMessage(error))
            }
        }

    /** 删除 IP 白名单条目。 */
    override suspend fun removeIpWhitelist(id: Long): SecurityLoadResult<Unit> = withContext(Dispatchers.IO) {
        safeWrite(path = "/api/security/ip-whitelist/$id", method = "DELETE")
    }

    /** 保存网页端 / APP 端最大会话数。 */
    override suspend fun setSessionPolicy(maxWebSessions: Int, maxAppSessions: Int): SecurityLoadResult<Unit> =
        withContext(Dispatchers.IO) {
            val web = coerceSessionLimit(maxWebSessions)
            val app = coerceSessionLimit(maxAppSessions)
            val webError = safeWriteConfig("max_web_sessions", web.toString()).error
            val appError = safeWriteConfig("max_app_sessions", app.toString()).error
            val error = listOfNotNull(webError, appError).firstOrNull()
            SecurityLoadResult(Unit, error)
        }

    private suspend fun safeWriteConfig(key: String, value: String): SecurityLoadResult<Unit> =
        withContext(Dispatchers.IO) {
            try {
                val conn = resolveConnectionOrNull() ?: return@withContext SecurityLoadResult(Unit, "安全后端未装配")
                val json = JSONObject().put("key", key).put("value", value).toString()
                request(conn, "POST", "/api/configs", json)
                SecurityLoadResult(Unit)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult(Unit, friendlyMessage(error))
            }
        }

    private suspend fun safeWrite(path: String, method: String): SecurityLoadResult<Unit> =
        withContext(Dispatchers.IO) {
            try {
                val conn = resolveConnectionOrNull() ?: return@withContext SecurityLoadResult(Unit, "安全后端未装配")
                request(conn, method, path, null)
                SecurityLoadResult(Unit)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult(Unit, friendlyMessage(error))
            }
        }

    /** 发起只读请求并解析；HTTP 400/非 2xx / 网络 / 解析错误均不抛出，转为 error 结果。 */
    private suspend fun <T> safeGet(path: String, parse: (String) -> T, fallback: T): SecurityLoadResult<T> {
        val conn = resolveConnectionOrNull() ?: return SecurityLoadResult(fallback, "安全后端未装配")
        return safeGetWithConnection(conn, path, parse, fallback)
    }

    private suspend fun <T> safeGetWithConnection(
        conn: Connection,
        path: String,
        parse: (String) -> T,
        fallback: T,
    ): SecurityLoadResult<T> {
        return try {
            SecurityLoadResult(parse(request(conn, "GET", path, null)))
        } catch (error: CancellationException) {
            throw error
        } catch (error: Exception) {
            SecurityLoadResult(fallback, friendlyMessage(error))
        }
    }

    private suspend fun resolveConnectionOrNull(): Connection? = try {
        resolveConnection()
    } catch (error: Exception) {
        null
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

    private fun request(conn: Connection, method: String, path: String, json: String?): String =
        PanelRequests.execute(
            method,
            conn.baseUrl + path,
            json = json,
            accessToken = conn.accessToken,
            localToken = conn.localToken,
        )

    private fun friendlyMessage(error: Throwable): String = when (error) {
        is PanelApiException -> {
            val serverMessage = error.serverMessage?.takeIf { it.isNotBlank() }
            serverMessage ?: "安全数据请求失败（HTTP ${error.statusCode}）"
        }
        is SecurityApiException -> {
            error.rawBody.takeIf { it.isNotBlank() }
                ?: "安全数据请求失败（HTTP ${error.code}）"
        }
        else -> error.message?.takeIf { it.isNotBlank() } ?: "安全数据请求失败"
    }

    private fun coerceSessionLimit(value: Int): Int = value.coerceIn(1, 20)

    private companion object {
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败诊断载体。 */
class SecurityApiException(
    val code: Int,
    val rawBody: String = "",
) : Exception("安全数据请求失败（HTTP $code${if (rawBody.isBlank()) "" else ": $rawBody"}）")
