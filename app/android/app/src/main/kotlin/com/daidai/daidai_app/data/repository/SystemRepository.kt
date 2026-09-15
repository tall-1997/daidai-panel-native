package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.repository.SshKey
import com.daidai.daidai_app.data.model.User
import com.daidai.daidai_app.data.model.panelErrorDetail
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import com.daidai.daidai_app.data.repository.PanelConnectionMode
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

class SystemRepository(context: Context) {
    private val appContext = context.applicationContext

    private data class Connection(val baseUrl: String, val accessToken: String?, val localToken: String?)

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

    private suspend fun execute(method: String, path: String, json: String? = null): String =
        try {
            val conn = resolveConnection()
            PanelRequests.execute(method, conn.baseUrl + path, json = json, accessToken = conn.accessToken, localToken = conn.localToken)
        } catch (failure: PanelApiException) {
            throw SystemRepositoryException(failure.statusCode, panelErrorDetail(failure.statusCode, failure.responseBody, "系统请求失败"))
        }

    suspend fun loadUsers(): List<User> = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/users")
        User.parseList(body)
    }

    suspend fun addUser(username: String, password: String, role: String) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("username", username).put("password", password).put("role", role).toString()
        execute("POST", "/api/users", payload)
    }

    suspend fun deleteUser(userId: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "/api/users/$userId")
    }

    suspend fun loadSshKeys(): List<SshKey> = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/ssh-keys")
        val data = runCatching { JSONObject(body).optJSONArray("data") }.getOrNull() ?: JSONArray()
        buildList {
            for (i in 0 until data.length()) {
                data.optJSONObject(i)?.let { add(SshKey.fromJson(it)) }
            }
        }
    }

    suspend fun addSshKey(name: String, key: String) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("name", name).put("private_key", key).toString()
        execute("POST", "/api/ssh-keys", payload)
    }

    suspend fun deleteSshKey(keyId: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "/api/ssh-keys/$keyId")
    }

    suspend fun loadPlatformTokens(): List<PlatformTokenInfo> = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/platform-tokens")
        PlatformTokenInfo.listFromResponse(body)
    }

    suspend fun addPlatformToken(name: String, token: String) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("platform_id", 1).put("name", name).put("token", token).toString()
        execute("POST", "/api/platform-tokens", payload)
    }

    suspend fun deletePlatformToken(tokenId: Long) = withContext(Dispatchers.IO) {
        execute("DELETE", "/api/platform-tokens/$tokenId")
    }

    suspend fun updateSmtpConfig(host: String, port: Int, ssl: Boolean, username: String, password: String) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("host", host).put("port", port).put("ssl", ssl).put("username", username).put("password", password).toString()
        execute("POST", "/api/smtp/config", payload)
    }

    suspend fun testSmtpConfig(): String = withContext(Dispatchers.IO) {
        val body = execute("POST", "/api/smtp/test", "{}")
        JSONObject(body).optString("message", "测试请求已发送")
    }

    suspend fun loadApps(): List<AppInfo> = withContext(Dispatchers.IO) {
        val body = execute("GET", "/api/apps")
        val data = runCatching { JSONObject(body).optJSONArray("data") }.getOrNull() ?: JSONArray()
        buildList {
            for (i in 0 until data.length()) {
                data.optJSONObject(i)?.let { add(AppInfo.fromJson(it)) }
            }
        }
    }

    suspend fun updateAppEnabled(appId: Long, enabled: Boolean) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("enabled", enabled).toString()
        execute("PUT", "/api/apps/$appId", payload)
    }

    data class AppInfo(
        val id: Long = 0,
        val name: String = "",
        val enabled: Boolean = true,
        val description: String = "",
    ) {
        companion object {
            fun fromJson(json: JSONObject) = AppInfo(
                id = json.optLong("id"),
                name = json.optString("name"),
                enabled = json.optBoolean("enabled", true),
                description = json.optString("description")
            )
        }
    }

    class SystemRepositoryException(code: Int, message: String) : Exception(message)
}
