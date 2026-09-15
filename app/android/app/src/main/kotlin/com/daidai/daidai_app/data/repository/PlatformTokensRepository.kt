package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.PLATFORM_TOKEN_MASK
import com.daidai.daidai_app.data.model.PlatformInfo
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.data.model.isMaskedPlatformToken
import com.daidai.daidai_app.data.model.platformCreatePayload
import com.daidai.daidai_app.data.model.panelErrorDetail
import com.daidai.daidai_app.data.model.platformTokenCreatePayload
import com.daidai.daidai_app.data.model.platformTokenUpdatePayload
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import org.json.JSONObject
import java.net.URLEncoder

/**
 * A1 平台/令牌管理仓库。
 *
 * 对齐后端两端契约：
 *   - Go 远端（panel/server/handler/platform_token.go）：
 *       GET/POST /api/platform-tokens/platforms，DELETE /api/platform-tokens/platforms/:id
 *       GET/POST /api/platform-tokens?platform_id，GET/PUT/DELETE /api/platform-tokens/:id
 *       PUT /api/platform-tokens/:id/enable|disable
 *   - 本地托管（LocalPanelStore.servePlatformTokens）：
 *       路径完全一致；平台列表与令牌均打码为 [PLATFORM_TOKEN_MASK]，
 *       本地端不提供平台删除（Go 端提供，级联删除其下令牌）。
 *
 * 令牌明文只在创建请求体里出现一次，读取响应永远为掩码，仓库不落盘明文。
 */
class PlatformTokensRepository(context: Context) {

    private val appContext = context.applicationContext

    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    // ---- 平台管理 ----

    /** GET /api/platform-tokens/platforms */
    suspend fun listPlatforms(): List<PlatformInfo> = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/platform-tokens/platforms", null)
        PlatformInfo.listFromResponse(body)
    }

    /** POST /api/platform-tokens/platforms。label 留空时用 name 兜底。 */
    suspend fun createPlatform(name: String, label: String = "", icon: String = ""): PlatformInfo? =
        withContext(Dispatchers.IO) {
            val payload = platformCreatePayload(name, label, icon)
            val body = execute("POST", "/api/platform-tokens/platforms", payload.toString())
            val created = JSONObject(body).optJSONObject("data")
            created?.let { PlatformInfo.fromJson(it) }
        }

    /** DELETE /api/platform-tokens/platforms/:id（Go 端级联删除其下令牌）。 */
    suspend fun deletePlatform(id: Long): String = withContext(Dispatchers.IO) {
        require(id > 0) { "无效的平台 ID" }
        val body = execute("DELETE", "/api/platform-tokens/platforms/$id", null)
        JSONObject(body).optString("message", "删除成功")
    }

    // ---- 令牌管理 ----

    /** GET /api/platform-tokens?platform_id=<可选> */
    suspend fun listTokens(platformId: Long? = null): List<PlatformTokenInfo> =
        withContext(Dispatchers.IO) {
            val query = platformId?.takeIf { it > 0 }
                ?.let { "?platform_id=$it" }
                .orEmpty()
            val body = execute("GET", "/api/platform-tokens$query", null)
            PlatformTokenInfo.listFromResponse(body)
        }

    /** POST /api/platform-tokens。token 必填且不得为掩码。 */
    suspend fun createToken(
        platformId: Long,
        name: String,
        token: String,
        service: String = "",
        serviceUser: String = "",
        remarks: String = "",
    ): PlatformTokenInfo? = withContext(Dispatchers.IO) {
        require(!isMaskedPlatformToken(token)) { "令牌内容不得为掩码占位值" }
        val payload = platformTokenCreatePayload(platformId, name, token, service, serviceUser, remarks)
        val body = execute("POST", "/api/platform-tokens", payload.toString())
        val created = JSONObject(body).optJSONObject("data")
        created?.let { PlatformTokenInfo.fromJson(it) }
    }

    /**
     * PUT /api/platform-tokens/:id。仅发送显式修改的字段；
     * [token] 为空或仍为掩码时不携带（服务端同样拒绝掩码写入）。
     */
    suspend fun updateToken(
        id: Long,
        name: String? = null,
        token: String? = null,
        service: String? = null,
        serviceUser: String? = null,
        remarks: String? = null,
    ): String = withContext(Dispatchers.IO) {
        require(id > 0) { "无效的令牌 ID" }
        val payload = platformTokenUpdatePayload(name, token, service, serviceUser, remarks)
        val body = execute("PUT", "/api/platform-tokens/$id", payload.toString())
        JSONObject(body).optString("message", "更新成功")
    }

    /** DELETE /api/platform-tokens/:id */
    suspend fun deleteToken(id: Long): String = withContext(Dispatchers.IO) {
        require(id > 0) { "无效的令牌 ID" }
        val body = execute("DELETE", "/api/platform-tokens/$id", null)
        JSONObject(body).optString("message", "删除成功")
    }

    /** PUT /api/platform-tokens/:id/enable|disable */
    suspend fun setTokenEnabled(id: Long, enabled: Boolean): String = withContext(Dispatchers.IO) {
        require(id > 0) { "无效的令牌 ID" }
        val action = if (enabled) "enable" else "disable"
        val body = execute("PUT", "/api/platform-tokens/$id/$action", "{}")
        JSONObject(body).optString("message", if (enabled) "已启用" else "已禁用")
    }

    // ---- 连接与请求 ----

    private suspend fun execute(method: String, path: String, json: String?): String {
        val conn = resolveConnection()
        return try {
            PanelRequests.execute(
                method,
                conn.baseUrl + path,
                json = json,
                accessToken = conn.accessToken,
                localToken = conn.localToken,
            )
        } catch (failure: PanelApiException) {
            throw PlatformTokensRepositoryException(
                failure.statusCode,
                panelErrorDetail(failure.statusCode, failure.responseBody, "平台令牌请求失败"),
            )
        }
    }

    private suspend fun resolveConnection(): Connection {
        val config = AppServices.configRepository(appContext).getConfig()
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

    /** 请求失败异常；[message] 已抽取服务端 error 文案。 */
    class PlatformTokensRepositoryException(
        val code: Int,
        message: String,
    ) : Exception(message)
}
