package com.daidai.daidai_app

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.ContentValues
import android.content.Context
import android.content.pm.PackageManager
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import android.util.Base64
import fi.iki.elonen.NanoHTTPD
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.URI
import java.time.Instant
import java.security.SecureRandom
import java.time.format.DateTimeFormatter
import java.util.Collections
import java.util.UUID
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.PBEKeySpec

class LocalPanelStore(
    private val appContext: Context,
    private val endpointProvider: () -> String = { "http://127.0.0.1:5700" },
    private val localTokenProvider: () -> String = { "" },
) : SQLiteOpenHelper(
    appContext,
    "daidai-local.db",
    null,
    SCHEMA_VERSION
) {
    private val configPrefs by lazy {
        appContext.getSharedPreferences("daidai-local-configs", Context.MODE_PRIVATE)
    }
    private val runningTaskIds = ConcurrentHashMap.newKeySet<Long>()
    private val taskProcesses = ConcurrentHashMap<Long, Process>()
    private val taskAbortRequested = ConcurrentHashMap.newKeySet<Long>()
    private val taskRunLogIds = ConcurrentHashMap<Long, Long>()
    private val taskRunCursors = ConcurrentHashMap<Long, Long>()
    private val taskRunLocks = ConcurrentHashMap<Long, Any>()
    private val taskFinalizedIds = ConcurrentHashMap.newKeySet<Long>()
    private val taskRunLogsMemory = ConcurrentHashMap<Long, MutableList<String>>()
    private val taskRunLogCharacters = ConcurrentHashMap<Long, Int>()
    private val taskRunPendingPersistence = ConcurrentHashMap<Long, Int>()
    private val taskRunStartedAt = ConcurrentHashMap<Long, Instant>()
    private val scriptRunExecutor = boundedExecutor("local-script-run")
    private val taskRunExecutor = boundedExecutor("local-task-run")
    private val scriptProcesses = ConcurrentHashMap<String, Process>()
    private val scriptRunLogsMemory = ConcurrentHashMap<String, MutableList<String>>()
    private val scriptRunLogCharacters = ConcurrentHashMap<String, Int>()
    private val scriptRunPendingPersistence = ConcurrentHashMap<String, Int>()
    private val scriptRunLocks = ConcurrentHashMap<String, Any>()
    private val localBackupService by lazy { LocalBackupService(appContext, { writableDatabase }, SCHEMA_VERSION) }
    private val terminalSessions = AndroidTerminalSessions()
    @Volatile private var lastScheduledBackupKey = ""

    internal data class LocalScriptResult(
        val logs: JSONArray,
        val status: String,
        val done: Boolean,
        val exitCode: Int?,
    )

    companion object {
        const val SCHEMA_VERSION = 15
        private const val MAX_SCRIPT_LOG_LINES = 500
        private const val MAX_SCRIPT_LOG_LINE_CHARS = 4096
        private const val MAX_SCRIPT_LOG_TOTAL_CHARS = 256_000
        private const val MAX_APP_LOG_BYTES = 512L * 1024
        private const val FALLBACK_WORKERS = 2
        private const val FALLBACK_QUEUE_CAPACITY = 32
        private const val LOG_PERSIST_BATCH_SIZE = 16

        private fun boundedExecutor(threadName: String) = ThreadPoolExecutor(
            FALLBACK_WORKERS,
            FALLBACK_WORKERS,
            0L,
            TimeUnit.MILLISECONDS,
            ArrayBlockingQueue(FALLBACK_QUEUE_CAPACITY),
            { runnable -> Thread(runnable, threadName).apply { isDaemon = true } },
            ThreadPoolExecutor.AbortPolicy(),
        )


        internal fun normalizeScriptPath(rawPath: String, allowRootAlias: Boolean = false): String {
            val decodedPath = percentDecodePath(rawPath)
            if (allowRootAlias && (decodedPath.isEmpty() || decodedPath == "/")) return ""
            require(decodedPath.isNotBlank()) { "脚本路径不能为空" }
            require(!decodedPath.startsWith("//") && !decodedPath.startsWith('\\')) { "脚本路径必须位于脚本目录内: $decodedPath" }
            require(!Regex("^[A-Za-z]:").containsMatchIn(decodedPath)) { "脚本路径必须位于脚本目录内: $decodedPath" }
            require('\\' !in decodedPath) { "脚本路径不能包含反斜杠: $decodedPath" }
            val workspacePath = decodedPath.removePrefix("/")
            require(!workspacePath.startsWith("data/") && !workspacePath.startsWith("system/") && !workspacePath.startsWith("sdcard/")) { "脚本路径不能指向系统目录: $decodedPath" }
            val segments = workspacePath.split('/')
            require(segments.none { it == ".." }) { "脚本路径不能包含 .. 段: $decodedPath" }
            require(segments.none { it.isBlank() || it == "." }) { "脚本路径包含无效段: $decodedPath" }
            return segments.joinToString("/")
        }

        private fun percentDecodePath(value: String): String {
            if ('%' !in value) return value
            val encodedBytes = Regex("%([0-9A-Fa-f]{2})").findAll(value)
                .map { it.groupValues[1].toInt(16) }
                .toList()
            if (encodedBytes.isEmpty() || encodedBytes.all { it == 0x25 }) return value
            val output = StringBuilder(value.length)
            val bytes = ByteArrayOutputStream()

            fun flushBytes() {
                if (bytes.size() > 0) {
                    output.append(String(bytes.toByteArray(), Charsets.UTF_8))
                    bytes.reset()
                }
            }

            var index = 0
            while (index < value.length) {
                val char = value[index]
                if (char == '%' && index + 2 < value.length) {
                    val high = value[index + 1].digitToIntOrNull(16)
                    val low = value[index + 2].digitToIntOrNull(16)
                    if (high != null && low != null) {
                        bytes.write((high shl 4) + low)
                        index += 3
                        continue
                    }
                }
                flushBytes()
                output.append(char)
                index++
            }
            flushBytes()
            return output.toString()
        }

        fun isRecoveryRequest(method: NanoHTTPD.Method, uri: String): Boolean {
            val base = uri.substringBefore("?").trimEnd('/')
            return when (method) {
                NanoHTTPD.Method.GET -> base == "/api/system/backups" || base == "/api/system/backup/download" || base == "/api/system/restore/progress"
                NanoHTTPD.Method.POST -> base == "/api/system/backup" || base == "/api/system/backup/upload" || base == "/api/system/restore"
                NanoHTTPD.Method.DELETE -> base == "/api/system/backup"
                else -> false
            }
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
                task_before TEXT NOT NULL DEFAULT '',
                task_after TEXT NOT NULL DEFAULT '',
                notify_on_failure INTEGER NOT NULL DEFAULT 0,
                notify_on_success INTEGER NOT NULL DEFAULT 0,
                notify_on_abort INTEGER NOT NULL DEFAULT 0,
                notification_channel_id INTEGER,
                status REAL NOT NULL DEFAULT 1,
                labels TEXT NOT NULL DEFAULT '[]',
                last_run_status TEXT NOT NULL DEFAULT '',
                last_run_logs TEXT NOT NULL DEFAULT '[]',
                last_log_id INTEGER NOT NULL DEFAULT 0,
                last_startup_auto_run_date TEXT NOT NULL DEFAULT '',
                last_run_at TEXT,
                timeout INTEGER NOT NULL DEFAULT 0,
                success_exit_codes TEXT NOT NULL DEFAULT '0',
                random_delay_seconds INTEGER,
                max_retries INTEGER NOT NULL DEFAULT 0,
                retry_interval INTEGER NOT NULL DEFAULT 0,
                depends_on INTEGER,
                sort_order INTEGER NOT NULL DEFAULT 0,
                subscription_locked INTEGER NOT NULL DEFAULT 0,
                log_path TEXT,
                last_running_time REAL,
                allow_multiple_instances INTEGER NOT NULL DEFAULT 0,
                schedule_policy TEXT NOT NULL DEFAULT 'skip',
                stop_schedule TEXT NOT NULL DEFAULT '',
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
                updated_at TEXT NOT NULL,
                UNIQUE(type, name, python_version)
            )""".trimIndent()
        )
        createScriptRuntimeTables(db)
        createTaskLogTables(db)
        createConfigTables(db)
        createNotificationTables(db)
        ensureDefaultAdmin(db)
        ensureSubscriptionsTable(db)
        createFallbackCoreTables(db)
        createManagementTables(db)
        createSecurityTables(db)
        ensureDefaultDeps(db)
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) createScriptRuntimeTables(db)
        if (oldVersion < 3) db.execSQL("ALTER TABLE tasks ADD COLUMN last_run_logs TEXT NOT NULL DEFAULT '[]'")
        if (oldVersion < 4) {
            db.execSQL("ALTER TABLE tasks ADD COLUMN last_log_id INTEGER NOT NULL DEFAULT 0")
            createTaskLogTables(db)
        }
        if (oldVersion < 5) createConfigTables(db)
        if (oldVersion < 6) {
            db.execSQL("ALTER TABLE tasks ADD COLUMN task_before TEXT NOT NULL DEFAULT ''")
            db.execSQL("ALTER TABLE tasks ADD COLUMN task_after TEXT NOT NULL DEFAULT ''")
        }
        if (oldVersion < 7) createNotificationTables(db)
        if (oldVersion < 8) createFallbackCoreTables(db)
        if (oldVersion < 9) createManagementTables(db)
        if (oldVersion < 10) createSecurityTables(db)
        if (oldVersion < 11) {
            // v0.4.7 wrote task log codes as 1=success, 2=failure, 3=running,
            // while all clients and the Go backend use 0=success, 1=failure, 2=running.
            db.execSQL("UPDATE task_logs_local SET status = CASE status WHEN 1 THEN 0 WHEN 2 THEN 1 WHEN 3 THEN 2 ELSE status END")
            createNotificationTables(db)
        }
        if (oldVersion in 4..11) {
            db.execSQL("ALTER TABLE task_logs_local ADD COLUMN log_cursor INTEGER NOT NULL DEFAULT 0")
        }
        if (oldVersion < 13) normalizeAndDeduplicateDependencies(db)
        if (oldVersion < 14) {
            addColumnIfMissing(db, "tasks", "notify_on_failure", "INTEGER NOT NULL DEFAULT 0")
            addColumnIfMissing(db, "tasks", "notify_on_success", "INTEGER NOT NULL DEFAULT 0")
            addColumnIfMissing(db, "tasks", "notify_on_abort", "INTEGER NOT NULL DEFAULT 0")
            addColumnIfMissing(db, "tasks", "notification_channel_id", "INTEGER")
            addColumnIfMissing(db, "notification_channels", "push_scope", "TEXT NOT NULL DEFAULT 'default'")
            addColumnIfMissing(db, "notification_channels", "today_send_count", "INTEGER NOT NULL DEFAULT 0")
            addColumnIfMissing(db, "notification_channels", "today_send_date", "TEXT NOT NULL DEFAULT ''")
            addColumnIfMissing(db, "notification_channels", "last_test_at", "TEXT NOT NULL DEFAULT ''")
            addColumnIfMissing(db, "notification_channels", "last_test_status", "TEXT NOT NULL DEFAULT ''")
        }
        if (oldVersion < 15) ensureBackupCompatibilityColumns(db)
    }

    private fun addColumnIfMissing(db: SQLiteDatabase, table: String, column: String, declaration: String) {
        val exists = db.rawQuery("PRAGMA table_info($table)", null).use { cursor ->
            val nameIndex = cursor.getColumnIndexOrThrow("name")
            generateSequence { if (cursor.moveToNext()) cursor.getString(nameIndex) else null }.any { it == column }
        }
        if (!exists) db.execSQL("ALTER TABLE $table ADD COLUMN $column $declaration")
    }

    private fun createFallbackCoreTables(db: SQLiteDatabase) {
        db.execSQL("""CREATE TABLE IF NOT EXISTS task_views (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, filters TEXT NOT NULL DEFAULT '[]', sort_rules TEXT NOT NULL DEFAULT '[]', hidden INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS subscription_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, subscription_id INTEGER NOT NULL, level TEXT NOT NULL DEFAULT 'info', message TEXT NOT NULL, created_at TEXT NOT NULL)""")
        runCatching { db.execSQL("ALTER TABLE tasks ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0") }
        ensureBackupCompatibilityColumns(db)
    }

    private fun createManagementTables(db: SQLiteDatabase) {
        runCatching { db.execSQL("ALTER TABLE local_users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'") }
        runCatching { db.execSQL("ALTER TABLE local_users ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1") }
        runCatching { db.execSQL("ALTER TABLE local_users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''") }
        db.execSQL("""CREATE TABLE IF NOT EXISTS ssh_keys (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, private_key TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS platforms (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, label TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS platform_tokens (id INTEGER PRIMARY KEY AUTOINCREMENT, platform_id INTEGER NOT NULL, name TEXT NOT NULL, token TEXT NOT NULL, remarks TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS open_api_apps (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, app_key TEXT NOT NULL UNIQUE, secret TEXT NOT NULL, scopes TEXT NOT NULL DEFAULT '', rate_limit INTEGER NOT NULL DEFAULT 60, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS open_api_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, app_id INTEGER NOT NULL, method TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', status_code INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)""")
        val now = Instant.now().toString()
        db.execSQL("INSERT INTO platforms(name,label,icon,created_at,updated_at) SELECT 'github','GitHub','',?,? WHERE NOT EXISTS(SELECT 1 FROM platforms WHERE name='github')", arrayOf(now, now))
    }

    private fun createSecurityTables(db: SQLiteDatabase) {
        db.execSQL("""CREATE TABLE IF NOT EXISTS security_login_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', status INTEGER NOT NULL, message TEXT NOT NULL DEFAULT '', client_name TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS security_sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL, access_token TEXT NOT NULL UNIQUE, ip TEXT NOT NULL DEFAULT '', client_name TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, expires_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS security_ip_whitelist (id INTEGER PRIMARY KEY AUTOINCREMENT, ip TEXT NOT NULL UNIQUE, remarks TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)""")
        db.execSQL("""CREATE TABLE IF NOT EXISTS security_audit_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)""")
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
                created_at TEXT NOT NULL,
                log_cursor INTEGER NOT NULL DEFAULT 0
            )""".trimIndent()
        )
    }

    private fun createNotificationTables(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS notification_channels (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                type TEXT NOT NULL,
                config TEXT NOT NULL DEFAULT '{}',
                push_scope TEXT NOT NULL DEFAULT 'default',
                enabled INTEGER NOT NULL DEFAULT 1,
                today_send_count INTEGER NOT NULL DEFAULT 0,
                today_send_date TEXT NOT NULL DEFAULT '',
                last_test_at TEXT NOT NULL DEFAULT '',
                last_test_status TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        val now = Instant.now().toString()
        db.execSQL("INSERT INTO notification_channels(name,type,config,enabled,created_at,updated_at) SELECT 'Android 本地通知','android_local','{}',1,?,? WHERE NOT EXISTS (SELECT 1 FROM notification_channels WHERE type='android_local')", arrayOf(now, now))
    }

    private fun createConfigTables(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE IF NOT EXISTS local_configs (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL DEFAULT '',
                updated_at TEXT NOT NULL
            )""".trimIndent()
        )
        ensureMirrorDefaults(db)
    }

    override fun onOpen(db: SQLiteDatabase) {
        super.onOpen(db)
        synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
            ensureMirrorDefaults(db)
        }
        normalizeAndDeduplicateDependencies(db)
        db.execSQL("UPDATE notification_channels SET type='pushplus' WHERE lower(type)='pludplus'")
        cleanupStorage(db)
    }

    private fun normalizeAndDeduplicateDependencies(db: SQLiteDatabase) {
        val rows = mutableListOf<Pair<Long, Triple<String, String, String>>>()
        db.query("dependencies", arrayOf("id", "type", "name", "python_version"), null, null, null, null, "updated_at DESC, id DESC").use { cursor ->
            while (cursor.moveToNext()) {
                val type = normalizeDependencyType(cursor.string("type")) ?: cursor.string("type")
                val version = if (type == "python") cursor.string("python_version").ifBlank { DependencyStorage.PYTHON_VERSION } else ""
                rows += cursor.long("id") to Triple(type, DependencyStorage.normalizedName(type, cursor.string("name")), version)
            }
        }
        val seen = mutableSetOf<Triple<String, String, String>>()
        val retained = rows.filter { (_, identity) -> seen.add(identity) }
        rows.filterNot { it in retained }.forEach { (id, _) -> db.delete("dependencies", "id = ?", arrayOf(id.toString())) }
        retained.forEach { (id, identity) ->
            db.update("dependencies", ContentValues().apply {
                put("type", identity.first); put("name", identity.second); put("python_version", identity.third)
            }, "id = ?", arrayOf(id.toString()))
        }
        runCatching { db.execSQL("CREATE UNIQUE INDEX IF NOT EXISTS idx_dependencies_identity ON dependencies(type, name, python_version)") }
    }

    private fun cleanupStorage(db: SQLiteDatabase) {
        db.execSQL("DELETE FROM task_logs_local WHERE id NOT IN (SELECT id FROM task_logs_local ORDER BY id DESC LIMIT ${DependencyStorage.MAX_TASK_LOGS})")
        db.execSQL("DELETE FROM script_runs WHERE id NOT IN (SELECT id FROM script_runs ORDER BY updated_at DESC LIMIT ${DependencyStorage.MAX_SCRIPT_RUNS})")
        DependencyStorage.retainNewest(backupsRoot(), DependencyStorage.MAX_BACKUPS)
        DependencyStorage.removeExpired(File(appContext.cacheDir, "script-runs"), DependencyStorage.TEMP_MAX_AGE_MILLIS)
        DependencyStorage.removeExpired(File(appContext.cacheDir, "portable-backups"), DependencyStorage.TEMP_MAX_AGE_MILLIS)
        DependencyStorage.trimDirectory(DependencyStorage.npmCache(appContext.filesDir), DependencyStorage.MAX_CACHE_BYTES)
    }

    private fun ensureMirrorDefaults(db: SQLiteDatabase) {
        val defaults = mapOf(
            AndroidLinuxRuntime.PIP_MIRROR_KEY to AndroidLinuxRuntime.PYTHON_PIP_ALIBABA_INDEX,
            AndroidLinuxRuntime.NPM_MIRROR_KEY to AndroidLinuxRuntime.NODE_NPM_NPMMIRROR_REGISTRY,
            AndroidLinuxRuntime.LINUX_MIRROR_KEY to AndroidLinuxRuntime.ALPINE_APK_HUAWEI_MIRROR,
        )
        val editor = configPrefs.edit()
        var preferencesChanged = false
        val legacyMirrors = db.query("local_configs", arrayOf("value"), "key = ?", arrayOf("dependency_mirrors"), null, null, null).use { cursor ->
            if (!cursor.moveToFirst()) null else runCatching { JSONObject(cursor.string("value")) }.getOrNull()
        }
        defaults.forEach { (key, defaultValue) ->
            val persisted = db.query("local_configs", arrayOf("value"), "key = ?", arrayOf(key), null, null, null).use { cursor ->
                if (cursor.moveToFirst()) cursor.string("value") else null
            }
            val imported = configPrefs.getString(key, null)
                ?: legacyMirrors?.optString(key)?.takeIf { it.isNotBlank() }
                ?: legacyMirrors?.optString(key.removeSuffix("_mirror"))?.takeIf { it.isNotBlank() }
            val value = AndroidLinuxRuntime.resolveMirrorValue(persisted, imported, defaultValue)
            if (persisted != value) {
                db.insertWithOnConflict("local_configs", null, ContentValues().apply {
                    put("key", key)
                    put("value", value)
                    put("updated_at", Instant.now().toString())
                }, SQLiteDatabase.CONFLICT_REPLACE)
            }
            if (configPrefs.getString(key, null) != value) {
                editor.putString(key, value)
                preferencesChanged = true
            }
        }
        if (preferencesChanged) check(editor.commit()) { "无法持久化镜像配置" }
    }


    private fun listUsers(): NanoHTTPD.Response {
        val rows = JSONArray()
        readableDatabase.query("local_users", arrayOf("id", "username", "role", "enabled", "created_at", "updated_at"), null, null, null, null, "id ASC").use { cursor ->
            while (cursor.moveToNext()) {
                rows.put(JSONObject().apply {
                    put("id", cursor.long("id"))
                    put("username", cursor.string("username"))
                    put("created_at", cursor.string("created_at"))
                    put("updated_at", cursor.string("updated_at"))
                    put("role", cursor.string("role"))
                    put("enabled", cursor.int("enabled") != 0)
                })
            }
        }
        return ok(JSONObject().put("data", rows).put("total", rows.length()))
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
            f.writeText("=== daidai-panel-native runtime log ===\n")
        }
    }

    fun appLog(tag: String, message: String) {
        synchronized(appLogFile) {
            if (appLogFile.length() > MAX_APP_LOG_BYTES) {
                val tail = appLogFile.readText().takeLast((MAX_APP_LOG_BYTES / 2).toInt())
                appLogFile.writeText("=== daidai-panel-native runtime log (trimmed) ===\n$tail")
            }
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
                login(session, body(session))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/refresh" ->
                refresh(session)
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/user" ->
                authenticated(session) { ok(JSONObject().put("user", userJson())) }
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/logout" ->
                authenticated(session) { revokeAccessToken(bearerToken(session), "logout"); ok(JSONObject().put("message", "ok")) }
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/captcha-config" ->
                ok(JSONObject().put("enabled", false).put("configured", true).put("implemented", false).put("required", false).put("captcha_id", "").put("require_after_failures", 0).put("message", ""))
            session.uri.startsWith("/api/auth/users") -> serveUsers(session, "/api/auth/users")
            session.method == NanoHTTPD.Method.GET && session.uri == "/api/auth/user-list" -> listUsers()
            session.method == NanoHTTPD.Method.PUT && session.uri == "/api/auth/password" -> changeOwnPassword(body(session))
            session.method == NanoHTTPD.Method.PUT && session.uri == "/api/auth/username" -> changeOwnUsername(body(session))
            session.method == NanoHTTPD.Method.POST && session.uri == "/api/auth/avatar" -> uploadAvatar(session)
            session.method == NanoHTTPD.Method.DELETE && session.uri == "/api/auth/avatar" -> deleteAvatar()
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "认证接口不存在")
        }
    }

    fun serveSecurity(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        if (!isAuthorized(session)) return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "本地会话已失效")
        val uri = session.uri.trimEnd('/')
        val tail = uri.removePrefix("/api/security/")
        val parts = tail.split('/').filter(String::isNotBlank)
        return when {
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/login-logs" -> listSecurityLogs(session, "security_login_logs")
            session.method == NanoHTTPD.Method.DELETE && uri == "/api/security/login-logs" -> {
                val deleted = writableDatabase.delete("security_login_logs", null, null)
                audit(session, "login_logs.clear", "deleted=$deleted")
                ok(JSONObject().put("message", "登录日志已清理").put("deleted", deleted))
            }
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/sessions" -> listSecuritySessions(session)
            session.method == NanoHTTPD.Method.DELETE && uri == "/api/security/sessions/others" -> revokeOtherSessions(session)
            session.method == NanoHTTPD.Method.DELETE && parts.firstOrNull() == "sessions" && parts.getOrNull(1)?.toLongOrNull() != null -> revokeSession(session, parts[1].toLong())
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/ip-whitelist" -> listIpWhitelist()
            session.method == NanoHTTPD.Method.POST && uri == "/api/security/ip-whitelist" -> createIpWhitelist(session, body(session))
            parts.firstOrNull() == "ip-whitelist" && parts.getOrNull(1)?.toLongOrNull() != null -> mutateIpWhitelist(session, parts[1].toLong(), parts.getOrNull(2))
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/login-stats" -> loginStats()
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/audit-logs" -> listSecurityLogs(session, "security_audit_logs")
            session.method == NanoHTTPD.Method.GET && uri == "/api/security/2fa/status" -> twoFaUnsupported()
            (session.method == NanoHTTPD.Method.POST && (uri == "/api/security/2fa/setup" || uri == "/api/security/2fa/verify" || uri == "/api/security/2fa")) ||
                (session.method == NanoHTTPD.Method.DELETE && uri == "/api/security/2fa") -> twoFaUnavailable()
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "安全接口不存在")
        }
    }

    private fun listSecurityLogs(session: NanoHTTPD.IHTTPSession, table: String): NanoHTTPD.Response {
        val page = (session.parameters["page"]?.firstOrNull()?.toIntOrNull() ?: 1).coerceAtLeast(1)
        val size = (session.parameters["page_size"]?.firstOrNull()?.toIntOrNull() ?: 100).coerceIn(1, 200)
        val username = session.parameters["username"]?.firstOrNull()?.trim().orEmpty()
        val where = if (username.isNotEmpty()) " WHERE username = ?" else ""
        val args = if (username.isNotEmpty()) arrayOf(username) else emptyArray()
        val total = readableDatabase.rawQuery("SELECT COUNT(*) FROM $table$where", args).use { if (it.moveToFirst()) it.getInt(0) else 0 }
        val rows = JSONArray()
        readableDatabase.rawQuery("SELECT * FROM $table$where ORDER BY id DESC LIMIT ? OFFSET ?", args + arrayOf(size.toString(), ((page - 1) * size).toString())).use { c ->
            while (c.moveToNext()) rows.put(JSONObject().apply {
                put("id", c.long("id")); put("username", c.string("username")); put("ip", c.string("ip")); put("created_at", c.string("created_at"))
                if (table == "security_login_logs") { put("status", c.int("status")); put("message", c.string("message")); put("client_name", c.string("client_name")); put("user_agent", c.string("user_agent")) }
                else { put("action", c.string("action")); put("detail", c.string("detail")) }
            })
        }
        return ok(JSONObject().put("data", rows).put("total", total).put("page", page).put("page_size", size))
    }

    private fun listSecuritySessions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val current = bearerToken(session)
        val rows = queryRows("SELECT id,username,access_token,ip,client_name,user_agent,created_at,expires_at FROM security_sessions ORDER BY id DESC") { c ->
            JSONObject().put("id", c.long("id")).put("username", c.string("username")).put("ip", c.string("ip"))
                .put("client_name", c.string("client_name")).put("user_agent", c.string("user_agent"))
                .put("created_at", c.string("created_at")).put("expires_at", c.string("expires_at")).put("current", c.string("access_token") == current)
        }
        return ok(JSONObject().put("data", rows).put("total", rows.length()))
    }

    private fun revokeOtherSessions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val token = bearerToken(session).orEmpty()
        val deleted = writableDatabase.delete("security_sessions", "access_token <> ?", arrayOf(token))
        audit(session, "sessions.revoke_others", "deleted=$deleted")
        return ok(JSONObject().put("message", "其他会话已撤销").put("deleted", deleted))
    }

    private fun revokeSession(session: NanoHTTPD.IHTTPSession, id: Long): NanoHTTPD.Response {
        val token = readableDatabase.query("security_sessions", arrayOf("access_token"), "id=?", arrayOf(id.toString()), null, null, null).use { if (it.moveToFirst()) it.getString(0) else null }
            ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "会话不存在")
        val deleted = writableDatabase.delete("security_sessions", "id=?", arrayOf(id.toString()))
        writableDatabase.delete("local_sessions", "access_token=?", arrayOf(token))
        if (deleted == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "会话不存在")
        audit(session, "session.revoke", "id=$id")
        return ok(JSONObject().put("message", "会话已撤销").put("id", id))
    }

    private fun listIpWhitelist(): NanoHTTPD.Response {
        val rows = queryRows("SELECT id,ip,remarks,enabled,created_at,updated_at FROM security_ip_whitelist ORDER BY id") { c ->
            JSONObject().put("id", c.long("id")).put("ip", c.string("ip")).put("remarks", c.string("remarks")).put("enabled", c.int("enabled") != 0).put("created_at", c.string("created_at")).put("updated_at", c.string("updated_at"))
        }
        return ok(JSONObject().put("data", rows).put("total", rows.length()))
    }

    private fun createIpWhitelist(session: NanoHTTPD.IHTTPSession, json: JSONObject): NanoHTTPD.Response {
        val ip = json.optString("ip").trim()
        if (ip.isEmpty()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "IP 地址不能为空")
        val now = Instant.now().toString()
        return try {
            val id = writableDatabase.insertOrThrow("security_ip_whitelist", null, ContentValues().apply { put("ip", ip); put("remarks", json.optString("remarks")); put("enabled", if (json.optBoolean("enabled", true)) 1 else 0); put("created_at", now); put("updated_at", now) })
            audit(session, "ip_whitelist.create", "id=$id ip=$ip")
            ok(JSONObject().put("data", JSONObject().put("id", id).put("ip", ip)))
        } catch (_: Exception) { error(NanoHTTPD.Response.Status.CONFLICT, "IP 已存在") }
    }

    private fun mutateIpWhitelist(session: NanoHTTPD.IHTTPSession, id: Long, action: String?): NanoHTTPD.Response {
        if (session.method == NanoHTTPD.Method.DELETE && action == null) {
            val deleted = writableDatabase.delete("security_ip_whitelist", "id=?", arrayOf(id.toString()))
            if (deleted == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "白名单不存在")
            audit(session, "ip_whitelist.delete", "id=$id")
            return ok(JSONObject().put("message", "已删除").put("id", id))
        }
        if ((session.method == NanoHTTPD.Method.POST || session.method == NanoHTTPD.Method.PUT) && (action == "enable" || action == "disable")) {
            val enabled = action == "enable"
            val changed = writableDatabase.update("security_ip_whitelist", ContentValues().apply { put("enabled", if (enabled) 1 else 0); put("updated_at", Instant.now().toString()) }, "id=?", arrayOf(id.toString()))
            if (changed == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "白名单不存在")
            audit(session, "ip_whitelist.$action", "id=$id")
            return ok(JSONObject().put("data", JSONObject().put("id", id).put("enabled", enabled)))
        }
        if (session.method == NanoHTTPD.Method.PUT && action == null) {
            val json = body(session); val values = ContentValues().apply { if (json.has("ip")) put("ip", json.optString("ip").trim()); if (json.has("remarks")) put("remarks", json.optString("remarks")); if (json.has("enabled")) put("enabled", if (json.optBoolean("enabled")) 1 else 0); put("updated_at", Instant.now().toString()) }
            val changed = writableDatabase.update("security_ip_whitelist", values, "id=?", arrayOf(id.toString()))
            if (changed == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "白名单不存在")
            audit(session, "ip_whitelist.update", "id=$id")
            return ok(JSONObject().put("data", JSONObject().put("id", id)))
        }
        return error(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED, "不支持的白名单操作")
    }

    private fun loginStats(): NanoHTTPD.Response {
        fun n(where: String) = readableDatabase.rawQuery("SELECT COUNT(*) FROM security_login_logs WHERE $where", null).use { if (it.moveToFirst()) it.getInt(0) else 0 }
        return ok(JSONObject().put("data", JSONObject().put("total", n("1=1")).put("success", n("status=0")).put("failed", n("status<>0")).put("today", n("date(created_at)=date('now')")).put("today_success", n("status=0 AND date(created_at)=date('now')")).put("today_failed", n("status<>0 AND date(created_at)=date('now')"))))
    }

    private fun twoFaUnsupported(): NanoHTTPD.Response = ok(JSONObject().put("data", JSONObject().put("enabled", false).put("supported", false).put("reason", "Kotlin fallback 尚未实现真实 TOTP；不会报告为已启用")))
    private fun twoFaUnavailable(): NanoHTTPD.Response = NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.CONFLICT, "application/json; charset=utf-8", JSONObject().put("error", "当前 Kotlin fallback 不支持真实 TOTP").put("supported", false).put("enabled", false).toString())

    private fun audit(session: NanoHTTPD.IHTTPSession, action: String, detail: String) {
        val username = currentUsername(session)
        writableDatabase.insert("security_audit_logs", null, ContentValues().apply { put("username", username); put("ip", requestIp(session)); put("action", action); put("detail", detail); put("created_at", Instant.now().toString()) })
    }

    private fun currentUsername(session: NanoHTTPD.IHTTPSession): String = bearerToken(session)?.let { token -> readableDatabase.rawQuery("SELECT username FROM security_sessions WHERE access_token=?", arrayOf(token)).use { if (it.moveToFirst()) it.getString(0) else "admin" } } ?: "admin"
    private fun requestIp(session: NanoHTTPD.IHTTPSession): String = session.headers["x-forwarded-for"]?.substringBefore(',')?.trim().takeUnless { it.isNullOrEmpty() } ?: session.remoteIpAddress.orEmpty()
    private fun clientName(session: NanoHTTPD.IHTTPSession): String = session.headers["x-client-name"]?.takeIf(String::isNotBlank) ?: "Flutter Android"

    fun isAuthorized(session: NanoHTTPD.IHTTPSession): Boolean {
        val token = bearerToken(session) ?: return false
        return readableDatabase.rawQuery(
            "SELECT 1 FROM local_sessions WHERE id = 1 AND access_token = ?",
            arrayOf(token)
        ).use(Cursor::moveToFirst)
    }

    fun serveTerminal(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response = authenticated(session) {
        val uri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val parts = uri.trim('/').split('/').filter(String::isNotBlank)
        val id = parts.getOrNull(2).orEmpty()
        val action = parts.getOrNull(3).orEmpty()
        try {
            when {
                session.method == NanoHTTPD.Method.POST && parts == listOf("terminal", "sessions") -> {
                    val payload = body(session)
                    val rows = payload.optInt("rows", 24)
                    val columns = payload.optInt("columns", 80)
                    val rootfsCommand = AndroidLinuxRuntime.guestCommand(
                        appContext,
                        appContext.filesDir,
                        listOf("/bin/bash", "-l"),
                    ) ?: return@authenticated error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "ROOTFS_TERMINAL_UNAVAILABLE")
                    val data = terminalSessions.create(rootfsCommand, terminalEnvironment(), appContext.filesDir, "/bin/bash", rows, columns)
                    NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.CREATED, "application/json; charset=utf-8", JSONObject().put("data", data).toString())
                }
                session.method == NanoHTTPD.Method.GET && id.isNotBlank() && action.isBlank() ->
                    ok(JSONObject().put("data", terminalSessions.get(id, session.parms["cursor"]?.toLongOrNull() ?: 0L)))
                session.method == NanoHTTPD.Method.POST && id.isNotBlank() && action == "input" -> {
                    val payload = body(session)
                    val encoding = payload.optString("encoding", "utf8")
                    val raw = payload.optString("data")
                    val bytes = if (encoding == "base64") Base64.decode(raw, Base64.DEFAULT) else raw.toByteArray(Charsets.UTF_8)
                    terminalSessions.write(id, bytes)
                    ok(JSONObject().put("status", "accepted"))
                }
                session.method == NanoHTTPD.Method.PUT && id.isNotBlank() && action == "resize" -> {
                    val payload = body(session)
                    terminalSessions.resize(id, payload.optInt("rows", 24), payload.optInt("columns", 80))
                    ok(JSONObject().put("status", "resized"))
                }
                session.method == NanoHTTPD.Method.PUT && id.isNotBlank() && action == "stop" ->
                    ok(JSONObject().put("data", terminalSessions.stop(id)))
                session.method == NanoHTTPD.Method.DELETE && id.isNotBlank() && action.isBlank() -> {
                    terminalSessions.remove(id)
                    ok(JSONObject().put("status", "deleted"))
                }
                else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "终端接口不存在")
            }
        } catch (error: NoSuchElementException) {
            error(NanoHTTPD.Response.Status.NOT_FOUND, error.message ?: "终端会话不存在")
        } catch (error: IllegalArgumentException) {
            error(NanoHTTPD.Response.Status.BAD_REQUEST, error.message ?: "终端请求无效")
        } catch (error: IllegalStateException) {
            error(NanoHTTPD.Response.Status.CONFLICT, error.message ?: "终端会话状态冲突")
        }
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

    fun serveUsers(session: NanoHTTPD.IHTTPSession, prefix: String = "/api/users"): NanoHTTPD.Response {
        val tail = session.uri.removePrefix(prefix).trim('/')
        val parts = if (tail.isBlank()) emptyList() else tail.split('/')
        val id = parts.firstOrNull()?.toLongOrNull()
        return when {
            session.method == NanoHTTPD.Method.GET && id == null -> listUsers()
            session.method == NanoHTTPD.Method.POST && id == null -> createUser(body(session))
            session.method == NanoHTTPD.Method.PUT && id != null && parts.getOrNull(1) == "reset-password" -> resetUserPassword(id, body(session).optString("password"))
            session.method == NanoHTTPD.Method.PUT && id != null -> updateUser(id, body(session))
            session.method == NanoHTTPD.Method.DELETE && id != null -> if (writableDatabase.delete("local_users", "id=?", arrayOf(id.toString())) > 0) ok(JSONObject().put("message", "删除成功")) else error(NanoHTTPD.Response.Status.NOT_FOUND, "用户不存在")
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "用户接口不存在")
        }
    }

    private fun createUser(json: JSONObject): NanoHTTPD.Response {
        val username=json.optString("username").trim(); val password=json.optString("password")
        if (username.isBlank() || password.length !in 6..128) return error(NanoHTTPD.Response.Status.BAD_REQUEST,"用户名不能为空且密码需为 6-128 位")
        val salt=ByteArray(16).also(SecureRandom()::nextBytes); val now=Instant.now().toString()
        val id=try { writableDatabase.insertOrThrow("local_users",null,ContentValues().apply { put("username",username);put("password_hash",hashPassword(password,salt));put("password_salt",Base64.encodeToString(salt,Base64.NO_WRAP));put("role",json.optString("role","operator"));put("enabled",1);put("created_at",now);put("updated_at",now) }) } catch (_: Exception) { return error(NanoHTTPD.Response.Status.CONFLICT,"用户名已存在") }
        return ok(JSONObject().put("message","创建成功").put("data",JSONObject().put("id",id).put("username",username)))
    }
    private fun updateUser(id:Long,json:JSONObject):NanoHTTPD.Response { val v=ContentValues().apply { if(json.has("role"))put("role",json.optString("role"));if(json.has("enabled"))put("enabled",if(json.optBoolean("enabled"))1 else 0);put("updated_at",Instant.now().toString()) };return if(writableDatabase.update("local_users",v,"id=?",arrayOf(id.toString()))>0)ok(JSONObject().put("message","更新成功"))else error(NanoHTTPD.Response.Status.NOT_FOUND,"用户不存在") }
    private fun resetUserPassword(id:Long,password:String):NanoHTTPD.Response { if(password.length !in 6..128)return error(NanoHTTPD.Response.Status.BAD_REQUEST,"密码需为 6-128 位");val salt=ByteArray(16).also(SecureRandom()::nextBytes);val v=ContentValues().apply{put("password_hash",hashPassword(password,salt));put("password_salt",Base64.encodeToString(salt,Base64.NO_WRAP));put("updated_at",Instant.now().toString())};return if(writableDatabase.update("local_users",v,"id=?",arrayOf(id.toString()))>0)ok(JSONObject().put("message","密码重置成功"))else error(NanoHTTPD.Response.Status.NOT_FOUND,"用户不存在") }
    private fun changeOwnPassword(json:JSONObject):NanoHTTPD.Response { val user=readableDatabase.rawQuery("SELECT id,password_hash,password_salt FROM local_users ORDER BY id LIMIT 1",null).use{c->if(!c.moveToFirst())null else Triple(c.long("id"),c.string("password_hash"),c.string("password_salt"))}?:return error(NanoHTTPD.Response.Status.NOT_FOUND,"管理员不存在");val salt=Base64.decode(user.third,Base64.NO_WRAP);if(hashPassword(json.optString("old_password"),salt)!=user.second)return error(NanoHTTPD.Response.Status.UNAUTHORIZED,"原密码错误");return resetUserPassword(user.first,json.optString("new_password")) }
    private fun changeOwnUsername(json:JSONObject):NanoHTTPD.Response { val name=json.optString("username").trim();if(name.isBlank())return error(NanoHTTPD.Response.Status.BAD_REQUEST,"用户名不能为空");return try{writableDatabase.update("local_users",ContentValues().apply{put("username",name);put("updated_at",Instant.now().toString())},"id=(SELECT id FROM local_users ORDER BY id LIMIT 1)",null);ok(JSONObject().put("message","用户名已修改").put("user",userJson()))}catch(_:Exception){error(NanoHTTPD.Response.Status.CONFLICT,"用户名已存在")} }
    private fun deleteAvatar():NanoHTTPD.Response { writableDatabase.execSQL("UPDATE local_users SET avatar_url='' WHERE id=(SELECT id FROM local_users ORDER BY id LIMIT 1)");return ok(JSONObject().put("message","头像已删除")) }
    private fun uploadAvatar(session:NanoHTTPD.IHTTPSession):NanoHTTPD.Response { if(!session.headers["content-type"].orEmpty().contains("multipart/form-data",true))return error(NanoHTTPD.Response.Status.BAD_REQUEST,"头像上传仅支持 multipart/form-data，字段名 avatar");val files=HashMap<String,String>();session.parseBody(files);val temp=files["avatar"]?:return error(NanoHTTPD.Response.Status.BAD_REQUEST,"multipart 缺少 avatar 文件");val source=File(temp);if(!source.isFile)return error(NanoHTTPD.Response.Status.BAD_REQUEST,"头像文件无效");val target=File(appContext.filesDir,"avatar-${System.currentTimeMillis()}.bin");source.copyTo(target,true);val url="/api/auth/avatar/file";writableDatabase.execSQL("UPDATE local_users SET avatar_url=? WHERE id=(SELECT id FROM local_users ORDER BY id LIMIT 1)",arrayOf(url));return ok(JSONObject().put("message","头像已上传").put("avatar_url",url)) }

    fun serveManagement(session:NanoHTTPD.IHTTPSession):NanoHTTPD.Response {
        val uri=session.uri.removePrefix("/api/v1").removePrefix("/api")
        return when {
            uri.startsWith("/ssh-keys") -> serveSimpleSecretCrud(session,uri,"/ssh-keys","ssh_keys","private_key")
            uri.startsWith("/platform-tokens") -> servePlatformTokens(session,uri)
            uri.startsWith("/open-api/apps") -> serveOpenApiSecretContract(session, uri) ?: serveOpenApi(session,uri)
            uri=="/sponsors" && session.method==NanoHTTPD.Method.GET -> ok(JSONObject().put("data",JSONObject().put("sponsors",JSONArray()).put("count",0).put("total_amount",0).put("updated_at",JSONObject.NULL)))
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND,"管理接口不存在")
        }
    }
    private fun serveSimpleSecretCrud(s:NanoHTTPD.IHTTPSession,u:String,p:String,t:String,secret:String):NanoHTTPD.Response { val id=u.removePrefix(p).trim('/').toLongOrNull();return when { s.method==NanoHTTPD.Method.GET&&id==null->{val a=JSONArray();readableDatabase.query(t,null,null,null,null,null,"id DESC").use{c->while(c.moveToNext())a.put(JSONObject().put("id",c.long("id")).put("name",c.string("name")).put(secret,"********").put("created_at",c.string("created_at")).put("updated_at",c.string("updated_at")))};ok(JSONObject().put("data",a))};s.method==NanoHTTPD.Method.GET&&id!=null->{readableDatabase.query(t,null,"id=?",arrayOf(id.toString()),null,null,null).use{c->if(!c.moveToFirst())return error(NanoHTTPD.Response.Status.NOT_FOUND,"记录不存在");ok(JSONObject().put("data",JSONObject().put("id",id).put("name",c.string("name")).put(secret,c.string(secret))))}};s.method==NanoHTTPD.Method.POST&&id==null->{val j=body(s);val now=Instant.now().toString();val n=j.optString("name").trim();val v=j.optString(secret);if(n.isBlank()||v.isBlank())return error(NanoHTTPD.Response.Status.BAD_REQUEST,"名称和密钥不能为空");val x=writableDatabase.insert(t,null,ContentValues().apply{put("name",n);put(secret,v);put("created_at",now);put("updated_at",now)});ok(JSONObject().put("message","创建成功").put("data",JSONObject().put("id",x)))};s.method==NanoHTTPD.Method.PUT&&id!=null->{val j=body(s);val v=ContentValues().apply{if(j.has("name"))put("name",j.optString("name"));if(j.optString(secret).isNotBlank()&&j.optString(secret)!="********")put(secret,j.optString(secret));put("updated_at",Instant.now().toString())};if(writableDatabase.update(t,v,"id=?",arrayOf(id.toString()))>0)ok(JSONObject().put("message","更新成功"))else error(NanoHTTPD.Response.Status.NOT_FOUND,"记录不存在")};s.method==NanoHTTPD.Method.DELETE&&id!=null->{writableDatabase.delete(t,"id=?",arrayOf(id.toString()));ok(JSONObject().put("message","删除成功"))};else->error(NanoHTTPD.Response.Status.NOT_FOUND,"接口不存在") } }
    private fun servePlatformTokens(s:NanoHTTPD.IHTTPSession,u:String):NanoHTTPD.Response { if(u=="/platform-tokens/platforms"){if(s.method==NanoHTTPD.Method.GET){val a=JSONArray();readableDatabase.query("platforms",null,null,null,null,null,"name").use{c->while(c.moveToNext())a.put(JSONObject().put("id",c.long("id")).put("name",c.string("name")).put("label",c.string("label")).put("icon",c.string("icon")))};return ok(JSONObject().put("data",a))};if(s.method==NanoHTTPD.Method.POST){val j=body(s);val now=Instant.now().toString();val id=writableDatabase.insert("platforms",null,ContentValues().apply{put("name",j.optString("name"));put("label",j.optString("label",j.optString("name")));put("icon",j.optString("icon"));put("created_at",now);put("updated_at",now)});return ok(JSONObject().put("data",JSONObject().put("id",id)))}};val tail=u.removePrefix("/platform-tokens").trim('/');val parts=tail.split('/');val id=parts.firstOrNull()?.toLongOrNull();val action=parts.getOrNull(1);if(id!=null&&action in setOf("enable","disable")&&s.method==NanoHTTPD.Method.PUT){writableDatabase.update("platform_tokens",ContentValues().apply{put("enabled",if(action=="enable")1 else 0);put("updated_at",Instant.now().toString())},"id=?",arrayOf(id.toString()));return ok(JSONObject().put("message","ok"))};return serveTokenCrud(s,id) }
    private fun serveTokenCrud(s:NanoHTTPD.IHTTPSession,id:Long?):NanoHTTPD.Response { return when {s.method==NanoHTTPD.Method.GET&&id==null->{val a=JSONArray();val q="SELECT t.*,p.name platform_name,p.label platform_label FROM platform_tokens t LEFT JOIN platforms p ON p.id=t.platform_id";readableDatabase.rawQuery(q,null).use{c->while(c.moveToNext())a.put(JSONObject().put("id",c.long("id")).put("platform_id",c.long("platform_id")).put("platform",JSONObject().put("name",c.string("platform_name")).put("label",c.string("platform_label"))).put("name",c.string("name")).put("token","********").put("remarks",c.string("remarks")).put("enabled",c.int("enabled")!=0))};ok(JSONObject().put("data",a))};s.method==NanoHTTPD.Method.POST&&id==null->{val j=body(s);val now=Instant.now().toString();val x=writableDatabase.insert("platform_tokens",null,ContentValues().apply{put("platform_id",j.optLong("platform_id"));put("name",j.optString("name"));put("token",j.optString("token"));put("remarks",j.optString("remarks"));put("enabled",1);put("created_at",now);put("updated_at",now)});ok(JSONObject().put("data",JSONObject().put("id",x)))};s.method==NanoHTTPD.Method.PUT&&id!=null->{val j=body(s);val v=ContentValues().apply{if(j.has("name"))put("name",j.optString("name"));if(j.optString("token").isNotBlank()&&j.optString("token")!="********")put("token",j.optString("token"));if(j.has("remarks"))put("remarks",j.optString("remarks"));put("updated_at",Instant.now().toString())};writableDatabase.update("platform_tokens",v,"id=?",arrayOf(id.toString()));ok(JSONObject().put("message","更新成功"))};s.method==NanoHTTPD.Method.DELETE&&id!=null->{writableDatabase.delete("platform_tokens","id=?",arrayOf(id.toString()));ok(JSONObject().put("message","删除成功"))};else->error(NanoHTTPD.Response.Status.NOT_FOUND,"平台令牌接口不存在")} }

    private fun serveOpenApiSecretContract(session: NanoHTTPD.IHTTPSession, uri: String): NanoHTTPD.Response? {
        val parts = uri.removePrefix("/open-api/apps").trim('/').split('/')
        val id = parts.firstOrNull()?.toLongOrNull() ?: return null
        return when {
            parts.getOrNull(1) == "reset-secret" && session.method == NanoHTTPD.Method.PUT -> {
                val secret = randomToken()
                val changed = writableDatabase.update("open_api_apps", ContentValues().apply { put("secret", secret); put("updated_at", Instant.now().toString()) }, "id=?", arrayOf(id.toString()))
                if (changed == 0) error(NanoHTTPD.Response.Status.NOT_FOUND, "应用不存在") else ok(JSONObject().put("message", "密钥已重置").put("data", JSONObject().put("app_secret", secret)))
            }
            parts.getOrNull(1) in setOf("view-secret", "show-secret") && session.method == NanoHTTPD.Method.POST -> viewOpenApiSecret(session, id)
            else -> null
        }
    }

    private fun viewOpenApiSecret(session: NanoHTTPD.IHTTPSession, id: Long): NanoHTTPD.Response {
        val token = bearerToken(session) ?: return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "本地会话已失效")
        val user = readableDatabase.rawQuery(
            "SELECT u.password_hash,u.password_salt,u.role FROM security_sessions s JOIN local_users u ON u.username=s.username WHERE s.access_token=? AND s.expires_at>? LIMIT 1",
            arrayOf(token, Instant.now().toString()),
        ).use { cursor -> if (!cursor.moveToFirst()) null else Triple(cursor.string("password_hash"), cursor.string("password_salt"), cursor.string("role")) }
            ?: return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "本地会话已失效")
        if (user.third != "admin") return error(NanoHTTPD.Response.Status.FORBIDDEN, "需要管理员权限")
        val password = body(session).optString("password")
        if (password.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "请输入密码")
        if (hashPassword(password, Base64.decode(user.second, Base64.NO_WRAP)) != user.first) return error(NanoHTTPD.Response.Status.UNAUTHORIZED, "管理员密码错误")
        val secret = readableDatabase.query("open_api_apps", arrayOf("secret"), "id=?", arrayOf(id.toString()), null, null, null).use { cursor ->
            if (!cursor.moveToFirst()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "应用不存在")
            cursor.string("secret")
        }
        return ok(JSONObject().put("data", JSONObject().put("app_secret", secret)))
    }

    private fun serveOpenApi(s:NanoHTTPD.IHTTPSession,u:String):NanoHTTPD.Response { val parts=u.removePrefix("/open-api/apps").trim('/').split('/');val id=parts.firstOrNull()?.toLongOrNull();val action=parts.getOrNull(1);if(id!=null&&action=="logs"&&s.method==NanoHTTPD.Method.GET)return ok(JSONObject().put("data",JSONArray()).put("total",0).put("page",1).put("page_size",20));if(id!=null&&action in setOf("enable","disable")&&s.method==NanoHTTPD.Method.PUT){writableDatabase.update("open_api_apps",ContentValues().apply{put("enabled",if(action=="enable")1 else 0);put("updated_at",Instant.now().toString())},"id=?",arrayOf(id.toString()));return ok(JSONObject().put("message","ok"))};if(id!=null&&action=="reset-secret"&&s.method==NanoHTTPD.Method.PUT){val secret=randomToken();writableDatabase.update("open_api_apps",ContentValues().apply{put("secret",secret);put("updated_at",Instant.now().toString())},"id=?",arrayOf(id.toString()));return ok(JSONObject().put("message","密钥已重置").put("data",JSONObject().put("secret",secret)))};if(id!=null&&action in setOf("view-secret","show-secret")&&s.method==NanoHTTPD.Method.POST){val password=body(s).optString("password");val valid=readableDatabase.rawQuery("SELECT password_hash,password_salt FROM local_users ORDER BY id LIMIT 1",null).use{c->c.moveToFirst()&&hashPassword(password,Base64.decode(c.string("password_salt"),Base64.NO_WRAP))==c.string("password_hash")};if(!valid)return error(NanoHTTPD.Response.Status.UNAUTHORIZED,"管理员密码错误");val secret=readableDatabase.query("open_api_apps",arrayOf("secret"),"id=?",arrayOf(id.toString()),null,null,null).use{c->if(c.moveToFirst())c.string("secret")else return error(NanoHTTPD.Response.Status.NOT_FOUND,"应用不存在")};return ok(JSONObject().put("data",JSONObject().put("secret",secret)))};return when{s.method==NanoHTTPD.Method.GET&&id==null->{val a=JSONArray();readableDatabase.query("open_api_apps",null,null,null,null,null,"id DESC").use{c->while(c.moveToNext())a.put(openApiJson(c))};ok(JSONObject().put("data",a))};s.method==NanoHTTPD.Method.POST&&id==null->{val j=body(s);if(j.optString("name").isBlank())return error(NanoHTTPD.Response.Status.BAD_REQUEST,"名称不能为空");val now=Instant.now().toString();val key=randomToken().take(24);val secret=randomToken();val x=writableDatabase.insert("open_api_apps",null,ContentValues().apply{put("name",j.optString("name"));put("app_key",key);put("secret",secret);put("scopes",j.optString("scopes"));put("rate_limit",j.optInt("rate_limit",60));put("enabled",1);put("created_at",now);put("updated_at",now)});ok(JSONObject().put("message","创建成功").put("data",JSONObject().put("id",x).put("app_key",key).put("secret",secret)))};s.method==NanoHTTPD.Method.PUT&&id!=null->{val j=body(s);val v=ContentValues().apply{if(j.has("name"))put("name",j.optString("name"));if(j.has("scopes"))put("scopes",j.optString("scopes"));if(j.has("rate_limit"))put("rate_limit",j.optInt("rate_limit"));put("updated_at",Instant.now().toString())};if(writableDatabase.update("open_api_apps",v,"id=?",arrayOf(id.toString()))>0)ok(JSONObject().put("message","更新成功"))else error(NanoHTTPD.Response.Status.NOT_FOUND,"应用不存在")};s.method==NanoHTTPD.Method.DELETE&&id!=null->{writableDatabase.delete("open_api_apps","id=?",arrayOf(id.toString()));ok(JSONObject().put("message","删除成功"))};else->error(NanoHTTPD.Response.Status.NOT_FOUND,"Open API 接口不存在")} }
    private fun openApiJson(c:Cursor)=JSONObject().put("id",c.long("id")).put("name",c.string("name")).put("app_key",c.string("app_key")).put("secret","********").put("scopes",c.string("scopes")).put("rate_limit",c.int("rate_limit")).put("enabled",c.int("enabled")!=0).put("created_at",c.string("created_at")).put("updated_at",c.string("updated_at"))

    fun serveConfigScript(session:NanoHTTPD.IHTTPSession):NanoHTTPD.Response { val file=File(appContext.filesDir,"config.sh");return when(session.method){NanoHTTPD.Method.GET->ok(JSONObject().put("content",if(file.isFile)file.readText() else "").put("path",file.absolutePath));NanoHTTPD.Method.PUT->{val content=body(session).optString("content");file.writeText(content);ok(JSONObject().put("message","配置脚本已保存"))};else->error(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED,"仅支持 GET/PUT")} }

    fun serveNotifications(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        val action = segments.getOrNull(2)
        return when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/notifications/types" -> notificationTypes()
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/notifications/send" -> sendNotificationRequest(body(session))
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("notification_channels", notificationRows())
            session.method == NanoHTTPD.Method.POST && id == null -> createNotification(body(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateNotification(id, body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE && action == null -> deleteNotification(id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable") -> setNotificationEnabled(id, action == "enable")
            id != null && session.method == NanoHTTPD.Method.POST && action == "test" -> sendNotificationByIds("呆呆面板测试通知", "通知渠道配置正常", setOf(id), includeDisabled = true, isTest = true)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "通知接口不存在")
        }
    }

    private fun notificationRows(): JSONArray {
        val rows = JSONArray()
        readableDatabase.query("notification_channels", null, null, null, null, null, "id ASC").use { cursor ->
            while (cursor.moveToNext()) rows.put(notificationJson(cursor))
        }
        return rows
    }

    private fun notificationJson(cursor: Cursor) = JSONObject().apply {
        put("id", cursor.long("id")); put("name", cursor.string("name")); put("type", cursor.string("type"))
        put("config", try { JSONObject(cursor.string("config")) } catch (_: Exception) { JSONObject() })
        put("push_scope", cursor.string("push_scope")); put("enabled", cursor.int("enabled") != 0)
        put("today_send_count", cursor.int("today_send_count")); put("last_test_at", cursor.string("last_test_at")); put("last_test_status", cursor.string("last_test_status"))
        put("created_at", cursor.string("created_at")); put("updated_at", cursor.string("updated_at"))
    }

    private fun notificationTypes(): NanoHTTPD.Response = ok(JSONObject().put("data", JSONArray()
        .put(JSONObject().put("type", "android_local").put("name", "Android 本地通知"))
        .put(JSONObject().put("type", "webhook").put("name", "Webhook"))
        .put(JSONObject().put("type", "telegram").put("name", "Telegram"))
        .put(JSONObject().put("type", "dingtalk").put("name", "钉钉"))
        .put(JSONObject().put("type", "feishu").put("name", "飞书"))
        .put(JSONObject().put("type", "bark").put("name", "Bark"))
        .put(JSONObject().put("type", "pushplus").put("name", "PushPlus"))
        .put(JSONObject().put("type", "serverchan").put("name", "Server酱"))
        .put(JSONObject().put("type", "pushdeer").put("name", "PushDeer"))
        .put(JSONObject().put("type", "discord").put("name", "Discord"))
        .put(JSONObject().put("type", "slack").put("name", "Slack"))
        .put(JSONObject().put("type", "ntfy").put("name", "ntfy"))
        .put(JSONObject().put("type", "gotify").put("name", "Gotify"))
        .put(JSONObject().put("type", "wxpusher").put("name", "WxPusher"))))

    private fun configString(json: JSONObject): String {
        val value = json.opt("config")
        return when (value) { is JSONObject -> value.toString(); is String -> JSONObject(value.ifBlank { "{}" }).toString(); else -> "{}" }
    }

    private fun createNotification(json: JSONObject): NanoHTTPD.Response {
        val name = json.optString("name").trim(); val type = normalizeNotificationType(json.optString("type"))
        if (name.isEmpty() || type !in supportedNotificationTypes) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "通知渠道名称或类型无效")
        val now = Instant.now().toString()
        val id = writableDatabase.insertOrThrow("notification_channels", null, ContentValues().apply {
            put("name", name); put("type", type); put("config", configString(json)); put("push_scope", normalizePushScope(json.optString("push_scope"))); put("enabled", if (json.optBoolean("enabled", true)) 1 else 0); put("created_at", now); put("updated_at", now)
        })
        return ok(JSONObject().put("data", JSONObject().put("id", id)).put("message", "创建成功"))
    }

    private fun updateNotification(id: Long, json: JSONObject): NanoHTTPD.Response {
        if (json.has("type") && normalizeNotificationType(json.optString("type")) !in supportedNotificationTypes) {
            return error(NanoHTTPD.Response.Status.BAD_REQUEST, "通知渠道类型无效")
        }
        if (json.has("name") && json.optString("name").trim().isEmpty()) {
            return error(NanoHTTPD.Response.Status.BAD_REQUEST, "通知渠道名称不能为空")
        }
        val values = ContentValues().apply {
            if (json.has("name")) put("name", json.optString("name").trim())
            if (json.has("type")) put("type", normalizeNotificationType(json.optString("type")))
            if (json.has("config")) put("config", configString(json))
            if (json.has("push_scope")) put("push_scope", normalizePushScope(json.optString("push_scope")))
            if (json.has("enabled")) put("enabled", if (json.optBoolean("enabled")) 1 else 0)
            put("updated_at", Instant.now().toString())
        }
        if (writableDatabase.update("notification_channels", values, "id=?", arrayOf(id.toString())) == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "通知渠道不存在")
        return ok(JSONObject().put("message", "保存成功"))
    }

    private fun setNotificationEnabled(id: Long, enabled: Boolean): NanoHTTPD.Response {
        val changed = writableDatabase.update("notification_channels", ContentValues().apply { put("enabled", if (enabled) 1 else 0); put("updated_at", Instant.now().toString()) }, "id=?", arrayOf(id.toString()))
        return if (changed == 0) error(NanoHTTPD.Response.Status.NOT_FOUND, "通知渠道不存在") else ok(JSONObject().put("message", "ok"))
    }

    private fun sendNotificationRequest(json: JSONObject): NanoHTTPD.Response {
        val title = json.optString("title").trim(); val content = json.optString("content").trim()
        if (title.isEmpty() || content.isEmpty()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "标题和正文不能为空")
        val ids = when {
            json.has("channel_id") -> setOf(json.optLong("channel_id"))
            json.optJSONArray("channel_ids") != null -> json.optJSONArray("channel_ids")!!.let { a -> (0 until a.length()).map { a.optLong(it) }.toSet() }
            else -> null
        }
        return sendNotificationByIds(title, content, ids, includeDisabled = false)
    }

    private fun sendNotificationByIds(title: String, content: String, ids: Set<Long>?, includeDisabled: Boolean, isTest: Boolean = false): NanoHTTPD.Response {
        val failures = JSONArray(); var sent = 0
        readableDatabase.query("notification_channels", arrayOf("id", "type", "config", "push_scope", "enabled"), null, null, null, null, "id ASC").use { cursor ->
            while (cursor.moveToNext()) {
                val id = cursor.long("id"); if (ids != null && id !in ids) continue
                if (ids == null && cursor.string("push_scope") == "bound") continue
                if (!includeDisabled && cursor.int("enabled") == 0) continue
                try {
                    sendChannel(cursor.string("type"), JSONObject(cursor.string("config")), title, content); sent++
                    val today = java.time.LocalDate.now().toString()
                    writableDatabase.execSQL("UPDATE notification_channels SET today_send_count=CASE WHEN today_send_date=? THEN today_send_count+1 ELSE 1 END,today_send_date=? WHERE id=?", arrayOf<Any?>(today, today, id))
                    if (isTest) writableDatabase.execSQL("UPDATE notification_channels SET last_test_at=?,last_test_status='success' WHERE id=?", arrayOf<Any?>(Instant.now().toString(), id))
                } catch (e: Exception) {
                    if (isTest) writableDatabase.execSQL("UPDATE notification_channels SET last_test_at=?,last_test_status='failed' WHERE id=?", arrayOf<Any?>(Instant.now().toString(), id))
                    failures.put(JSONObject().put("id", id).put("error", e.message ?: "发送失败"))
                }
            }
        }
        if (sent == 0 && failures.length() > 0) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, failures.optJSONObject(0)?.optString("error") ?: "发送失败")
        if (sent == 0 && ids != null) return error(NanoHTTPD.Response.Status.NOT_FOUND, "未找到已启用的目标通知渠道")
        return ok(JSONObject().put("message", "已发送 $sent 个渠道").put("sent", sent).put("failures", failures))
    }

    private fun normalizePushScope(value: String): String = if (value.trim().lowercase() == "bound") "bound" else "default"
    private fun normalizeNotificationType(value: String): String = when (value.trim().lowercase()) {
        "pludplus" -> "pushplus"
        else -> value.trim().lowercase()
    }

    private fun deleteNotification(id: Long): NanoHTTPD.Response {
        writableDatabase.beginTransaction()
        return try {
            writableDatabase.execSQL("UPDATE tasks SET notification_channel_id=NULL WHERE notification_channel_id=?", arrayOf(id))
            val deleted = writableDatabase.delete("notification_channels", "id=?", arrayOf(id.toString()))
            if (deleted == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "通知渠道不存在")
            writableDatabase.setTransactionSuccessful()
            ok(JSONObject().put("data", JSONObject().put("id", id)))
        } finally {
            writableDatabase.endTransaction()
        }
    }

    private fun sendChannel(type: String, config: JSONObject, title: String, content: String) {
        when (type) {
            "android_local" -> postAndroidNotification("panel_channel", title, content)
            "webhook" -> httpPost(config.optString("url").ifBlank { config.optString("webhook") }, JSONObject().put("title", title).put("content", content), emptyMap())
            "telegram" -> requireBusinessSuccess("telegram", httpPost(config.optString("api_host", "https://api.telegram.org").trimEnd('/') + "/bot" + config.optString("token") + "/sendMessage", JSONObject().put("chat_id", config.optString("chat_id")).put("text", "$title\n$content"), emptyMap()))
            "dingtalk" -> {
                require(config.optString("secret").isBlank()) { "Kotlin fallback 暂不支持钉钉加签，请使用完整 Go Core 或移除 secret" }
                requireBusinessSuccess("dingtalk", httpPost(config.optString("webhook"), JSONObject().put("msgtype", "markdown").put("markdown", JSONObject().put("title", title).put("text", "### $title\n$content")), emptyMap()))
            }
            "feishu" -> {
                require(config.optString("secret").isBlank()) { "Kotlin fallback 暂不支持飞书加签，请使用完整 Go Core 或移除 secret" }
                requireBusinessSuccess("feishu", httpPost(config.optString("webhook"), JSONObject().put("msg_type", "text").put("content", JSONObject().put("text", "$title\n$content")), emptyMap()))
            }
            "bark" -> requireBusinessSuccess("bark", httpPost(config.optString("server", "https://api.day.app").trimEnd('/') + "/push", JSONObject().put("device_key", config.optString("key")).put("title", title).put("body", content), emptyMap()))
            "pushplus" -> requireBusinessSuccess("pushplus", httpPost("https://www.pushplus.plus/send", JSONObject().put("token", config.optString("token")).put("title", title).put("content", content).put("topic", config.optString("topic")).put("template", config.optString("template", "html")), emptyMap()))
            "serverchan" -> requireBusinessSuccess("serverchan", httpPost("https://sctapi.ftqq.com/" + config.optString("key") + ".send", "title=" + java.net.URLEncoder.encode(title, "UTF-8") + "&desp=" + java.net.URLEncoder.encode(content, "UTF-8"), emptyMap(), "application/x-www-form-urlencoded"))
            "pushdeer" -> requireBusinessSuccess("pushdeer", httpPost(config.optString("server", "https://api2.pushdeer.com").trimEnd('/') + "/message/push", JSONObject().put("pushkey", config.optString("key")).put("text", title).put("desp", content), emptyMap()))
            "discord" -> httpPost(config.optString("webhook"), JSONObject().put("content", "**$title**\n$content"), emptyMap())
            "slack" -> httpPost(config.optString("webhook"), JSONObject().put("text", "*$title*\n$content"), emptyMap())
            "ntfy" -> {
                val base = config.optString("server", "https://ntfy.sh").trimEnd('/'); val topic = config.optString("topic")
                val headers = mutableMapOf("Title" to title); config.optString("token").takeIf { it.isNotBlank() }?.let { headers["Authorization"] = "Bearer $it" }
                httpPost("$base/$topic", content, headers, "text/plain; charset=utf-8")
            }
            "gotify" -> httpPost(config.optString("server").trimEnd('/') + "/message?token=" + java.net.URLEncoder.encode(config.optString("token"), "UTF-8"), JSONObject().put("title", title).put("message", content).put("priority", 5), emptyMap())
            "wxpusher" -> requireBusinessSuccess("wxpusher", httpPost(config.optString("server", "https://wxpusher.zjiecode.com").trimEnd('/') + "/api/send/message", JSONObject().put("appToken", config.optString("app_token")).put("summary", title).put("content", content).put("contentType", 1).put("uids", JSONArray(config.optString("uids").split(',').map(String::trim).filter(String::isNotEmpty))), emptyMap()))
            else -> throw IllegalArgumentException("不支持的通知类型")
        }
    }

    private val supportedNotificationTypes = setOf("android_local", "webhook", "telegram", "dingtalk", "feishu", "bark", "pushplus", "serverchan", "pushdeer", "discord", "slack", "ntfy", "gotify", "wxpusher")

    private fun requireBusinessSuccess(type: String, responseBody: String) {
        val response = runCatching { JSONObject(responseBody) }.getOrElse { throw IllegalStateException("$type 返回无效 JSON") }
        val success = when (type) {
            "telegram" -> response.optBoolean("ok", false)
            "pushplus" -> response.optInt("code", -1) == 200
            "dingtalk" -> response.optInt("errcode", -1) == 0
            "feishu" -> response.optInt("code", response.optInt("StatusCode", -1)) == 0
            "bark" -> response.optInt("code", -1) == 200
            "serverchan" -> response.optInt("code", response.optInt("errno", -1)) == 0
            "pushdeer" -> response.optInt("code", -1) == 0
            "wxpusher" -> response.optInt("code", -1) in setOf(0, 1000)
            else -> true
        }
        if (!success) throw IllegalStateException(response.optString("msg").ifBlank { response.optString("errmsg").ifBlank { "$type 业务响应失败" } })
    }

    private fun httpPost(url: String, payload: Any, headers: Map<String, String>, contentType: String = "application/json; charset=utf-8"): String {
        val uri = URI(url); if (uri.scheme !in setOf("http", "https") || uri.host.isNullOrBlank()) throw IllegalArgumentException("仅支持 HTTP(S) 地址")
        if (InetAddress.getAllByName(uri.host).any(::isPrivateNotificationTarget)) throw IllegalArgumentException("拒绝 localhost/private 通知目标")
        val connection = uri.toURL().openConnection() as HttpURLConnection
        connection.connectTimeout = 5000; connection.readTimeout = 10000; connection.instanceFollowRedirects = false; connection.requestMethod = "POST"; connection.doOutput = true
        connection.setRequestProperty("Content-Type", contentType); headers.forEach(connection::setRequestProperty)
        connection.outputStream.use { it.write(payload.toString().toByteArray(Charsets.UTF_8)) }
        val code = connection.responseCode
        val responseBody = (if (code in 200..299) connection.inputStream else connection.errorStream)
            ?.bufferedReader()?.use { it.readText() }.orEmpty()
        connection.disconnect()
        if (code !in 200..299) throw IllegalStateException("通知服务返回 HTTP $code")
        return responseBody
    }

    private fun isPrivateNotificationTarget(address: InetAddress): Boolean {
        if (address.isAnyLocalAddress || address.isLoopbackAddress || address.isSiteLocalAddress || address.isLinkLocalAddress || address.isMulticastAddress) return true
        val bytes = address.address
        if (bytes.size != 16) return false
        val first = bytes[0].toInt() and 0xff; val second = bytes[1].toInt() and 0xff
        if ((first and 0xfe) == 0xfc) return true // IPv6 unique-local fc00::/7
        if (first == 0x20 && second == 0x01 && bytes[2].toInt() == 0x0d && bytes[3].toInt() == 0xb8) return true // documentation-only
        return bytes.take(10).all { it.toInt() == 0 } && bytes[10].toInt() == -1 && bytes[11].toInt() == -1 && isPrivateNotificationTarget(InetAddress.getByAddress(bytes.copyOfRange(12, 16)))
    }

    private fun postAndroidNotification(channelId: String, title: String, content: String) {
        if (android.os.Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(appContext, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            throw IllegalStateException("POST_NOTIFICATIONS_DENIED: 请在系统设置中允许通知")
        }
        val manager = appContext.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (android.os.Build.VERSION.SDK_INT >= 26) manager.createNotificationChannel(NotificationChannel(channelId, if (channelId == "task_channel") "任务通知" else "面板通知", NotificationManager.IMPORTANCE_DEFAULT))
        manager.notify((System.nanoTime() and 0x7fffffff).toInt(), NotificationCompat.Builder(appContext, channelId).setSmallIcon(R.mipmap.ic_launcher).setContentTitle(title).setContentText(content).setStyle(NotificationCompat.BigTextStyle().bigText(content)).setAutoCancel(true).build())
    }

    fun serveTasks(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        val action = segments.getOrNull(2)
        return when {
            normalizedUri == "/tasks/views" || normalizedUri == "/tasks/views/reorder" || normalizedUri.startsWith("/tasks/views/") -> serveTaskViews(session, normalizedUri)
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/cron/templates" ->
                cronTemplates()
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/tasks/cron/parse" ->
                cronParse(body(session))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/notification-channels" ->
                ok(JSONObject().put("data", notificationRows()))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/tasks/export" -> exportTasks()
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/tasks/import" -> importTasks(bodyOrUploadedJson(session))
            normalizedUri.startsWith("/tasks/batch/") -> serveTaskBatch(session, action)
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("tasks", taskRows())
            session.method == NanoHTTPD.Method.POST && id == null -> createTask(body(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateTask(id, try { body(session) } catch (_: Exception) { JSONObject() })
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("tasks", id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable", "run", "stop", "pin", "unpin") ->
                updateTaskStatus(id, action!!)
            id != null && session.method == NanoHTTPD.Method.GET && (action == "latest-log" || action == "log") -> latestTaskLogResponse(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "live-logs" -> liveTaskLogResponse(id, session)
            id != null && session.method == NanoHTTPD.Method.GET && action == "stats" -> taskStats(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "log-files" -> taskLogFiles(id)
            id != null && session.method == NanoHTTPD.Method.POST && action == "copy" -> copyTask(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == null -> taskDetail(id)
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "任务接口尚未实现")
        }
    }

    fun serveLogs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        val id = segments.getOrNull(1)?.toLongOrNull()
        return when {
            session.method == NanoHTTPD.Method.GET && id != null && segments.getOrNull(2) == "stream" -> taskLogStream(id, session)
            session.method == NanoHTTPD.Method.GET && id != null -> taskLogByIdJson(id)?.let(::ok)
                ?: error(NanoHTTPD.Response.Status.NOT_FOUND, "日志不存在")
            session.method == NanoHTTPD.Method.DELETE && id != null -> delete("task_logs_local", id)
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/logs/batch-delete" -> deleteLogs(body(session))
            session.method == NanoHTTPD.Method.DELETE && normalizedUri == "/logs/clean" -> cleanLogs(session)
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/logs/clean" -> cleanLogs(session)
            session.method == NanoHTTPD.Method.GET -> {
                val query = LocalLogQueryContract.build(session.parms)
                val source = " FROM task_logs_local l LEFT JOIN tasks t ON t.id = l.task_id${query.where}"
                val total = readableDatabase.rawQuery("SELECT COUNT(*)$source", query.args).use {
                    if (it.moveToFirst()) it.getLong(0) else 0L
                }
                val taskLogs = queryRows(
                    "SELECT l.*, t.name task_name$source ORDER BY l.started_at DESC, l.id DESC LIMIT ? OFFSET ?",
                    query.args + arrayOf(query.limit.toString(), query.offset.toString()),
                ) { taskLogJson(it).optJSONObject("data") ?: JSONObject() }
                ok(JSONObject()
                    .put("data", taskLogs)
                    .put("total", total)
                    .put("page", query.offset / query.limit + 1)
                    .put("page_size", query.limit))
            }
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
            id != null && session.method == NanoHTTPD.Method.PUT && action == null -> updateEnv(id, try { body(session) } catch (_: Exception) { JSONObject() })
            id != null && session.method == NanoHTTPD.Method.DELETE -> delete("envs", id)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable") ->
                updateEnvEnabled(id, action == "enable")
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("move-top", "cancel-top") -> moveEnvTop(id, action == "move-top")
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "环境变量接口尚未实现")
        }
    }

    fun serveScripts(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalizedUri = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val segments = normalizedUri.trim('/').split('/')
        return try {
            when {
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts/tree" -> scriptTree()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/scripts" -> paginated("scripts", scriptRows())
            session.method == NanoHTTPD.Method.GET && normalizedUri.startsWith("/scripts/content") -> scriptContent(session.parms["path"].orEmpty())
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
            session.method == NanoHTTPD.Method.GET && segments.size == 4 && segments[0] == "scripts" && segments[1] == "run" && segments[3] == "logs" -> scriptRunLogs(segments[2], session)
            session.method == NanoHTTPD.Method.PUT && segments.size == 4 && segments[0] == "scripts" && segments[1] == "run" && segments[3] == "stop" -> stopScriptRun(segments[2])
            session.method == NanoHTTPD.Method.DELETE && segments.size == 3 && segments[0] == "scripts" && segments[1] == "run" -> clearScriptRun(segments[2])
            else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本接口尚未实现")
            }
        } catch (error: IllegalArgumentException) {
            error(NanoHTTPD.Response.Status.BAD_REQUEST, error.message ?: "脚本路径无效")
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
                val logs = JSONArray(cursor.string("logs_json"))
                // Actual process lines belong in panel logs without synthetic prefixes.
                for (index in 0 until logs.length()) {
                    lines += logs.optString(index)
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
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/deps/python-runtime-default" -> setPythonDefault(body(session))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/python-runtime-default" -> ok(JSONObject().put("data", JSONObject().put("version", configValue("python_runtime_default", "3.14"))))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/pip" -> installedPipResponse()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/npm" -> installedNpmResponse()
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/mirrors" -> persistedMirrors()
            session.method == NanoHTTPD.Method.PUT && normalizedUri == "/deps/mirrors" -> setMirrors(body(session))
            session.method == NanoHTTPD.Method.GET && normalizedUri == "/deps/export" -> exportDependencies(session.parms["type"].orEmpty())
            session.method == NanoHTTPD.Method.GET && id != null && action == "log-stream" -> dependencyLog(id)
            session.method == NanoHTTPD.Method.GET && id != null && action == "status" -> dependencyStatus(id)
            session.method == NanoHTTPD.Method.PUT && id != null && action == "cancel" ->
                updateDependencyStatus(id, "cancelled", "Dependency operation cancelled")
            session.method == NanoHTTPD.Method.PUT && id != null && action == "reinstall" ->
                reinstallDependency(id)
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/deps/batch-delete" ->
                deleteDependencies(body(session))
            session.method == NanoHTTPD.Method.POST && normalizedUri == "/deps/batch-reinstall" ->
                reinstallDependencies(body(session))
            session.method == NanoHTTPD.Method.GET && id == null -> paginated("dependencies", dependencyRows(session))
            session.method == NanoHTTPD.Method.POST && id == null -> createDependencies(body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE -> deleteDependency(id)
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
            session.method == NanoHTTPD.Method.POST && key.isBlank() -> setConfig(try { body(session) } catch (_: Exception) { JSONObject() })
            session.method == NanoHTTPD.Method.PUT && key.isNotBlank() && normalizedUri != "/configs/batch" -> setConfig(bodyWithKey(session, key))
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
            val value = configPrefs.getString("auto_install_deps", "true") ?: "true"
            data.put("auto_install_deps", JSONObject().put("value", value).put("default_value", "true"))
        }
        return ok(JSONObject().put("data", data))
    }

    private fun getConfig(key: String): NanoHTTPD.Response {
        val value = configValue(key, "")
        return ok(JSONObject().put("data", JSONObject().put("key", key).put("value", value).put("default_value", value)))
    }

    private fun bodyWithKey(session: NanoHTTPD.IHTTPSession, key: String): JSONObject {
        return try { body(session).apply { if (!has("key")) put("key", key) } } catch (_: Exception) { JSONObject().apply { put("key", key) } }
    }

    private fun setConfig(json: JSONObject): NanoHTTPD.Response {
        val configKey = json.optString("key").ifBlank { json.optString("_key") }
        val value = normalizedConfigValue(configKey, json.optString("value"))
            ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "$configKey 必须是合法的 HTTP(S) 地址")
        synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
            upsertConfig(configKey, value)
            if (configKey in mirrorConfigKeys) AndroidLinuxRuntime.ensureRootfsReady(appContext)
        }
        return ok(JSONObject().put("message", "配置已保存"))
    }

    private fun setConfigs(configs: JSONObject): NanoHTTPD.Response {
        val values = linkedMapOf<String, String>()
        for (key in configs.keys()) {
            values[key] = normalizedConfigValue(key, configs.optString(key))
                ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "$key 必须是合法的 HTTP(S) 地址")
        }
        synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
            values.forEach { (key, value) -> upsertConfig(key, value) }
            if (values.keys.any { it in mirrorConfigKeys }) AndroidLinuxRuntime.ensureRootfsReady(appContext)
        }
        return ok(JSONObject().put("message", "配置已保存"))
    }

    private fun deleteConfig(key: String): NanoHTTPD.Response {
        if (key in mirrorConfigKeys) {
            synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
                val defaultValue = defaultMirrorValue(key)
                upsertConfig(key, defaultValue)
                AndroidLinuxRuntime.ensureRootfsReady(appContext)
            }
        } else {
            writableDatabase.delete("local_configs", "key = ?", arrayOf(key))
        }
        return ok(JSONObject().put("message", "配置已删除"))
    }

    private fun upsertConfig(key: String, value: String, persistPreference: Boolean = true) {
        require(key.isNotBlank()) { "配置 key 不能为空" }
        val normalizedValue = normalizedConfigValue(key, value)
            ?: throw IllegalArgumentException("$key 必须是合法的 HTTP(S) 地址")
        val values = ContentValues().apply {
            put("key", key)
            put("value", normalizedValue)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.insertWithOnConflict("local_configs", null, values, SQLiteDatabase.CONFLICT_REPLACE)
        if (key == "allow_unverified_android_abi_wheels" || key == "auto_install_deps") {
            configPrefs.edit().putString(key, normalizedValue).apply()
        }
        if (persistPreference && key in mirrorConfigKeys) {
            check(configPrefs.edit().putString(key, normalizedValue).commit()) { "无法持久化镜像配置" }
        }
    }

    private fun normalizedConfigValue(key: String, value: String): String? =
        if (key in mirrorConfigKeys) {
            if (value.isBlank()) defaultMirrorValue(key) else AndroidLinuxRuntime.normalizeMirrorUrl(value)
        } else value

    private fun defaultMirrorValue(key: String): String = when (key) {
        AndroidLinuxRuntime.PIP_MIRROR_KEY -> AndroidLinuxRuntime.PYTHON_PIP_ALIBABA_INDEX
        AndroidLinuxRuntime.NPM_MIRROR_KEY -> AndroidLinuxRuntime.NODE_NPM_NPMMIRROR_REGISTRY
        AndroidLinuxRuntime.LINUX_MIRROR_KEY -> AndroidLinuxRuntime.ALPINE_APK_HUAWEI_MIRROR
        else -> ""
    }

    private val mirrorConfigKeys: Set<String>
        get() = setOf(AndroidLinuxRuntime.PIP_MIRROR_KEY, AndroidLinuxRuntime.NPM_MIRROR_KEY, AndroidLinuxRuntime.LINUX_MIRROR_KEY)

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
        val uri = (session.uri ?: "").substringBefore("?").trimEnd('/')
        return try {
            when {
                session.method == NanoHTTPD.Method.GET && uri == "/api/system/backups" ->
                    ok(JSONObject().put("data", localBackupService.list()))
                session.method == NanoHTTPD.Method.POST && uri == "/api/system/backup" ->
                    ok(JSONObject().put("data", localBackupService.create(body(session))))
                session.method == NanoHTTPD.Method.POST && uri == "/api/system/backup/upload" -> {
                    val files = HashMap<String, String>()
                    session.parseBody(files)
                    val path = files["file"] ?: files["content"] ?: files.values.firstOrNull { File(it).isFile }
                        ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "缺少备份文件")
                    val name = session.parms["filename"] ?: session.parms["file"] ?: File(path).name
                    ok(JSONObject().put("data", localBackupService.saveUpload(File(path), name)))
                }
                session.method == NanoHTTPD.Method.GET && uri == "/api/system/backup/download" -> {
                    val file = localBackupService.resolve(session.parms["filename"].orEmpty())
                        ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "备份文件不存在")
                    NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.OK, "application/octet-stream", file.inputStream(), file.length()).apply {
                        addHeader("Content-Disposition", "attachment; filename=\"${file.name}\"")
                        addHeader("Cache-Control", "no-store")
                    }
                }
                session.method == NanoHTTPD.Method.POST && uri == "/api/system/restore" ->
                    ok(JSONObject().put("data", localBackupService.restore(body(session))))
                session.method == NanoHTTPD.Method.GET && uri == "/api/system/restore/progress" -> restoreProgress()
                session.method == NanoHTTPD.Method.DELETE && uri == "/api/system/backup" ->
                    ok(JSONObject().put("data", localBackupService.delete(session.parms["filename"].orEmpty())))
                else -> error(NanoHTTPD.Response.Status.NOT_FOUND, "backup route not found")
            }
        } catch (error: NoSuchElementException) {
            error(NanoHTTPD.Response.Status.NOT_FOUND, error.message ?: "备份文件不存在")
        } catch (error: IllegalArgumentException) {
            error(NanoHTTPD.Response.Status.BAD_REQUEST, error.message ?: "备份请求无效")
        }
    }

    // ===== Dashboard (Android local summary) =====

    fun serveDashboard(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        if (session.method != NanoHTTPD.Method.GET) {
            return error(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED, "GET only")
        }
        val taskCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM tasks", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
        val envCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM envs", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
        val depCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM dependencies", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
        val subCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM local_subscriptions", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
        val enabledTasks = readableDatabase.rawQuery("SELECT COUNT(*) FROM tasks WHERE status > 0", null).use { it.moveToFirst(); it.getLong(0) }
        val runningTasks = runningTaskIds.size
        val successLogs = readableDatabase.rawQuery("SELECT COUNT(*) FROM task_logs_local WHERE status=1", null).use { it.moveToFirst(); it.getLong(0) }
        val failedLogs = readableDatabase.rawQuery("SELECT COUNT(*) FROM task_logs_local WHERE status=2", null).use { it.moveToFirst(); it.getLong(0) }
        val recentLogs = queryRows("SELECT * FROM task_logs_local ORDER BY id DESC LIMIT 10") { taskLogJson(it) }
        val dailyStats = queryRows("SELECT substr(created_at,1,10) day, SUM(CASE WHEN status=1 THEN 1 ELSE 0 END) success, SUM(CASE WHEN status=2 THEN 1 ELSE 0 END) failed FROM task_logs_local GROUP BY substr(created_at,1,10) ORDER BY day DESC LIMIT 7") { c -> JSONObject().put("date", c.string("day")).put("success", c.long("success")).put("failed", c.long("failed")) }
        val data = JSONObject().apply {
            put("mode", "android_local")
            put("version", "0.3.15")
            put("core_status", "Kotlin fallback (Go Core requires Android <=15)")
            put("tasks", taskCount)
            put("envs", envCount)
            put("deps", depCount)
            put("subscriptions", subCount)
            put("task_count", taskCount)
            put("enabled_tasks", enabledTasks)
            put("running_tasks", runningTasks)
            put("success_logs", successLogs)
            put("failed_logs", failedLogs)
            put("recent_logs", recentLogs)
            put("daily_stats", dailyStats)
        }
        return ok(JSONObject().put("data", data))
    }

    
fun serveDashboardStats(): JSONObject {
    
    val taskCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM tasks", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val envCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM envs", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val depCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM dependencies", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val scriptCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM scripts", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val logCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM task_logs_local", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val successCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM task_logs_local WHERE status = 1", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val failedCount = try { readableDatabase.rawQuery("SELECT COUNT(*) FROM task_logs_local WHERE status = 2", null).use { it.moveToFirst(); it.getLong(0) } } catch (_: Exception) { 0L }
    
    val successRate = if (logCount > 0) (successCount * 100 / logCount) else 0
    
    return JSONObject()
    
        .put("tasks", JSONObject().put("total", taskCount).put("enabled", taskCount).put("disabled", 0).put("running", 0))
    
        .put("logs", JSONObject().put("total", logCount).put("success", successCount).put("failed", failedCount).put("aborted", 0).put("success_rate", successRate))
    
        .put("scripts", JSONObject().put("total", scriptCount))
    
        .put("envs", JSONObject().put("total", envCount))
    
        .put("deps", JSONObject().put("total", depCount))
    
}



    // ===== Subscriptions (Android local) =====

    private fun ensureDefaultDeps(db: SQLiteDatabase) {
        val defaults = arrayOf(
            arrayOf("python", "python", "3.14", "installed"),
            arrayOf("shell", "shell", "android-16", "installed"),
            arrayOf("node", "node", "lts", "installed"),
            arrayOf("git", "git", "android", "installed"),
            arrayOf("ssh", "ssh", "android", "installed")
        )
        val now = java.time.Instant.now().toString()
        for (dep in defaults) {
            val values = ContentValues().apply {
                put("name", dep[0])
                put("type", dep[1])
                put("python_version", dep[2])
                put("version", dep[2])
                put("status", dep[3])
                put("log", "Android local runtime: " + dep[0] + " " + dep[2])
                put("created_at", now)
                put("updated_at", now)
            }
            db.insertWithOnConflict("dependencies", null, values, SQLiteDatabase.CONFLICT_IGNORE)
        }
    }

    private fun ensureSubscriptionsTable(db: SQLiteDatabase) {
        db.execSQL("""
            CREATE TABLE IF NOT EXISTS local_subscriptions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL DEFAULT '',
                url TEXT NOT NULL,
                enabled INTEGER NOT NULL DEFAULT 0,
                type TEXT NOT NULL DEFAULT 'public-remote',
                last_sync TEXT,
                branch TEXT NOT NULL DEFAULT '',
                schedule TEXT NOT NULL DEFAULT '',
                whitelist TEXT NOT NULL DEFAULT '',
                blacklist TEXT NOT NULL DEFAULT '',
                depend_on TEXT NOT NULL DEFAULT '',
                pre_script TEXT NOT NULL DEFAULT '',
                hook_script TEXT NOT NULL DEFAULT '',
                auto_add_task INTEGER NOT NULL DEFAULT 0,
                auto_del_task INTEGER NOT NULL DEFAULT 0,
                status INTEGER NOT NULL DEFAULT 0,
                last_pull_at TEXT,
                save_dir TEXT NOT NULL DEFAULT '',
                ssh_key_id INTEGER,
                auth_type TEXT NOT NULL DEFAULT '',
                auth_username TEXT NOT NULL DEFAULT '',
                ca_cert_path TEXT NOT NULL DEFAULT '',
                sub_path TEXT NOT NULL DEFAULT '',
                alias TEXT NOT NULL DEFAULT '',
                force_overwrite INTEGER NOT NULL DEFAULT 1,
                created_at TEXT NOT NULL DEFAULT (datetime('now')),
                updated_at TEXT NOT NULL DEFAULT (datetime('now'))
            )
        """)
        ensureBackupCompatibilityColumns(db)
    }

    private fun ensureBackupCompatibilityColumns(db: SQLiteDatabase) {
        val taskColumns = mapOf(
            "last_startup_auto_run_date" to "TEXT NOT NULL DEFAULT ''", "last_run_at" to "TEXT",
            "timeout" to "INTEGER NOT NULL DEFAULT 0", "success_exit_codes" to "TEXT NOT NULL DEFAULT '0'",
            "random_delay_seconds" to "INTEGER", "max_retries" to "INTEGER NOT NULL DEFAULT 0",
            "retry_interval" to "INTEGER NOT NULL DEFAULT 0", "depends_on" to "INTEGER",
            "sort_order" to "INTEGER NOT NULL DEFAULT 0", "subscription_locked" to "INTEGER NOT NULL DEFAULT 0",
            "log_path" to "TEXT", "last_running_time" to "REAL", "allow_multiple_instances" to "INTEGER NOT NULL DEFAULT 0",
            "schedule_policy" to "TEXT NOT NULL DEFAULT 'skip'", "stop_schedule" to "TEXT NOT NULL DEFAULT ''",
        )
        taskColumns.forEach { (name, declaration) -> addColumnIfMissing(db, "tasks", name, declaration) }
        val subscriptionColumns = mapOf(
            "branch" to "TEXT NOT NULL DEFAULT ''", "schedule" to "TEXT NOT NULL DEFAULT ''",
            "whitelist" to "TEXT NOT NULL DEFAULT ''", "blacklist" to "TEXT NOT NULL DEFAULT ''",
            "depend_on" to "TEXT NOT NULL DEFAULT ''", "pre_script" to "TEXT NOT NULL DEFAULT ''",
            "hook_script" to "TEXT NOT NULL DEFAULT ''", "auto_add_task" to "INTEGER NOT NULL DEFAULT 0",
            "auto_del_task" to "INTEGER NOT NULL DEFAULT 0", "status" to "INTEGER NOT NULL DEFAULT 0",
            "last_pull_at" to "TEXT", "save_dir" to "TEXT NOT NULL DEFAULT ''", "ssh_key_id" to "INTEGER",
            "auth_type" to "TEXT NOT NULL DEFAULT ''", "auth_username" to "TEXT NOT NULL DEFAULT ''",
            "ca_cert_path" to "TEXT NOT NULL DEFAULT ''", "sub_path" to "TEXT NOT NULL DEFAULT ''",
            "alias" to "TEXT NOT NULL DEFAULT ''", "force_overwrite" to "INTEGER NOT NULL DEFAULT 1",
        )
        val hasSubscriptions = db.rawQuery("SELECT 1 FROM sqlite_master WHERE type='table' AND name='local_subscriptions'", null).use { it.moveToFirst() }
        if (hasSubscriptions) subscriptionColumns.forEach { (name, declaration) -> addColumnIfMissing(db, "local_subscriptions", name, declaration) }
    }

    fun serveSubscriptions(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val normalized = session.uri.removePrefix("/api/v1").removePrefix("/api")
        val parts = normalized.trim('/').split('/')
        val id = parts.getOrNull(1)?.toLongOrNull()
        val action = parts.drop(2).joinToString("/")
        return when {
            session.method == NanoHTTPD.Method.GET && normalized == "/subscriptions" -> listSubscriptions()
            session.method == NanoHTTPD.Method.POST && normalized == "/subscriptions" -> addSubscription(readBody(session))
            id != null && session.method == NanoHTTPD.Method.PUT && action.isBlank() -> updateSubscription(id, body(session))
            id != null && session.method == NanoHTTPD.Method.DELETE && action.isBlank() -> deleteSubscription(normalized)
            id != null && session.method == NanoHTTPD.Method.PUT && action in setOf("enable", "disable") -> enableSubscription(id, action == "enable")
            id != null && session.method == NanoHTTPD.Method.PUT && action == "pull" -> pullSubscription(id)
            id != null && session.method == NanoHTTPD.Method.PUT && action == "pull/stop" -> stopSubscriptionPull(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "logs" -> subscriptionLogs(id)
            id != null && session.method == NanoHTTPD.Method.GET && action == "pull-stream" -> subscriptionPullStream(id)
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
                for (key in listOf("branch", "schedule", "whitelist", "blacklist", "depend_on", "pre_script", "hook_script", "save_dir", "auth_type", "auth_username", "ca_cert_path", "sub_path", "alias")) put(key, cursor.string(key))
                for (key in listOf("auto_add_task", "auto_del_task", "force_overwrite")) put(key, cursor.int(key) != 0)
                put("ssh_key_id", if (cursor.isNull(cursor.getColumnIndexOrThrow("ssh_key_id"))) JSONObject.NULL else cursor.long("ssh_key_id"))
                put("created_at", cursor.string("created_at"))
                put("updated_at", cursor.string("updated_at"))
            }
        }
        return ok(JSONObject().put("data", rows))
    }

    private fun readBody(session: NanoHTTPD.IHTTPSession): String {
        val body = HashMap<String, String>()
        try { session.parseBody(body) } catch (_: Exception) { }
        return body["postData"] ?: ""
    }

    private fun addSubscription(body: String): NanoHTTPD.Response {
        val json = JSONObject(body)
        val values = ContentValues().apply {
            put("name", json.optString("name", ""))
            put("url", json.optString("url", ""))
            put("enabled", if (json.optBoolean("enabled", true)) 1 else 0)
            put("type", json.optString("type", "public-remote"))
            for (key in listOf("branch", "schedule", "whitelist", "blacklist", "depend_on", "pre_script", "hook_script", "save_dir", "auth_type", "auth_username", "ca_cert_path", "sub_path", "alias")) if (json.has(key)) put(key, json.optString(key))
            for (key in listOf("auto_add_task", "auto_del_task", "force_overwrite")) if (json.has(key)) put(key, if (json.optBoolean(key)) 1 else 0)
            if (json.has("ssh_key_id") && !json.isNull("ssh_key_id")) put("ssh_key_id", json.optLong("ssh_key_id"))
        }
        ensureSubscriptionsTable(writableDatabase)
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


    private fun updateSubscription(id: Long, json: JSONObject): NanoHTTPD.Response {
        val values = ContentValues().apply {
            for (key in listOf("name", "url", "type", "branch", "schedule", "whitelist", "blacklist", "depend_on", "pre_script", "hook_script", "save_dir", "auth_type", "auth_username", "ca_cert_path", "sub_path", "alias")) if (json.has(key)) put(key, json.optString(key))
            for (key in listOf("auto_add_task", "auto_del_task", "force_overwrite")) if (json.has(key)) put(key, if (json.optBoolean(key)) 1 else 0)
            if (json.has("ssh_key_id")) { if (json.isNull("ssh_key_id")) putNull("ssh_key_id") else put("ssh_key_id", json.optLong("ssh_key_id")) }
            if (json.has("enabled")) put("enabled", if (json.optBoolean("enabled")) 1 else 0)
            put("updated_at", Instant.now().toString())
        }
        if (writableDatabase.update("local_subscriptions", values, "id=?", arrayOf(id.toString())) == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "subscription not found")
        return ok(JSONObject().put("data", JSONObject().put("id", id)))
    }

    private fun enableSubscription(id: Long, enabled: Boolean): NanoHTTPD.Response {
        val count = writableDatabase.update("local_subscriptions", ContentValues().apply { put("enabled", if (enabled) 1 else 0); put("updated_at", Instant.now().toString()) }, "id=?", arrayOf(id.toString()))
        if (count == 0) return error(NanoHTTPD.Response.Status.NOT_FOUND, "subscription not found")
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("enabled", enabled)))
    }

    private fun recordSubscriptionLog(id: Long, level: String, message: String) {
        writableDatabase.insert("subscription_logs", null, ContentValues().apply { put("subscription_id", id); put("level", level); put("message", message); put("created_at", Instant.now().toString()) })
    }

    private fun pullSubscription(id: Long): NanoHTTPD.Response {
        val record = readableDatabase.query("local_subscriptions", arrayOf("url", "name", "type", "branch", "save_dir"), "id=?", arrayOf(id.toString()), null, null, null).use { c ->
            if (!c.moveToFirst()) null else listOf(c.getString(0), c.getString(1), c.getString(2), c.getString(3), c.getString(4))
        }
            ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "subscription not found")
        if (record[2] == "git-repo" || record[0].endsWith(".git")) return pullGitSubscription(id, record[0], record[1], record[3], record[4])
        return try {
            val connection = java.net.URL(record[0]).openConnection() as HttpURLConnection
            connection.connectTimeout = 15000; connection.readTimeout = 30000; connection.instanceFollowRedirects = true
            val code = connection.responseCode
            if (code !in 200..299) throw IllegalStateException("HTTP $code")
            val filename = (record[1].ifBlank { "subscription-$id" }).replace(Regex("[^A-Za-z0-9._-]"), "_") + ".js"
            val output = File(scriptsRoot(), filename)
            connection.inputStream.use { input -> output.outputStream().use { input.copyTo(it) } }
            connection.disconnect()
            writableDatabase.execSQL("UPDATE local_subscriptions SET last_sync=?,updated_at=? WHERE id=?", arrayOf<Any?>(Instant.now().toString(), Instant.now().toString(), id))
            recordSubscriptionLog(id, "info", "Downloaded ${output.length()} bytes to $filename")
            ok(JSONObject().put("data", JSONObject().put("id", id).put("path", filename).put("bytes", output.length()).put("status", "success")))
        } catch (e: Exception) {
            recordSubscriptionLog(id, "error", e.message ?: e.javaClass.simpleName)
            error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "pull failed: ${e.message}")
        }
    }

    private fun pullGitSubscription(id: Long, url: String, name: String, branch: String, saveDir: String): NanoHTTPD.Response {
        val directoryName = saveDir.ifBlank { name.ifBlank { "subscription-$id" } }.replace(Regex("[^A-Za-z0-9._-]"), "_")
        val target = File(scriptsRoot(), directoryName).apply { mkdirs() }
        val guest = if (File(target, ".git").isDirectory) {
            AndroidLinuxRuntime.guestCommand(appContext, target, listOf("/usr/bin/git", "-C", "/workspace", "pull", "--ff-only"))
        } else {
            val args = mutableListOf("/usr/bin/git", "clone", "--depth", "1")
            if (branch.isNotBlank()) args += listOf("--branch", branch)
            args += listOf(url, "/workspace")
            AndroidLinuxRuntime.guestCommand(appContext, target, args)
        } ?: return error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "Git runtime unavailable for ${AndroidLinuxRuntime.currentAbi()}")
        val result = runLocalProcess(guest, target, JSONArray().put("Pulling Git subscription $url"), ScriptCompatibility.INSTALL_TIMEOUT_SECONDS)
        val log = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        recordSubscriptionLog(id, if (result.exitCode == 0) "info" else "error", log)
        if (result.exitCode != 0) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "Git pull failed")
        writableDatabase.execSQL("UPDATE local_subscriptions SET last_sync=?,last_pull_at=?,updated_at=? WHERE id=?", arrayOf<Any?>(Instant.now().toString(), Instant.now().toString(), Instant.now().toString(), id))
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("path", directoryName).put("status", "success")))
    }

    private fun stopSubscriptionPull(id: Long): NanoHTTPD.Response {
        recordSubscriptionLog(id, "info", "Pull stop requested")
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("stopped", true)))
    }

    private fun subscriptionLogArray(id: Long): JSONArray = queryRows("SELECT * FROM subscription_logs WHERE subscription_id=? ORDER BY id DESC LIMIT 200", arrayOf(id.toString())) { c -> JSONObject().put("id", c.long("id")).put("level", c.string("level")).put("message", c.string("message")).put("created_at", c.string("created_at")) }
    private fun subscriptionLogs(id: Long) = ok(JSONObject().put("data", subscriptionLogArray(id)))
    private fun subscriptionPullStream(id: Long): NanoHTTPD.Response {
        val rows = subscriptionLogArray(id); val text = buildString { for (i in 0 until rows.length()) append("data: ").append(rows.getJSONObject(i).toString()).append("\n\n"); append("event: done\ndata: {\"done\":true}\n\n") }
        return NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.OK, "text/event-stream; charset=utf-8", text).apply { addHeader("Cache-Control", "no-cache") }
    }

    private fun serveTaskViews(session: NanoHTTPD.IHTTPSession, uri: String): NanoHTTPD.Response {
        val id = uri.substringAfter("/tasks/views/", "").toLongOrNull()
        if (session.method == NanoHTTPD.Method.GET && uri == "/tasks/views") return NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.OK, "application/json", taskViewRows().toString())
        val json = if (session.method in setOf(NanoHTTPD.Method.POST, NanoHTTPD.Method.PUT)) body(session) else JSONObject()
        if (session.method == NanoHTTPD.Method.PUT && uri == "/tasks/views/reorder") {
            val views=json.optJSONArray("views")?:JSONArray(); for(i in 0 until views.length()){ val v=views.optJSONObject(i)?:continue; writableDatabase.execSQL("UPDATE task_views SET sort_order=?,hidden=?,updated_at=? WHERE id=?", arrayOf<Any?>(v.optInt("sort_order"),if(v.optBoolean("hidden"))1 else 0,Instant.now().toString(),v.optLong("id"))) }; return ok(JSONObject().put("data", taskViewRows()))
        }
        if (session.method == NanoHTTPD.Method.DELETE && id != null) { writableDatabase.delete("task_views","id=?",arrayOf(id.toString())); return ok(JSONObject().put("data", JSONObject().put("deleted",id))) }
        if ((session.method == NanoHTTPD.Method.POST && uri == "/tasks/views") || (session.method == NanoHTTPD.Method.PUT && id != null)) {
            val now=Instant.now().toString(); val v=ContentValues().apply { put("name",json.optString("name","视图"));put("filters",json.optString("filters","[]"));put("sort_rules",json.optString("sort_rules","[]"));put("hidden",if(json.optBoolean("hidden"))1 else 0);put("sort_order",json.optInt("sort_order"));put("updated_at",now) }
            val result=if(id==null){v.put("created_at",now);writableDatabase.insertOrThrow("task_views",null,v)}else{writableDatabase.update("task_views",v,"id=?",arrayOf(id.toString()));id}; return ok(JSONObject().put("data",JSONObject().put("id",result)))
        }
        return error(NanoHTTPD.Response.Status.NOT_FOUND,"task view route not found")
    }
    private fun taskViewRows(): JSONArray = queryRows("SELECT * FROM task_views ORDER BY sort_order,id") { c -> JSONObject().put("id",c.long("id")).put("name",c.string("name")).put("filters",c.string("filters")).put("sort_rules",c.string("sort_rules")).put("hidden",c.int("hidden")==1).put("sort_order",c.int("sort_order")) }

    private fun copyTask(id: Long): NanoHTTPD.Response {
        val j=readableDatabase.query("tasks",null,"id=?",arrayOf(id.toString()),null,null,null).use { c -> if(!c.moveToFirst()) null else JSONObject().put("name",c.string("name")+" 副本").put("command",c.string("command")).put("cron_expression",c.string("cron_expression")).put("task_type",c.string("task_type")).put("python_version",c.string("python_version")).put("task_before",c.string("task_before")).put("task_after",c.string("task_after")).put("labels",JSONArray(c.string("labels"))) } ?: return error(NanoHTTPD.Response.Status.NOT_FOUND,"task not found")
        return createTask(j)
    }
    private fun taskLogFiles(id: Long): NanoHTTPD.Response {
        val rows = queryRows(
            "SELECT id,created_at,length(content) size FROM task_logs_local WHERE task_id=? ORDER BY id DESC",
            arrayOf(id.toString()),
        ) { cursor ->
            JSONObject(LocalLogQueryContract.logFile(
                taskId = id,
                logId = cursor.long("id"),
                size = cursor.long("size"),
                createdAt = cursor.string("created_at"),
            ))
        }
        return ok(JSONObject().put("data", rows))
    }

    private fun deleteLogs(json: JSONObject): NanoHTTPD.Response { val ids=json.optJSONArray("ids")?:json.optJSONArray("log_ids")?:JSONArray();var n=0;for(i in 0 until ids.length())n+=writableDatabase.delete("task_logs_local","id=?",arrayOf(ids.optLong(i).toString()));return ok(JSONObject().put("data",JSONObject().put("deleted",n))) }
    private fun cleanLogs(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response { val days=session.parms["days"]?.toIntOrNull();val n=if(days==null)writableDatabase.delete("task_logs_local",null,null) else writableDatabase.delete("task_logs_local","created_at < datetime('now', ?)",arrayOf("-$days days"));return ok(JSONObject().put("data",JSONObject().put("deleted",n))) }

    private fun moveEnvTop(id: Long, top: Boolean): NanoHTTPD.Response { val order=if(top)-1 else id.toInt();writableDatabase.execSQL("UPDATE envs SET sort_order=?,updated_at=? WHERE id=?",arrayOf<Any?>(order,Instant.now().toString(),id));return ok(JSONObject().put("data",JSONObject().put("id",id).put("top",top))) }

    private fun persistedMirrors(): NanoHTTPD.Response = synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
        ensureMirrorDefaults(writableDatabase)
        ok(JSONObject().put("data", mirrorResponseData()))
    }

    private fun setMirrors(json: JSONObject): NanoHTTPD.Response {
        val keys = mirrorConfigKeys
        val mirrors = linkedMapOf<String, String>()
        for (key in keys) {
            val requested = if (json.has(key)) json.optString(key) else configValue(key, defaultMirrorValue(key))
            val value = normalizedConfigValue(key, requested)
                ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "$key 必须是合法的 HTTP(S) 地址")
            mirrors[key] = value
        }
        synchronized(AndroidLinuxRuntime.mirrorConfigLock) {
            val db = writableDatabase
            db.beginTransaction()
            try {
                mirrors.forEach { (key, value) -> upsertConfig(key, value, persistPreference = false) }
                db.setTransactionSuccessful()
            } finally {
                db.endTransaction()
            }
            check(configPrefs.edit().apply { mirrors.forEach { (key, value) -> putString(key, value) } }.commit()) {
                "无法同步镜像运行时配置"
            }
            AndroidLinuxRuntime.ensureRootfsReady(appContext)
        }
        return ok(JSONObject().put("data", mirrorResponseData()))
    }

    private fun mirrorResponseData(): JSONObject {
        val rootfs = AndroidLinuxRuntime.statusJson(appContext).optJSONObject("rootfs")
        val manager = rootfs?.optString("package_manager").orEmpty().ifBlank { "apk" }
        return JSONObject()
            .put(AndroidLinuxRuntime.PIP_MIRROR_KEY, configValue(AndroidLinuxRuntime.PIP_MIRROR_KEY, AndroidLinuxRuntime.PYTHON_PIP_ALIBABA_INDEX))
            .put(AndroidLinuxRuntime.NPM_MIRROR_KEY, configValue(AndroidLinuxRuntime.NPM_MIRROR_KEY, AndroidLinuxRuntime.NODE_NPM_NPMMIRROR_REGISTRY))
            .put(AndroidLinuxRuntime.LINUX_MIRROR_KEY, configValue(AndroidLinuxRuntime.LINUX_MIRROR_KEY, AndroidLinuxRuntime.ALPINE_APK_HUAWEI_MIRROR))
            .put("linux_package_manager", manager)
            .put("linux_distribution", rootfs?.optString("distribution").orEmpty().ifBlank { "alpine" })
            .put("linux_mirror_supported", manager == "apk")
            .put("linux_mirror_label", if (manager == "apk") "Alpine APK（华为云默认）" else "Linux")
            .put("linux_mirror_message", if (manager == "apk") "默认使用华为云，支持任意合法 HTTP(S) 镜像及官方源" else "当前包管理器暂不支持镜像设置")
    }
    private fun setPythonDefault(json: JSONObject): NanoHTTPD.Response { val version=json.optString("version","3.14");upsertConfig("python_runtime_default",version);return ok(JSONObject().put("data",JSONObject().put("version",version))) }
    private fun exportDependencies(type: String): NanoHTTPD.Response { val lines=mutableListOf<String>();readableDatabase.query("dependencies",arrayOf("name","version"),if(type.isBlank())null else "type=?",if(type.isBlank())null else arrayOf(normalizeDependencyType(type)?:type),null,null,"name").use{c->while(c.moveToNext())lines += c.string("name") + if(c.string("version").isBlank()) "" else if(type=="npm"||type=="nodejs") "@${c.string("version")}" else "==${c.string("version")}"};return NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.OK,"text/plain; charset=utf-8",lines.joinToString("\n")) }

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
            .put("task_before", cursor.string("task_before"))
            .put("task_after", cursor.string("task_after"))
            .put("notify_on_failure", cursor.int("notify_on_failure") != 0)
            .put("notify_on_success", cursor.int("notify_on_success") != 0)
            .put("notify_on_abort", cursor.int("notify_on_abort") != 0)
            .put("notification_channel_id", if (cursor.isNull(cursor.getColumnIndexOrThrow("notification_channel_id"))) JSONObject.NULL else cursor.long("notification_channel_id"))
            .put("status", cursor.double("status"))
            .put("labels", JSONArray(cursor.string("labels")))
            .put("last_run_status", taskRunStatusCode(cursor.string("last_run_status")))
            .put("last_run_at", if (cursor.string("last_run_status").isBlank()) JSONObject.NULL else cursor.string("updated_at"))
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

    private fun taskRunStatusCode(value: String): Any = when (value.trim().lowercase()) {
        "success" -> 0
        "failed" -> 1
        "aborted", "stopped" -> 2
        "running" -> 2
        else -> JSONObject.NULL
    }

    private fun dependencyRows(session: NanoHTTPD.IHTTPSession): JSONArray {
        runCatching { syncInstalledRuntimeDependencies("Discovered from installed runtime packages") }
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
        DependencyStorage.retainNewest(backupsRoot(), DependencyStorage.MAX_BACKUPS)
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
        DependencyStorage.retainNewest(backupsRoot(), DependencyStorage.MAX_BACKUPS)
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
        val source = File(appContext.cacheDir, "portable-backups/portable-backup-${System.nanoTime()}").apply { mkdirs() }
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
        val path = cleanScriptPath(json.optString("path", json.optString("filename")))
        val file = scriptFile(path)
        file.parentFile?.mkdirs()
        val content = json.optString("content")
        file.writeText(content)
        val version = recordScriptVersion(path, content, json.optString("message", "V1 初始版本"))
        return ok(JSONObject().put("message", "保存成功").put("version", version).put("data", scriptFileJson(path, file)))
    }

    private fun createScriptDirectory(json: JSONObject): NanoHTTPD.Response {
        val path = cleanScriptPath(json.optString("path", json.optString("name")))
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
        val dir = cleanScriptPath(session.parms["dir"].orEmpty(), allowRootAlias = true)
        val name = cleanScriptPath(originalName)
        require('/' !in name) { "上传文件名必须是文件名" }
        val path = listOf(dir, name).filter(String::isNotBlank).joinToString("/")
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
        require('/' !in newName) { "新名称必须是文件名" }
        val source = scriptFile(oldPath)
        if (!source.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val targetPath = listOf(oldPath.substringBeforeLast('/', ""), newName.substringAfterLast('/')).filter(String::isNotBlank).joinToString("/")
        val target = scriptFile(targetPath)
        if (target.exists()) return error(NanoHTTPD.Response.Status.CONFLICT, "目标已存在")
        target.parentFile?.mkdirs()
        if (!source.renameTo(target)) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "重命名失败")
        return ok(JSONObject().put("message", "重命名成功").put("new_path", targetPath))
    }

    private fun moveScript(json: JSONObject): NanoHTTPD.Response {
        val sourcePath = cleanScriptPath(json.optString("source_path"))
        val targetDir = cleanScriptPath(json.optString("target_dir"), allowRootAlias = true)
        val source = scriptFile(sourcePath)
        if (!source.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val targetPath = listOf(targetDir, source.name).filter(String::isNotBlank).joinToString("/")
        val target = scriptFile(targetPath)
        if (target.exists()) return error(NanoHTTPD.Response.Status.CONFLICT, "目标已存在")
        target.parentFile?.mkdirs()
        if (!source.renameTo(target)) return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, "移动失败")
        return ok(JSONObject().put("message", "移动成功").put("new_path", targetPath))
    }

    private fun copyScript(json: JSONObject): NanoHTTPD.Response {
        val sourcePath = cleanScriptPath(json.optString("source_path"))
        val targetDir = cleanScriptPath(json.optString("target_dir"), allowRootAlias = true)
        val source = scriptFile(sourcePath)
        if (!source.exists()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val requestedName = json.optString("new_name")
        val name = if (requestedName.isBlank()) source.name else cleanScriptPath(requestedName)
        require('/' !in name) { "新名称必须是文件名" }
        val targetPath = listOf(targetDir, name).filter(String::isNotBlank).joinToString("/")
        val target = scriptFile(targetPath)
        if (target.exists()) return error(NanoHTTPD.Response.Status.CONFLICT, "目标已存在")
        target.parentFile?.mkdirs()
        if (source.isDirectory) source.copyRecursively(target, overwrite = false) else source.copyTo(target, overwrite = false)
        return NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.CREATED, "application/json; charset=utf-8", JSONObject().put("message", "复制成功").put("new_path", targetPath).toString())
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
        if (code.isBlank()) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "运行代码不能为空")
        val runId = "android-local-${UUID.randomUUID()}"
        val ext = when (language.lowercase()) {
            "javascript", "node", "nodejs" -> ".js"
            "typescript" -> ".ts"
            "shell", "sh", "bash" -> ".sh"
            "go" -> ".go"
            else -> ".py"
        }
        val temp = File(appContext.cacheDir, "script-runs/$runId$ext").apply { parentFile?.mkdirs(); writeText(code) }
        createRunningScriptRun(runId)
        try {
            scriptRunExecutor.execute { executeAsyncScriptRun(runId, temp, "inline-$language$ext", language, true) }
        } catch (_: RejectedExecutionException) {
            finishScriptRun(runId, "failed", 75)
            temp.delete()
            return error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "脚本执行队列已满，请稍后重试")
        }
        return startedScriptRun(runId)
    }

    private fun runScript(json: JSONObject): NanoHTTPD.Response {
        val path = cleanScriptPath(json.optString("path", "script.py"))
        val file = scriptFile(path)
        if (!file.exists() || !file.isFile) return error(NanoHTTPD.Response.Status.NOT_FOUND, "脚本不存在")
        val runId = "android-local-${UUID.randomUUID()}"
        createRunningScriptRun(runId)
        try {
            scriptRunExecutor.execute { executeAsyncScriptRun(runId, file, path, json.optString("language"), false) }
        } catch (_: RejectedExecutionException) {
            finishScriptRun(runId, "failed", 75)
            return error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "脚本执行队列已满，请稍后重试")
        }
        return startedScriptRun(runId)
    }

    private fun startedScriptRun(runId: String): NanoHTTPD.Response {
        val result = LocalScriptResult(JSONArray(), "running", false, null)
        return NanoHTTPD.newFixedLengthResponse(NanoHTTPD.Response.Status.CREATED, "application/json; charset=utf-8", scriptRunResponse(runId, result).toString())
    }

    private fun scriptRunResponse(runId: String, result: LocalScriptResult): JSONObject = JSONObject()
        .put("message", "脚本已启动").put("run_id", runId)
        .put("data", JSONObject().put("run_id", runId).put("status", result.status)
            .put("logs", result.logs).put("done", result.done).put("exit_code", result.exitCode))

    private fun executeAsyncScriptRun(runId: String, file: File, displayPath: String, languageHint: String, deleteAfter: Boolean) {
        try {
            appendScriptRunLog(runId, "Android local fallback executing script: $displayPath")
            val workingDir = file.parentFile ?: scriptsRoot()
            ensureQingLongShims(workingDir)
            if (!prepareScriptDependencies(runId, file)) { finishScriptRun(runId, "failed", 2); return }
            val result = executeWithAutoInstall(null, runtimeForFile(file), { line -> appendScriptRunLog(runId, line) }) {
                runAsyncScriptProcess(runId, file, displayPath, languageHint, workingDir)
            }
            if (scriptRunStatus(runId) != "stopped") {
                finishScriptRun(runId, result.status, result.exitCode ?: 127)
            }
        } catch (error: Exception) {
            if (scriptRunStatus(runId) != "stopped") { appendScriptRunLog(runId, "Script start failed: ${error.message ?: error.javaClass.simpleName}"); finishScriptRun(runId, "failed", 127) }
        } finally {
            scriptProcesses.remove(runId); scriptRunLocks.remove(runId); scriptRunLogsMemory.remove(runId)
            scriptRunLogCharacters.remove(runId); scriptRunPendingPersistence.remove(runId)
            if (deleteAfter) runCatching { file.delete() }
        }
    }

    private fun runAsyncScriptProcess(runId: String, file: File, displayPath: String, languageHint: String, workingDir: File): LocalScriptResult {
        val logs = JSONArray()
        val command = scriptCommand(file, displayPath, languageHint)
            ?: return LocalScriptResult(
                logs.put("Runtime not available for script type: ${file.extension.ifBlank { "unknown" }}"),
                "failed",
                true,
                127,
            )
        val commandLine = "Command: ${command.first().substringAfterLast('/')}"
        logs.put(commandLine)
        appendScriptRunLog(runId, commandLine)
        return try {
            val process = ProcessBuilder(command).directory(workingDir).redirectErrorStream(true)
                .apply { applyProcessEnvironment(command, environment(), workingDir) }.start()
            scriptProcesses[runId] = process
            if (scriptRunStatus(runId) == "stopped") {
                process.destroyForcibly()
                return LocalScriptResult(logs.put("Script stopped before output"), "stopped", true, 130)
            }
            process.inputStream.bufferedReader().useLines { lines ->
                lines.forEach { line ->
                    logs.put(line)
                    appendScriptRunLog(runId, line)
                }
            }
            val exit = process.waitFor()
            val finalLine = if (exit == 0) "Script completed successfully" else "Script failed with exit code $exit"
            logs.put(finalLine)
            appendScriptRunLog(runId, finalLine)
            LocalScriptResult(logs, if (exit == 0) "success" else "failed", true, exit)
        } catch (error: Exception) {
            val line = "Script start failed: ${error.message ?: error.javaClass.simpleName}"
            appendScriptRunLog(runId, line)
            LocalScriptResult(logs.put(line), "failed", true, 127)
        } finally {
            scriptProcesses.remove(runId)
        }
    }

    private fun executeStructuredCommand(command: String, taskId: Long? = null, onLine: ((String) -> Unit)? = null, timeoutSeconds: Long = 300): LocalScriptResult {
        val logs = JSONArray().put("Android local fallback executing shell command: $command")
        onLine?.invoke("Android local fallback executing shell command: $command")
        if (command.isBlank()) {
            return LocalScriptResult(logs.put("Empty command"), "failed", true, 1)
        }
        val scan = ShellCompatibility.scan(command)
        val shell = if (scan.requiresBash) AndroidLinuxRuntime.GuestShell.BASH else AndroidLinuxRuntime.GuestShell.SH
        if (scan.requiresBash) logs.put("Shell route: /bin/bash (${scan.matchedRules.joinToString()})")
        val rootfsCommand = AndroidLinuxRuntime.shellTextCommand(appContext, appContext.filesDir, command, shell)
            ?: return LocalScriptResult(logs.put("ROOTFS_SHELL_UNAVAILABLE: first-class Linux runtime is required for task commands"), "failed", true, 127)
        return runLocalProcess(rootfsCommand, appContext.filesDir, logs, timeoutSeconds, onLine, taskId)
    }

    private fun executeHookCommand(command: String, taskId: Long, onLine: ((String) -> Unit)? = null): LocalScriptResult {
        val scan = ShellCompatibility.scan(command)
        val shell = if (scan.requiresBash) AndroidLinuxRuntime.GuestShell.BASH else AndroidLinuxRuntime.GuestShell.SH
        val rootfsCommand = AndroidLinuxRuntime.shellTextCommand(appContext, appContext.filesDir, command, shell)
            ?: return LocalScriptResult(JSONArray().put("ROOTFS_SHELL_UNAVAILABLE: task hooks require the Linux runtime"), "failed", true, 127)
        return runLocalProcess(
            rootfsCommand,
            appContext.filesDir,
            JSONArray().put("Android local fallback executing task hook with ${shell.executable}"),
            onLine = onLine,
            taskId = taskId,
        )
    }

    private fun executeScriptFile(file: File, displayPath: String, languageHint: String = "", args: List<String> = emptyList(), timeoutSeconds: Long = 300, extraEnvironment: Map<String, String> = emptyMap(), onLine: ((String) -> Unit)? = null, taskId: Long? = null): LocalScriptResult {
        val logs = JSONArray().put("Android local fallback executing script: $displayPath")
        val command = scriptCommand(file, displayPath, languageHint)
            ?: return LocalScriptResult(
                logs.put("Runtime not available for script type: ${file.extension.ifBlank { languageHint.ifBlank { "unknown" } }}. Supported: .sh (shell), .py (python), .js (node)."),
                "failed",
                true,
                127,
            )

        return runLocalProcess(command + args, file.parentFile ?: scriptsRoot(), logs, timeoutSeconds, onLine, taskId, extraEnvironment).also { result ->
            recordDetectedDependencies(result.logs)
        }
    }

    private fun scriptCommand(file: File, displayPath: String, languageHint: String): List<String>? {
        val ext = file.extension.lowercase()
        val nativeDir = appContext.applicationInfo.nativeLibraryDir.orEmpty()
        fun native(name: String): String? = File(nativeDir, name).takeIf { it.isFile && isRuntimeEntryVerified(it) }?.absolutePath
        return when {
            ext in setOf("sh", "bash") || languageHint.equals("shell", ignoreCase = true) || languageHint.equals("bash", ignoreCase = true) -> {
                val scan = ShellCompatibility.scan(runCatching { file.readText(Charsets.UTF_8) }.getOrDefault(""))
                val requiresBash = ext == "bash" || languageHint.equals("bash", true) || scan.requiresBash
                val shell = if (requiresBash) AndroidLinuxRuntime.GuestShell.BASH else AndroidLinuxRuntime.GuestShell.SH
                AndroidLinuxRuntime.shellCommand(appContext, file, file.parentFile ?: scriptsRoot(), shell)
            }
            ext == "py" || languageHint.equals("python", ignoreCase = true) -> {
                val guestPythonPath = "/host-files/deps/python/${DependencyStorage.PYTHON_VERSION}/site-packages"
                AndroidLinuxRuntime.guestCommand(appContext, file.parentFile ?: scriptsRoot(), listOf("/usr/bin/env", "PYTHONPATH=$guestPythonPath", "/usr/bin/python3", "/workspace/${file.name}"))
                    ?: AndroidPythonRuntime.ensureReady(appContext)?.let { listOf(it.executable, it.wrapperScript, file.absolutePath) }
                    ?: run {
                    val sysPy = try {
                        val p = ProcessBuilder("which", "python3").redirectErrorStream(true).start()
                        p.waitFor()
                        if (p.exitValue() == 0) p.inputStream.bufferedReader().readText().trim() else null
                    } catch (_: Exception) { null }
                    if (sysPy != null) listOf(sysPy, file.absolutePath) else null
                }
            }
            ext == "js" || ext == "mjs" || languageHint.equals("javascript", ignoreCase = true) ->
                AndroidLinuxRuntime.guestCommand(appContext, file.parentFile ?: scriptsRoot(), listOf("/usr/bin/env", "NODE_PATH=/host-files/deps/nodejs/node_modules:/usr/local/lib/node_modules:/usr/lib/node_modules", "/usr/bin/node", "/workspace/${file.name}"))
                    ?: AndroidNodeRuntime.ensureReady(appContext)?.let { listOf(it.executable, AndroidNodeRuntime.wrapperPath, file.absolutePath) }
            ext == "ts" || languageHint.equals("typescript", ignoreCase = true) ->
                AndroidLinuxRuntime.guestCommand(appContext, file.parentFile ?: scriptsRoot(), listOf("/usr/bin/env", "NODE_PATH=/host-files/deps/nodejs/node_modules:/usr/local/lib/node_modules:/usr/lib/node_modules", "/usr/bin/node", "-e", typeScriptEvalCode(), "/workspace/${file.name}"))
                    ?: AndroidNodeRuntime.ensureReady(appContext)?.let { listOf(it.executable, AndroidNodeRuntime.wrapperPath, "-e", typeScriptEvalCode(), file.absolutePath) }
            ext == "go" || languageHint.equals("go", ignoreCase = true) -> native("libyaegi_exec.so")?.let { listOf(it, file.absolutePath) }
                ?: AndroidLinuxRuntime.guestCommand(appContext, file.parentFile ?: scriptsRoot(), listOf("/usr/bin/go", "run", "/workspace/${file.name}"))
            else -> null
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

    private fun runLocalProcess(command: List<String>, workingDir: File, logs: JSONArray, timeoutSeconds: Long = 300, onLine: ((String) -> Unit)? = null, taskId: Long? = null, extraEnvironment: Map<String, String> = emptyMap()): LocalScriptResult {
        logs.put("Command: ${command.first().substringAfterLast('/')}")
        return try {
            val process = ProcessBuilder(command)
                .directory(workingDir)
                .redirectErrorStream(true)
                .apply {
                    applyProcessEnvironment(command, environment(), workingDir, extraEnvironment)
                }
                .start()
            if (taskId != null && !registerTaskProcess(taskId, process)) {
                return LocalScriptResult(logs.put("Task aborted before process start"), "aborted", true, 130)
            }
            val output = Collections.synchronizedList(mutableListOf<String>())
            val reader = Thread {
                process.inputStream.bufferedReader().useLines { lines ->
                    lines.forEach { line -> output += line; onLine?.invoke(line) }
                }
            }.also { it.start() }
            val finished = process.waitFor(timeoutSeconds, TimeUnit.SECONDS)
            if (!finished) {
                process.destroyForcibly()
                reader.join(1_000)
                output.forEach { logs.put(it) }
                logs.put("Process timed out after $timeoutSeconds seconds")
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
        } finally {
            if (taskId != null) taskProcesses.remove(taskId)
        }
    }

    private fun registerTaskProcess(taskId: Long, process: Process): Boolean {
        taskProcesses[taskId] = process
        if (!taskAbortRequested.contains(taskId)) return true
        terminateTaskProcess(process)
        taskProcesses.remove(taskId, process)
        return false
    }

    private fun terminateTaskProcess(process: Process) {
        LocalTaskProcessTerminator.terminate(process)
    }

    private fun runtimeForFile(file: File): String = when (file.extension.lowercase()) {
        "py" -> "python"
        "js", "mjs", "ts" -> "nodejs"
        else -> ""
    }

    private fun runtimeForCommand(command: String): String = when {
        Regex("(^|\\s)(python|python3)(\\s|$)").containsMatchIn(command) -> "python"
        Regex("(^|\\s)(node|nodejs)(\\s|$)").containsMatchIn(command) -> "nodejs"
        else -> ""
    }

    private fun executeWithAutoInstall(
        taskId: Long?,
        runtime: String,
        onLine: ((String) -> Unit)?,
        execute: () -> LocalScriptResult,
    ): LocalScriptResult {
        var result = execute()
        if (!configBool("auto_install_deps", true) || runtime.isBlank()) return result
        val attempted = mutableSetOf<LocalTaskFallbackSemantics.DependencyCandidate>()
        repeat(LocalTaskFallbackSemantics.MAX_DEPENDENCY_INSTALLS) { installCount ->
            if (result.exitCode == 0 || result.status == "aborted") return result
            val output = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
            val candidate = LocalTaskFallbackSemantics.nextDependency(runtime, output, attempted, installCount) ?: return result
            attempted.add(candidate)
            onLine?.invoke("Auto-installing missing dependency: ${candidate.type}/${candidate.name}")
            val install = installDependencyForFallback(candidate.type, candidate.name, onLine, taskId)
            if (install.first != "installed") return result
            onLine?.invoke("Dependency installed; retrying task")
            result = execute()
        }
        return result
    }

    private fun syncInstalledRuntimeDependencies(log: String) {
        queryPipInstalledPackages().forEach { (name, version) ->
            upsertInstalledDependency(name, "python", version, log)
        }
        queryNpmInstalledPackages().forEach { (name, version) ->
            upsertInstalledDependency(name, "nodejs", version, log)
        }
    }

    private fun upsertInstalledDependency(name: String, type: String, version: String = "", log: String) {
        val normalizedName = DependencyStorage.normalizedName(type, name)
        val runtimeVersion = if (type == "python") DependencyStorage.PYTHON_VERSION else ""
        val now = Instant.now().toString()
        val existingId = readableDatabase.query(
            "dependencies",
            arrayOf("id"),
            "name = ? AND type = ? AND python_version = ?",
            arrayOf(normalizedName, type, runtimeVersion),
            null,
            null,
            null,
            "1",
        ).use { cursor -> if (cursor.moveToFirst()) cursor.long("id") else null }
        val values = ContentValues().apply {
            put("name", normalizedName)
            put("type", type)
            put("python_version", runtimeVersion)
            put("version", version)
            put("status", "installed")
            put("log", log)
            put("updated_at", now)
            if (existingId == null) put("created_at", now)
        }
        if (existingId == null) writableDatabase.insertOrThrow("dependencies", null, values)
        else writableDatabase.update("dependencies", values, "id = ?", arrayOf(existingId.toString()))
    }

    private fun prepareScriptDependencies(runId: String, file: File): Boolean {
        val scan = ScriptCompatibility.scan(file)
        if (scan.missingCompanionFiles.isNotEmpty()) {
            scan.missingCompanionFiles.forEach { appendScriptRunLog(runId, "MISSING_COMPANION_FILE: $it (required from ${file.parentFile?.absolutePath})") }
            return false
        }
        val dependencies = (scan.pythonPackages.map { "python" to it } + scan.nodePackages.map { "nodejs" to it })
            .distinct().take(ScriptCompatibility.MAX_AUTO_INSTALLS)
        if (dependencies.isEmpty()) return true
        appendScriptRunLog(runId, "Compatibility scan found ${dependencies.size} allowlisted dependencies; unknown imports are not auto-installed")
        for ((type, name) in dependencies) {
            appendScriptRunLog(runId, "Dependency install started: $type/$name")
            val result = installDependencyForFallback(
                type,
                name,
                onLine = { line -> appendScriptRunLog(runId, line) },
            )
            if (result.second.isNotBlank()) appendScriptRunLog(runId, "Dependency install result: ${result.first}\n${result.second.take(1000)}")
            if (result.first != "installed") {
                appendScriptRunLog(runId, "Dependency install failed: $type/$name (${result.first})")
                return false
            }
            val version = if (type == "python") queryPipInstalledPackages()[DependencyStorage.normalizedName(type, name)].orEmpty()
                else queryNpmInstalledPackages()[DependencyStorage.normalizedName(type, name)].orEmpty()
            upsertInstalledDependency(name, type, version, "Auto-installed while running script")
        }
        return true
    }

    private fun copyManagedHelper(assetName: String, target: File) {
        val content = appContext.assets.open("helpers/$assetName").bufferedReader().use { it.readText() }
        val current = target.takeIf(File::isFile)?.readText()
        if (current == null || current.contains("DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1")) {
            target.parentFile?.mkdirs()
            if (current != content) target.writeText(content)
        }
    }

    private fun ensureNodeNotifyPackage(root: File) {
        val target = File(root, "node_modules/notify")
        listOf("package.json", "index.cjs", "index.mjs").forEach { name ->
            val content = appContext.assets.open("helpers/notify-package/$name").bufferedReader().use { it.readText() }
            val file = File(target, name)
            val current = file.takeIf(File::isFile)?.readText()
            if (current == null || current.contains("DAIDAI_PANEL_MANAGED_NOTIFY_HELPER v1")) {
                file.parentFile?.mkdirs()
                if (current != content) file.writeText(content)
            }
        }
    }

    private fun ensureQingLongShims(workingDir: File) {
        workingDir.mkdirs()
        val scripts = scriptsRoot()
        copyManagedHelper("notify.py", File(scripts, "notify.py"))
        copyManagedHelper("sendNotify.js", File(scripts, "sendNotify.js"))
        ensureNodeNotifyPackage(scripts)
        if (workingDir.canonicalFile != scripts.canonicalFile) {
            copyManagedHelper("notify.py", File(workingDir, "notify.py"))
            copyManagedHelper("sendNotify.js", File(workingDir, "sendNotify.js"))
        }
        val common = File(workingDir, "common.js")
        if (!common.exists()) common.writeText(
            "'use strict';\nmodule.exports = { sleep: ms => new Promise(resolve => setTimeout(resolve, Number(ms) || 0)) };\n"
        )
    }

    private fun runtimeEnvironment(workingDir: File = scriptsRoot()): MutableMap<String, String> {
        val env = AndroidLinuxRuntime.baseEnvironment(appContext, workingDir).apply {
            putAll(
                mapOf(
                    "DAIDAI_ANDROID_LOCAL" to "1",
                    "DAIDAI_SCRIPTS_DIR" to scriptsRoot().absolutePath,
                    "DAIDAI_NOTIFY_PY" to File(scriptsRoot(), "notify.py").absolutePath,
                    "DAIDAI_SEND_NOTIFY_JS" to File(scriptsRoot(), "sendNotify.js").absolutePath,
                    "DAIDAI_NOTIFY_URL" to "${endpointProvider().trimEnd('/')}/api/v1/notifications/send",
                    "DAIDAI_NOTIFY_ORIGIN" to endpointProvider().trimEnd('/'),
                    "DAIDAI_NOTIFY_TOKEN" to localTokenProvider(),
                    "DAIDAI_NOTIFY_LOCAL_TOKEN" to localTokenProvider(),
                    "DAIDAI_NOTIFY_TIMEOUT" to "15000",
                    "DAIDAI_API_BASE" to "${endpointProvider().trimEnd('/')}/api/v1",
                    "DAIDAI_TOKEN" to localTokenProvider(),
                    "QL_DIR" to scriptsRoot().absolutePath,
                    "QL_DATA_DIR" to appContext.filesDir.absolutePath,
                    "QL_SCRIPT_DIR" to workingDir.absolutePath,
                    "DAIDAI_ALLOW_UNVERIFIED_ANDROID_ABI_WHEELS" to if (configBool("allow_unverified_android_abi_wheels", false)) "1" else "0",
                    "DAIDAI_TEST_AUTO_INSTALL" to if (configBool("auto_install_deps", true)) "1" else "0",
                    "PYTHONPATH" to listOf(
                        scriptsRoot().absolutePath,
                        DependencyStorage.pythonSitePackages(appContext.filesDir).absolutePath,
                    ).joinToString(File.pathSeparator),
                    "PIP_TARGET" to DependencyStorage.pythonSitePackages(appContext.filesDir).absolutePath,
                    "NODE_PATH" to listOf(
                        scriptsRoot().absolutePath,
                        AndroidNodeRuntime.ensureReady(appContext)?.modules.orEmpty(),
                        File(AndroidNodeRuntime.depsDir(appContext), "node_modules").absolutePath,
                    ).filter(String::isNotBlank).joinToString(File.pathSeparator),
                    "NODE_OPTIONS" to "--require=${File(scriptsRoot(), "sendNotify.js").absolutePath}",
                    "NPM_CONFIG_IGNORE_SCRIPTS" to "true",
                    "NPM_CONFIG_CACHE" to DependencyStorage.npmCache(appContext.filesDir).absolutePath,
                )
            )
        }
        AndroidPythonRuntime.ensureReady(appContext)?.let { runtime ->
            env["PYTHONHOME"] = runtime.home
            env["PYTHONPATH"] = listOf(
                scriptsRoot().absolutePath,
                runtime.sitePackages,
                DependencyStorage.pythonSitePackages(appContext.filesDir).absolutePath,
            ).joinToString(File.pathSeparator)
        }
        AndroidNodeRuntime.ensureReady(appContext)?.let { runtime ->
            env["NPM_CONFIG_GLOBALCONFIG"] = File(runtime.home, "etc/npmrc").absolutePath
        }
        val grouped = linkedMapOf<String, MutableList<String>>()
        readableDatabase.query(
            "envs",
            arrayOf("name", "value"),
            "enabled = 1",
            null,
            null,
            null,
            "id ASC"
        ).use { cursor ->
            while (cursor.moveToNext()) {
                val name = cursor.string("name").trim()
                if (name.matches(Regex("[A-Za-z_][A-Za-z0-9_]*")) && name !in reservedRuntimeEnvironmentNames) {
                    grouped.getOrPut(name) { mutableListOf() } += cursor.string("value")
                }
            }
        }
        grouped.forEach { (name, values) -> env[name] = LocalTaskFallbackSemantics.joinEnvironmentValues(values) }
        return env
    }

    private fun applyProcessEnvironment(
        command: List<String>,
        target: MutableMap<String, String>,
        workingDir: File,
        extraEnvironment: Map<String, String> = emptyMap(),
    ) {
        LocalTaskFallbackSemantics.applyRuntimeEnvironment(target, runtimeEnvironment(workingDir))
        target.putAll(extraEnvironment)
        if (command.firstOrNull()?.substringAfterLast('/') !in setOf("liboperit_proot.so", "libdaidai_proot.so")) return
        target.remove("PYTHONHOME")
        target["HOME"] = "/root"
        target["PWD"] = "/workspace"
        target["TMPDIR"] = "/tmp"
        target["PYTHONPATH"] = "/host-files/deps/python/${DependencyStorage.PYTHON_VERSION}/site-packages:/workspace"
        target["NODE_PATH"] = "/host-files/deps/nodejs/node_modules:/usr/local/lib/node_modules:/usr/lib/node_modules:/workspace"
        target.remove("NODE_OPTIONS")
        target["QL_DIR"] = "/host-files"
        target["QL_DATA_DIR"] = "/host-files"
        target["QL_SCRIPT_DIR"] = "/workspace"
    }

    private fun terminalEnvironment(): MutableMap<String, String> = AndroidLinuxRuntime.baseEnvironment(appContext, appContext.filesDir).apply {
        put("HOME", "/root")
        put("PWD", "/host-files")
        put("TMPDIR", "/tmp")
        put("TERM", "xterm-256color")
        put("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
        remove("PYTHONHOME")
        remove("PYTHONPATH")
        remove("NODE_OPTIONS")
    }

    private val reservedRuntimeEnvironmentNames = setOf(
        "DAIDAI_ANDROID_LOCAL",
        "DAIDAI_SCRIPTS_DIR",
        "DAIDAI_NOTIFY_PY",
        "DAIDAI_SEND_NOTIFY_JS",
        "DAIDAI_NOTIFY_URL",
        "DAIDAI_NOTIFY_ORIGIN",
        "DAIDAI_NOTIFY_TOKEN",
        "DAIDAI_NOTIFY_LOCAL_TOKEN",
        "DAIDAI_NOTIFY_TIMEOUT",
        "DAIDAI_API_BASE",
        "DAIDAI_TOKEN",
        "QL_DIR",
        "QL_DATA_DIR",
        "QL_SCRIPT_DIR",
        "PYTHONHOME",
        "PYTHONPATH",
        "PIP_TARGET",
        "NODE_PATH",
        "NODE_OPTIONS",
        "NPM_CONFIG_GLOBALCONFIG",
        "NPM_CONFIG_IGNORE_SCRIPTS",
        "NPM_CONFIG_CACHE",
        "LD_LIBRARY_PATH",
        "LD_PRELOAD",
    )

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

    private fun scriptRunLogs(runId: String, session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
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
            val persisted = JSONArray(cursor.string("logs_json"))
            val logLines = scriptRunLogsMemory[runId]?.let { memory -> synchronized(memory) { memory.toList() } }
                ?: (0 until persisted.length()).map { persisted.optString(it) }
            val requestedCursor = requestLogCursor(session).coerceAtMost(logLines.size.toLong()).toInt()
            val incrementalLines = logLines.drop(requestedCursor)
            val status = cursor.string("status")
            val failed = status == "failed" || status == "stopped" ||
                (!cursor.isNull(cursor.getColumnIndexOrThrow("exit_code")) && cursor.int("exit_code") != 0)
            val payload = JSONObject()
                .put("logs", JSONArray(incrementalLines))
                .put("content", incrementalLines.joinToString("\n"))
                .put("log_count", logLines.size)
                .put("cursor", logLines.size)
                .put("done", cursor.int("done") == 1)
                .put("status", status)
            ScriptRunLogPresentation.errorSummary(logLines, failed)?.let { payload.put("error", it) }
            if (!cursor.isNull(cursor.getColumnIndexOrThrow("exit_code"))) {
                payload.put("exit_code", cursor.int("exit_code"))
            }
            ok(JSONObject().put("data", payload))
        }
    }

    private fun stopScriptRun(runId: String): NanoHTTPD.Response {
        val currentStatus = scriptRunStatus(runId) ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "运行记录不存在")
        if (currentStatus != "running") return ok(JSONObject().put("message", "运行已结束"))
        val process = scriptProcesses[runId]
        process?.destroy()
        if (process != null && !process.waitFor(1, TimeUnit.SECONDS)) process.destroyForcibly()
        appendScriptRunLog(runId, "Android local fallback script run stopped")
        finishScriptRun(runId, "stopped", 130)
        return ok(JSONObject().put("message", "已停止"))
    }

    private fun clearScriptRun(runId: String): NanoHTTPD.Response {
        val status = scriptRunStatus(runId) ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "运行记录不存在")
        if (status == "running") return error(NanoHTTPD.Response.Status.CONFLICT, "运行中的记录不能清除")
        writableDatabase.delete("script_runs", "id = ?", arrayOf(runId)); scriptRunLogsMemory.remove(runId)
        scriptRunLogCharacters.remove(runId); scriptRunPendingPersistence.remove(runId); scriptRunLocks.remove(runId)
        return ok(JSONObject().put("message", "已清除"))
    }

    private fun createRunningScriptRun(runId: String) {
        val now = Instant.now().toString()
        scriptRunLogsMemory[runId] = Collections.synchronizedList(mutableListOf())
        scriptRunLogCharacters[runId] = 0
        scriptRunPendingPersistence[runId] = 0
        writableDatabase.insertOrThrow("script_runs", null, ContentValues().apply {
            put("id", runId); put("status", "running"); put("logs_json", "[]"); put("done", 0)
            putNull("exit_code"); put("created_at", now); put("updated_at", now)
        })
    }

    private fun scriptRunStatus(runId: String): String? = readableDatabase.query(
        "script_runs", arrayOf("status"), "id = ?", arrayOf(runId), null, null, null
    ).use { if (it.moveToFirst()) it.string("status") else null }

    private fun appendScriptRunLog(runId: String, rawLine: String) {
        synchronized(scriptRunLocks.computeIfAbsent(runId) { Any() }) {
            val memory = scriptRunLogsMemory.computeIfAbsent(runId) { Collections.synchronizedList(mutableListOf()) }
            val line = rawLine.take(MAX_SCRIPT_LOG_LINE_CHARS)
            memory += line
            var characters = (scriptRunLogCharacters[runId] ?: 0) + line.length
            while (memory.size > MAX_SCRIPT_LOG_LINES || characters > MAX_SCRIPT_LOG_TOTAL_CHARS) {
                characters -= memory.removeAt(0).length
            }
            scriptRunLogCharacters[runId] = characters
            val pending = (scriptRunPendingPersistence[runId] ?: 0) + 1
            scriptRunPendingPersistence[runId] = pending
            if (pending >= LOG_PERSIST_BATCH_SIZE) persistScriptRunLogs(runId, memory)
        }
    }

    private fun persistScriptRunLogs(runId: String, memory: List<String>) {
        val logs = JSONArray(); memory.forEach { logs.put(it) }
        writableDatabase.update("script_runs", ContentValues().apply {
            put("logs_json", logs.toString()); put("updated_at", Instant.now().toString())
        }, "id = ?", arrayOf(runId))
        scriptRunPendingPersistence[runId] = 0
    }

    private fun finishScriptRun(runId: String, status: String, exitCode: Int) {
        synchronized(scriptRunLocks.computeIfAbsent(runId) { Any() }) {
            scriptRunLogsMemory[runId]?.let { persistScriptRunLogs(runId, it) }
            writableDatabase.update("script_runs", ContentValues().apply {
                put("status", status); put("done", 1); put("exit_code", exitCode); put("updated_at", Instant.now().toString())
            }, "id = ?", arrayOf(runId))
        }
    }

    private fun saveScriptRun(runId: String, status: String, logs: JSONArray, done: Boolean, exitCode: Int?) {
        val now = Instant.now().toString()
        val bounded = JSONArray()
        val first = (logs.length() - MAX_SCRIPT_LOG_LINES).coerceAtLeast(0)
        for (i in first until logs.length()) bounded.put(logs.optString(i).take(MAX_SCRIPT_LOG_LINE_CHARS))
        val values = ContentValues().apply {
            put("id", runId); put("status", status); put("logs_json", bounded.toString()); put("done", if (done) 1 else 0)
            if (exitCode == null) putNull("exit_code") else put("exit_code", exitCode)
            put("created_at", now); put("updated_at", now)
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
        val clean = cleanScriptPath(path)
        val root = scriptsRoot().canonicalFile
        return File(root, clean.replace('/', File.separatorChar)).canonicalFile.also {
            require(it.toPath().startsWith(root.toPath())) { "脚本路径越界" }
        }
    }

    private fun cleanScriptPath(path: String, allowRootAlias: Boolean = false): String =
        normalizeScriptPath(path, allowRootAlias)

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
        val channelId = taskNotificationChannelId(json)
            ?: if (json.has("notification_channel_id") && !json.isNull("notification_channel_id") && json.optLong("notification_channel_id") > 0) {
                return error(NanoHTTPD.Response.Status.BAD_REQUEST, "通知渠道不存在")
            } else null
        val now = Instant.now().toString()
        val values = ContentValues().apply {
            put("name", json.optString("name", "未命名任务"))
            put("command", json.optString("command"))
            put("cron_expression", json.optString("cron_expression"))
            put("task_type", json.optString("task_type", "manual"))
            put("python_version", json.optString("python_version"))
            put("task_before", json.optString("task_before"))
            put("task_after", json.optString("task_after"))
            put("timeout", json.optInt("timeout", 0).coerceIn(0, 604800))
            put("max_retries", json.optInt("max_retries", 0).coerceIn(0, 20))
            put("retry_interval", json.optInt("retry_interval", 60).coerceIn(0, 86400))
            if (json.has("depends_on") && !json.isNull("depends_on")) put("depends_on", json.optLong("depends_on"))
            put("stop_schedule", json.optString("stop_schedule"))
            put("notify_on_failure", if (json.optBoolean("notify_on_failure")) 1 else 0)
            put("notify_on_success", if (json.optBoolean("notify_on_success")) 1 else 0)
            put("notify_on_abort", if (json.optBoolean("notify_on_abort")) 1 else 0)
            channelId?.let { put("notification_channel_id", it) }
            put("status", json.optDouble("status", 1.0))
            put("labels", json.optJSONArray("labels")?.toString() ?: "[]")
            put("created_at", now)
            put("updated_at", now)
        }
        val id = writableDatabase.insertOrThrow("tasks", null, values)
        return taskDetail(id)
    }

    private fun taskDetail(id: Long): NanoHTTPD.Response {
        val cursor = readableDatabase.query("tasks", null, "id = ?", arrayOf(id.toString()), null, null, null)
        return if (cursor.moveToFirst()) {
            val data = JSONObject().apply {
                put("id", cursor.long("id"))
                put("name", cursor.string("name"))
                put("command", cursor.string("command"))
                put("task_type", cursor.string("task_type"))
                put("status", cursor.double("status"))
                put("cron_expression", cursor.string("cron_expression"))
                put("python_version", cursor.string("python_version"))
                put("task_before", cursor.string("task_before"))
                put("task_after", cursor.string("task_after"))
                put("timeout", cursor.int("timeout"))
                put("max_retries", cursor.int("max_retries"))
                put("retry_interval", cursor.int("retry_interval"))
                put("depends_on", if (cursor.isNull(cursor.getColumnIndexOrThrow("depends_on"))) JSONObject.NULL else cursor.long("depends_on"))
                put("stop_schedule", cursor.string("stop_schedule"))
                put("notify_on_failure", cursor.int("notify_on_failure") != 0)
                put("notify_on_success", cursor.int("notify_on_success") != 0)
                put("notify_on_abort", cursor.int("notify_on_abort") != 0)
                put("notification_channel_id", if (cursor.isNull(cursor.getColumnIndexOrThrow("notification_channel_id"))) JSONObject.NULL else cursor.long("notification_channel_id"))
                put("labels", JSONArray(cursor.string("labels")))
                put("last_run_status", taskRunStatusCode(cursor.string("last_run_status")))
                put("last_run_at", if (cursor.string("last_run_status").isBlank()) JSONObject.NULL else cursor.string("updated_at"))
                put("last_log_id", cursor.long("last_log_id"))
                put("created_at", cursor.string("created_at"))
                put("updated_at", cursor.string("updated_at"))
            }
            cursor.close()
            ok(JSONObject().put("data", data))
        } else {
            cursor.close()
            error(NanoHTTPD.Response.Status.NOT_FOUND, "task not found")
        }
    }

    private fun updateTask(id: Long, json: JSONObject): NanoHTTPD.Response {
        val channelId = taskNotificationChannelId(json)
        if (json.has("notification_channel_id") && !json.isNull("notification_channel_id") && json.optLong("notification_channel_id") > 0 && channelId == null) {
            return error(NanoHTTPD.Response.Status.BAD_REQUEST, "通知渠道不存在")
        }
        val values = ContentValues().apply {
            listOf("name", "command", "cron_expression", "task_type", "python_version", "task_before", "task_after", "stop_schedule").forEach { key ->
                if (json.has(key)) put(key, json.optString(key))
            }
            listOf("notify_on_failure", "notify_on_success", "notify_on_abort").forEach { key ->
                if (json.has(key)) put(key, if (json.optBoolean(key)) 1 else 0)
            }
            if (json.has("notification_channel_id")) {
                if (json.isNull("notification_channel_id")) putNull("notification_channel_id")
                else if (channelId == null) putNull("notification_channel_id") else put("notification_channel_id", channelId)
            }
            if (json.has("status")) put("status", json.optDouble("status"))
            listOf("timeout", "max_retries", "retry_interval").forEach { key -> if (json.has(key)) put(key, json.optInt(key)) }
            if (json.has("depends_on")) { if (json.isNull("depends_on")) putNull("depends_on") else put("depends_on", json.optLong("depends_on")) }
            if (json.has("labels")) put("labels", json.optJSONArray("labels")?.toString() ?: "[]")
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
        return taskDetail(id)
    }

    private fun taskNotificationChannelId(json: JSONObject): Long? {
        if (!json.has("notification_channel_id") || json.isNull("notification_channel_id")) return null
        val id = json.optLong("notification_channel_id")
        if (id <= 0) return null
        return readableDatabase.query("notification_channels", arrayOf("id"), "id=?", arrayOf(id.toString()), null, null, null)
            .use { if (it.moveToFirst()) id else null }
    }

    private fun updateTaskStatus(id: Long, action: String): NanoHTTPD.Response {
        if (action == "pin" || action == "unpin") {
            writableDatabase.execSQL("UPDATE tasks SET pinned=?, updated_at=? WHERE id=?", arrayOf<Any?>(if (action == "pin") 1 else 0, Instant.now().toString(), id))
            return ok(JSONObject().put("data", JSONObject().put("id", id).put("pinned", action == "pin")))
        }
        if (action == "stop") {
            val lock = taskRunLocks.computeIfAbsent(id) { Any() }
            synchronized(lock) {
                if (!runningTaskIds.contains(id) || taskFinalizedIds.contains(id)) {
                    return ok(JSONObject().put("data", JSONObject().put("id", id).put("run_status", "finished")))
                }
                taskAbortRequested.add(id)
                taskProcesses[id]?.let(::terminateTaskProcess)
            }
            return ok(JSONObject().put("data", JSONObject().put("id", id).put("run_status", "aborting")))
        }
        val status = when (action) {
            "disable" -> 0.0
            "run" -> 2.0
            else -> 1.0
        }
        val values = ContentValues().apply {
            put("status", status)
            put("updated_at", Instant.now().toString())
        }
        writableDatabase.update("tasks", values, "id = ?", arrayOf(id.toString()))
        if (action == "run") {
            if (!enqueueTask(id)) {
                return error(NanoHTTPD.Response.Status.CONFLICT, "任务正在运行")
            }
            return NanoHTTPD.newFixedLengthResponse(
                NanoHTTPD.Response.Status.ACCEPTED,
                "application/json; charset=utf-8",
                JSONObject()
                    .put("message", "任务已开始")
                    .put("data", JSONObject().put("id", id).put("status", 2.0).put("run_status", "running"))
                    .toString(),
            )
        }
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("status", status)))
    }

    private fun insertTaskLog(taskId: Long, result: LocalScriptResult, startedAt: Instant, endedAt: Instant): Long {
        val content = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        val statusCode = when (result.status) {
            "success" -> 0
            "failed" -> 1
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
            put("log_cursor", result.logs.length())
        }
        return writableDatabase.insertOrThrow("task_logs_local", null, values)
    }

    private fun createRunningTaskLog(taskId: Long, startedAt: Instant, initialLine: String): Long {
        val values = ContentValues().apply {
            put("task_id", taskId); put("content", initialLine); put("logs_json", JSONArray().put(initialLine).toString())
            put("status", 2); putNull("exit_code"); put("duration", 0.0); put("started_at", startedAt.toString())
            put("ended_at", ""); put("created_at", startedAt.toString()); put("log_cursor", 1)
        }
        return writableDatabase.insertOrThrow("task_logs_local", null, values)
    }

    private fun finishTaskLog(taskId: Long, logId: Long, result: LocalScriptResult, startedAt: Instant, endedAt: Instant) {
        val values = ContentValues().apply {
            put("content", (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) })
            put("logs_json", result.logs.toString()); put("status", when (result.status) { "success" -> 0; "aborted", "stopped" -> 2; else -> 1 })
            if (result.exitCode == null) putNull("exit_code") else put("exit_code", result.exitCode)
            put("duration", (endedAt.toEpochMilli() - startedAt.toEpochMilli()) / 1000.0)
            put("ended_at", endedAt.toString())
            put("log_cursor", taskRunCursors[taskId] ?: result.logs.length().toLong())
        }
        writableDatabase.update("task_logs_local", values, "id = ?", arrayOf(logId.toString()))
    }

    internal data class ScheduledTask(val id: Long, val cronExpression: String)
    internal data class ScheduledTaskStop(val id: Long, val stopSchedule: String)

    internal fun enabledScheduledTasks(): List<ScheduledTask> {
        val tasks = mutableListOf<ScheduledTask>()
        readableDatabase.query("tasks", arrayOf("id", "cron_expression"), "status > 0 AND task_type='cron' AND cron_expression <> ''", null, null, null, null).use { cursor ->
            while (cursor.moveToNext()) tasks += ScheduledTask(cursor.long("id"), cursor.string("cron_expression"))
        }
        return tasks
    }

    internal fun scheduledTaskStops(): List<ScheduledTaskStop> {
        val tasks = mutableListOf<ScheduledTaskStop>()
        readableDatabase.query("tasks", arrayOf("id", "stop_schedule"), "status > 0 AND stop_schedule <> ''", null, null, null, null).use { cursor ->
            while (cursor.moveToNext()) tasks += ScheduledTaskStop(cursor.long("id"), cursor.string("stop_schedule"))
        }
        return tasks
    }

    internal fun stopScheduledTask(id: Long) {
        taskAbortRequested.add(id)
        taskProcesses[id]?.let(::terminateTaskProcess)
    }

    internal fun runScheduledBackupIfDue(now: java.time.ZonedDateTime) {
        if (!configBool("backup_schedule_enabled", false)) return
        val clock = configValue("backup_schedule_time", "03:00").split(':')
        val hour = clock.getOrNull(0)?.toIntOrNull() ?: return
        val minute = clock.getOrNull(1)?.toIntOrNull() ?: return
        if (now.hour != hour || now.minute != minute) return
        val frequency = configValue("backup_schedule_frequency", "daily")
        val matches = when (frequency) {
            "weekly" -> now.dayOfWeek.value % 7 == configValue("backup_schedule_weekday", "0").toIntOrNull()
            "monthly" -> now.dayOfMonth == configValue("backup_schedule_monthday", "1").toIntOrNull()
            else -> frequency == "daily"
        }
        if (!matches) return
        val key = "$frequency:${now.toLocalDate()}:$hour:$minute"
        if (key == lastScheduledBackupKey) return
        lastScheduledBackupKey = key
        runCatching {
            localBackupService.create(JSONObject().put("password", configValue("backup_schedule_password", "")))
        }.onSuccess { appLog("Backup", "Scheduled backup completed: ${it.optString("filename")}") }
            .onFailure { appLog("Backup", "Scheduled backup failed: ${it.message ?: it.javaClass.simpleName}") }
    }

    private fun enqueueTask(id: Long): Boolean {
        if (!runningTaskIds.add(id)) return false
        val startedAt = Instant.now()
        initializeTaskRun(id, startedAt, "任务已入队，正在启动...")
        try {
            taskRunExecutor.execute {
                try {
                    executeTaskAndSave(id, alreadyClaimed = true)
                } catch (error: Throwable) {
                    appendTaskRunLog(id, "任务执行异常: ${error.message ?: error.javaClass.simpleName}")
                    finishCrashedTask(id, startedAt)
                    appLog("Task", "Task $id crashed: ${error.message ?: error.javaClass.simpleName}")
                } finally {
                    clearTaskRun(id)
                }
            }
        } catch (_: RejectedExecutionException) {
            appendTaskRunLog(id, "任务执行队列已满，请稍后重试")
            finishCrashedTask(id, startedAt)
            clearTaskRun(id)
            return false
        }
        return true
    }

    private fun initializeTaskRun(id: Long, startedAt: Instant, initialLine: String) {
        taskRunStartedAt[id] = startedAt
        taskRunLocks.computeIfAbsent(id) { Any() }
        taskFinalizedIds.remove(id)
        taskRunLogsMemory[id] = Collections.synchronizedList(mutableListOf(initialLine))
        taskRunLogCharacters[id] = initialLine.length
        taskRunPendingPersistence[id] = 0
        taskRunCursors[id] = 1L
        taskRunLogIds[id] = createRunningTaskLog(id, startedAt, initialLine)
    }

    private fun clearTaskRun(id: Long) {
        runningTaskIds.remove(id); taskProcesses.remove(id); taskAbortRequested.remove(id); taskFinalizedIds.remove(id)
        taskRunLogIds.remove(id); taskRunCursors.remove(id); taskRunStartedAt.remove(id); taskRunLogsMemory.remove(id)
        taskRunLogCharacters.remove(id); taskRunPendingPersistence.remove(id)
        taskRunLocks.remove(id)
    }

    private fun finishCrashedTask(id: Long, startedAt: Instant) {
        synchronized(taskRunLocks.computeIfAbsent(id) { Any() }) {
            val lines = taskRunLogsMemory[id]?.let { synchronized(it) { it.toList() } }.orEmpty()
            val aborted = taskAbortRequested.contains(id)
            val result = LocalScriptResult(JSONArray(lines), if (aborted) "aborted" else "failed", true, if (aborted) 130 else 1)
            val endedAt = Instant.now()
            taskRunLogIds[id]?.let { finishTaskLog(id, it, result, startedAt, endedAt) }
            writableDatabase.update("tasks", ContentValues().apply {
                put("status", 1.0); put("last_run_status", result.status); put("last_run_logs", result.logs.toString())
                taskRunLogIds[id]?.let { put("last_log_id", it) }; put("updated_at", endedAt.toString())
            }, "id = ?", arrayOf(id.toString()))
            taskFinalizedIds.add(id)
        }
    }

    private fun appendTaskRunLog(id: Long, line: String) {
        val logs = taskRunLogsMemory[id] ?: return
        val snapshot = synchronized(logs) {
            val boundedLine = line.take(MAX_SCRIPT_LOG_LINE_CHARS)
            logs += boundedLine
            var characters = (taskRunLogCharacters[id] ?: 0) + boundedLine.length
            while (logs.size > MAX_SCRIPT_LOG_LINES || characters > MAX_SCRIPT_LOG_TOTAL_CHARS) {
                characters -= logs.removeAt(0).length
            }
            taskRunLogCharacters[id] = characters
            logs.toList()
        }
        val cursor = taskRunCursors.compute(id) { _, current -> (current ?: 0L) + 1L } ?: snapshot.size.toLong()
        val pending = taskRunPendingPersistence.compute(id) { _, value -> (value ?: 0) + 1 } ?: 1
        if (pending >= LOG_PERSIST_BATCH_SIZE) {
            taskRunLogIds[id]?.let { logId -> writableDatabase.update("task_logs_local", ContentValues().apply {
                put("content", snapshot.joinToString("\n")); put("logs_json", JSONArray(snapshot).toString()); put("log_cursor", cursor)
            }, "id = ?", arrayOf(logId.toString())) }
            taskRunPendingPersistence[id] = 0
        }
    }

    /** Unified manual/cron execution path. A task cannot overlap itself. */
    internal fun executeTaskAndSave(id: Long, alreadyClaimed: Boolean = false): Pair<LocalScriptResult, Long>? {
        if (!alreadyClaimed && !runningTaskIds.add(id)) return null
        val ownsLifecycle = !alreadyClaimed
        try {
            val startedAt = taskRunStartedAt[id] ?: Instant.now().also { initializeTaskRun(id, it, "任务已启动") }
            appLog("Task", "Running task $id")
            appendTaskRunLog(id, "任务已启动")
            val executed = runTaskNow(id) { line -> appendTaskRunLog(id, line) }
            val lock = taskRunLocks.computeIfAbsent(id) { Any() }
            val final = synchronized(lock) {
                val aborted = taskAbortRequested.contains(id)
                if (aborted) appendTaskRunLog(id, "任务已中止")
                val lines = taskRunLogsMemory[id]?.let { synchronized(it) { it.toList() } }.orEmpty()
                val result = LocalScriptResult(JSONArray(lines), if (aborted) "aborted" else executed.status, true, if (aborted) 130 else executed.exitCode)
                val endedAt = Instant.now()
                val logId = taskRunLogIds[id]?.also { finishTaskLog(id, it, result, startedAt, endedAt) }
                    ?: insertTaskLog(id, result, startedAt, endedAt)
                writableDatabase.update("tasks", ContentValues().apply {
                    put("status", 1.0); put("last_run_status", result.status); put("last_run_logs", result.logs.toString())
                    put("last_log_id", logId); put("updated_at", endedAt.toString())
                }, "id = ?", arrayOf(id.toString()))
                taskFinalizedIds.add(id)
                result to logId
            }
            val result = final.first
            val logId = final.second
            appLog("Task", "Task $id result: ${result.status} exit=${result.exitCode}")
            dispatchTaskCompletionNotification(id, result)
            return result to logId
        } finally {
            if (ownsLifecycle) clearTaskRun(id)
        }
    }

    private fun dispatchTaskCompletionNotification(taskId: Long, result: LocalScriptResult) {
        try {
            val settings = readableDatabase.query(
                "tasks",
                arrayOf("name", "notify_on_failure", "notify_on_success", "notify_on_abort", "notification_channel_id"),
                "id=?",
                arrayOf(taskId.toString()),
                null,
                null,
                null,
            ).use { cursor ->
                if (!cursor.moveToFirst()) return
                val channelIndex = cursor.getColumnIndexOrThrow("notification_channel_id")
                TaskNotificationSettings(
                    name = cursor.string("name"),
                    notifyFailure = cursor.int("notify_on_failure") != 0,
                    notifySuccess = cursor.int("notify_on_success") != 0,
                    notifyAbort = cursor.int("notify_on_abort") != 0,
                    channelId = if (cursor.isNull(channelIndex)) null else cursor.getLong(channelIndex),
                )
            }
            val enabled = LocalTaskFallbackSemantics.shouldNotify(
                result.status,
                settings.notifyFailure,
                settings.notifySuccess,
                settings.notifyAbort,
            )
            if (!enabled) return
            val content = when (result.status) {
                "success" -> "任务执行成功"
                "aborted" -> "任务已终止"
                else -> "任务执行失败（exit=${result.exitCode ?: "unknown"}）"
            }
            val response = sendNotificationByIds(settings.name, content, settings.channelId?.let(::setOf), includeDisabled = false)
            if (response.status.requestStatus >= 400) {
                throw IllegalStateException("通知渠道发送失败（HTTP ${response.status.requestStatus}）")
            }
            appLog("Notification", "Task $taskId notification dispatched: ${result.status}")
        } catch (error: Exception) {
            appLog("Notification", "Task $taskId notification failed: ${error.message ?: error.javaClass.simpleName}")
        }
    }

    private data class TaskNotificationSettings(
        val name: String,
        val notifyFailure: Boolean,
        val notifySuccess: Boolean,
        val notifyAbort: Boolean,
        val channelId: Long?,
    )

    private fun runTaskNow(id: Long, onLine: ((String) -> Unit)? = null): LocalScriptResult {
        return readableDatabase.query(
            "tasks",
            arrayOf("command", "name", "task_before", "task_after", "timeout", "max_retries", "retry_interval", "depends_on"),
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
            val before = cursor.string("task_before").trim()
            val after = cursor.string("task_after").trim()
            val timeout = cursor.int("timeout").takeIf { it > 0 }?.toLong() ?: 300L
            val maxRetries = cursor.int("max_retries").coerceIn(0, 20)
            val retryInterval = cursor.int("retry_interval").coerceIn(0, 86400)
            val dependsIndex = cursor.getColumnIndexOrThrow("depends_on")
            if (!cursor.isNull(dependsIndex)) {
                val dependencyID = cursor.getLong(dependsIndex)
                val dependencySucceeded = readableDatabase.query("tasks", arrayOf("last_run_status"), "id=?", arrayOf(dependencyID.toString()), null, null, null).use { dep -> dep.moveToFirst() && dep.string("last_run_status") == "success" }
                if (!dependencySucceeded) return@use LocalScriptResult(JSONArray().put("依赖任务不存在或上次执行未成功"), "failed", true, 3)
            }
            val logs = JSONArray()
            if (before.isNotEmpty()) {
                val hook = executeHookCommand(before, id) { line -> onLine?.invoke("[before] $line") }
                logs.put("[before] ${if (hook.status == "success") "success" else "failed"}")
                for (i in 0 until hook.logs.length()) logs.put("[before] ${hook.logs.optString(i)}")
            }
            fun runMain(): LocalScriptResult {
                return if (taskAbortRequested.contains(id)) {
                LocalScriptResult(JSONArray().put("Task aborted before main command"), "aborted", true, 130)
            } else if (Regex("^task(?:\\s|$)").containsMatchIn(command)) {
                val plan = try {
                    LocalTaskFallbackSemantics.parseTaskCommand(command) { candidate ->
                        runCatching { scriptFile(candidate).isFile }.getOrDefault(false)
                    }
                } catch (error: IllegalArgumentException) {
                    return LocalScriptResult(JSONArray().put(error.message ?: "任务命令无效"), "failed", true, 2)
                }
                val scriptPath = cleanScriptPath(plan.scriptPath)
                val file = scriptFile(scriptPath)
                val environments = try {
                    LocalTaskFallbackSemantics.taskEnvironments(plan, runtimeEnvironment(file.parentFile ?: scriptsRoot()))
                } catch (error: IllegalArgumentException) {
                    return LocalScriptResult(JSONArray().put(error.message ?: "任务环境无效"), "failed", true, 2)
                }
                var result = LocalScriptResult(JSONArray(), "success", true, 0)
                for (selected in environments) {
                    val prefixedOutput: ((String) -> Unit)? = if (plan.mode == "conc") {
                        { line -> onLine?.invoke("[${plan.envName}#${selected.index}] $line") }
                    } else onLine
                    result = executeWithAutoInstall(id, runtimeForFile(file), prefixedOutput) {
                        executeScriptFile(file, scriptPath, args = plan.scriptArgs, timeoutSeconds = minOf(plan.timeoutSeconds, timeout), extraEnvironment = selected.values, onLine = prefixedOutput, taskId = id)
                    }
                    if (result.status != "success") break
                }
                result
            } else {
                executeWithAutoInstall(id, runtimeForCommand(command), onLine) { executeStructuredCommand(command, id, onLine, timeout) }
                }
            }
            var main = runMain()
            var retry = 0
            while (main.status == "failed" && retry < maxRetries && !taskAbortRequested.contains(id)) {
                retry++
                logs.put("[第 $retry 次重试，等待 $retryInterval 秒]")
                if (retryInterval > 0) Thread.sleep(retryInterval * 1000L)
                if (!taskAbortRequested.contains(id)) main = runMain()
            }
            for (i in 0 until main.logs.length()) logs.put(main.logs.optString(i))
            // after always runs, including when the main command failed. Its failure is diagnostic only.
            if (after.isNotEmpty() && !taskAbortRequested.contains(id)) {
                val hook = executeHookCommand(after, id) { line -> onLine?.invoke("[after] $line") }
                logs.put("[after] ${if (hook.status == "success") "success" else "failed"}")
                for (i in 0 until hook.logs.length()) logs.put("[after] ${hook.logs.optString(i)}")
            }
            LocalScriptResult(logs, main.status, true, main.exitCode)
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
                "run" -> put("status", 1.0)
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
        if (action == "run") {
            val results = JSONArray()
            for (index in 0 until ids.length()) {
                val taskId = ids.optLong(index)
                val execution = executeTaskAndSave(taskId)
                results.put(JSONObject().put("id", taskId).put("status", execution?.first?.status ?: "conflict").put("log_id", execution?.second ?: JSONObject.NULL))
            }
            return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("results", results)))
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids)))
    }

    private fun latestTaskLogResponse(id: Long): NanoHTTPD.Response {
        val payload = latestTaskLogJson(id)
            ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "该任务还没有日志记录")
        return ok(payload)
    }

    private fun liveTaskLogResponse(id: Long, session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val requestedCursor = requestLogCursor(session)
        if (runningTaskIds.contains(id)) {
            val lines = taskRunLogsMemory[id]?.let { logs ->
                synchronized(logs) { logs.toList() }
            }.orEmpty()
            val startedAt = taskRunStartedAt[id]?.toString() ?: Instant.now().toString()
            val latestCursor = taskRunCursors[id] ?: lines.size.toLong()
            val resumed = LocalTaskFallbackSemantics.linesAfterCursor(lines, requestedCursor, latestCursor)
            val data = JSONObject()
                .put("task_id", id)
                .put("status", 2)
                .put("run_status", "running")
                .put("done", false)
                .put("logs", JSONArray(resumed.map { it.second }))
                .put("content", resumed.joinToString("\n") { it.second })
                .put("cursor", latestCursor)
                .put("log_id", taskRunLogIds[id] ?: JSONObject.NULL)
                .put("started_at", startedAt)
            return ok(JSONObject(data.toString()).put("data", data))
        }
        val payload = latestTaskLogJson(id)
            ?: return ok(JSONObject().put("data", JSONObject()
                .put("task_id", id)
                .put("status", 0.5)
                .put("run_status", "pending")
                .put("done", false)
                .put("logs", JSONArray())
                .put("content", "")
                .put("cursor", requestedCursor)
                .put("log_id", JSONObject.NULL)))
        val data = payload.optJSONObject("data") ?: return ok(payload)
        val lines = data.optJSONArray("logs")?.let { logs -> (0 until logs.length()).map { logs.optString(it) } }.orEmpty()
        val resumed = LocalTaskFallbackSemantics.linesAfterCursor(lines, requestedCursor, data.optLong("cursor", lines.size.toLong()))
        data.put("logs", JSONArray(resumed.map { it.second })).put("content", resumed.joinToString("\n") { it.second })
        payload.put("logs", data.getJSONArray("logs")).put("content", data.getString("content"))
        return ok(payload)
    }

    private fun latestTaskLogJson(taskId: Long): JSONObject? {
        return readableDatabase.rawQuery(
            "SELECT l.*, t.name task_name FROM task_logs_local l LEFT JOIN tasks t ON t.id=l.task_id WHERE l.task_id=? ORDER BY l.id DESC LIMIT 1",
            arrayOf(taskId.toString()),
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use null
            taskLogJson(cursor)
        }
    }

    private fun taskLogByIdJson(logId: Long): JSONObject? {
        return readableDatabase.rawQuery(
            "SELECT l.*, t.name task_name FROM task_logs_local l LEFT JOIN tasks t ON t.id=l.task_id WHERE l.id=?",
            arrayOf(logId.toString()),
        ).use { cursor ->
            if (!cursor.moveToFirst()) return@use null
            taskLogJson(cursor)
        }
    }

    private fun taskLogJson(cursor: Cursor): JSONObject {
        val logs = runCatching { JSONArray(cursor.string("logs_json")) }.getOrDefault(JSONArray())
        val payload = JSONObject()
            .put("id", cursor.long("id"))
            .put("task_id", cursor.long("task_id"))
            .put("content", cursor.string("content"))
            .put("logs", logs)
            .put("status", cursor.int("status"))
            .put("run_status", when (cursor.int("status")) { 0 -> "success"; 1 -> "failed"; 2 -> "running"; else -> "unknown" })
            .put("done", cursor.int("status") != 2)
            .put("exit_code", if (cursor.isNull(cursor.getColumnIndexOrThrow("exit_code"))) JSONObject.NULL else cursor.int("exit_code"))
            .put("duration", cursor.double("duration"))
            .put("started_at", cursor.string("started_at"))
            .put("ended_at", cursor.string("ended_at"))
            .put("created_at", cursor.string("created_at"))
            .put("cursor", cursor.long("log_cursor"))
            .put("log_id", cursor.long("id"))
        cursor.getColumnIndex("task_name").takeIf { it >= 0 }?.let { index ->
            payload.put("task_name", cursor.getString(index) ?: "")
        }
        return JSONObject(payload.toString()).put("data", payload)
    }

    private fun taskLogStream(id: Long, session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val running = runningTaskIds.contains(id)
        val logs = if (running) {
            val lines = taskRunLogsMemory[id]?.let { current ->
                synchronized(current) { current.toList() }
            }.orEmpty()
            JSONArray(lines)
        } else {
            latestTaskLogJson(id)?.optJSONObject("data")?.optJSONArray("logs")
                ?: JSONArray().put("Task log not found")
        }
        val payload = StringBuilder()
        val lines = (0 until logs.length()).map { logs.optString(it) }
        val latestCursor = if (running) taskRunCursors[id] ?: lines.size.toLong()
            else latestTaskLogJson(id)?.optJSONObject("data")?.optLong("cursor", lines.size.toLong()) ?: lines.size.toLong()
        for ((cursor, line) in LocalTaskFallbackSemantics.linesAfterCursor(lines, requestLogCursor(session), latestCursor)) {
            payload.append("id: ").append(cursor).append('\n')
            payload.append("data: ").append(line.replace("\n", "\\n")).append("\n\n")
        }
        payload.append("event: done\n")
        payload.append("data: ").append(if (running) "reconnect" else "finished").append("\n\n")
        return NanoHTTPD.newFixedLengthResponse(
            NanoHTTPD.Response.Status.OK,
            "text/event-stream; charset=utf-8",
            payload.toString()
        )
    }

    private fun requestLogCursor(session: NanoHTTPD.IHTTPSession): Long = LocalTaskFallbackSemantics.cursor(
        session.parms["cursor"], session.headers.entries.firstOrNull { it.key.equals("last-event-id", true) }?.value,
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
            put("task_before", json.optString("task_before"))
            put("task_after", json.optString("task_after"))
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
        val mode = json.optString("mode", "merge").trim().lowercase()
        if (mode !in setOf("merge", "replace")) return error(NanoHTTPD.Response.Status.BAD_REQUEST, "导入模式仅支持 merge 或 replace")
        val errors = JSONArray()
        var imported = 0
        val items = (0 until envs.length()).map { index ->
            envs.optJSONObject(index)?.also { require(it.optString("name").isNotBlank()) { "第 ${index + 1} 条变量名为空" } }
                ?: throw IllegalArgumentException("第 ${index + 1} 条变量格式无效")
        }
        writableDatabase.beginTransaction()
        try {
            if (mode == "replace") writableDatabase.delete("envs", null, null)
            for (item in items) {
                upsertEnv(item)
                imported++
            }
            writableDatabase.setTransactionSuccessful()
        } catch (error: Exception) {
            errors.put(error.message ?: "导入失败")
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("message", "已按 $mode 模式导入 $imported 个环境变量").put("mode", mode).put("imported", imported).put("errors", errors).put("data", JSONObject().put("mode", mode).put("imported", imported).put("errors", errors)))
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
        val remarks = json.optString("remarks")
        val existing = readableDatabase.query("envs", arrayOf("id"), "name = ? AND remarks = ?", arrayOf(name, remarks), null, null, null).use { cursor ->
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
                    "rename" -> {
                        val item = json.optJSONArray("items")?.let { if (index < it.length()) it.optJSONObject(index) else null }
                        val name = item?.optString("name") ?: json.optString("name")
                        if (name.isNotBlank()) writableDatabase.update("envs", ContentValues().apply { put("name", name); put("updated_at", Instant.now().toString()) }, "id=?", arrayOf(id.toString()))
                    }
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
        val depType = normalizeDependencyType(json.optString("type", "nodejs"))
            ?: return error(NanoHTTPD.Response.Status.BAD_REQUEST, "UNSUPPORTED_DEPENDENCY_TYPE: Android fallback supports pip/python, npm/nodejs, and rootfs system packages")
        val now = Instant.now().toString()
        val ids = JSONArray()
        val statuses = JSONArray()
        val results = ArrayList<Triple<String, String, Pair<String, String>>>()
        for (index in 0 until names.length()) {
            val name = names.optString(index).trim()
            if (name.isEmpty()) continue
            val rawResult = installDependencyForFallback(depType, name)
            val installResult = if (rawResult.first == "installed") rawResult else "failed" to rawResult.second
            results.add(Triple(name, depType, installResult))
            statuses.put(JSONObject().put("name", name).put("status", installResult.first).put("log", installResult.second.substring(0, Math.min(500, installResult.second.length))))
        }
        val installedPkgs = if (results.any { it.second == "python" && it.third.first == "installed" }) {
            queryPipInstalledPackages()
        } else { emptyMap() }
        writableDatabase.beginTransaction()
        try {
            for (triple in results) {
                val ver = installedPkgs[DependencyStorage.normalizedName(triple.second, triple.first)] ?: ""
                val normalizedName = DependencyStorage.normalizedName(triple.second, triple.first)
                val runtimeVersion = if (triple.second == "python") "3.14" else ""
                val values = ContentValues().apply {
                    put("name", normalizedName)
                    put("type", triple.second)
                    put("python_version", runtimeVersion)
                    put("version", ver)
                    put("status", triple.third.first)
                    put("log", triple.third.second)
                    put("updated_at", now)
                }
                val existingId = writableDatabase.query("dependencies", arrayOf("id"), "type = ? AND name = ? AND python_version = ?", arrayOf(triple.second, normalizedName, runtimeVersion), null, null, null).use { cursor ->
                    if (cursor.moveToFirst()) cursor.long("id") else null
                }
                if (existingId == null) {
                    values.put("created_at", now)
                    ids.put(writableDatabase.insertOrThrow("dependencies", null, values))
                } else {
                    writableDatabase.update("dependencies", values, "id = ?", arrayOf(existingId.toString()))
                    ids.put(existingId)
                }
            }
            if (installedPkgs.isNotEmpty()) {
                val existingNames = results.map { it.first.lowercase() }.toSet()
                val skipNames = setOf("python", "shell", "node", "git", "ssh", "pip")
                for ((pkgName, pkgVer) in installedPkgs) {
                    if (pkgName in existingNames || pkgName in skipNames) continue
                    val cursor = writableDatabase.query("dependencies", arrayOf("id"), "type = ? AND name = ? AND python_version = ?", arrayOf("python", pkgName, "3.14"), null, null, null)
                    val exists = cursor.count > 0
                    cursor.close()
                    if (!exists) {
                        val values = ContentValues().apply {
                            put("name", pkgName)
                            put("type", "python")
                            put("python_version", "3.14")
                            put("version", pkgVer)
                            put("status", "installed")
                            put("log", "Auto-installed as sub-dependency")
                            put("created_at", now)
                            put("updated_at", now)
                        }
                        writableDatabase.insertWithOnConflict("dependencies", null, values, SQLiteDatabase.CONFLICT_IGNORE)
                    }
                }
            }
            writableDatabase.setTransactionSuccessful()
        } finally {
            writableDatabase.endTransaction()
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("statuses", statuses)))
    }

    private fun queryPipInstalledPackages(): Map<String, String> {
        val runtime = AndroidPythonRuntime.ensureReady(appContext) ?: return emptyMap()
        val target = DependencyStorage.pythonSitePackages(appContext.filesDir).apply { mkdirs() }
        val script = File(appContext.filesDir, "runtimes/python-3.14/prefix/bin/pip_list.py")
        script.parentFile?.mkdirs()
        val pyCode = listOf(
            "import sys,os,json",
            "sp=os.environ.get('PIP_TARGET','')",
            "sys.path.insert(0,sp)",
            "from pip._internal.cli.main import main as pip_main",
            "import io",
            "old=sys.stdout",
            "sys.stdout=io.StringIO()",
            "try:",
            "    pip_main(['list','--format=json','--path',sp])",
            "except: pass",
            "out=sys.stdout.getvalue()",
            "sys.stdout=old",
            "print(out)"
        ).joinToString("\n")
        script.writeText(pyCode)
        val command = listOf(runtime.executable, runtime.wrapperScript, script.absolutePath)
        val logs = JSONArray()
        val result = runLocalProcess(command, target, logs)
        val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        return try {
            val start = text.indexOf("[")
            val end = text.lastIndexOf("]") + 1
            if (start >= 0 && end > start) {
                val pkgs = org.json.JSONArray(text.substring(start, end))
                (0 until pkgs.length()).associate { i ->
                    val pkg = pkgs.getJSONObject(i)
                    pkg.getString("name").lowercase() to pkg.getString("version")
                }
            } else { emptyMap() }
        } catch (_: Exception) { emptyMap() }
    }

    private fun normalizeDependencyType(raw: String): String? = when (raw.trim().lowercase()) {
        "python", "pip" -> "python"
        "node", "nodejs", "npm" -> "nodejs"
        "system", "linux", "os", "apk", "apt", "apt-get", "yum", "dnf" -> raw.trim().lowercase()
        else -> null
    }

    private fun installedPipResponse(): NanoHTTPD.Response {
        if (AndroidPythonRuntime.ensureReady(appContext) == null) {
            return error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: Python runtime is not ready")
        }
        val packages = queryPipInstalledPackages()
        val rows = JSONArray()
        packages.toSortedMap().forEach { (name, version) -> rows.put(JSONObject().put("name", name).put("version", version)) }
        return ok(rows)
    }

    private fun queryNpmInstalledPackages(): Map<String, String> {
        val runtime = AndroidNodeRuntime.ensureReady(appContext) ?: return emptyMap()
        val deps = AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
        val npm = File(runtime.modules, "npm/bin/npm-cli.js")
        val command = listOf(runtime.executable, AndroidNodeRuntime.wrapperPath, npm.absolutePath, "list", "--json", "--depth=0", "--prefix", deps.absolutePath)
        val result = runLocalProcess(command, deps, JSONArray())
        val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        val start = text.indexOf('{')
        val end = text.lastIndexOf('}') + 1
        if (start < 0 || end <= start) return emptyMap()
        return try {
            val dependencies = JSONObject(text.substring(start, end)).optJSONObject("dependencies") ?: JSONObject()
            dependencies.keys().asSequence().associate { name ->
                DependencyStorage.normalizedName("nodejs", name) to dependencies.optJSONObject(name)?.optString("version").orEmpty()
            }
        } catch (_: Exception) { emptyMap() }
    }

    private fun nodeDependencyResolvable(runtime: AndroidNodeRuntime.NodeRuntimePaths, packageName: String): Pair<Boolean, String> {
        val deps = AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
        val code = "const name=process.argv[1];const root=process.argv[2];console.log(require.resolve(name,{paths:[root]}));"
        val result = runLocalProcess(
            listOf(runtime.executable, AndroidNodeRuntime.wrapperPath, "-e", code, packageName, deps.absolutePath),
            deps,
            JSONArray().put("Verifying Node dependency resolution: $packageName"),
            30,
        )
        val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
        return (result.exitCode == 0) to text
    }

    private fun installedNpmResponse(): NanoHTTPD.Response {
        if (AndroidNodeRuntime.ensureReady(appContext) == null) {
            return error(NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE, "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: bundled Node runtime is not ready")
        }
        val dependencies = JSONObject()
        queryNpmInstalledPackages().toSortedMap().forEach { (name, version) ->
            dependencies.put(name, JSONObject().put("version", version))
        }
        return ok(JSONObject().put("dependencies", dependencies))
    }

    private fun installDependencyForFallback(depType: String, name: String, onLine: ((String) -> Unit)? = null, taskId: Long? = null): Pair<String, String> {
        val runtimeVersion = when (depType) {
            "python" -> DependencyStorage.PYTHON_VERSION
            "nodejs" -> DependencyStorage.NODE_VERSION
            else -> "rootfs"
        }
        return DependencyStorage.withInstallLock(depType, name, runtimeVersion) {
            val installedVersion = when (depType) {
                "python" -> queryPipInstalledPackages()[DependencyStorage.normalizedName(depType, name)]
                "nodejs" -> queryNpmInstalledPackages()[DependencyStorage.normalizedName(depType, name)]
                else -> null
            }
            if (DependencyStorage.satisfies(depType, name, installedVersion)) {
                return@withInstallLock "installed" to "Already installed${installedVersion?.let { ": $it" }.orEmpty()}; skipped network installation"
            }
            installDependencyForFallbackUnlocked(depType, name, onLine, taskId)
        }
    }

    private fun installDependencyForFallbackUnlocked(depType: String, name: String, onLine: ((String) -> Unit)? = null, taskId: Long? = null): Pair<String, String> {
        val mirrors = AndroidLinuxRuntime.mirrorConfig(appContext)
        if (depType == "python") {
            val allowUnverifiedNative = configBool("allow_unverified_android_abi_wheels", false)
            if (name.equals("pycryptodome", ignoreCase = true) && !allowUnverifiedNative) {
                return "blocked" to "UNVERIFIED_ANDROID_ABI_WHEEL_BLOCKED: enable allow_unverified_android_abi_wheels before installing native wheels"
            }
            if (AndroidLinuxRuntime.guestRuntimeAvailable(appContext, "/usr/bin/pip3")) {
                val target = DependencyStorage.pythonSitePackages(appContext.filesDir).apply { mkdirs() }
                val command = AndroidLinuxRuntime.guestCommand(appContext, appContext.filesDir, listOf("/usr/bin/pip3", "install", "--only-binary=:all:", "--target", "/host-files/deps/python/${DependencyStorage.PYTHON_VERSION}/site-packages", "-i", mirrors.pipMirror, name))
                    ?: return "unavailable" to "ROOTFS_PYTHON_UNAVAILABLE"
                val result = runLocalProcess(command, target, JSONArray().put("Installing Python dependency in ${AndroidLinuxRuntime.currentAbi()} rootfs"), ScriptCompatibility.INSTALL_TIMEOUT_SECONDS, onLine, taskId)
                val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
                return if (result.exitCode == 0) "installed" to text else "failed" to text
            }
            val runtime = AndroidPythonRuntime.ensureReady(appContext)
            runtime ?: return "unavailable" to "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: Python runtime is not ready"
            val installScript = File(appContext.filesDir, "runtimes/python-3.14/prefix/bin/pip_install.py")
            installScript.parentFile?.mkdirs()
            installScript.writeText("import sys, os\nsp = os.path.join(os.environ.get('PYTHONHOME',''), 'lib/python3.14/site-packages')\nsys.path.insert(0, sp)\nfrom pip._internal.cli.main import main as pip_main\nresult = pip_main(sys.argv[1:])\nsys.exit(result)\n")

            val target = DependencyStorage.pythonSitePackages(appContext.filesDir).apply { mkdirs() }
            val pipArguments = AndroidLinuxRuntime.pipInstallArguments(mirrors.pipMirror, target.absolutePath, name)
            val command = listOf(runtime.executable, runtime.wrapperScript, installScript.absolutePath) + pipArguments
            val logs = JSONArray()
                .put("Installing Python dependency: " + name)
                .put("pip_index=${mirrors.pipMirror}")
            val result = runLocalProcess(command, target, logs, ScriptCompatibility.INSTALL_TIMEOUT_SECONDS, onLine, taskId)
            val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
            if (result.exitCode != 0) {
                val noWheel = text.contains("No matching distribution found", true) || text.contains("Could not find a version", true)
                return "failed" to if (noWheel) "$text\nANDROID_WHEEL_UNAVAILABLE: no compatible Android wheel; source builds are disabled" else text
            }
            val verified = queryPipInstalledPackages().containsKey(DependencyStorage.normalizedName("python", name))
            return if (verified) "installed" to "$text\nPost-install verification: pip list confirmed $name"
            else "failed" to "$text\nPOST_VERIFY_FAILED: pip list did not report $name"
        }
        if (depType == "nodejs") {
            val restricted = setOf("@tencent-qqmail/agently-cli", "agent-browser", "clawhub")
            val allowUnverifiedNative = configBool("allow_unverified_android_abi_wheels", false)
            if (restricted.contains(name.lowercase()) && !allowUnverifiedNative) {
                return "blocked" to "RESTRICTED_NODE_PACKAGE_BLOCKED: enable allow_unverified_android_abi_wheels before installing restricted automation packages"
            }
            if (AndroidLinuxRuntime.guestRuntimeAvailable(appContext, "/usr/bin/npm")) {
                val deps = AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
                val installSpec = DependencyStorage.nodeInstallPackageSpec(name)
                val command = AndroidLinuxRuntime.guestCommand(appContext, appContext.filesDir, listOf("/usr/bin/npm", "install", "--ignore-scripts", "--no-audit", "--no-fund", "--prefix", "/host-files/deps/nodejs", "--registry", mirrors.npmMirror, "--", installSpec))
                    ?: return "unavailable" to "ROOTFS_NODE_UNAVAILABLE"
                val result = runLocalProcess(command, deps, JSONArray().put(DependencyStorage.nodeInstallCompatibilityNotice(name)).put("Installing Node dependency in ${AndroidLinuxRuntime.currentAbi()} rootfs: $installSpec"), ScriptCompatibility.INSTALL_TIMEOUT_SECONDS, onLine, taskId)
                val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
                return if (result.exitCode == 0) "installed" to text else "failed" to text
            }
            val directNode = AndroidNodeRuntime.ensureReady(appContext)
            return directNode?.let { runtime ->
                val deps = AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
                val cache = DependencyStorage.npmCache(appContext.filesDir).apply { mkdirs() }
                val npm = File(runtime.modules, "npm/bin/npm-cli.js")
                val installSpec = DependencyStorage.nodeInstallPackageSpec(name)
                val npmArguments = AndroidLinuxRuntime.npmInstallArguments(mirrors.npmMirror, deps.absolutePath, cache.absolutePath, installSpec)
                val command = listOf(runtime.executable, AndroidNodeRuntime.wrapperPath, npm.absolutePath) + npmArguments
                val logs = JSONArray()
                    .put(DependencyStorage.nodeInstallCompatibilityNotice(name))
                    .put("Installing Node dependency from network source: $installSpec")
                    .put("npm_registry=${mirrors.npmMirror}")
                    .put("allow_unverified_android_abi_wheels=$allowUnverifiedNative")
                val before = queryNpmInstalledPackages()
                val result = runLocalProcess(command, deps.also { it.mkdirs() }, logs, ScriptCompatibility.INSTALL_TIMEOUT_SECONDS, onLine, taskId)
                DependencyStorage.trimDirectory(cache, DependencyStorage.MAX_CACHE_BYTES)
                val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
                if (result.exitCode != 0) "failed" to text else {
                    val after = queryNpmInstalledPackages()
                    val normalized = DependencyStorage.normalizedName("nodejs", name)
                    val listed = after.containsKey(normalized)
                    val resolved = if (listed) true to "" else nodeDependencyResolvable(runtime, normalized)
                    val changed = after.any { (pkg, version) -> before[pkg] != version }
                    if (listed) "installed" to "$text\nPost-install verification: npm list confirmed $normalized"
                    else if (resolved.first) "installed" to "$text\nPost-install verification: require.resolve confirmed $normalized"
                    else "failed" to "$text\nPOST_VERIFY_FAILED: npm list and require.resolve did not report $normalized\n${resolved.second}" + if (changed) "\nObserved package tree changes during install" else ""
                }
            } ?: ("unavailable" to "RUNTIME_PACKAGE_MANAGER_UNAVAILABLE: ${AndroidNodeRuntime.failureReason().ifBlank { "bundled Node runtime is not ready" }}")
        }
        if (depType in setOf("system", "linux", "os", "apk", "apt", "apt-get", "yum", "dnf")) {
            val preferredManager = when (depType) {
                "apk", "apt", "apt-get", "yum", "dnf" -> depType
                else -> ""
            }
            if (!AndroidLinuxRuntime.isSafeSystemPackageSpec(name)) {
                return "blocked" to "UNSAFE_SYSTEM_PACKAGE_SPEC: only package names with letters, digits, dots, underscores, plus, colon, and dash are allowed"
            }
            val command = AndroidLinuxRuntime.systemPackageInstallCommand(appContext, name, preferredManager, mirrors)
                ?: return "unavailable" to "ROOTFS_PACKAGE_MANAGER_UNAVAILABLE: packaged rootfs, PRoot, BusyBox, or supported package manager is missing"
            val logs = JSONArray()
                .put("Installing rootfs system package: $name")
                .put("preferred_package_manager=${preferredManager.ifBlank { "auto" }}")
            val result = runLocalProcess(command, appContext.filesDir, logs, ScriptCompatibility.INSTALL_TIMEOUT_SECONDS, onLine, taskId)
            val text = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
            return if (result.exitCode == 0) "installed" to text else "failed" to text
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
            val framedLog = log.replace("\r\n", "\n").replace('\r', '\n').split('\n').joinToString("\n") { "data: $it" }
            val payload = "event: log\n$framedLog\n\nevent: done\ndata: $status\n\n"
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
        val statuses = JSONArray()
        for (index in 0 until ids.length()) {
            val id = ids.optLong(index)
            val record = readableDatabase.query("dependencies", arrayOf("name", "type"), "id = ?", arrayOf(id.toString()), null, null, null).use { cursor ->
                if (cursor.moveToFirst()) cursor.string("name") to cursor.string("type") else null
            }
            if (record == null) continue
            val result = uninstallDependencyForFallback(record.second, record.first)
            if (result.first) writableDatabase.delete("dependencies", "id = ?", arrayOf(id.toString()))
            else updateDependencyRecord(id, "failed", result.second, "")
            statuses.put(JSONObject().put("id", id).put("status", if (result.first) "removed" else "failed").put("log", result.second))
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("statuses", statuses)))
    }

    private fun deleteDependency(id: Long): NanoHTTPD.Response {
        val record = readableDatabase.query("dependencies", arrayOf("name", "type"), "id=?", arrayOf(id.toString()), null, null, null).use { cursor ->
            if (cursor.moveToFirst()) cursor.string("name") to cursor.string("type") else null
        } ?: return error(NanoHTTPD.Response.Status.NOT_FOUND, "依赖不存在")
        val result = uninstallDependencyForFallback(record.second, record.first)
        if (!result.first) {
            updateDependencyRecord(id, "failed", result.second, "")
            return error(NanoHTTPD.Response.Status.INTERNAL_ERROR, result.second.ifBlank { "物理卸载失败" })
        }
        writableDatabase.delete("dependencies", "id=?", arrayOf(id.toString()))
        return ok(JSONObject().put("message", "卸载成功").put("data", JSONObject().put("id", id).put("status", "removed")))
    }

    private fun uninstallDependencyForFallback(depType: String, name: String): Pair<Boolean, String> {
        val normalized = DependencyStorage.normalizedName(depType, name)
        val runtimeVersion = if (depType == "python") DependencyStorage.PYTHON_VERSION else DependencyStorage.NODE_VERSION
        return DependencyStorage.withInstallLock(depType, name, runtimeVersion) {
            val runtime = if (depType == "python") AndroidPythonRuntime.ensureReady(appContext) else null
            val command = when (depType) {
                "python" -> {
                    if (runtime == null && AndroidLinuxRuntime.guestRuntimeAvailable(appContext, "/usr/bin/pip3")) {
                        AndroidLinuxRuntime.guestCommand(appContext, appContext.filesDir, listOf("/usr/bin/pip3", "uninstall", "-y", normalized))
                            ?: return@withInstallLock false to "Rootfs Python runtime is not ready"
                    } else {
                    runtime ?: return@withInstallLock false to "Python runtime is not ready"
                    val script = File(appContext.filesDir, "runtimes/python-3.14/prefix/bin/pip_uninstall.py")
                    script.parentFile?.mkdirs()
                    script.writeText(
                        "import importlib.metadata,os,shutil,sys\n" +
                            "root=os.path.realpath(os.environ['PIP_TARGET'])\n" +
                            "dist=importlib.metadata.distribution(sys.argv[1])\n" +
                            "for item in (dist.files or []):\n" +
                            " p=os.path.realpath(os.path.join(root,str(item)))\n" +
                            " if p.startswith(root+os.sep):\n" +
                            "  shutil.rmtree(p,ignore_errors=True) if os.path.isdir(p) else os.path.exists(p) and os.remove(p)\n" +
                            "shutil.rmtree(os.path.realpath(str(dist._path)),ignore_errors=True)\n",
                    )
                    listOf(runtime.executable, runtime.wrapperScript, script.absolutePath, normalized)
                    }
                }
                "nodejs" -> {
                    val node = AndroidNodeRuntime.ensureReady(appContext)
                    if (node == null && AndroidLinuxRuntime.guestRuntimeAvailable(appContext, "/usr/bin/npm")) {
                        AndroidLinuxRuntime.guestCommand(appContext, appContext.filesDir, listOf("/usr/bin/npm", "uninstall", "--ignore-scripts", "--prefix", "/host-files/deps/nodejs", normalized))
                            ?: return@withInstallLock false to "Rootfs Node runtime is not ready"
                    } else {
                    node ?: return@withInstallLock false to "Node runtime is not ready"
                    val deps = AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
                    val npm = File(node.modules, "npm/bin/npm-cli.js")
                    listOf(node.executable, AndroidNodeRuntime.wrapperPath, npm.absolutePath, "uninstall", "--ignore-scripts", "--cache", DependencyStorage.npmCache(appContext.filesDir).absolutePath, "--prefix", deps.absolutePath, normalized)
                    }
                }
                else -> return@withInstallLock false to "Physical uninstall is unavailable for $depType"
            }
            val workingDir = if (depType == "python") DependencyStorage.pythonSitePackages(appContext.filesDir) else AndroidNodeRuntime.depsDir(appContext).also(DependencyStorage::ensureNodePackageManifest)
            val result = runLocalProcess(command, workingDir.apply { mkdirs() }, JSONArray(), ScriptCompatibility.INSTALL_TIMEOUT_SECONDS)
            val log = (0 until result.logs.length()).joinToString("\n") { result.logs.optString(it) }
            (result.exitCode == 0) to log
        }
    }

    private fun reinstallDependency(id: Long): NanoHTTPD.Response {
        val record = readableDatabase.query("dependencies", arrayOf("name", "type"), "id = ?", arrayOf(id.toString()), null, null, null).use { cursor ->
            if (!cursor.moveToFirst()) return error(NanoHTTPD.Response.Status.NOT_FOUND, "依赖不存在")
            cursor.string("name") to cursor.string("type")
        }
        val depType = normalizeDependencyType(record.second)
        if (depType == null) {
            val message = "UNSUPPORTED_DEPENDENCY_TYPE: ${record.second} cannot be installed on Android fallback"
            updateDependencyRecord(id, "failed", message, "")
            return error(NanoHTTPD.Response.Status.BAD_REQUEST, message)
        }
        val result = installDependencyForFallback(depType, record.first)
        val status = if (result.first == "installed") "installed" else "failed"
        val version = when (depType) {
            "python" -> queryPipInstalledPackages()[DependencyStorage.normalizedName("python", record.first)].orEmpty()
            else -> queryNpmInstalledPackages()[DependencyStorage.normalizedName("nodejs", record.first)].orEmpty()
        }
        updateDependencyRecord(id, status, result.second, version)
        return ok(JSONObject().put("data", JSONObject().put("id", id).put("status", status).put("version", version)))
    }

    private fun updateDependencyRecord(id: Long, status: String, log: String, version: String) {
        writableDatabase.update("dependencies", ContentValues().apply {
            put("status", status); put("log", log); put("version", version); put("updated_at", Instant.now().toString())
        }, "id = ?", arrayOf(id.toString()))
    }

    private fun reinstallDependencies(json: JSONObject): NanoHTTPD.Response {
        val ids = json.optJSONArray("ids") ?: JSONArray()
        val statuses = JSONArray()
        for (index in 0 until ids.length()) {
            val id = ids.optLong(index)
            reinstallDependency(id)
            val status = readableDatabase.query("dependencies", arrayOf("status"), "id = ?", arrayOf(id.toString()), null, null, null).use { cursor ->
                if (cursor.moveToFirst()) cursor.string("status") else "failed"
            }
            statuses.put(JSONObject().put("id", id).put("status", status))
        }
        return ok(JSONObject().put("data", JSONObject().put("ids", ids).put("statuses", statuses)))
    }

    private fun pythonRuntimes(): NanoHTTPD.Response = ok(
        JSONObject()
            .put("default_version", "3.14")
            .put(
                "data",
                JSONArray().put(
                    JSONObject()
                        .put("version", "3.14")
                        .put("label", "Python 3.14")
                        .put("default", true)
                        .put("available", true)
                        .put("venv_healthy", true)
                        .put("message", "Android 本地兼容运行时可用")
                )
            )
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

    private fun login(session: NanoHTTPD.IHTTPSession, json: JSONObject): NanoHTTPD.Response {
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
        val now = Instant.now().toString()
        writableDatabase.insert("security_login_logs", null, ContentValues().apply { put("username", username); put("ip", requestIp(session)); put("status", if (valid) 0 else 1); put("message", if (valid) "登录成功" else "用户名或密码错误"); put("client_name", clientName(session)); put("user_agent", session.headers["user-agent"].orEmpty()); put("created_at", now) })
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
        writableDatabase.insert("security_sessions", null, ContentValues().apply { put("username", username); put("access_token", accessToken); put("ip", requestIp(session)); put("client_name", clientName(session)); put("user_agent", session.headers["user-agent"].orEmpty()); put("created_at", now); put("expires_at", Instant.now().plusSeconds(30L * 24 * 60 * 60).toString()) })
        writableDatabase.insert("security_audit_logs", null, ContentValues().apply { put("username", username); put("ip", requestIp(session)); put("action", "auth.login"); put("detail", "登录成功"); put("created_at", now) })
        return ok(JSONObject().put("message", "登录成功").put("access_token", accessToken).put("refresh_token", refreshToken).put("user", userJson()))
    }

    private fun revokeAccessToken(token: String?, action: String) {
        if (token == null) return
        writableDatabase.delete("security_sessions", "access_token=?", arrayOf(token))
        writableDatabase.delete("local_sessions", "access_token=?", arrayOf(token))
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
        val oldAccessToken = readableDatabase.rawQuery("SELECT access_token FROM local_sessions WHERE id=1", null).use { if (it.moveToFirst()) it.getString(0) else "" }
        writableDatabase.update("local_sessions", values, "id = 1", null)
        writableDatabase.update("security_sessions", ContentValues().apply { put("access_token", accessToken); put("expires_at", Instant.now().plusSeconds(30L * 24 * 60 * 60).toString()) }, "access_token=?", arrayOf(oldAccessToken))
        return ok(JSONObject().put("access_token", accessToken))
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
    override fun close() {
        terminalSessions.close()
        scriptProcesses.values.forEach(::terminateTaskProcess)
        taskProcesses.values.forEach(::terminateTaskProcess)
        scriptProcesses.clear()
        taskProcesses.clear()
        scriptRunExecutor.shutdownNow()
        taskRunExecutor.shutdownNow()
        runningTaskIds.clear()
        taskAbortRequested.clear()
        taskRunLogIds.clear()
        taskRunCursors.clear()
        taskRunLocks.clear()
        taskFinalizedIds.clear()
        taskRunLogsMemory.clear()
        taskRunLogCharacters.clear()
        taskRunPendingPersistence.clear()
        taskRunStartedAt.clear()
        scriptRunLogsMemory.clear()
        scriptRunLogCharacters.clear()
        scriptRunPendingPersistence.clear()
        scriptRunLocks.clear()
        super.close()
    }

}
