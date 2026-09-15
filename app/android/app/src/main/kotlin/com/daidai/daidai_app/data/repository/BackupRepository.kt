package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.BackupRecord
import com.daidai.daidai_app.data.model.HealthCheckItem
import com.daidai.daidai_app.data.model.HealthCheckResult
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.daidai.daidai_app.data.remote.PanelRequests
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 阶段 4-5 系统管理仓库：备份 + 健康检查的读写数据源。
 *
 * 复用阶段 2 仪表盘的认证约定与连接解析方式（与 [DashboardRepository] 一致）：
 *   - REMOTE：baseUrl = serverUrl，Authorization: Bearer <access_token>
 *   - MANAGED_LOCAL：baseUrl/token 由 [PanelCoreController] 提供，x-daidai-local-token 头
 *
 * 覆盖后端端点（panel/server/handler/system.go）：
 *   GET    /api/system/backups     -> ListBackups（备份列表）
 *   POST   /api/system/backup      -> Backup（立即创建备份）
 *   DELETE /api/system/backup?filename=<name>（删除单个备份，写操作）
 *   GET    /api/system/health-check           -> 读最近一次健康检查快照
 *   POST   /api/system/health-check           -> 立即重新执行健康检查
 */
class BackupRepository(
    context: Context,
) {
    private val appContext = context.applicationContext

    /** endpoint 连接描述：每次请求前解析一次（本地模式启动后 URL/token 可能变化）。 */
    private data class Connection(
        val baseUrl: String,
        val accessToken: String?,
        val localToken: String?,
    )

    /** 拉取全部备份记录。只读。 */
    suspend fun listBackups(): List<BackupRecord> = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/backups", null)
        val data = jsonArray(body, "data") ?: throw IllegalStateException("响应缺少 data 数组")
        val records = buildList {
            for (i in 0 until data.length()) {
                data.optJSONObject(i)?.let { add(BackupRecord.fromJson(it)) }
            }
        }
        records.sortedByDescending { it.createdAt }
    }

    /** 新建一份备份（写操作）。name 可选，password 留空表示无密码。 */
    suspend fun createBackup(name: String? = null): JsonResult = withContext(Dispatchers.IO) {
        val payload = JSONObject().apply {
            put("password", "")
            name?.takeIf { it.isNotBlank() }?.let { put("name", it) }
        }.toString()
        val body = execute("POST", resolveConnection(), "/api/system/backup", payload)
        parseCommon(body)
    }

    /** 删除指定备份（写操作）。后端通过 filename 查询参数定位文件。 */
    suspend fun deleteBackup(name: String): JsonResult = withContext(Dispatchers.IO) {
        val body = execute(
            "DELETE",
            resolveConnection(),
            "/api/system/backup?filename=" + java.net.URLEncoder.encode(name, "UTF-8"),
            null,
        )
        parseCommon(body)
    }

    /** 读取最近一次健康检查快照。只读。 */
    suspend fun healthCheckSnapshot(): HealthCheckResult = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/health-check", null)
        parseHealth(body)
    }

    /** 立即重新执行健康检查。只读（GET 卷 / 写一致，后端无副作用变更数据）。 */
    suspend fun runHealthCheck(): HealthCheckResult = withContext(Dispatchers.IO) {
        val body = execute("POST", resolveConnection(), "/api/system/health-check", null)
        parseHealth(body)
    }

    // ---- 解析 ----

    private fun parseHealth(body: String): HealthCheckResult {
        val data = jsonObject(body, "data") ?: throw IllegalStateException("响应缺少 data 字段")
        val rawItems = data.optJSONArray("items") ?: return HealthCheckResult(lastCheckedAt = data.optString("last_checked_at"))
        val items = buildList {
            for (i in 0 until rawItems.length()) {
                rawItems.optJSONObject(i)?.let { add(HealthCheckItem.fromJson(it)) }
            }
        }
        return HealthCheckResult(items, data.optString("last_checked_at"))
    }

    private fun parseCommon(body: String): JsonResult {
        val json = JSONObject(body)
        return JsonResult(
            message = json.optString("message"),
            raw = body,
        )
    }

    private fun jsonObject(body: String, key: String): JSONObject? {
        val obj = JSONObject(body)
        val v = obj.opt(key) ?: return null
        return v as? JSONObject
    }

    private fun jsonArray(body: String, key: String): org.json.JSONArray? {
        val obj = JSONObject(body)
        val v = obj.opt(key) ?: return null
        return v as? org.json.JSONArray
    }

    // ---- 连接与 HTTP ----

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

    private fun execute(method: String, conn: Connection, path: String, body: String?): String =
        try {
            PanelRequests.execute(
                method,
                conn.baseUrl + path,
                json = body,
                accessToken = conn.accessToken,
                localToken = conn.localToken,
            )
        } catch (failure: PanelApiException) {
            throw BackupRepositoryException(failure.statusCode, failure.responseBody)
        }

    /** 后端成功响应的通用载体（备份创建/删除）。 */
    data class JsonResult(
        val message: String = "",
        val raw: String = "",
    )

    /** 网络/解析失败的诊断载体。 */
    class BackupRepositoryException(
        val code: Int,
        val rawBody: String = "",
    ) : Exception("系统操作请求失败（HTTP $code${if (rawBody.isBlank()) "" else ": $rawBody"}）")

}
