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
import java.time.Instant
import java.security.SecureRandom
import java.time.format.DateTimeFormatter
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.PBEKeySpec

class LocalPanelStore(private val appContext: Context) : SQLiteOpenHelper(
    appContext,
    "daidai-local.db",
    null,
    SCHEMA_VERSION
) {
    companion object {
        const val SCHEMA_VERSION = 1
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
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

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

    fun isPublicRequest(session: NanoHTTPD.IHTTPSession): Boolean =
        session.uri in setOf(
            "/api/v1/health",
            "/api/health",
            "/api/auth/check-init",
            "/api/auth/init",
            "/api/auth/login",
            "/api/auth/refresh",
            "/api/auth/captcha-config"
        )

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
        val segments = session.uri.trim('/').split('/')
        val id = segments.getOrNull(2)?.toLongOrNull()
        val action = segments.getOrNull(3)
        return when {
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/tasks/views" ->
                ok(JSONObject().put("data", JSONArray()))
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/tasks/cron/templates" ->
                cronTemplates()
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/tasks/cron/parse" ->
                cronParse(body(session))
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/tasks/notification-channels" ->
                ok(JSONObject().put("data", JSONArray()))
            session.uri.startsWith("/api/tasks/batch/") -> serveTaskBatch(session, action)
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("tasks", taskRows())
            session.method == NanoHTTPD.Method.POST && id == null -> createTask(body(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateTask(id, body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("tasks", id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable", "run", "stop") ->
                updateTaskStatus(id, action!!)
            id != null && session.method == NanoHTTPD.Method.GET && action == "latest-log" -> taskLog(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "live-logs" -> taskLog(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "stats" -> taskStats(id)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "任务接口尚未实现")
        }
    }

    fun serveEnvs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val segments = session.uri.trim('/').split('/')
        val id = segments.getOrNull(2)?.toLongOrNull()
        val action = segments.getOrNull(3)
        return when {
            session.method == NanoHTTPD.Method.GET && session.uri.endsWith("/groups") -> envGroups()
            session.uri.startsWith("/api/envs/batch") -> serveEnvBatch(session, action)
            session.method == NanoHTTPD.Method.PUT && session.uri == "/api/envs/sort" -> sortEnvs(body(session))
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
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/tree" -> scriptTree()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts" -> paginated("scripts", scriptRows())
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/content" -> scriptContent(session.parms["path"].orEmpty())
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/scripts/content" -> saveScriptContent(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/directory" -> createScriptDirectory(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/upload" -> uploadScript(session)
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/format" -> formatScript(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/scripts/run-code" -> runCode(body(session))
            normalizedUri.startsWith("/scripts/run/") && normalizedUri.endsWith("/logs") -> ok(JSONObject().put("data", JSONObject().put("logs", JSONArray()).put("status", "success")))
            normalizedUri.startsWith("/scripts/run/") && session.method == NanoHTTPD.Method.PUT -> ok(JSONObject().put("message", "stopped"))
            normalizedUri.startsWith("/scripts/run/") && session.method == NanoHTTPD.Method.DELETE -> ok(JSONObject().put("message", "cleared"))
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本接口尚未实现")
        }
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

    private fun scriptTree(): NanoHTTPD.Response = ok(JSONObject().put("data", scriptRows()))

    private fun scriptContent(path: String): NanoHTTPD.Response {
        val file = scriptFile(path)
        if (!file.exists() || !file.isFile) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        return ok(JSONObject().put("data", JSONObject().put("path", path).put("content", file.readText()).put("is_binary", false)))
    }

    private fun saveScriptContent(json: JSONObject): NanoHTTPD.Response {
        val path = json.optString("path", json.optString("filename", "script.py")).ifBlank { "script.py" }
        val file = scriptFile(path)
        file.parentFile?.mkdirs()
        file.writeText(json.optString("content"))
        return ok(JSONObject().put("data", scriptFileJson(path, file)))
    }

    private fun createScriptDirectory(json: JSONObject): NanoHTTPD.Response {
        val path = json.optString("path", json.optString("name", "new-folder"))
        val dir = scriptFile(path)
        dir.mkdirs()
        return ok(JSONObject().put("data", JSONObject().put("path", path)))
    }

    private fun uploadScript(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val json = runCatching { body(session) }.getOrElse { JSONObject() }
        val path = json.optString("path", json.optString("filename", "uploaded.txt")).ifBlank { "uploaded.txt" }
        val content = json.optString("content", "")
        val file = scriptFile(path)
        file.parentFile?.mkdirs()
        file.writeText(content)
        return ok(JSONObject().put("path", path).put("paths", JSONArray().put(path)).put("uploaded_count", 1))
    }

    private fun formatScript(json: JSONObject): NanoHTTPD.Response = ok(
        JSONObject().put("data", JSONObject().put("content", json.optString("content")).put("formatter", "android-local"))
    )

    private fun runCode(json: JSONObject): NanoHTTPD.Response = ok(
        JSONObject().put("data", JSONObject().put("run_id", "android-local-${Instant.now().toEpochMilli()}").put("status", "success").put("logs", JSONArray().put("Android local fallback accepted run-code")))
    )

    private fun scriptFile(path: String): File {
        val clean = path.replace('\\', '/').split('/').filter { it.isNotBlank() && it != "." && it != ".." }.joinToString(File.separator)
        return File(scriptsRoot(), clean.ifBlank { "script.py" }).canonicalFile.also {
            require(it.path.startsWith(scriptsRoot().canonicalPath)) { "invalid script path" }
        }
    }

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
            values.clear()
            values.put("status", 1.0)
            values.put("last_run_status", "failed")
            values.put("updated_at", Instant.now().toString())
            writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
        }
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("status", status)))
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

    private fun taskLog(id: Long): NanoHTTPD.Response = ok(
        JSONObject().put(
            "data",
            JSONObject()
                .put("task_id", id)
                .put("status", "failed")
                .put("content", "Android runtime component is required before task execution")
                .put("logs", JSONArray().put("Android runtime component is required before task execution"))
        )
    )

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
                val log = "Installed in Android local fallback metadata store ($depType). Runtime execution uses bundled Core when available."
                val values = ContentValues().apply {
                    put("name", name)
                    put("type", depType)
                    put("python_version", json.optString("python_version"))
                    put("status", "installed")
                    put("log", log)
                    put("created_at", now)
                    put("updated_at", now)
                }
                ids.put(writableDatabase.insertOrThrow("dependencies", null, values))
            }
            writableDatabase.setTransactionSuccessful()
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("status", "installed")))
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
