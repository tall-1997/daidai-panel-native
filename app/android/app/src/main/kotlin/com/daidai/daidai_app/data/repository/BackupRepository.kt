package com.daidai.daidai_app.data.repository

import android.content.Context
import android.net.Uri
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.model.BackupRecord
import com.daidai.daidai_app.data.model.ConfigScriptContent
import com.daidai.daidai_app.data.model.HealthCheckItem
import com.daidai.daidai_app.data.model.HealthCheckResult
import com.daidai.daidai_app.data.model.MachineCode
import com.daidai.daidai_app.data.model.PanelLogPage
import com.daidai.daidai_app.data.model.PanelSettingsSnapshot
import com.daidai.daidai_app.data.model.RestoreProgress
import com.daidai.daidai_app.data.model.panelErrorDetail
import com.daidai.daidai_app.di.AppServices
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import com.daidai.daidai_app.data.remote.PanelSession
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okio.Buffer
import okio.buffer
import okio.source
import okio.ForwardingSink
import okio.sink
import org.json.JSONObject
import java.io.File
import java.io.FileInputStream
import java.io.InputStream
import java.net.URLEncoder

/**
 * 阶段 4-5 系统管理仓库：备份 + 健康检查 + 系统管理高阶能力。
 *
 * 复用阶段 2 仪表盘的认证约定与连接解析方式（与 [DashboardRepository] 一致）：
 *   - REMOTE：baseUrl = serverUrl，Authorization: Bearer <access_token>
 *   - MANAGED_LOCAL：baseUrl/token 由 [PanelCoreController] 提供，x-daidai-local-token 头
 *
 * 覆盖后端端点（panel/server/handler/system.go / handler/system_update.go）：
 *   GET    /api/system/backups                -> ListBackups（备份列表）
 *   POST   /api/system/backup                 -> Backup（立即创建备份）
 *   DELETE /api/system/backup?filename=<name> -> 删除单个备份
 *   GET/POST /api/system/health-check         -> 健康检查快照
 *   GET    /api/system/machine-code           -> 机器码
 *   GET    /api/system/panel-settings         -> 面板设置（兼容 Go/Local 两种包装）
 *   POST   /api/configs                       -> 保存面板设置项（panel_title/panel_icon）
 *   GET/PUT /api/system/config-script         -> 配置脚本读写
 *   GET    /api/system/panel-log?lines...     -> 面板日志（容错 Go/Local 两种格式）
 *   POST   /api/system/backup/upload          -> SAF 备份导入
 *   GET    /api/system/backup/download?filename -> SAF 备份导出
 *   POST   /api/system/restore                -> 备份恢复
 *   GET    /api/system/restore/progress       -> 恢复进度
 *   GET    /api/system/version                -> 面板版本
 *   GET    /api/system/update-status          -> 更新状态（Android 显示 immutable_apk 语义）
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

    // ---- C8/C9 系统高阶能力 ----

    /** GET /api/system/machine-code */
    suspend fun machineCode(): MachineCode = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/machine-code", null)
        MachineCode.fromJson(body)
    }

    /** GET /api/system/version */
    suspend fun panelVersion(): String = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/version", null)
        val data = JSONObject(body).optJSONObject("data") ?: JSONObject()
        data.optString("version").ifBlank { data.optString("core_version") }
    }

    /**
     * GET /api/system/panel-settings。
     *
     * Go 端 data 是 `{key:value(string)}`；本地端 data 是 `{key:{value,default_value}}`。
     * 两种形态都归一化为 [PanelSettingsSnapshot]。
     */
    suspend fun panelSettings(): PanelSettingsSnapshot = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/panel-settings", null)
        PanelSettingsSnapshot.fromJson(body)
    }

    /**
     * 写入面板设置（panel_title / panel_icon）。
     * Go: POST /api/configs 或 PUT /api/configs/:key；本地：PUT /api/system/panel-settings 不支持，
     * 因此统一走 POST /api/configs。失败时按 panelErrorDetail 抛出异常。
     */
    suspend fun savePanelSettings(panelTitle: String, panelIcon: String): String =
        withContext(Dispatchers.IO) {
            val payload = JSONObject()
                .put("panel_title", panelTitle)
                .put("panel_icon", panelIcon)
            // 批量保存（configs 数组格式）；Go BatchSet: {"configs":{...}}；本地端暂未提供批量接口。
            // 这里退化为多次 POST /api/configs {key,value}，确保两端都能落盘。
            val conn = resolveConnection()
            var lastMessage = ""
            for (key in listOf("panel_title", "panel_icon")) {
                val one = JSONObject().put("key", key).put("value", payload.optString(key))
                val body = execute("POST", conn, "/api/configs", one.toString())
                lastMessage = JSONObject(body).optString("message", "配置已更新")
            }
            lastMessage
        }

    /** GET /api/system/config-script -> {content, path} */
    suspend fun configScript(): ConfigScriptContent = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/config-script", null)
        ConfigScriptContent.fromJson(body)
    }

    /** PUT /api/system/config-script -> {message} */
    suspend fun saveConfigScript(content: String): String = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("content", content).toString()
        val body = execute("PUT", resolveConnection(), "/api/system/config-script", payload)
        JSONObject(body).optString("message", "配置脚本已保存")
    }

    /**
     * GET /api/system/panel-log?lines=&keyword=&level=。
     * 本地端目前走 serveLogs 返回任务日志（数组），Go 端返回 {data:{logs:[...],total,n}}。
     * 解析器已对两种格式做兼容，见 [PanelLogPage.fromJson]。
     */
    suspend fun panelLog(lines: Int = 300, keyword: String = "", level: String = ""): PanelLogPage =
        withContext(Dispatchers.IO) {
            val safeLines = lines.coerceIn(1, 2000)
            val query = buildString {
                append("?lines=").append(safeLines)
                if (keyword.isNotBlank()) append("&keyword=").append(URLEncoder.encode(keyword, "UTF-8"))
                if (level.isNotBlank()) append("&level=").append(URLEncoder.encode(level, "UTF-8"))
            }
            val body = execute("GET", resolveConnection(), "/api/system/panel-log$query", null)
            PanelLogPage.fromJson(body)
        }

    /** GET /api/system/restore/progress -> RestoreProgress */
    suspend fun restoreProgress(): RestoreProgress = withContext(Dispatchers.IO) {
        val body = execute("GET", resolveConnection(), "/api/system/restore/progress", null)
        RestoreProgress.fromJson(body)
    }

    /**
     * POST /api/system/restore {filename, password} -> message。
     * 恢复是危险操作，UI 必须先弹二次确认。后端 409 表示「仍有活跃任务」。
     */
    suspend fun restoreBackup(filename: String, password: String = ""): JsonResult =
        withContext(Dispatchers.IO) {
            require(filename.isNotBlank()) { "文件名不能为空" }
            val payload = JSONObject()
                .put("filename", filename)
                .put("password", password)
                .toString()
            val body = execute("POST", resolveConnection(), "/api/system/restore", payload)
            parseCommon(body)
        }

    /**
     * GET /api/system/update-status。返回原始 JSON 字符串（不做不可更新猜测），
     * 由 UI 层依据 deployment_type=android_apk 或 update_manager=platform_installer
     * 诚实展示「Android 端由平台安装器接管更新」语义，不伪造支持。
     */
    suspend fun updateStatus(): com.daidai.daidai_app.data.model.PanelUpdateStatus =
        withContext(Dispatchers.IO) {
            val body = execute("GET", resolveConnection(), "/api/system/update-status", null)
            com.daidai.daidai_app.data.model.PanelUpdateStatus.fromJson(body)
        }

    // ---- 备份上传/下载（SAF 文件流） ----

    /** 上传备份（写操作，multipart POST /api/system/backup/upload）。
     *  [file] 为源文件；服务端 512MB 上限，超出时 [BackupRepositoryException]。
     *  [onProgress] 为可选进度回调 (bytesWritten, totalBytes)。
     */
    suspend fun uploadBackup(
        file: File,
        onProgress: ((Long, Long) -> Unit)? = null,
    ): JsonResult = withContext(Dispatchers.IO) {
        require(file.isFile) { "源文件不存在" }
        val conn = resolveConnection()
        val request = buildRequest(conn, "/api/system/backup/upload")
            .post(multipartBodyWithProgress(file, onProgress))
            .build()
        val body = executeRaw(conn, request)
        parseCommon(body)
    }

    /**
     * 下载备份到目标 [target]（流式写盘，覆盖）。
     * 后端返回 application/octet-stream + Content-Disposition；本地/远端一致。
     * 进度回调 [onProgress] (bytesWritten, totalBytes)，其中 totalBytes 未知时为 -1。
     */
    suspend fun downloadBackup(
        filename: String,
        target: File,
        onProgress: ((Long, Long) -> Unit)? = null,
    ): Long = withContext(Dispatchers.IO) {
        val conn = resolveConnection()
        val path = "/api/system/backup/download?filename=" + URLEncoder.encode(filename, "UTF-8")
        val initial = buildRequest(conn, path).get().build()
        var response = PanelRequests.sharedClient.newCall(initial).execute()
        if (response.code == 401) {
            response.close()
            if (PanelSession.tryRefresh()) {
                val refreshed = buildRequest(
                    conn.copy(accessToken = PanelSession.accessTokenProvider?.invoke()),
                    path,
                ).get().build()
                response = PanelRequests.sharedClient.newCall(refreshed).execute()
            }
        }
        try {
            if (!response.isSuccessful) {
                val raw = response.body?.string().orEmpty()
                throw BackupRepositoryException(response.code, panelErrorDetail(response.code, raw, "备份下载失败"))
            }
            val src = response.body ?: throw IllegalStateException("下载响应无 body")
            val total = src.contentLength()
            target.sink().buffer().use { sink ->
                val buffer = okio.Buffer()
                var written = 0L
                val source = src.source()
                while (true) {
                    val read = source.read(buffer, 8192L)
                    if (read == -1L) break
                    sink.write(buffer, read)
                    written += read
                    onProgress?.let { it(written, total) }
                }
                sink.flush()
                written
            }
        } finally {
            response.close()
        }
    }

    /** 备份下载的缓存目录（app cacheDir/backup-downloads）。 */
    fun downloadCacheDir(): File = File(appContext.cacheDir, "backup-downloads").apply { mkdirs() }

    /** 从 SAF content:// Uri 复制到缓存文件（用于上传前的临时文件）。 */
    suspend fun stageUriToCache(uri: Uri, displayName: String? = null): File =
        withContext(Dispatchers.IO) {
            val safeName = displayName?.substringAfterLast('/')?.takeIf { it.isNotBlank() }
                ?: "upload-${System.currentTimeMillis()}.bin"
            val out = File(appContext.cacheDir, "staged/$safeName").apply {
                parentFile?.mkdirs()
            }
            val resolver = appContext.contentResolver
            val input: InputStream = resolver.openInputStream(uri)
                ?: throw IllegalStateException("无法读取所选文件")
            input.use { src -> out.outputStream().use { dst -> src.copyTo(dst) } }
            out
        }

    /**
     * 从 content:// 读取并写入到指定目标文件。用于把下载的缓存文件转存到 SAF 目录。
     */
    suspend fun copyFileToUri(source: File, target: Uri): Boolean = withContext(Dispatchers.IO) {
        val resolver = appContext.contentResolver
        runCatching {
            val output = resolver.openOutputStream(target, "w")
                ?: return@runCatching false
            output.use { out -> FileInputStream(source).use { input -> input.copyTo(out) } }
            true
        }.getOrDefault(false)
    }

    /**
     * 在 app 外部下载目录中落盘（Android 10+ 通过 MediaStore.Downloads）。
     * 失败则回退到 cacheDir 并返回该 File（避免完全不可用）。
     */
    suspend fun persistToDownloads(filename: String, source: File): Uri? = withContext(Dispatchers.IO) {
        val resolver = appContext.contentResolver
        val mimeType = "application/octet-stream"
        runCatching {
            val values = android.content.ContentValues().apply {
                put(android.provider.MediaStore.Downloads.DISPLAY_NAME, filename)
                put(android.provider.MediaStore.Downloads.MIME_TYPE, mimeType)
                if (android.os.Build.VERSION.SDK_INT >= 29) {
                    put(android.provider.MediaStore.Downloads.RELATIVE_PATH, "Download/")
                }
                put(android.provider.MediaStore.Downloads.IS_PENDING, 1)
            }
            val collection = if (android.os.Build.VERSION.SDK_INT >= 29) {
                android.provider.MediaStore.Downloads.EXTERNAL_CONTENT_URI
            } else {
                android.provider.MediaStore.Files.getContentUri("external")
            }
            val uri = resolver.insert(collection, values) ?: return@runCatching null
            resolver.openOutputStream(uri).use { out ->
                FileInputStream(source).use { input -> input.copyTo(out!!) }
            }
            values.clear()
            if (android.os.Build.VERSION.SDK_INT >= 29) {
                values.put(android.provider.MediaStore.Downloads.IS_PENDING, 0)
            }
            resolver.update(uri, values, null, null)
            uri
        }.getOrNull()
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

    /** 直接构造带认证头的请求（供 multipart 上传 / 流式下载复用 sharedClient）。 */
    private fun buildRequest(conn: Connection, path: String): Request.Builder {
        val url = conn.baseUrl + path
        val builder = Request.Builder().url(url)
        PanelRequests.originOf(url)?.let { builder.header("Origin", it) }
        conn.accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        conn.localToken?.takeIf { it.isNotBlank() }?.let { builder.header("x-daidai-local-token", it) }
        return builder
    }

    /** 使用共享 client 执行请求；401 时走 [PanelSession.tryRefresh] 单飞刷新后重试一次。 */
    private fun executeRaw(conn: Connection, request: Request): String {
        var response = PanelRequests.sharedClient.newCall(request).execute()
        try {
            if (response.code == 401 && PanelSession.tryRefresh()) {
                response.close()
                val retry = request.newBuilder()
                    .header("Authorization", "Bearer ${PanelSession.accessTokenProvider?.invoke().orEmpty()}")
                    .build()
                response = PanelRequests.sharedClient.newCall(retry).execute()
            }
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw BackupRepositoryException(response.code, panelErrorDetail(response.code, body, "系统操作失败"))
            }
            return body
        } finally {
            response.close()
        }
    }

    /** multipart 文件上传体；字段名 file，带可选写入进度回调。 */
    private fun multipartBodyWithProgress(
        file: File,
        onProgress: ((Long, Long) -> Unit)?,
    ): RequestBody {
        val fileBody = file.asRequestBody("application/octet-stream".toMediaType())
        val multipart = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", file.name, fileBody)
            .build()
        return if (onProgress == null) multipart else ProgressRequestBody(multipart, onProgress)
    }

    /** 包装请求体，按字节回调写入进度（上传节流 >=64KB 或完成时触发）。 */
    private class ProgressRequestBody(
        private val delegate: RequestBody,
        private val onProgress: (Long, Long) -> Unit,
    ) : RequestBody() {
        override fun contentType() = delegate.contentType()
        override fun contentLength(): Long = delegate.contentLength()
        override fun isOneShot() = delegate.isOneShot()

        override fun writeTo(sink: okio.BufferedSink) {
            val total = runCatching { contentLength() }.getOrDefault(-1L)
            val counting = CountingSink(sink, total, onProgress)
            val buffered = counting.buffer()
            delegate.writeTo(buffered)
            buffered.flush()
            counting.commit()
        }
    }

    /** okio ForwardingSink 计数器。 */
    private class CountingSink(
        downstream: okio.BufferedSink,
        private val total: Long,
        private val onProgress: (Long, Long) -> Unit,
    ) : ForwardingSink(downstream) {
        private var written = 0L
        private var lastEmit = 0L

        override fun write(source: okio.Buffer, byteCount: Long) {
            super.write(source, byteCount)
            written += byteCount
            if (written - lastEmit >= 64 * 1024 || (total >= 0 && written >= total)) {
                lastEmit = written
                onProgress(written, total)
            }
        }

        fun commit() {
            if (written != lastEmit) onProgress(written, total)
        }
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
