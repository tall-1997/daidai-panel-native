package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.model.TaskWritePayload
import com.daidai.daidai_app.data.model.TaskView
import com.daidai.daidai_app.data.model.TaskStats
import com.daidai.daidai_app.data.model.TaskBatchResult
import okhttp3.HttpUrl.Companion.toHttpUrl
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

    /** 置顶/取消置顶（PUT /api/tasks/:id/pin|unpin）。 */
    suspend fun setPinned(id: Long, pinned: Boolean)

    /** 复制任务（POST /api/tasks/:id/copy），返回新任务 id。 */
    suspend fun copyTask(id: Long): Long

    /** 批量运行（POST /api/tasks/batch/run，最多 10 个）。 */
    suspend fun batchRun(ids: List<Long>): Int

    suspend fun runTask(id: Long)

    /** 启停切换：启用走 /enable，禁用走 /disable。 */
    suspend fun toggleTask(id: Long, enabled: Boolean)

    suspend fun batchToggle(ids: List<Long>, enabled: Boolean): TaskBatchResult
    suspend fun batchAddLabels(ids: List<Long>, labels: List<String>): TaskBatchResult
    suspend fun getViews(): List<TaskView>
    suspend fun saveView(view: TaskView)
    suspend fun deleteView(id: Long)
    suspend fun getViewTasks(view: TaskView): List<Task>
    suspend fun getStats(id: Long, days: Int = 7): TaskStats
    suspend fun restoreSubscriptionDefault(id: Long)
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
    private val transport = TaskLogTransport(httpClient, accessToken, localToken)

    override suspend fun batchToggle(ids: List<Long>, enabled: Boolean): TaskBatchResult =
        batchMutation(ids, if (enabled) "enable" else "disable")

    override suspend fun batchAddLabels(ids: List<Long>, labels: List<String>): TaskBatchResult {
        check(localToken.isNullOrBlank()) { "本地面板尚未实现批量追加标签" }
        val valid = labels.map(String::trim).filter { it.isNotEmpty() && !it.startsWith("分组:") && !it.startsWith("subscription:") }.distinct()
        require(valid.isNotEmpty()) { "请输入有效标签（保留前缀不可使用）" }
        return batchMutation(ids, "add-labels", JSONObject().put("labels", JSONArray(valid)))
    }

    private suspend fun batchMutation(ids: List<Long>, action: String, payload: JSONObject = JSONObject()): TaskBatchResult {
        require(ids.isNotEmpty() && ids.all { it > 0 } && ids.distinct().size == ids.size) { "请选择有效且无重复的任务" }
        payload.put("task_ids", JSONArray(ids))
        return TaskBatchResult.fromJson(transport.json("$baseUrl/api/tasks/batch/$action", "PUT", payload.toString()), ids.size)
    }

    override suspend fun getViews(): List<TaskView> = TaskView.parseList(transport.json("$baseUrl/api/tasks/views"))

    override suspend fun saveView(view: TaskView) {
        transport.json("$baseUrl/api/tasks/views" + if (view.id > 0) "/${view.id}" else "",
            if (view.id > 0) "PUT" else "POST", view.toJson().toString())
    }

    override suspend fun deleteView(id: Long) { transport.json("$baseUrl/api/tasks/views/$id", "DELETE") }

    override suspend fun getViewTasks(view: TaskView): List<Task> {
        check(localToken.isNullOrBlank()) { "本地面板尚未实现视图筛选，视图配置已保存" }
        val url = "$baseUrl/api/tasks".toHttpUrl().newBuilder().addQueryParameter("all", "1")
            .addQueryParameter("filters", view.filters).addQueryParameter("sort_rules", view.sortRules).build()
        return Task.parseList(transport.json(url.toString()))
    }

    override suspend fun getStats(id: Long, days: Int): TaskStats {
        require(days in 1..365) { "统计天数应为 1 至 365" }
        check(localToken.isNullOrBlank()) { "本地面板 stats 当前仅返回占位零值，暂无法提供真实统计" }
        return TaskStats.fromJson(JSONObject(transport.json("$baseUrl/api/tasks/$id/stats?days=$days")).getJSONObject("data"))
    }

    override suspend fun restoreSubscriptionDefault(id: Long) {
        check(localToken.isNullOrBlank()) { "本地面板尚未实现恢复订阅默认" }
        transport.json("$baseUrl/api/tasks/$id/restore-subscription-default", "PUT")
    }

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

    override suspend fun setPinned(id: Long, pinned: Boolean) {
        withContext(Dispatchers.IO) {
            execute("PUT", "$baseUrl/api/tasks/$id/${if (pinned) "pin" else "unpin"}")
        }
    }

    override suspend fun copyTask(id: Long): Long = withContext(Dispatchers.IO) {
        val body = execute("POST", "$baseUrl/api/tasks/$id/copy")
        JSONObject(body).optJSONObject("data")?.optLong("id") ?: 0L
    }

    override suspend fun batchRun(ids: List<Long>): Int = withContext(Dispatchers.IO) {
        require(ids.isNotEmpty()) { "请选择要运行的任务" }
        require(ids.size <= 10) { "批量运行最多 10 个任务" }
        val payload = JSONObject().put("task_ids", JSONArray(ids))
        val body = execute("POST", "$baseUrl/api/tasks/batch/run", payload.toString())
        TaskBatchResult.fromJson(body, ids.size).accepted
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

    private suspend fun execute(method: String, url: String, json: String? = null): String =
        transport.json(url, method, json)

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
