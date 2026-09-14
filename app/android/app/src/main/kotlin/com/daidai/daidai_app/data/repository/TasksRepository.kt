package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.model.TaskWritePayload
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
 * 任务模块数据源契约（阶段 3-1）。
 *
 * 与既有 `PanelHttpClient` 认证约定保持一致：
 *  - 远程面板使用 `Authorization: Bearer <accessToken>`
 *  - 托管本地面板使用 `X-Daidai-Local-Token: <localToken>`
 */
interface TasksRepository {
    /** 拉取全部任务（`all=1` 一次性返回）。 */
    suspend fun getTasks(): List<Task>

    /** 新建任务，返回后端回执对象（可能为空 body）。 */
    suspend fun createTask(payload: TaskWritePayload)

    /** 更新指定任务。 */
    suspend fun updateTask(id: Long, payload: TaskWritePayload)

    /** 删除指定任务。 */
    suspend fun deleteTask(id: Long)

    /** 立即运行指定任务（PUT /api/tasks/:id/run）。 */
    suspend fun runTask(id: Long)

    /** 启停切换：启用走 /enable，禁用走 /disable。 */
    suspend fun toggleTask(id: Long, enabled: Boolean)
}

/**
 * 读写 `GET/POST/PUT/DELETE /api/tasks` 及 `PUT /api/tasks/:id/{run,enable,disable}`
 * 的自包含实现。独立于 `PanelHttpClient`，不改动既有网络层；baseUrl 应已去掉尾部斜杠。
 */
class PanelTasksRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : TasksRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun getTasks(): List<Task> = withContext(Dispatchers.IO) {
        val body = execute("GET", "$baseUrl/api/tasks?all=1")
        Task.parseList(body)
    }

    override suspend fun createTask(payload: TaskWritePayload) {
        withContext(Dispatchers.IO) {
            execute("POST", "$baseUrl/api/tasks", payload.toJson().toString())
        }
    }

    override suspend fun updateTask(id: Long, payload: TaskWritePayload) {
        withContext(Dispatchers.IO) {
            execute("PUT", "$baseUrl/api/tasks/$id", payload.toJson().toString())
        }
    }

    override suspend fun deleteTask(id: Long) {
        withContext(Dispatchers.IO) {
            execute("DELETE", "$baseUrl/api/tasks/$id")
        }
    }

    override suspend fun runTask(id: Long) {
        withContext(Dispatchers.IO) {
            execute("PUT", "$baseUrl/api/tasks/$id/run")
        }
    }

    override suspend fun toggleTask(id: Long, enabled: Boolean) {
        withContext(Dispatchers.IO) {
            val action = if (enabled) "enable" else "disable"
            execute("PUT", "$baseUrl/api/tasks/$id/$action")
        }
    }

    private fun execute(method: String, url: String, json: String? = null): String =
        PanelRequests.execute(method, url, json, accessToken = accessToken, localToken = localToken)

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        fun defaultHttpClient(): OkHttpClient = PanelRequests.sharedClient
    }
}

/** HTTP 失败：保留状态码、原始响应与可读错误描述。 */
class TasksApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException(serverMessage?.takeIf(String::isNotEmpty) ?: "任务请求失败（HTTP $statusCode）")