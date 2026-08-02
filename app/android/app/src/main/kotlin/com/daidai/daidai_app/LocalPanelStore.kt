package com.daidai.daidai_app

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import android.util.Base64
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.URLDecoder
import java.time.Instant
import java.security.SecureRandom
import java.time.format.DateTimeFormatter
import java.util.Collections
import java.util.concurrent.TimeUnit
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.PBEKeySpec

class LocalPanelStore(private val appContext: Context) : SQLiteOpenHelper(
    appContext,
    "daidai-local.db",
    null,
    SCHEMA_VERSION
) {
    private val configPrefs by lazy {
        appContext.getSharedPreferences("daidai-local-configs", Context.MODE_PRIVATE)
    }
    private data class LocalScriptResult(
        val logs: JSONArray,
        val status: String,
        val done: Boolean,
        val exitCode: Int?,
    )

    companion object {
        const val SCHEMA_VERSION = 5

        fun isRecoveryRequest(method: NanoHTTPD.Method, uri: String): Boolean = when (method to uri) {
            NanoHTTPD.Method.GET to "/api/system/backups",
            NanoHTTPD.Method.POST to "/api/system/backup/upload",
            NanoHTTPD.Method.GET to "/api/system/backup/download",
            NanoHTTPD.Method.POST to "/api/system/restore",
            NanoHTTPD.Method.GET to "/api/system/restore/progress" -> true
            else -> false
        }
    }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE local_users (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                username TEXT NOT NULL UNIQUE,
                password_hash TEXT NOT NULL,
                password_salt TEXT NOT NULL,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        db.execSQL(
            """CREATE TABLE local_sessions (
                id INTEGER PRIMARY KEY CHECK (id = 1),
                access_token TEXT NOT NULL,
                refresh_token TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        db.execSQL(
            """CREATE TABLE tasks (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                command TEXT NOT NULL DEFAULT '',
                cron_expression TEXT NOT NULL DEFAULT '',
                task_type TEXT NOT NULL DEFAULT 'manual',
                python_version TEXT NOT NULL DEFAULT '',
                status REAL NOT NULL DEFAULT 1,
                labels TEXT NOT NULL DEFAULT '[]',
                last_run_status TEXT NOT NULL DEFAULT '',
                last_run_logs TEXT NOT NULL DEFAULT '[]',
                last_log_id INTEGER NOT NULL DEFAULT 0,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        db.execSQL(
            """CREATE TABLE envs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                value TEXT NOT NULL DEFAULT '',
                remarks TEXT NOT NULL DEFAULT '',
                enabled INTEGER NOT NULL DEFAULT 1,
                groups_json TEXT NOT NULL DEFAULT '[]',
                sort_order INTEGER NOT NULL DEFAULT 0,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        db.execSQL(
            """CREATE TABLE dependencies (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                type TEXT NOT NULL,
                python_version TEXT NOT NULL DEFAULT '',
                version TEXT NOT NULL DEFAULT '',
                status TEXT NOT NULL DEFAULT 'queued',
                log TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        createScriptRuntimeTables(db)
        createTaskLogTables(db)
        createConfigTables(db)
        ensureDefaultAdmin(db)
        ensureSubscriptionsTable(db)
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) createScriptRuntimeTables(db)
        if (oldVersion < 3) db.execSQL("ALTER TABLE tasks ADD COLUMN last_run_logs TEXT NOT NULL DEFAULT '[]'")
        if (oldVersion < 4) {
            db.execSQL("ALTER TABLE tasks ADD COLUMN last_log_id INTEGER NOT NULL DEFAULT 0")
            createTaskLogTables(db)
        }
        if (oldVersion < 5) createConfigTables(db)
    }

    private fun createScriptRuntimeTables(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS script_versions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                script_path TEXT NOT NULL,
                content TEXT NOT NULL DEFAULT '',
                version INTEGER NOT NULL,
                message TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL
            )""".trimIndent()
        )
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS script_runs (
                id TEXT PRIMARY KEY,
                status TEXT NOT NULL,
                logs_json TEXT NOT NULL DEFAULT '[]',
                done INTEGER NOT NULL DEFAULT 0,
                exit_code INTEGER,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
    }

    private fun createTaskLogTables(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS task_logs_local (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                task_id INTEGER NOT NULL,
                content TEXT NOT NULL DEFAULT '',
                logs_json TEXT NOT NULL DEFAULT '[]',
                status INTEGER NOT NULL DEFAULT 2,
                exit_code INTEGER,
                duration REAL NOT NULL DEFAULT 0,
                started_at TEXT NOT NULL,
                ended_at TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL
            )""".trimIndent()
        )
    }

    private fun createConfigTables(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS local_configs (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL DEFAULT '',
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
    }


    private fun ensureDefaultAdmin(db: SQLiteDatabase) {
        val salt = ByteArray(16).also(SecureRandom()::nextBytes)
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("username", "admin")
            put("password_hash", hashPassword("admin123", salt))
            put("password_salt", Base64.encodeToString(salt, Base64.NO_WRAP))
            put("created_at", now)
            put("updated_at", now)
        }
        db.insertWithOnConflict("local_users", null, values, SQLiteDatabase.CONFLICT_IGNORE)
    }


    // App runtime log file for device testing
    private val appLogFile by lazy {
        File(appContext.filesDir, "app-runtime.log").also { f ->
            f.writeText("=== daidai-panel-native runtime log ===" + "\n")
        }
    }

    fun appLog(tag: String, message: String) {
        synchronized(appLogFile) {
            appLogFile.appendText(Instant.now().toString() + " [" + tag + "] " + message + "\n")
        }
    }

    fun readAppLog(): String = appLogFile.readText()

    fun serveAuth(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        return when {
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/check-init" ->
                ok(JSONObject().put("need_init", count(readableDatabase, "local_users") == 0))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/init" ->
                initializeAdmin(body(session))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/login" ->
                login(body(session))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/refresh" ->
                refresh(session)
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/user" ->
                authenticated(session) { ok(JSONObject().put("data", userJson())) }
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/logout" ->
                authenticated(session) { ok(JSONObject().put("message", "ok")) }
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/captcha-config" ->
                ok(JSONObject().put("data", JSONObject().put("enabled", false).put("configured", true)))
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "认证接口不存在")
        }
    }

    fun isAuthorized(session: NanoHTTPD.IHTTPSession): Boolean {
        val token = bearerToken(session) ?: return false
        return readableDatabase.rawQuery(
            "SELECT 1 FROM local_sessions WHERE id = 1 AND access_token = ?",
            arrayOf(token)
        ).use(Cursor::moveToFirst)
    }

    fun dashboard(): JSONObject {
        val db = readableDatabase
        val taskCount = count(db, "tasks")
        val enabledTasks = count(db, "tasks", "status > 0")
        val runningTasks = count(db, "tasks", "status = 2")
        return JSONObject().put(
            "data",
            JSONObject()
                .put("task_count", taskCount)
                .put("enabled_tasks", enabledTasks)
                .put("running_tasks", runningTasks)
                .put("success_logs", 0)
                .put("failed_logs", 0)
                .put("recent_logs", JSONArray())
                .put("daily_stats", JSONArray())
        )
    }

    fun serveTasks(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        val action = segments.getOrNull(2)
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/views" ->
                ok(JSONObject().put("data", JSONArray()))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/cron/templates" ->
                cronTemplates()
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/tasks/cron/parse" ->
                cronParse(body(session))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/notification-channels" ->
                ok(JSONObject().put("data", JSONArray()))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/export" -> exportTasks()
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/tasks/import" -> importTasks(bodyOrUploadedJson(session))
            normalizedUri.startsWith("/tasks/batch/") -> serveTaskBatch(session, action)
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("tasks", taskRows())
            session.method == NanoHTTPD.Method.POST && id == null -> createTask(body(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateTask(id, body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("tasks", id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable", "run", "stop") ->
                updateTaskStatus(id, action!!)
            id != null && session.method == NanoHTTPD.Method.GET && action == "latest-log" -> latestTaskLogResponse(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "live-logs" -> liveTaskLogResponse(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "stats" -> taskStats(id)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "任务接口尚未实现")
        }
    }

    fun serveLogs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        return when {
            session.method == NanoHTTPD.Method.GET && id != null && segments.getOrNull(2) == "stream" -> taskLogStream(id)
            session.method == NanoHTTPD.Method.GET && id != null -> taskLogByIdJson(id)?.let(::ok)
                ?: error(NanoHTTPD.Response.Status.NOT_FOUND, "日志不存在")
            session.method == NanoHTTPD.Method.GET -> ok(JSONObject().put("data", JSONArray()).put("total", 0).put("page", 1).put("page_size", 0))
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "日志接口尚未实现")
        }
    }

    fun serveEnvs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        val action = segments.getOrNull(2)
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri.endsWith("/groups") -> envGroups()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/envs/export" -> exportEnvs(asObject = true)
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/envs/export-all" -> exportEnvs(asObject = false)
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/envs/export-files" -> exportEnvFiles(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/envs/import" -> importEnvs(bodyOrUploadedJson(session))
            normalizedUri.startsWith("/envs/batch") -> serveEnvBatch(session, action)
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/envs/sort" -> sortEnvs(body(session))
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("envs", envRows())
            session.method == NanoHTTPD.Method.POST && id == null -> createEnv(body(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateEnv(id, body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("envs", id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable") ->
                updateEnvEnabled(id, action == "enable")
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "环境变量接口尚未实现")
        }
    }

    fun serveScripts(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/tree" -> scriptTree()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts" -> paginated("scripts", scriptRows())
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/content" -> scriptContent(session.parms["path"].orEmpty())
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/download" -> downloadScript(session)
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/scripts/content" -> saveScriptContent(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/directory" -> createScriptDirectory(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/upload" -> uploadScript(session)
            session.method == NanoHTTPD.Method.DELETE && normalizedUri == "/scripts" -> deleteScript(session)
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/scripts/rename" -> renameScript(body(session))
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/scripts/move" -> moveScript(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/copy" -> copyScript(body(session))
            session.method == NanoHTTPD.Method.DELETE && normalizedUri == "/scripts/batch" -> batchDeleteScripts(body(session))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/versions" -> listScriptVersions(session)
            session.method == NanoHTTPD.Method.DELETE && normalizedUri == "/scripts/versions" -> clearScriptVersions(session)
            session.method == NanoHTTPD.Method.GET && segments.size == 3 && segments[0] == "scripts" && segments[1] == "versions" -> getScriptVersion(segments[2].toLongOrNull())
            session.method == NanoHTTPD.Method.PUT && segments.size == 4 && segments[0] == "scripts" && segments[1] == "versions" && segments[3] == "rollback" -> rollbackScriptVersion(segments[2].toLongOrNull())
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/format" -> formatScript(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/run" -> runScript(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/run-code" -> runCode(body(session))
            session.method == NanoHTTPD.Method.GET && segments.size == 4 && segments[0] == "scripts" && segments[1] == "run" && segments[3] == "logs" -> scriptRunLogs(segments[2])
            session.method == NanoHTTPD.Method.PUT && segments.size == 4 && segments[0] == "scripts" && segments[1] == "run" && segments[3] == "stop" -> stopScriptRun(segments[2])
            session.method == NanoHTTPD.Method.DELETE && segments.size == 3 && segments[0] == "scripts" && segments[1] == "run" -> clearScriptRun(segments[2])
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本接口尚未实现")
        }
    }

    fun panelLog(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val maxLines = session.parms["lines"]?.toIntOrNull()?.coerceIn(1, 1000) ?: 200
        val keyword = session.parms["keyword"]?.trim().orEmpty()
        val level = session.parms["level"]?.trim().orEmpty()
        val lines = mutableListOf(
            "INFO Android local fallback panel is serving API requests",
            "INFO Script workspace root: app sandbox scripts directory",
            "INFO Runtime mode: android_local_fallback",
            "WARN Embedded Go core may be unavailable; fallback endpoints are active"
        )
        readableDatabase.rawQuery(
            "SELECT id, status, logs_json, updated_at FROM script_runs ORDER BY updated_at DESC LIMIT 50",
            null
        ).use { cursor ->
            while (cursor.moveToNext()) {
                val runID = cursor.string("id")
                val status = cursor.string("status")
                val logs = JSONArray(cursor.string("logs_json"))
                for (index in 0 until logs.length()) {
                    lines += "INFO script[$runID][$status] ${logs.optString(index)}"
                }
            }
        }
        readableDatabase.rawQuery(
            "SELECT id, name, last_run_status, last_run_logs FROM tasks WHERE last_run_logs <> '[]' ORDER BY updated_at DESC LIMIT 50",
            null
        ).use { cursor ->
            while (cursor.moveToNext()) {
                val taskID = cursor.long("id")
                val taskName = cursor.string("name")
                val status = cursor.string("last_run_status").ifBlank { "unknown" }
                val logs = JSONArray(cursor.string("last_run_logs"))
                for (index in 0 until logs.length()) {
                    lines += "INFO task[$taskID][$taskName][$status] ${logs.optString(index)}"
                }
            }
        }
        val filtered = lines.filter { line ->
            (keyword.isEmpty() || line.contains(keyword, ignoreCase = true)) &&
                (level.isEmpty() || line.startsWith(level.uppercase(), ignoreCase = true))
        }.takeLast(maxLines)
        return ok(JSONObject().put("data", JSONObject().put("logs", JSONArray(filtered)).put("total", filtered.size)))
    }

    fun serveDependencies(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        val action = segments.getOrNull(2)
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/python-runtimes" -> pythonRuntimes()
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/deps/python-runtime-default" ->
                ok(JSONObject().put("data", JSONObject().put("version", body(session).optString("version", "3.12"))))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/pip" -> ok(JSONArray())
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/npm" ->
                ok(JSONObject().put("dependencies", JSONObject()))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/mirrors" -> mirrors()
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/deps/mirrors" -> ok(body(session))
            session.method == NanoHTTPD.Method.GET && id != null && action == "log-stream" -> dependencyLog(id)
            session.method == NanoHTTPD.Method.GET && id != null && action == "status" -> dependencyStatus(id)
            session.method == NanoHTTPD.Method.PUT && id != null && action == "cancel" ->
                updateDependencyStatus(id, "cancelled", "Dependency operation cancelled")
            session.method == NanoHTTPD.Method.PUT && id != null && action == "reinstall" ->
                updateDependencyStatus(id, "installed", "Android local dependency record restored")
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/deps/batch-delete" ->
                deleteDependencies(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/deps/batch-reinstall" ->
                reinstallDependencies(body(session))
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("dependencies", dependencyRows(session))
            session.method == NanoHTTPD.Method.POST && id == null -> createDependencies(body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("dependencies", id)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "依赖接口尚未实现")
        }
    }

    fun serveConfigs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val key = segments.getOrNull(1)?.trim().orEmpty()
        return when {
            session.method == NanoHTTPD.Method.GET && key.isBlank() -> listConfigs()
            session.method == NanoHTTPD.Method.GET && key.isNotBlank() -> getConfig(key)
            session.method == NanoHTTPD.Method.POST && key.isBlank() -> setConfig(body(session))
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/configs/batch" -> setConfigs(body(session).optJSONObject("configs") ?: JSONObject())
            session.method == NanoHTTPD.Method.DELETE && key.isNotBlank() -> deleteConfig(key)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "配置接口尚未实现")
        }
    }

    private fun listConfigs(): NanoHTTPD.Response {
        val data = JSONObject()
        readableDatabase.query("local_configs", arrayOf("key", "value"), null, null, null, null, "key ASC").use { cursor ->
            while (cursor.moveToNext()) {
                val value = cursor.string("value")
                data.put(cursor.string("key"), JSONObject().put("value", value).put("default_value", value))
            }
        }
        if (!data.has("allow_unverified_android_abi_wheels")) {
            val value = configPrefs.getString("allow_unverified_android_abi_wheels", "false") ?: "false"
            data.put("allow_unverified_android_abi_wheels", JSONObject().put("value", value).put("default_value", "false"))
        }
        if (!data.has("auto_install_deps")) {
            val value = configPrefs.getString("auto_install_deps", "false") ?: "false"
            data.put("auto_install_deps", JSONObject().put("value", value).put("default_value", "false"))
        }
        return ok(JSONObject().put("data", data))
    }

    private fun getConfig(key: String): NanoHTTPD.Response {
        val value = configValue(key, "")
        return ok(JSONObject().put("data", JSONObject().put("key", key).put("value", value).put("default_value", value)))
    }

    private fun setConfig(json: JSONObject): NanoHTTPD.Response {
        upsertConfig(json.optString("key"), json.optString("value"))
        return ok(JSONObject().put("message", "配置已保存"))
    }

    private fun setConfigs(configs: JSONObject): NanoHTTPD.Response {
        for (key in configs.keys()) upsertConfig(key, configs.optString(key))
        return ok(JSONObject().put("message", "配置已保存"))
    }

    private fun deleteConfig(key: String): NanoHTTPD.Response {
        writableDatabase.delete("local_configs", "key = ?", arrayOf(key))
        return ok(JSONObject().put("message", "配置已删除"))
    }

    private fun upsertConfig(key: String, value: String) {
        require(key.isNotBlank()) { "配置 key 不能为空" }
        val values = ContentValues().apply {
            put("key", key)
            put("value", value)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.insertWithOnConflict("local_configs", null, values, SQLiteDatabase.CONFLICT_REPLACE)
        if (key == "allow_unverified_android_abi_wheels" || key == "auto_install_deps") {
            configPrefs.edit().putString(key, value).apply()
        }
    }

    private fun configValue(key: String, fallback: String): String = readableDatabase.query(
        "local_configs",
        arrayOf("value"),
        "key = ?",
        arrayOf(key),
        null,
        null,
        null
    ).use { cursor ->
        if (cursor.moveToFirst()) cursor.string("value") else configPrefs.getString(key, fallback) ?: fallback
    }

    private fun configBool(key: String, fallback: Boolean): Boolean = when (configValue(key, if (fallback) "true" else "false").lowercase()) {
        "true", "1", "yes", "on" -> true
        "false", "0", "no", "off" -> false
        else -> fallback
    }

    fun serveBackup(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        return when {
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/system/backups" -> listBackups()
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/system/backup" -> createBackup(body(session))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/system/backup/upload" -> uploadBackup(session)
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/system/backup/download" -> downloadBackup(session)
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/system/restore" -> restoreBackup(body(session))
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/system/restore/progress" -> restoreProgress()
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "备份接口尚未实现")
        }
    }


    // ===== Dashboard (Android local summary) =====

    fun serveDashboard(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        if (session.method != NanoHTTPD.Method.GET) {
            return error(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED, "GET only")
        }
        val taskCount = readableDatabase.rawQuery("SELECT COUNT(*) FROM tasks", null).use {
            it.moveToFirst(); it.getLong(0)
        }
        val envCount = readableDatabase.rawQuery("SELECT COUNT(*) FROM envs", null).use {
            it.moveToFirst(); it.getLong(0)
        }
        val depCount = readableDatabase.rawQuery("SELECT COUNT(*) FROM local_dependencies", null).use {
            it.moveToFirst(); it.getLong(0)
        }
        val subCount = readableDatabase.rawQuery("SELECT COUNT(*) FROM local_subscriptions", null).use {
            it.moveToFirst(); it.getLong(0)
        }
        val data = JSONObject().apply {
            put("mode", "android_local")
            put("version", "0.3.15")
            put("core_status", "Kotlin fallback (Go Core requires Android <=15)")
            put("tasks", taskCount)
            put("envs", envCount)
            put("deps", depCount)
            put("subscriptions", subCount)
            put("hostname", configValue("hostname", "android-device"))
            put("platform", "arm64-v8a")
        }
        return ok(JSONObject().put("data", data))
    }



    // ===== Subscriptions (Android local) =====

    private fun ensureSubscriptionsTable(db: SQLiteDatabase) {
        db.execSQL("""
            CREATE TABLE IF NOT EXISTS local_subscriptions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL DEFAULT '',
                url TEXT NOT NULL,
                enabled INTEGER NOT NULL DEFAULT 0,
                type TEXT NOT NULL DEFAULT 'public-remote',
                last_sync TEXT,
                created_at TEXT NOT NULL DEFAULT (datetime('now')),
                updated_at TEXT NOT NULL DEFAULT (datetime('now'))
            )
        """)
    }

    fun serveSubscriptions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val uri = session.uri ?: ""
        return when {
            session.method == NanoHTTPD.Method.GET && uri == "/api/subscriptions" -> listSubscriptions()
            session.method == NanoHTTPD.Method.POST && uri == "/api/subscriptions" -> addSubscription(body(session).toString())
            session.method == NanoHTTPD.Method.DELETE && uri.startsWith("/api/subscriptions/") -> deleteSubscription(uri)
            session.method == NanoHTTPD.Method.PUT && uri.startsWith("/api/subscriptions/refresh/") -> refreshSubscription(uri)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "subscription route not found")
        }
    }

    private fun listSubscriptions(): NanoHTTPD.Response {
        val rows = queryRows("SELECT * FROM local_subscriptions ORDER BY id DESC") { cursor ->
            JSONObject().apply {
                put("id", cursor.long("id"))
                put("name", cursor.string("name"))
                put("url", cursor.string("url"))
                put("enabled", cursor.int("enabled") == 1)
                put("type", cursor.string("type"))
                put("last_sync", cursor.string("last_sync"))
                put("created_at", cursor.string("created_at"))
                put("updated_at", cursor.string("updated_at"))
            }
        }
        return ok(JSONObject().put("data", rows))
    }

    private fun addSubscription(body: String): NanoHTTPD.Response {
        val json = JSONObject(body)
        val values = ContentValues().apply {
            put("name", json.optString("name", ""))
            put("url", json.optString("url", ""))
            put("enabled", if (json.optBoolean("enabled", true)) 1 else 0)
            put("type", json.optString("type", "public-remote"))
        }
        val id = writableDatabase.insert("local_subscriptions", null, values)
        return if (id > 0) {
            ok(JSONObject().put("data", JSONObject().put("id", id)))
        } else {
            error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "add failed")
        }
    }

    private fun deleteSubscription(uri: String): NanoHTTPD.Response {
        val id = uri.substringAfterLast("/").toLongOrNull() ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "invalid id")
        writableDatabase.delete("local_subscriptions", "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("deleted", id)))
    }

    private fun refreshSubscription(uri: String): NanoHTTPD.Response {
        val id = uri.substringAfterLast("/").toLongOrNull() ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "invalid id")
        writableDatabase.execSQL("UPDATE local_subscriptions SET last_sync = datetime('now'), updated_at = datetime('now') WHERE id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("refreshed", id).put("last_sync", System.currentTimeMillis().toString())))
    }


    private fun taskRows(): JSONArray = queryRows(
        "SELECT * FROM tasks ORDER BY id DESC"
    ) { cursor ->
        JSONObject()
            .put("id", cursor.long("id"))
            .put("name", cursor.string("name"))
            .put("command", cursor.string("command"))
            .put("cron_expression", cursor.string("cron_expression"))
            .put("task_type", cursor.string("task_type"))
            .put("python_version", cursor.string("python_version"))
            .put("status", cursor.double("status"))
            .put("labels", JSONArray(cursor.string("labels")))
            .put("last_run_status", cursor.string("last_run_status"))
            .put("created_at", cursor.string("created_at"))
            .put("updated_at", cursor.string("updated_at"))
    }

    private fun envRows(): JSONArray = queryRows(
        "SELECT * FROM envs ORDER BY sort_order ASC, id DESC"
    ) { cursor ->
        val groups = JSONArray(cursor.string("groups_json"))
        JSONObject()
            .put("id", cursor.long("id"))
            .put("name", cursor.string("name"))
            .put("value", cursor.string("value"))
            .put("remarks", cursor.string("remarks"))
            .put("enabled", cursor.int("enabled") == 1)
            .put("group", (0 until groups.length()).joinToString(",") { groups.optString(it) })
            .put("groups", groups)
            .put("sort_order", cursor.int("sort_order"))
            .put("created_at", cursor.string("created_at"))
            .put("updated_at", cursor.string("updated_at"))
    }

    private fun dependencyRows(session: NanoHTTPD.IHTTPSession): JSONArray {
        val type = session.parms["type"]?.trim().orEmpty()
        val pythonVersion = session.parms["python_version"]?.trim().orEmpty()
        val clauses = mutableListOf<String>()
        val args = mutableListOf<String>()
        if (type.isNotEmpty()) {
            clauses += "type = ?"
            args += type
        }
        if (type == "python" && pythonVersion.isNotEmpty()) {
            clauses += "python_version = ?"
            args += pythonVersion
        }
        val where = if (clauses.isEmpty()) "" else " WHERE ${clauses.joinToString(" AND ")}"
        return queryRows(
            "SELECT * FROM dependencies$where ORDER BY id DESC",
            args.toTypedArray()
        ) { cursor ->
            JSONObject()
                .put("id", cursor.long("id"))
                .put("name", cursor.string("name"))
                .put("type", cursor.string("type"))
                .put("python_version", cursor.string("python_version"))
                .put("version", cursor.string("version"))
                .put("status", cursor.string("status"))
                .put("created_at", cursor.string("created_at"))
                .put("updated_at", cursor.string("updated_at"))
        }
    }

    private fun scriptsRoot(): File = File(appContext.filesDir, "scripts").apply { mkdirs() }

    private fun backupsRoot(): File = File(appContext.filesDir, "backups").apply { mkdirs() }

    private fun restoreTargetRoot(): File = File(appContext.filesDir, "portable-restore").apply { mkdirs() }

    private fun listBackups(): NanoHTTPD.Response {
        val files = JSONArray()
        backupsRoot().listFiles()
            ?.filter { it.isFile && it.name.endsWith(".enc") }
            ?.sortedByDescending { it.lastModified() }
            ?.forEach { file ->
                files.put(
                    JSONObject()
                        .put("filename", file.name)
                        .put("size", file.length())
                        .put("created_at", Instant.ofEpochMilli(file.lastModified()).toString()),
                )
            }
        return ok(JSONObject().put("data", files))
    }

    private fun createBackup(json: JSONObject): NanoHTTPD.Response {
        val password = json.optString("password")
        if (password.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "Android 本地备份需要密码")
        val source = buildPortableBackupSource()
        val runtimeRequirements = JSONObject()
            .put("schemaVersion", SCHEMA_VERSION)
            .put("runtimeManifest", "manifest.json")
            .put("platform", "android")
        val bytes = PortableBackupEnvelope().exportDirectory(source, password.toCharArray(), runtimeRequirements)
        val filename = "daidai-android-${DateTimeFormatter.ISO_INSTANT.format(Instant.now()).replace(':', '-')}.enc"
        val output = File(backupsRoot(), filename)
        output.outputStream().use { stream ->
            stream.write(bytes)
            stream.fd.sync()
        }
        source.deleteRecursively()
        return ok(
            JSONObject()
                .put("filename", filename)
                .put("size", output.length())
                .put("encrypted", true)
                .put("envelope", PortableBackupEnvelope.readManifest(bytes)),
        )
    }

    private fun downloadBackup(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val file = backupFile(session.parms["filename"].orEmpty())
            ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "备份文件不存在")
        return NanoHTTPD.newFixedLengthResponse(
            NanoHTTPD.Response.Status.OK,
            "application/octet-stream",
            file.inputStream(),
            file.length(),
        ).apply {
            addHeader("Content-Disposition", "attachment; filename=\"${file.name}\"")
        }
    }

    private fun uploadBackup(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val files = HashMap<String, String>()
        session.parseBody(files)
        val uploaded = files["file"] ?: files["content"] ?: files.values.firstOrNull()
            ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "缺少备份文件")
        val originalName = session.parms["filename"]
            ?: File(uploaded).name.takeIf { it.endsWith(".enc") }
            ?: "imported-${System.nanoTime()}.enc"
        val clean = originalName.substringAfterLast('/').substringAfterLast('\\')
            .let { if (it.endsWith(".enc")) it else "$it.enc" }
        val bytes = File(uploaded).readBytes()
        PortableBackupEnvelope.readManifest(bytes)
        val output = File(backupsRoot(), clean)
        output.outputStream().use { stream ->
            stream.write(bytes)
            stream.fd.sync()
        }
        return ok(JSONObject().put("filename", output.name).put("size", output.length()).put("encrypted", true))
    }

    private fun restoreBackup(json: JSONObject): NanoHTTPD.Response {
        val filename = json.optString("filename")
        val password = json.optString("password")
        if (password.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "恢复加密备份需要密码")
        val file = backupFile(filename) ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "备份文件不存在")
        return try {
            PortableBackupEnvelope().restoreDirectory(file.readBytes(), password.toCharArray(), restoreTargetRoot())
            ok(
                JSONObject()
                    .put("status", "completed")
                    .put("stage", "atomic_restore")
                    .put("filename", filename)
                    .put("message", "备份已完成解密、校验和原子恢复暂存"),
            )
        } catch (_: WrongBackupPasswordException) {
            error(NanoHTTPD.Response.Status.UNAUTHORIZED, "备份密码错误")
        }
    }

    private fun restoreProgress(): NanoHTTPD.Response = ok(
        JSONObject()
            .put("active", false)
            .put("status", "idle")
            .put("stage", "idle")
            .put("percent", 100)
            .put("source", "android_portable_envelope"),
    )

    private fun backupFile(filename: String): File? {
        val clean = filename.substringAfterLast('/').substringAfterLast('\\')
        if (clean.isBlank() || !clean.endsWith(".enc")) return null
        val file = File(backupsRoot(), clean).canonicalFile
        return file.takeIf { it.path.startsWith(backupsRoot().canonicalPath) && it.isFile }
    }

    private fun buildPortableBackupSource(): File {
        val source = File(appContext.cacheDir, "portable-backup-${System.nanoTime()}").apply { mkdirs() }
        readableDatabase.rawQuery("PRAGMA wal_checkpoint(FULL)", null).use { it.moveToFirst() }
        readableDatabase.path?.let { path ->
            val database = File(path)
            if (database.isFile) database.copyTo(File(source, "database/daidai-local.db").apply { parentFile?.mkdirs() }, overwrite = true)
            listOf("-wal", "-shm").forEach { suffix ->
                val companion = File("$path$suffix")
                if (companion.isFile) {
                    companion.copyTo(
                        File(source, "database/${database.name}$suffix").apply { parentFile?.mkdirs() },
                        overwrite = true,
                    )
                }
            }
        }
        if (scriptsRoot().exists()) {
            scriptsRoot().walkTopDown().filter { it.isFile }.forEach { file ->
                val relative = scriptsRoot().toPath().relativize(file.toPath()).joinToString("/") { it.toString() }
                file.copyTo(File(source, "scripts/$relative").apply { parentFile?.mkdirs() }, overwrite = true)
            }
        }
        File(source, "manifest/runtime-requirements.json").apply {
            parentFile?.mkdirs()
            writeText(JSONObject().put("schemaVersion", SCHEMA_VERSION).put("platform", "android").toString())
        }
        return source
    }

    private fun scriptRows(): JSONArray {
        val rows = JSONArray()
        scriptsRoot().walkTopDown().filter { it.isFile }.forEach { file ->
            val rel = file.relativeTo(scriptsRoot()).path.replace(File.separatorChar, '/')
            rows.put(scriptFileJson(rel, file))
        }
        return rows
    }

    private fun scriptTree(): NanoHTTPD.Response = ok(JSONObject().put("data", buildScriptTree(scriptsRoot())))

    private fun buildScriptTree(dir: File, prefix: String = ""): JSONArray {
        val result = JSONArray()
        val entries = dir.listFiles()?.sortedWith(compareBy<File> { !it.isDirectory }.thenBy { it.name.lowercase() }).orEmpty()
        for (entry in entries) {
            val rel = if (prefix.isBlank()) entry.name else "$prefix/${entry.name}"
            if (entry.isDirectory) {
                result.put(
                    JSONObject()
                        .put("key", rel)
                        .put("title", entry.name)
                        .put("isLeaf", false)
                        .put("type", "directory")
                        .put("children", buildScriptTree(entry, rel))
                )
            } else {
                result.put(
                    JSONObject()
                        .put("key", rel)
                        .put("title", entry.name)
                        .put("isLeaf", true)
                        .put("type", "file")
                        .put("extension", entry.extension.lowercase().let { if (it.isBlank()) "" else ".$it" })
                        .put("size", entry.length())
                        .put("mtime", entry.lastModified() / 1000.0)
                )
            }
        }
        return result
    }

    private fun scriptContent(path: String): NanoHTTPD.Response {
        val file = scriptFile(path)
        if (!file.exists() || !file.isFile) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        return ok(JSONObject().put("data", JSONObject().put("path", path).put("content", file.readText()).put("is_binary", false)))
    }

    private fun downloadScript(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val path = session.parms["path"].orEmpty()
        val file = scriptFile(path)
        if (!file.exists() || !file.isFile) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        return NanoHTTPD.newFixedLengthResponse(
            NanoHTTPD.Response.Status.OK,
            "application/octet-stream",
            file.inputStream(),
            file.length()
        ).apply {
            addHeader("Content-Disposition", "attachment; filename=\"${file.name}\"")
            addHeader("Cache-Control", "no-store")
        }
    }

    private fun saveScriptContent(json: JSONObject): NanoHTTPD.Response {
        val path = json.optString("path", json.optString("filename", "script.py")).ifBlank { "script.py" }
        val file = scriptFile(path)
        file.parentFile?.mkdirs()
        val content = json.optString("content")
        file.writeText(content)
        val version = recordScriptVersion(path, content, json.optString("message", "V1 初始版本"))
        return ok(JSONObject().put("message", "保存成功").put("version", version).put("data", scriptFileJson(path, file)))
    }

    private fun createScriptDirectory(json: JSONObject): NanoHTTPD.Response {
        val path = json.optString("path", json.optString("name", "new-folder"))
        val dir = scriptFile(path)
        dir.mkdirs()
        return ok(JSONObject().put("data", JSONObject().put("path", path)))
    }

    private fun uploadScript(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val files = HashMap<String, String>()
        session.parseBody(files)
        val uploaded = files["file"] ?: files.values.firstOrNull()
        val originalName = firstPresent(
            decodeBase64Utf8(session.parms["filename_b64"]),
            session.parms["filename"],
            session.parms["file"],
            uploaded?.let { File(it).name }
        ) ?: "script.txt"
        val dir = cleanScriptPath(session.parms["dir"].orEmpty())
        val path = cleanScriptPath(listOf(dir, originalName).filter(String::isNotBlank).joinToString("/"))
            .ifBlank { "script.txt" }
        val content = when {
            uploaded != null && File(uploaded).isFile -> File(uploaded).readText()
            files["postData"] != null -> files["postData"].orEmpty()
            else -> ""
        }
        val file = scriptFile(path)
        file.parentFile?.mkdirs()
        file.writeText(content)
        recordScriptVersion(path, content, "上传文件")
        return ok(JSONObject().put("message", "上传成功").put("path", path).put("paths", JSONArray().put(path)).put("uploaded_count", 1))
    }

    private fun deleteScript(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val path = session.parms["path"].orEmpty()
        val type = session.parms["type"].orEmpty()
        val file = scriptFile(path)
        if (!file.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        if (type == "directory" || file.isDirectory) file.deleteRecursively() else file.delete()
        return ok(JSONObject().put("message", "删除成功"))
    }

    private fun renameScript(json: JSONObject): NanoHTTPD.Response {
        val oldPath = cleanScriptPath(json.optString("old_path"))
        val newName = cleanScriptPath(json.optString("new_name"))
        if (oldPath.isBlank() || newName.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "路径不能为空")
        val source = scriptFile(oldPath)
        if (!source.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val targetPath = listOf(oldPath.substringBeforeLast('/', ""), newName.substringAfterLast('/')).filter(String::isNotBlank).joinToString("/")
        val target = scriptFile(targetPath)
        target.parentFile?.mkdirs()
        if (!source.renameTo(target)) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "重命名失败")
        return ok(JSONObject().put("message", "重命名成功").put("new_path", targetPath))
    }

    private fun moveScript(json: JSONObject): NanoHTTPD.Response {
        val sourcePath = cleanScriptPath(json.optString("source_path"))
        val targetDir = cleanScriptPath(json.optString("target_dir")).trim('/')
        val source = scriptFile(sourcePath)
        if (!source.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val targetPath = listOf(targetDir, source.name).filter(String::isNotBlank).joinToString("/")
        val target = scriptFile(targetPath)
        target.parentFile?.mkdirs()
        if (!source.renameTo(target)) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "移动失败")
        return ok(JSONObject().put("message", "移动成功").put("path", targetPath))
    }

    private fun copyScript(json: JSONObject): NanoHTTPD.Response {
        val sourcePath = cleanScriptPath(json.optString("source_path"))
        val targetPath = cleanScriptPath(json.optString("target_path"))
        val source = scriptFile(sourcePath)
        if (!source.exists() || !source.isFile) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val target = scriptFile(targetPath)
        target.parentFile?.mkdirs()
        source.copyTo(target, overwrite = true)
        return ok(JSONObject().put("message", "复制成功").put("path", targetPath))
    }

    private fun batchDeleteScripts(json: JSONObject): NanoHTTPD.Response {
        val paths = json.optJSONArray("paths") ?: JSONArray()
        for (index in 0 until paths.length()) {
            val file = scriptFile(paths.optString(index))
            if (file.exists()) {
                if (file.isDirectory) file.deleteRecursively() else file.delete()
            }
        }
        return ok(JSONObject().put("message", "删除成功"))
    }

    private fun formatScript(json: JSONObject): NanoHTTPD.Response = ok(
        JSONObject().put("data", JSONObject().put("content", json.optString("content")).put("formatter", "android-local"))
    )

    private fun runCode(json: JSONObject): NanoHTTPD.Response {
        val language = json.optString("language", "python")
        val code = json.optString("code")
        val runId = "android-local-${Instant.now().toEpochMilli()}"
        if (code.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "运行代码不能为空")
        val ext = when (language.lowercase()) {
            "javascript", "node", "nodejs" -> ".js"
            "typescript" -> ".ts"
            "shell", "sh", "bash" -> ".sh"
            "go" -> ".go"
            else -> ".py"
        }
        val temp = File(appContext.cacheDir, "script-runs/$runId$ext").apply { parentFile?.mkdirs() }
        temp.writeText(code)
        val result = executeScriptFile(temp, "inline-$language$ext", language)
        saveScriptRun(runId, result.status, result.logs, result.done, result.exitCode)
        temp.delete()
        return ok(scriptRunResponse(runId, result))
    }

    private fun runScript(json: JSONObject): NanoHTTPD.Response {
        val path = json.optString("path", "script.py")
        val file = scriptFile(path)
        if (!file.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val runId = "android-local-${Instant.now().toEpochMilli()}"
        val result = executeScriptFile(file, path, json.optString("language"))
        saveScriptRun(runId, result.status, result.logs, result.done, result.exitCode)
        return ok(scriptRunResponse(runId, result))
    }

    private fun scriptRunResponse(runId: String, result: LocalScriptResult): JSONObject = JSONObject()
        .put("message", "脚本已执行")
        .put("run_id", runId)
        .put(
            "data",
            JSONObject()
                .put("run_id", runId)
                .put("status", result.status)
                .put("logs", result.logs)
                .put("done", result.done)
                .put("exit_code", result.exitCode)
        )

    private fun executeScriptFile(file: File, displayPath: String, languageHint: String = ""): LocalScriptResult {
        val logs = JSONArray().put("Android local fallback executing script: $displayPath")
        val command = scriptCommand(file, displayPath, languageHint)
            ?: return LocalScriptResult(
                logs.put("Missing Android runtime for script type: ${file.extension.ifBlank { languageHint.ifBlank { "unknown" } }}"),
                "failed",
                true,
                127,
            )

        return runLocalProcess(command, file.parentFile ?: scriptsRoot(), logs).also { result ->
            recordDetectedDependencies(result.logs)
        }
    }

    private fun scriptCommand(file: File, displayPath: String, languageHint: String): List<String>? {
        val ext = file.extension.lowercase()
        val nativeDir = appContext.applicationInfo.nativeLibraryDir.orEmpty()
        fun native(name: String): String? = File(nativeDir, name).takeIf { it.isFile && isRuntimeEntryVerified(it) }?.absolutePath
        return when {
            ext == "sh" || languageHint.equals("shell", ignoreCase = true) -> listOf("/system/bin/sh", file.absolutePath)
            ext == "py" || languageHint.equals("python", ignoreCase = true) -> AndroidPythonRuntime.ensureReady(appContext)?.let { listOf(it.executable, it.home, file.absolutePath) }
            ext == "js" || ext == "mjs" || languageHint.equals("javascript", ignoreCase = true) -> AndroidNodeRuntime.ensureReady(appContext)?.let { listOf(it.executable, file.absolutePath) }
            ext == "ts" || languageHint.equals("typescript", ignoreCase = true) -> AndroidNodeRuntime.ensureReady(appContext)?.let { listOf(it.executable, "-e", typeScriptEvalCode(), file.absolutePath) }
            ext == "go" || languageHint.equals("go", ignoreCase = true) -> native("libyaegi_exec.so")?.let { listOf(it, file.absolutePath) }
            else -> listOf("/system/bin/sh", file.absolutePath).takeIf { displayPath.endsWith(".sh") }
        }
    }

    private fun isRuntimeEntryVerified(file: File): Boolean {
        if (!file.isFile) return false
        val sample = runCatching { file.inputStream().use { it.readBytes().toString(Charsets.ISO_8859_1) } }.getOrDefault("")
        if (sample.contains("RUNTIME_STUB_OK")) return false
        return true
    }

    private fun typeScriptEvalCode(): String =
        "const fs=require('fs');const vm=require('vm');const ts=require('typescript');" +
            "const file=process.argv[1];const code=fs.readFileSync(file,'utf8');" +
            "const out=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;" +
            "vm.runInThisContext(out,{filename:file});"

    private fun runLocalProcess(command: List<String>, workingDir: File, logs: JSONArray): LocalScriptResult {
        logs.put("Command: ${command.first().substringAfterLast('/')}")
        return try {
            val process = ProcessBuilder(command)
                .directory(workingDir)
                .redirectErrorStream(true)
                .apply { environment().putAll(runtimeEnvironment()) }
                .start()
            val output = Collections.synchronizedList(mutableListOf<String>())
            val reader = Thread {
                process.inputStream.bufferedReader().useLines { lines ->
                    lines.forEach { output += it }
                }
            }.also { it.start() }
            val finished = process.waitFor(60, TimeUnit.SECONDS)
            if (!finished) {
                process.destroyForcibly()
                reader.join(1_000)
                output.forEach { logs.put(it) }
                logs.put("Script timed out after 60 seconds")
                LocalScriptResult(logs, "failed", true, 124)
            } else {
                reader.join(1_000)
                output.forEach { logs.put(it) }
                val exit = process.exitValue()
                if (exit == 0) logs.put("Script completed successfully") else logs.put("Script failed with exit code $exit")
                LocalScriptResult(logs, if (exit == 0) "success" else "failed", true, exit)
            }
        } catch (error: Exception) {
            logs.put("Script start failed: ${error.message ?: error.javaClass.simpleName}")
            LocalScriptResult(logs, "failed", true, 127)
        }
    }

    private fun runtimeEnvironment(): MutableMap<String, String> {
        val env = mutableMapOf(
            "HOME" to appContext.filesDir.absolutePath,
            "TMPDIR" to appContext.cacheDir.absolutePath,
            "DAIDAI_ANDROID_LOCAL" to "1",
            "DAIDAI_ALLOW_UNVERIFIED_ANDROID_ABI_WHEELS" to if (configBool("allow_unverified_android_abi_wheels", false)) "1" else "0",
            "DAIDAI_TEST_AUTO_INSTALL" to if (configBool("auto_install_deps", false)) "1" else "0",
            "LD_LIBRARY_PATH" to appContext.applicationInfo.nativeLibraryDir.orEmpty(),
            "PYTHONPATH" to AndroidPythonRuntime.depsDir(appContext).absolutePath,
            "PIP_TARGET" to AndroidPythonRuntime.depsDir(appContext).absolutePath,
            "NODE_PATH" to listOf(
                AndroidNodeRuntime.ensureReady(appContext)?.modules.orEmpty(),
                File(appContext.filesDir, "deps/nodejs/lib/node_modules").absolutePath,
            ).filter(String::isNotBlank).joinToString(File.pathSeparator),
            "NPM_CONFIG_IGNORE_SCRIPTS" to "true",
        )
        AndroidNodeRuntime.ensureReady(appContext)?.let { runtime ->
            env["NPM_CONFIG_GLOBALCONFIG"] = File(runtime.home, "etc/npmrc").absolutePath
        }
        readableDatabase.query(
            "envs",
            arrayOf("name", "value"),
            "enabled = 1",
            null,
            null,
            null,
            null
        ).use { cursor ->
            while (cursor.moveToNext()) {
                val name = cursor.string("name").trim()
                if (name.matches(Regex("[A-Za-z_][A-Za-z0-9_]*"))) env[name] = cursor.string("value")
            }
        }
        return env
    }

    private fun recordDetectedDependencies(logs: JSONArray) {
        val missingPython = Regex("ModuleNotFoundError: No module named ['\"]([^'\"]+)['\"]")
        for (index in 0 until logs.length()) {
            val match = missingPython.find(logs.optString(index)) ?: continue
            recordDependency(match.groupValues[1], "python", "missing", "Detected missing Python module during script execution")
        }
    }

    private fun recordDependency(name: String, type: String, status: String, log: String) {
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("name", name)
            put("type", type)
            put("status", status)
            put("log", log)
            put("created_at", now)
            put("updated_at", now)
        }
        writableDatabase.insert("dependencies", null, values)
    }

    private fun scriptRunLogs(runId: String): NanoHTTPD.Response {
        return readableDatabase.query(
            "script_runs",
            arrayOf("status", "logs_json", "done", "exit_code"),
            "id = ?",
            arrayOf(runId),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use error(NanoHTTPD.Response.Status.NOT_FOUND, "运行记录不存在")
            val payload = JSONObject()
                .put("logs", JSONArray(cursor.string("logs_json")))
                .put("done", cursor.int("done") == 1)
                .put("status", cursor.string("status"))
            if (!cursor.isNull(cursor.getColumnIndexOrThrow("exit_code"))) {
                payload.put("exit_code", cursor.int("exit_code"))
            }
            ok(JSONObject().put("data", payload))
        }
    }

    private fun stopScriptRun(runId: String): NanoHTTPD.Response {
        val logs = JSONArray().put("Android local fallback script run stopped")
        val values = ContentValues().apply {
            put("status", "stopped")
            put("logs_json", logs.toString())
            put("done", 1)
            put("exit_code", 130)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("script_runs", values, "id = ?", arrayOf(runId))
        return ok(JSONObject().put("message", "已停止"))
    }

    private fun clearScriptRun(runId: String): NanoHTTPD.Response {
        writableDatabase.delete("script_runs", "id = ?", arrayOf(runId))
        return ok(JSONObject().put("message", "已清除"))
    }

    private fun saveScriptRun(runId: String, status: String, logs: JSONArray, done: Boolean, exitCode: Int?) {
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("id", runId)
            put("status", status)
            put("logs_json", logs.toString())
            put("done", if (done) 1 else 0)
            if (exitCode == null) putNull("exit_code") else put("exit_code", exitCode)
            put("created_at", now)
            put("updated_at", now)
        }
        writableDatabase.insertWithOnConflict("script_runs", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    private fun recordScriptVersion(path: String, content: String, message: String): Int {
        val normalized = cleanScriptPath(path)
        val version = readableDatabase.rawQuery(
            "SELECT COALESCE(MAX(version), 0) + 1 FROM script_versions WHERE script_path = ?",
            arrayOf(normalized)
        ).use { cursor -> if (cursor.moveToFirst()) cursor.getInt(0) else 1 }
        val values = ContentValues().apply {
            put("script_path", normalized)
            put("content", content)
            put("version", version)
            put("message", message.ifBlank { "V$version 更新" })
            put("created_at", Instant.now().toString())
        }
        writableDatabase.insertOrThrow("script_versions", null, values)
        return version
    }

    private fun listScriptVersions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val path = cleanScriptPath(session.parms["path"].orEmpty())
        val rows = JSONArray()
        readableDatabase.query(
            "script_versions",
            arrayOf("id", "script_path", "content", "version", "message", "created_at"),
            "script_path = ?",
            arrayOf(path),
            null,
            null,
            "version DESC",
            "50"
        ).use { cursor ->
            while (cursor.moveToNext()) {
                rows.put(
                    JSONObject()
                        .put("id", cursor.long("id"))
                        .put("script_path", cursor.string("script_path"))
                        .put("version", cursor.int("version"))
                        .put("message", cursor.string("message"))
                        .put("content_length", cursor.string("content").length)
                        .put("created_at", cursor.string("created_at"))
                )
            }
        }
        return ok(JSONObject().put("data", rows))
    }

    private fun clearScriptVersions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val path = cleanScriptPath(session.parms["path"].orEmpty())
        val deleted = writableDatabase.delete("script_versions", "script_path = ?", arrayOf(path))
        return ok(JSONObject().put("message", "版本历史已清空").put("cleared_count", deleted))
    }

    private fun getScriptVersion(id: Long?): NanoHTTPD.Response {
        if (id == null) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "版本 ID 无效")
        return readableDatabase.query(
            "script_versions",
            arrayOf("id", "script_path", "content", "version", "message", "created_at"),
            "id = ?",
            arrayOf(id.toString()),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use error(NanoHTTPD.Response.Status.NOT_FOUND, "版本不存在")
            ok(
                JSONObject().put(
                    "data",
                    JSONObject()
                        .put("id", cursor.long("id"))
                        .put("script_path", cursor.string("script_path"))
                        .put("content", cursor.string("content"))
                        .put("version", cursor.int("version"))
                        .put("message", cursor.string("message"))
                        .put("content_length", cursor.string("content").length)
                        .put("created_at", cursor.string("created_at"))
                )
            )
        }
    }

    private fun rollbackScriptVersion(id: Long?): NanoHTTPD.Response {
        if (id == null) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "版本 ID 无效")
        return readableDatabase.query(
            "script_versions",
            arrayOf("script_path", "content", "version"),
            "id = ?",
            arrayOf(id.toString()),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use error(NanoHTTPD.Response.Status.NOT_FOUND, "版本不存在")
            val path = cursor.string("script_path")
            val content = cursor.string("content")
            val file = scriptFile(path)
            file.parentFile?.mkdirs()
            file.writeText(content)
            val version = recordScriptVersion(path, content, "回滚到 v${cursor.int("version")}")
            ok(JSONObject().put("message", "回滚成功").put("version", version))
        }
    }

    private fun scriptFile(path: String): File {
        val clean = path.replace('\\', '/').split('/').filter { it.isNotBlank() && it != "." && it != ".." }.joinToString(File.separator)
        return File(scriptsRoot(), clean.ifBlank { "script.py" }).canonicalFile.also {
            require(it.path.startsWith(scriptsRoot().canonicalPath)) { "invalid script path" }
        }
    }

    private fun cleanScriptPath(path: String): String = runCatching { URLDecoder.decode(path, "UTF-8") }.getOrElse { path }
        .replace('\\', '/')
        .split('/')
        .map(String::trim)
        .filter { it.isNotBlank() && it != "." && it != ".." }
        .joinToString("/")

    private fun firstPresent(vararg values: String?): String? = values
        .map { it?.trim().orEmpty() }
        .firstOrNull { it.isNotEmpty() }

    private fun decodeBase64Utf8(value: String?): String? = runCatching {
        val raw = value?.trim().orEmpty()
        if (raw.isEmpty()) return@runCatching null
        String(Base64.decode(raw, Base64.DEFAULT), Charsets.UTF_8)
    }.getOrNull()

    private fun scriptFileJson(path: String, file: File): JSONObject = JSONObject()
        .put("path", path)
        .put("name", file.name)
        .put("type", "file")
        .put("size", file.length())
        .put("created_at", Instant.ofEpochMilli(file.lastModified()).toString())
        .put("updated_at", Instant.ofEpochMilli(file.lastModified()).toString())

    private fun createTask(json: JSONObject): NanoHTTPD.Response {
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("name", json.optString("name", "未命名任务"))
            put("command", json.optString("command"))
            put("cron_expression", json.optString("cron_expression"))
            put("task_type", json.optString("task_type", "manual"))
            put("python_version", json.optString("python_version"))
            put("status", json.optDouble("status", 1.0))
            put("labels", json.optJSONArray("labels")?.toString() ?: "[]")
            put("created_at", now)
            put("updated_at", now)
        }
        val id = writableDatabase.insertOrThrow("tasks", null, values)
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun updateTask(id: Long, json: JSONObject): NanoHTTPD.Response {
        val values = ContentValues().apply {
            listOf("name", "command", "cron_expression", "task_type", "python_version").forEach { key ->
                if (json.has(key)) put(key, json.optString(key))
            }
            if (json.has("status")) put("status", json.optDouble("status"))
            if (json.has("labels")) put("labels", json.optJSONArray("labels")?.toString() ?: "[]")
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun updateTaskStatus(id: Long, action: String): NanoHTTPD.Response {
        val status = when (action) {
            "disable" -> 0.0
            "run" -> 2.0
            else -> 1.0
        }
        val values = ContentValues().apply {
            put("status", status)
            if (action == "stop") put("last_run_status", "aborted")
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
        if (action == "run") {
            val startedAt = Instant.now()
            val result = runTaskNow(id)
            val endedAt = Instant.now()
            val logId = insertTaskLog(id, result, startedAt, endedAt)
            values.clear()
            values.put("status", 1.0)
            values.put("last_run_status", result.status)
            values.put("last_run_logs", result.logs.toString())
            values.put("last_log_id", logId)
            values.put("updated_at", Instant.now().toString())
            writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
            return ok(JSONObject().put("message", "任务已执行").put("data", JSONObject().put("id", id).put("status", 1.0).put("run_status", result.status).put("log_id", logId).put("logs", result.logs)))
        }
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("status", status)))
    }

    private fun insertTaskLog(taskId: Long, result: LocalScriptResult, startedAt: Instant, endedAt: Instant): Long {
        val content = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        val statusCode = when (result.status) {
            "success" -> 0
            "running" -> 2
            else -> 1
        }
        val values = ContentValues().apply {
            put("task_id", taskId)
            put("content", content)
            put("logs_json", result.logs.toString())
            put("status", statusCode)
            if (result.exitCode == null) putNull("exit_code") else put("exit_code", result.exitCode)
            put("duration", (endedAt.toEpochMilli() - startedAt.toEpochMilli()) / 1000.0)
            put("started_at", startedAt.toString())
            put("ended_at", endedAt.toString())
            put("created_at", startedAt.toString())
        }
        return writableDatabase.insertOrThrow("task_logs_local", null, values)
    }

    private fun runTaskNow(id: Long): LocalScriptResult {
        return readableDatabase.query(
            "tasks",
            arrayOf("command", "name"),
            "id = ?",
            arrayOf(id.toString()),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) {
                return@use LocalScriptResult(JSONArray().put("Task not found"), "failed", true, 404)
            }
            val command = cursor.string("command").trim()
            val path = when {
                command.startsWith("task ") -> command.removePrefix("task ").trim()
                command.startsWith("script ") -> command.removePrefix("script ").trim()
                command.contains("/") || command.contains(".") -> command
                else -> ""
            }
            if (path.isBlank()) {
                return@use LocalScriptResult(JSONArray().put("Android local fallback only supports task commands in the form: task <script-path>"), "failed", true, 2)
            }
            val file = scriptFile(path)
            if (!file.exists()) {
                return@use LocalScriptResult(JSONArray().put("Script does not exist: $path"), "failed", true, 404)
            }
            executeScriptFile(file, path)
        }
    }

    private fun serveTaskBatch(
        session: NanoHTTPD.IHTTPSession,
        action: String?
    ): NanoHTTPD.Response {
        val json = body(session)
        val ids = json.optJSONArray("task_ids") ?: JSONArray()
        val values = ContentValues().apply {
            when (action) {
                "enable" -> put("status", 1.0)
                "disable" -> put("status", 0.0)
                "run" -> {
                    put("status", 1.0)
                    put("last_run_status", "failed")
                }
            }
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.beginTransaction()
        try {
            for (index in 0 until ids.length()) {
                val id = ids.optLong(index)
                if (action == "delete") {
                    writableDatabase.delete("tasks", "id = ?", arrayOf(id.toString()))
                } else {
                    writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
                }
            }
            writableDatabase.setTransactionSuccessful()
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids)))
    }

    private fun latestTaskLogResponse(id: Long): NanoHTTPD.Response {
        val payload = latestTaskLogJson(id)
            ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "该任务还没有日志记录")
        return ok(payload)
    }

    private fun liveTaskLogResponse(id: Long): NanoHTTPD.Response {
        val payload = latestTaskLogJson(id)
            ?: return ok(JSONObject().put("data", JSONObject().put("task_id", id).put("status", 2).put("done", false).put("logs", JSONArray()).put("content", "")))
        return ok(payload)
    }

    private fun latestTaskLogJson(taskId: Long): JSONObject? {
        return readableDatabase.query(
            "task_logs_local",
            arrayOf("id", "task_id", "content", "logs_json", "status", "exit_code", "duration", "started_at", "ended_at", "created_at"),
            "task_id = ?",
            arrayOf(taskId.toString()),
            null,
            null,
            "id DESC",
            "1"
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use null
            taskLogJson(cursor)
        }
    }

    private fun taskLogByIdJson(logId: Long): JSONObject? {
        return readableDatabase.query(
            "task_logs_local",
            arrayOf("id", "task_id", "content", "logs_json", "status", "exit_code", "duration", "started_at", "ended_at", "created_at"),
            "id = ?",
            arrayOf(logId.toString()),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use null
            taskLogJson(cursor)
        }
    }

    private fun taskLogJson(cursor: Cursor): JSONObject {
        val logs = runCatching { JSONArray(cursor.string("logs_json")) }.getOrDefault(JSONArray())
        return JSONObject()
            .put("id", cursor.long("id"))
            .put("task_id", cursor.long("task_id"))
            .put("content", cursor.string("content"))
            .put("logs", logs)
            .put("status", cursor.int("status"))
            .put("exit_code", if (cursor.isNull(cursor.getColumnIndexOrThrow("exit_code"))) JSONObject.NULL else cursor.int("exit_code"))
            .put("duration", cursor.double("duration"))
            .put("started_at", cursor.string("started_at"))
            .put("ended_at", cursor.string("ended_at"))
            .put("created_at", cursor.string("created_at"))
            .let { payload -> JSONObject(payload.toString()).put("data", payload) }
    }

    private fun taskLogStream(id: Long): NanoHTTPD.Response {
        val payloadJson = latestTaskLogJson(id)?.optJSONObject("data")
        val logs = payloadJson?.optJSONArray("logs") ?: JSONArray().put("Task log not found")
        val payload = StringBuilder()
        for (index in 0 until logs.length()) {
            payload.append("data: ").append(logs.optString(index).replace("\n", "\\n")).append("\n\n")
        }
        payload.append("event: done\n")
        payload.append("data: finished\n\n")
        return NanoHTTPD.newFixedLengthResponse(
            NanoHTTPD.Response.Status.OK,
            "text/event-stream; charset=utf-8",
            payload.toString()
        )
    }

    private fun taskStats(id: Long): NanoHTTPD.Response = ok(
        JSONObject().put(
            "data",
            JSONObject()
                .put("task_id", id)
                .put("total_runs", 0)
                .put("success_runs", 0)
                .put("failed_runs", 0)
        )
    )

    private fun cronTemplates(): NanoHTTPD.Response = ok(
        JSONObject().put(
            "data",
            JSONArray()
                .put(JSONObject().put("name", "每小时").put("expression", "0 * * * *"))
                .put(JSONObject().put("name", "每天零点").put("expression", "0 0 * * *"))
                .put(JSONObject().put("name", "每周一零点").put("expression", "0 0 * * 1"))
        )
    )

    private fun cronParse(json: JSONObject): NanoHTTPD.Response {
        val expression = json.optString("expression", json.optString("cron_expression")).trim()
        val valid = expression.split(Regex("\\s+")).size in 5..6
        if (!valid) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "Cron 表达式格式无效")
        return ok(JSONObject().put("data", JSONObject().put("valid", true).put("expression", expression)))
    }

    private fun exportTasks(): NanoHTTPD.Response = ok(JSONObject().put("data", taskRows()))

    private fun importTasks(json: JSONObject): NanoHTTPD.Response {
        val tasks = json.optJSONArray("tasks") ?: json.optJSONArray("data") ?: JSONArray()
        val errors = JSONArray()
        var imported = 0
        for (index in 0 until tasks.length()) {
            val item = tasks.optJSONObject(index)
            if (item == null) {
                errors.put("第 ${index + 1} 条任务格式无效")
                continue
            }
            runCatching {
                upsertTask(item)
                imported++
            }.onFailure { error ->
                errors.put("${item.optString("name", "第 ${index + 1} 条")}: ${error.message ?: "导入失败"}")
            }
        }
        return ok(JSONObject().put("message", "已导入 $imported 个任务").put("imported", imported).put("errors", errors).put("data", JSONObject().put("imported", imported).put("errors", errors)))
    }

    private fun upsertTask(json: JSONObject) {
        val now = Instant.now().toString()
        val name = json.optString("name").trim().ifBlank { "未命名任务" }
        val values = ContentValues().apply {
            put("name", name)
            put("command", json.optString("command"))
            put("cron_expression", json.optString("cron_expression"))
            put("task_type", json.optString("task_type", "manual"))
            put("python_version", json.optString("python_version"))
            put("status", json.optDouble("status", 1.0))
            put("labels", (json.optJSONArray("labels") ?: JSONArray()).toString())
            put("updated_at", now)
        }
        val existing = readableDatabase.query("tasks", arrayOf("id"), "name = ?", arrayOf(name), null, null, null).use { cursor ->
            if (cursor.moveToFirst()) cursor.long("id") else 0L
        }
        if (existing > 0) {
            writableDatabase.update("tasks", values, "id = ?", arrayOf(existing.toString()))
        } else {
            values.put("created_at", now)
            writableDatabase.insertOrThrow("tasks", null, values)
        }
    }

    private fun createEnv(json: JSONObject): NanoHTTPD.Response {
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("name", json.optString("name"))
            put("value", json.optString("value"))
            put("remarks", json.optString("remarks"))
            put("enabled", if (json.optBoolean("enabled", true)) 1 else 0)
            put("groups_json", normalizeGroups(json).toString())
            put("created_at", now)
            put("updated_at", now)
        }
        val id = writableDatabase.insertOrThrow("envs", null, values)
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun exportEnvs(asObject: Boolean): NanoHTTPD.Response {
        val rows = envRows()
        if (!asObject) return ok(JSONObject().put("data", rows))
        val data = JSONObject()
        for (index in 0 until rows.length()) {
            val item = rows.getJSONObject(index)
            data.put(item.optString("name"), item.optString("value"))
        }
        return ok(JSONObject().put("data", data))
    }

    private fun exportEnvFiles(json: JSONObject): NanoHTTPD.Response {
        val rows = envRows()
        val dotenv = StringBuilder()
        val jsonObject = JSONObject()
        for (index in 0 until rows.length()) {
            val item = rows.getJSONObject(index)
            if (json.optBoolean("enabled_only") && !item.optBoolean("enabled", true)) continue
            val name = item.optString("name")
            val value = item.optString("value")
            dotenv.append(name).append('=').append(value.replace("\n", "\\n")).append('\n')
            jsonObject.put(name, value)
        }
        return ok(JSONObject().put("data", JSONObject().put("env", dotenv.toString()).put("json", jsonObject.toString())))
    }

    private fun importEnvs(json: JSONObject): NanoHTTPD.Response {
        val envs = json.optJSONArray("envs") ?: json.optJSONArray("data") ?: JSONArray()
        val errors = JSONArray()
        var imported = 0
        for (index in 0 until envs.length()) {
            val item = envs.optJSONObject(index)
            if (item == null) {
                errors.put("第 ${index + 1} 条变量格式无效")
                continue
            }
            runCatching {
                upsertEnv(item)
                imported++
            }.onFailure { error ->
                errors.put("${item.optString("name", "第 ${index + 1} 条")}: ${error.message ?: "导入失败"}")
            }
        }
        return ok(JSONObject().put("message", "已导入 $imported 个环境变量").put("imported", imported).put("errors", errors).put("data", JSONObject().put("imported", imported).put("errors", errors)))
    }

    private fun upsertEnv(json: JSONObject) {
        val now = Instant.now().toString()
        val name = json.optString("name").trim()
        require(name.isNotBlank()) { "变量名不能为空" }
        val values = ContentValues().apply {
            put("name", name)
            put("value", json.optString("value"))
            put("remarks", json.optString("remarks"))
            put("enabled", if (json.optBoolean("enabled", true)) 1 else 0)
            put("groups_json", normalizeGroups(json).toString())
            put("updated_at", now)
        }
        val existing = readableDatabase.query("envs", arrayOf("id"), "name = ?", arrayOf(name), null, null, null).use { cursor ->
            if (cursor.moveToFirst()) cursor.long("id") else 0L
        }
        if (existing > 0) {
            writableDatabase.update("envs", values, "id = ?", arrayOf(existing.toString()))
        } else {
            values.put("created_at", now)
            writableDatabase.insertOrThrow("envs", null, values)
        }
    }

    private fun updateEnv(id: Long, json: JSONObject): NanoHTTPD.Response {
        val values = ContentValues().apply {
            listOf("name", "value", "remarks").forEach { key ->
                if (json.has(key)) put(key, json.optString(key))
            }
            if (json.has("enabled")) put("enabled", if (json.optBoolean("enabled")) 1 else 0)
            if (json.has("group") || json.has("groups")) put("groups_json", normalizeGroups(json).toString())
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("envs", values, "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun updateEnvEnabled(id: Long, enabled: Boolean): NanoHTTPD.Response {
        val values = ContentValues().apply {
            put("enabled", if (enabled) 1 else 0)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("envs", values, "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("enabled", enabled)))
    }

    private fun serveEnvBatch(
        session: NanoHTTPD.IHTTPSession,
        action: String?
    ): NanoHTTPD.Response {
        val json = body(session)
        val ids = json.optJSONArray("ids") ?: JSONArray()
        writableDatabase.beginTransaction()
        try {
            for (index in 0 until ids.length()) {
                val id = ids.optLong(index)
                when (action) {
                    "enable", "disable" -> updateEnvEnabledRecord(id, action == "enable")
                    "group" -> {
                        val values = ContentValues().apply {
                            put("groups_json", normalizeGroups(json).toString())
                            put("updated_at", Instant.now().toString())
                        }
                        writableDatabase.update("envs", values, "id = ?", arrayOf(id.toString()))
                    }
                    null -> if (session.method == NanoHTTPD.Method.DELETE) {
                        writableDatabase.delete("envs", "id = ?", arrayOf(id.toString()))
                    }
                }
            }
            writableDatabase.setTransactionSuccessful()
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids)))
    }

    private fun updateEnvEnabledRecord(id: Long, enabled: Boolean) {
        val values = ContentValues().apply {
            put("enabled", if (enabled) 1 else 0)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("envs", values, "id = ?", arrayOf(id.toString()))
    }

    private fun sortEnvs(json: JSONObject): NanoHTTPD.Response {
        val sourceId = json.optLong("source_id")
        val targetId = json.optLong("target_id")
        val values = ContentValues().apply { put("sort_order", targetId) }
        writableDatabase.update("envs", values, "id = ?", arrayOf(sourceId.toString()))
        return ok(JSONObject().put("data", JSONObject().put("source_id", sourceId).put("target_id", targetId)))
    }

    private fun createDependencies(json: JSONObject): NanoHTTPD.Response {
        val names = json.optJSONArray("names") ?: JSONArray()
        val now = Instant.now().toString()
        val ids = JSONArray()
        writableDatabase.beginTransaction()
        try {
            for (index in 0 until names.length()) {
                val name = names.optString(index).trim()
                if (name.isEmpty()) continue
                val depType = json.optString("type", "nodejs")
                val installResult = installDependencyForFallback(depType, name)
                val values = ContentValues().apply {
                    put("name", name)
                    put("type", depType)
                    put("python_version", json.optString("python_version"))
                    put("status", installResult.first)
                    put("log", installResult.second)
                    put("created_at", now)
                    put("updated_at", now)
                }
                ids.put(writableDatabase.insertOrThrow("dependencies", null, values))
            }
            writableDatabase.setTransactionSuccessful()
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("status", "recorded")))
    }

    private fun installDependencyForFallback(depType: String, name: String): Pair<String, String> {
        if (depType == "python") {
            val allowUnverifiedNative = configBool("allow_unverified_android_abi_wheels", false)
            if (name.equals("pycryptodome", ignoreCase = true) && !allowUnverifiedNative) {
                return "blocked" to "UNVERIFIED_ANDROID_ABI_WHEEL_BLOCKED: enable allow_unverified_android_abi_wheels before installing native wheels"
            }
            val runtime = AndroidPythonRuntime.ensureReady(appContext)
                ?: return "unavailable" to "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: Python runtime is not ready"
            val indexURL = configValue("pip_mirror", "https://pypi.org/simple").ifBlank { "https://pypi.org/simple" }
            val command = listOf(runtime.executable, runtime.home, "-m", "pip", "install", "--no-input", "--prefer-binary", "--index-url", indexURL, "--target", runtime.deps, name)
            val logs = JSONArray()
                .put("Installing Python dependency from network source: $name")
                .put("pip_index_url=$indexURL")
                .put("allow_unverified_android_abi_wheels=$allowUnverifiedNative")
            val result = runLocalProcess(command, AndroidPythonRuntime.depsDir(appContext), logs)
            val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
            return if (result.exitCode == 0) "installed" to text else "failed" to text
        }
        if (depType == "nodejs") {
            val restricted = setOf("@tencent-qqmail/agently-cli", "agent-browser", "clawhub")
            val allowUnverifiedNative = configBool("allow_unverified_android_abi_wheels", false)
            if (restricted.contains(name.lowercase()) && !allowUnverifiedNative) {
                return "blocked" to "RESTRICTED_NODE_PACKAGE_BLOCKED: enable allow_unverified_android_abi_wheels before installing restricted automation packages"
            }
            return AndroidNodeRuntime.ensureReady(appContext)?.let { runtime ->
                val deps = AndroidNodeRuntime.depsDir(appContext)
                val npm = File(runtime.modules, "npm/bin/npm-cli.js")
                val registry = configValue("npm_mirror", "https://registry.npmjs.org").ifBlank { "https://registry.npmjs.org" }
                val command = listOf(runtime.executable, npm.absolutePath, "install", "--ignore-scripts", "--registry", registry, "--prefix", deps.absolutePath, name)
                val logs = JSONArray()
                    .put("Installing Node dependency from network source: $name")
                    .put("npm_registry=$registry")
                    .put("allow_unverified_android_abi_wheels=$allowUnverifiedNative")
                val result = runLocalProcess(command, deps, logs)
                val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
                if (result.exitCode == 0) "installed" to text else "failed" to text
            } ?: ("unavailable" to "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: bundled Node runtime is not ready")
        }
        return "unavailable" to "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: $depType is not supported on Android fallback"
    }

    private fun dependencyLog(id: Long): NanoHTTPD.Response {
        val cursor = readableDatabase.query(
            "dependencies",
            arrayOf("status", "log"),
            "id = ?",
            arrayOf(id.toString()),
            null,
            null,
            null
        )
        cursor.use {
            if (!it.moveToFirst()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "依赖不存在")
            val status = it.string("status")
            val log = it.string("log")
            val payload = "event: log\ndata: $log\n\nevent: done\ndata: $status\n\n"
            return NanoHTTPD.newFixedLengthResponse(
                NanoHTTPD.Response.Status.OK,
                "text/event-stream; charset=utf-8",
                payload
            )
        }
    }

    private fun dependencyStatus(id: Long): NanoHTTPD.Response {
        return readableDatabase.query(
            "dependencies",
            arrayOf("id", "status", "log"),
            "id = ?",
            arrayOf(id.toString()),
            null,
            null,
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use error(NanoHTTPD.Response.Status.NOT_FOUND, "依赖不存在")
            ok(
                JSONObject().put(
                    "data",
                    JSONObject()
                        .put("id", cursor.long("id"))
                        .put("status", cursor.string("status"))
                        .put("log", cursor.string("log"))
                )
            )
        }
    }

    private fun updateDependencyStatus(id: Long, status: String, log: String): NanoHTTPD.Response {
        val values = ContentValues().apply {
            put("status", status)
            put("log", log)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("dependencies", values, "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("status", status)))
    }

    private fun deleteDependencies(json: JSONObject): NanoHTTPD.Response {
        val ids = json.optJSONArray("ids") ?: JSONArray()
        for (index in 0 until ids.length()) {
            writableDatabase.delete("dependencies", "id = ?", arrayOf(ids.optLong(index).toString()))
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids)))
    }

    private fun reinstallDependencies(json: JSONObject): NanoHTTPD.Response {
        val ids = json.optJSONArray("ids") ?: JSONArray()
        for (index in 0 until ids.length()) {
            updateDependencyStatus(
                ids.optLong(index),
                "installed",
                "Android local dependency record reinstalled"
            )
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids)))
    }

    private fun pythonRuntimes(): NanoHTTPD.Response = ok(
        JSONObject()
            .put("default_version", "3.12")
            .put(
                "data",
                JSONArray().put(
                    JSONObject()
                        .put("version", "3.12")
                        .put("label", "Python 3.12")
                        .put("default", true)
                        .put("available", true)
                        .put("venv_healthy", true)
                        .put("message", "Android 本地兼容运行时可用")
                )
            )
    )

    private fun mirrors(): NanoHTTPD.Response = ok(
        JSONObject()
            .put("pip_mirror", "https://pypi.org/simple")
            .put("npm_mirror", "https://registry.npmjs.org")
            .put("linux_mirror", "")
            .put("linux_package_manager", "android-local")
            .put("linux_distribution", "android")
            .put("linux_mirror_supported", true)
            .put("linux_mirror_label", "Android Local")
            .put("linux_mirror_message", "本地 fallback 记录依赖状态，真实执行由内置 Core 接管")
    )

    private fun envGroups(): NanoHTTPD.Response {
        val unique = linkedSetOf<String>()
        val rows = envRows()
        for (index in 0 until rows.length()) {
            val groups = rows.getJSONObject(index).optJSONArray("groups") ?: continue
            for (groupIndex in 0 until groups.length()) unique += groups.optString(groupIndex)
        }
        return ok(JSONObject().put("data", JSONArray(unique.toList())))
    }

    private fun normalizeGroups(json: JSONObject): JSONArray {
        val groups = json.optJSONArray("groups")
        if (groups != null) return groups
        return JSONArray(
            json.optString("group")
                .split(',')
                .map(String::trim)
                .filter(String::isNotEmpty)
        )
    }

    private fun delete(table: String, id: Long): NanoHTTPD.Response {
        writableDatabase.delete(table, "id = ?", arrayOf(id.toString()))
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun body(session: NanoHTTPD.IHTTPSession): JSONObject {
        val files = HashMap<String, String>()
        session.parseBody(files)
        val raw = when {
            files["postData"] != null -> files["postData"].orEmpty()
            files["content"] != null -> File(files.getValue("content")).readText()
            else -> ""
        }.trim()
        return if (raw.isEmpty()) JSONObject() else JSONObject(raw)
    }

    private fun bodyOrUploadedJson(session: NanoHTTPD.IHTTPSession): JSONObject {
        val files = HashMap<String, String>()
        session.parseBody(files)
        val raw = when {
            files["file"] != null -> File(files.getValue("file")).readText()
            files["content"] != null -> File(files.getValue("content")).readText()
            files["postData"] != null -> files["postData"].orEmpty()
            else -> ""
        }.trim()
        return if (raw.isEmpty()) JSONObject() else JSONObject(raw)
    }

    private fun paginated(name: String, rows: JSONArray): NanoHTTPD.Response = ok(
        JSONObject()
            .put("data", rows)
            .put("total", rows.length())
            .put("page", 1)
            .put("page_size", rows.length())
            .put("resource", name)
    )

    private fun ok(json: JSONObject): NanoHTTPD.Response = NanoHTTPD.newFixedLengthResponse(
        NanoHTTPD.Response.Status.OK,
        "application/json; charset=utf-8",
        json.toString()
    )

    private fun ok(json: JSONArray): NanoHTTPD.Response = NanoHTTPD.newFixedLengthResponse(
        NanoHTTPD.Response.Status.OK,
        "application/json; charset=utf-8",
        json.toString()
    )

    private fun error(status: NanoHTTPD.Response.Status, message: String): NanoHTTPD.Response =
        NanoHTTPD.newFixedLengthResponse(
            status,
            "application/json; charset=utf-8",
            JSONObject().put("error", message).toString()
        )

    private fun initializeAdmin(json: JSONObject): NanoHTTPD.Response {
        if (count(readableDatabase, "local_users") > 0) {
            return error(NanoHTTPD.Response.Status.CONFLICT, "本地管理员已经初始化")
        }
        val username = json.optString("username").trim()
        val password = json.optString("password")
        if (username.length < 2 || password.length < 6) {
            return error(NanoHTTPD.Response.Status.BAD_REQUEST, "用户名至少 2 位，密码至少 6 位")
        }
        val salt = ByteArray(16).also(SecureRandom()::nextBytes)
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("username", username)
            put("password_hash", hashPassword(password, salt))
            put("password_salt", Base64.encodeToString(salt, Base64.NO_WRAP))
            put("created_at", now)
            put("updated_at", now)
        }
        writableDatabase.insertOrThrow("local_users", null, values)
        return ok(JSONObject().put("message", "initialized"))
    }

    private fun login(json: JSONObject): NanoHTTPD.Response {
        val username = json.optString("username").trim()
        val password = json.optString("password")
        val cursor = readableDatabase.query(
            "local_users",
            arrayOf("password_hash", "password_salt"),
            "username = ?",
            arrayOf(username),
            null,
            null,
            null
        )
        val valid = cursor.use {
            if (!it.moveToFirst()) false
            else {
                val salt = Base64.decode(it.string("password_salt"), Base64.NO_WRAP)
                hashPassword(password, salt) == it.string("password_hash")
            }
        }
        if (!valid) return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "用户名或密码错误")
        val accessToken = randomToken()
        val refreshToken = randomToken()
        val values = ContentValues().apply {
            put("id", 1)
            put("access_token", accessToken)
            put("refresh_token", refreshToken)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.insertWithOnConflict(
            "local_sessions",
            null,
            values,
            SQLiteDatabase.CONFLICT_REPLACE
        )
        return ok(
            JSONObject().put(
                "data",
                JSONObject()
                    .put("access_token", accessToken)
                    .put("refresh_token", refreshToken)
                    .put("user", userJson())
            )
        )
    }

    private fun refresh(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val refreshToken = bearerToken(session)
            ?: return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "缺少刷新凭据")
        val valid = readableDatabase.rawQuery(
            "SELECT 1 FROM local_sessions WHERE id = 1 AND refresh_token = ?",
            arrayOf(refreshToken)
        ).use(Cursor::moveToFirst)
        if (!valid) return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "刷新凭据已失效")
        val accessToken = randomToken()
        val values = ContentValues().apply {
            put("access_token", accessToken)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("local_sessions", values, "id = 1", null)
        return ok(JSONObject().put("data", JSONObject().put("access_token", accessToken)))
    }

    private fun userJson(): JSONObject {
        return readableDatabase.rawQuery(
            "SELECT id, username, created_at, updated_at FROM local_users ORDER BY id LIMIT 1",
            null
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use JSONObject()
            JSONObject()
                .put("id", cursor.long("id"))
                .put("username", cursor.string("username"))
                .put("role", "admin")
                .put("enabled", true)
                .put("created_at", cursor.string("created_at"))
                .put("updated_at", cursor.string("updated_at"))
        }
    }

    private fun authenticated(
        session: NanoHTTPD.IHTTPSession,
        action: () -> NanoHTTPD.Response
    ): NanoHTTPD.Response = if (isAuthorized(session)) {
        action()
    } else {
        error(NanoHTTPD.Response.Status.UNAUTHORIZED, "本地会话已失效")
    }

    private fun bearerToken(session: NanoHTTPD.IHTTPSession): String? {
        val value = session.headers["authorization"]?.trim().orEmpty()
        if (!value.startsWith("Bearer ", ignoreCase = true)) return null
        return value.substringAfter(' ').trim().takeIf(String::isNotEmpty)
    }

    private fun hashPassword(password: String, salt: ByteArray): String {
        val spec = PBEKeySpec(password.toCharArray(), salt, 120_000, 256)
        val hash = SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256")
            .generateSecret(spec)
            .encoded
        spec.clearPassword()
        return Base64.encodeToString(hash, Base64.NO_WRAP)
    }

    private fun randomToken(): String {
        val bytes = ByteArray(32).also(SecureRandom()::nextBytes)
        return Base64.encodeToString(
            bytes,
            Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING
        )
    }

    private fun count(db: SQLiteDatabase, table: String, selection: String? = null): Int {
        val query = "SELECT COUNT(*) FROM $table${if (selection == null) "" else " WHERE $selection"}"
        return db.rawQuery(query, null).use { cursor ->
            if (cursor.moveToFirst()) cursor.getInt(0) else 0
        }
    }

    private fun queryRows(
        sql: String,
        args: Array<String> = emptyArray(),
        convert: (Cursor) -> JSONObject
    ): JSONArray {
        val result = JSONArray()
        readableDatabase.rawQuery(sql, args).use { cursor ->
            while (cursor.moveToNext()) result.put(convert(cursor))
        }
        return result
    }

    private fun Cursor.string(column: String): String = getString(getColumnIndexOrThrow(column)) ?: ""
    private fun Cursor.long(column: String): Long = getLong(getColumnIndexOrThrow(column))
    private fun Cursor.int(column: String): Int = getInt(getColumnIndexOrThrow(column))
    private fun Cursor.double(column: String): Double = getDouble(getColumnIndexOrThrow(column))
}
