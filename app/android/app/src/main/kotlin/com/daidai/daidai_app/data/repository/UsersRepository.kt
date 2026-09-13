package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.ResetPasswordPayload
import com.daidai.daidai_app.data.model.User
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 用户管理模块数据源契约（阶段 4-4）。
 *
 * 与既有 `PanelHttpClient` 认证约定保持一致：
 *  - 远程面板使用 `Authorization: Bearer <accessToken>`
 *  - 托管本地面板使用 `X-Daidai-Local-Token: <localToken>`
 * 二者均未提供时按未认证请求发出。
 */
interface UsersRepository {
    /** 拉取全部用户（GET /api/auth/users 或 GET /api/users）。 */
    suspend fun getUsers(): List<User>

    /** 新建用户（POST /api/users），成功后请调用方刷新列表。 */
    suspend fun createUser(username: String, password: String, role: String)

    /** 更新用户角色 / 启停（PUT /api/users/:id）。 */
    suspend fun updateUser(id: Long, role: String?, enabled: Boolean?)

    /** 删除用户（DELETE /api/users/:id）。 */
    suspend fun deleteUser(id: Long)

    /** 重置指定用户的密码（PUT /api/users/:id/reset-password）。 */
    suspend fun resetPassword(id: Long, payload: ResetPasswordPayload)
}

/**
 * 读写 `GET/POST/PUT/DELETE /api/users` 及 `PUT /api/users/:id/reset-password`
 * 的自包含实现。独立于 `PanelHttpClient`，不改动既有网络层；baseUrl 应已去掉尾部斜杠。
 */
class PanelUsersRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : UsersRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun getUsers(): List<User> = withContext(Dispatchers.IO) {
        val body = execute("GET", "$baseUrl/api/auth/users")
        User.parseList(body)
    }

    override suspend fun createUser(username: String, password: String, role: String) {
        withContext(Dispatchers.IO) {
            val json = JSONObject()
                .put("username", username)
                .put("password", password)
                .put("role", role)
            execute("POST", "$baseUrl/api/users", json.toString())
        }
    }

    override suspend fun updateUser(id: Long, role: String?, enabled: Boolean?) {
        withContext(Dispatchers.IO) {
            val json = JSONObject()
            role?.let { json.put("role", it) }
            enabled?.let { json.put("enabled", it) }
            execute("PUT", "$baseUrl/api/users/$id", json.toString())
        }
    }

    override suspend fun deleteUser(id: Long) {
        withContext(Dispatchers.IO) {
            execute("DELETE", "$baseUrl/api/users/$id")
        }
    }

    override suspend fun resetPassword(id: Long, payload: ResetPasswordPayload) {
        withContext(Dispatchers.IO) {
            execute("PUT", "$baseUrl/api/users/$id/reset-password", payload.toJson().toString())
        }
    }

    private fun execute(method: String, url: String, json: String? = null): String {
        val builder = Request.Builder().url(url)
        accessToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("Authorization", "Bearer $it") }
        localToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("X-Daidai-Local-Token", it) }
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
                throw UsersApiException(response.code, body, message)
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
class UsersApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException(serverMessage?.takeIf(String::isNotEmpty) ?: "用户请求失败（HTTP $statusCode）")
