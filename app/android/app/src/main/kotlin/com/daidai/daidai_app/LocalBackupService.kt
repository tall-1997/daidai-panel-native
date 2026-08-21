package com.daidai.daidai_app

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.util.Base64
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.gzip.GzipCompressorInputStream
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.security.MessageDigest
import javax.crypto.AEADBadTagException
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec
import java.time.Instant
import java.time.format.DateTimeFormatter

/** Local JSON creator plus modern Go JSON/tgz/AES-GCM backup importer. */
internal class LocalBackupService(
    private val context: Context,
    private val database: () -> SQLiteDatabase,
    private val schemaVersion: Int,
) {
    private val backupDir get() = File(context.filesDir, "backups").apply { mkdirs() }
    private val scriptsDir get() = File(context.filesDir, "scripts").apply { mkdirs() }

    data class StoredFile(val file: File)

    fun list(): JSONArray = JSONArray().also { result ->
        backupDir.listFiles().orEmpty()
            .filter { it.isFile && supportedName(it.name) }
            .sortedByDescending(File::lastModified)
            .forEach { file ->
                result.put(JSONObject()
                    .put("filename", file.name)
                    .put("size", file.length())
                    .put("created_at", Instant.ofEpochMilli(file.lastModified()).toString())
                    .put("encrypted", file.name.endsWith(".enc", true)))
            }
    }

    fun create(request: JSONObject): JSONObject {
        rejectPassword(request)
        val selection = normalizedSelection(request.optJSONObject("selection"))
        require(selection.keys().asSequence().any { selection.optBoolean(it) }) { "请至少选择一个备份项" }
        val data = JSONObject()
        val db = database()
        val tasks = if (selection.optBoolean("tasks")) rows(db, "tasks") else JSONArray()
        val envs = if (selection.optBoolean("env_vars")) rows(db, "envs") else JSONArray()
        val subscriptions = if (selection.optBoolean("subscriptions")) rows(db, "local_subscriptions") else JSONArray()
        val dependencies = if (selection.optBoolean("dependencies")) rows(db, "dependencies") else JSONArray()
        val systemConfigs = if (selection.optBoolean("configs")) rows(db, "local_configs") else JSONArray()
        val notifications = if (selection.optBoolean("notifications")) rows(db, "notification_channels") else JSONArray()
        val scripts = if (selection.optBoolean("scripts")) scriptSnapshot() else JSONArray()

        data.put("tasks", tasks)
            .put("env_vars", envs)
            .put("subscriptions", subscriptions)
            .put("dependencies", dependencies)
            .put("system_configs", systemConfigs)
            .put("notify_channels", notifications)
            .put("scripts", scripts)
            .put("configs", JSONObject().put("system_configs", systemConfigs).put("notify_channels", notifications))

        val manifest = JSONObject()
            .put("format", "daidai-panel-backup")
            .put("version", "0.4.0-kotlin-json")
            .put("source", "android-kotlin-fallback")
            .put("schema_version", schemaVersion)
            .put("created_at", Instant.now().toString())
            .put("selection", selection)
            .put("data", data)
            // These aliases let legacy JSON readers consume the fallback backup directly.
            .put("tasks", tasks).put("env_vars", envs).put("subscriptions", subscriptions)
            .put("dependencies", dependencies).put("system_configs", systemConfigs)
            .put("notify_channels", notifications).put("scripts", scripts)
        validate(manifest)
        val filename = "daidai-android-${DateTimeFormatter.ISO_INSTANT.format(Instant.now()).replace(':', '-')}.json"
        val output = safeFile(filename, mustExist = false)!!
        val temporary = File(backupDir, ".${output.name}.${System.nanoTime()}.tmp")
        try {
            temporary.outputStream().buffered().use { it.write(manifest.toString(2).toByteArray(Charsets.UTF_8)) }
            if (!temporary.renameTo(output)) throw IllegalStateException("无法保存备份文件")
        } finally {
            temporary.delete()
        }
        return JSONObject().put("filename", output.name).put("size", output.length()).put("encrypted", false).put("selection", selection)
    }

    fun saveUpload(uploaded: File, requestedName: String?): JSONObject {
        require(uploaded.isFile) { "缺少备份文件" }
        require(uploaded.length() in 1..MAX_BACKUP_BYTES.toLong()) { "备份文件为空或过大" }
        val bytes = uploaded.readBytes()
        val rawName = requestedName?.takeIf { it.isNotBlank() } ?: uploaded.name
        val clean = rawName.substringAfterLast('/').substringAfterLast('\\')
        require(supportedName(clean)) { "仅支持 .json、.tgz、.tar.gz 或 .enc 备份" }
        // Encrypted uploads cannot be authenticated until restore supplies a password. Plain
        // uploads are parsed completely here, so malformed archives are never persisted.
        if (clean.endsWith(".enc", true)) require(bytes.size >= 12 + 16) { "加密备份格式无效" }
        else prepareBytes(bytes, clean, "")
        val output = safeFile(clean, mustExist = false) ?: throw IllegalArgumentException("非法备份文件名")
        val temporary = File(backupDir, ".${output.name}.${System.nanoTime()}.tmp")
        try {
            temporary.writeBytes(bytes)
            if (!temporary.renameTo(output)) throw IllegalStateException("无法保存上传备份")
        } finally { temporary.delete() }
        return JSONObject().put("filename", output.name).put("size", output.length()).put("encrypted", output.name.endsWith(".enc", true))
    }

    fun resolve(filename: String): File? = safeFile(filename, mustExist = true)

    fun delete(filename: String): JSONObject {
        val file = resolve(filename) ?: throw NoSuchElementException("备份文件不存在")
        if (!file.delete()) throw IllegalStateException("删除备份失败")
        return JSONObject().put("filename", file.name).put("deleted", true)
    }

    fun restore(request: JSONObject): JSONObject {
        val filename = request.optString("filename")
        val file = resolve(filename) ?: throw NoSuchElementException("备份文件不存在")
        require(file.length() in 1..MAX_BACKUP_BYTES.toLong()) { "备份文件为空或过大" }
        // Authentication, decompression and complete validation happen before staging, DB
        // transactions, or live script changes, preserving data on wrong password/format.
        val prepared = prepareBytes(file.readBytes(), file.name, request.optString("password"))
        val requested = request.optJSONObject("selection")
        val selected = if (requested == null) prepared.selection else intersectSelection(prepared.selection, normalizedSelection(requested))
        require(selected.keys().asSequence().any { selected.optBoolean(it) }) { "没有可恢复的选中项" }

        // Decode and write every script into an isolated tree before touching live state.
        val staging = File(context.filesDir, ".scripts-restore-${System.nanoTime()}")
        val rollback = File(context.filesDir, ".scripts-rollback-${System.nanoTime()}")
        if (selected.optBoolean("scripts")) {
            staging.mkdirs()
            prepared.scripts.forEach { script ->
                val target = File(staging, script.first).canonicalFile
                require(target.path.startsWith(staging.canonicalPath + File.separator)) { "脚本路径越界" }
                target.parentFile?.mkdirs()
                target.writeBytes(script.second)
            }
        }

        val db = database()
        var scriptsActivated = false
        db.beginTransaction()
        try {
            if (selected.optBoolean("tasks")) replace(db, "tasks", prepared.tasks, taskColumns, mapOf("name" to "", "command" to ""))
            if (selected.optBoolean("env_vars")) replace(db, "envs", prepared.envs, envColumns, mapOf("name" to "", "value" to ""))
            if (selected.optBoolean("configs")) replace(db, "local_configs", prepared.configs, configColumns, mapOf("key" to "", "value" to ""))
            if (selected.optBoolean("subscriptions")) replace(db, "local_subscriptions", prepared.subscriptions, subscriptionColumns, mapOf("url" to ""))
            if (selected.optBoolean("notifications")) replace(db, "notification_channels", prepared.notifications, notificationColumns, mapOf("name" to "", "type" to ""))
            if (selected.optBoolean("dependencies")) replace(db, "dependencies", prepared.dependencies, dependencyColumns, mapOf("name" to "", "type" to ""))
            if (selected.optBoolean("scripts")) {
                if (scriptsDir.exists() && !scriptsDir.renameTo(rollback)) throw IllegalStateException("无法暂存当前脚本")
                if (!staging.renameTo(scriptsDir)) {
                    rollback.renameTo(scriptsDir)
                    throw IllegalStateException("无法启用恢复脚本")
                }
                scriptsActivated = true
            }
            db.setTransactionSuccessful()
        } catch (error: Exception) {
            if (scriptsActivated) {
                scriptsDir.deleteRecursively()
                rollback.renameTo(scriptsDir)
            }
            throw error
        } finally {
            try {
                db.endTransaction()
            } catch (error: Exception) {
                if (scriptsActivated) {
                    scriptsDir.deleteRecursively()
                    rollback.renameTo(scriptsDir)
                }
                throw error
            } finally {
                staging.deleteRecursively()
            }
        }
        rollback.deleteRecursively()
        val counts = JSONObject()
            .put("tasks", if (selected.optBoolean("tasks")) prepared.tasks.length() else 0)
            .put("env_vars", if (selected.optBoolean("env_vars")) prepared.envs.length() else 0)
            .put("configs", if (selected.optBoolean("configs")) prepared.configs.length() else 0)
            .put("subscriptions", if (selected.optBoolean("subscriptions")) prepared.subscriptions.length() else 0)
            .put("notifications", if (selected.optBoolean("notifications")) prepared.notifications.length() else 0)
            .put("dependencies", if (selected.optBoolean("dependencies")) prepared.dependencies.length() else 0)
            .put("scripts", if (selected.optBoolean("scripts")) prepared.scripts.size else 0)
        return JSONObject().put("status", "completed").put("stage", "atomic_restore").put("filename", filename).put("selection", selected).put("counts", counts)
    }

    private data class Prepared(
        val selection: JSONObject,
        val tasks: JSONArray,
        val envs: JSONArray,
        val configs: JSONArray,
        val subscriptions: JSONArray,
        val notifications: JSONArray,
        val dependencies: JSONArray,
        val scripts: List<Pair<String, ByteArray>>,
    )

    private fun prepareBytes(stored: ByteArray, filename: String, password: String): Prepared {
        require(stored.size <= MAX_BACKUP_BYTES) { "备份文件过大" }
        val plain = if (filename.endsWith(".enc", true)) decryptGoEnvelope(stored, password) else stored
        return if (looksLikeGzip(plain) || filename.endsWith(".tgz", true) || filename.endsWith(".tar.gz", true)) {
            prepareTarGzip(plain)
        } else {
            try { validate(JSONObject(plain.toString(Charsets.UTF_8))) }
            catch (e: Exception) { throw IllegalArgumentException("备份 JSON 格式无效", e) }
        }
    }

    private fun decryptGoEnvelope(input: ByteArray, password: String): ByteArray {
        require(password.isNotBlank()) { "恢复加密备份需要密码" }
        require(input.size >= 12 + 16) { "加密备份格式无效" }
        require(input.size <= MAX_BACKUP_BYTES) { "备份文件过大" }
        return try {
            val key = MessageDigest.getInstance("SHA-256").digest(password.toByteArray(Charsets.UTF_8))
            Cipher.getInstance("AES/GCM/NoPadding").run {
                init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, input, 0, 12))
                doFinal(input, 12, input.size - 12)
            }.also { require(it.size <= MAX_EXPANDED_BYTES) { "解密后的备份过大" } }
        } catch (e: AEADBadTagException) {
            throw IllegalArgumentException("备份密码错误或文件已损坏", e)
        } catch (e: IllegalArgumentException) { throw e }
        catch (e: Exception) { throw IllegalArgumentException("加密备份格式无效", e) }
    }

    private fun prepareTarGzip(input: ByteArray): Prepared {
        var manifestBytes: ByteArray? = null
        val scripts = mutableListOf<Pair<String, ByteArray>>()
        val seen = hashSetOf<String>()
        var entries = 0
        var total = 0L
        try {
            TarArchiveInputStream(GzipCompressorInputStream(ByteArrayInputStream(input), false)).use { tar ->
                while (true) {
                    val entry = tar.nextEntry ?: break
                    require(++entries <= MAX_ENTRIES) { "归档 entry 数量过多" }
                    val name = normalizedArchivePath(entry.name)
                    require(!entry.isSymbolicLink && !entry.isLink) { "归档不允许符号链接或硬链接" }
                    require(entry.isDirectory || entry.isFile) { "归档包含不支持的 entry" }
                    require(entry.size >= 0 && entry.size <= MAX_ENTRY_BYTES) { "归档 entry 过大" }
                    if (entry.isDirectory) continue
                    total += entry.size
                    require(total <= MAX_EXPANDED_BYTES) { "归档解压后过大" }
                    val bytes = readEntry(tar, entry.size.toInt())
                    when {
                        name == "manifest.json" -> {
                            require(manifestBytes == null) { "归档包含重复 manifest.json" }
                            manifestBytes = bytes
                        }
                        name.startsWith("files/scripts/") -> {
                            val scriptPath = normalizeScriptPath(name.removePrefix("files/scripts/"))
                            require(seen.add(scriptPath)) { "归档包含重复脚本路径" }
                            scripts += scriptPath to bytes
                        }
                    }
                }
            }
            val rawManifest = manifestBytes ?: throw IllegalArgumentException("归档缺少 manifest.json")
            require(rawManifest.size <= MAX_MANIFEST_BYTES) { "manifest.json 过大" }
            val base = validate(JSONObject(rawManifest.toString(Charsets.UTF_8)))
            val combined = if (scripts.isEmpty()) base.scripts else {
                require(base.scripts.isEmpty()) { "脚本同时存在于 manifest 和归档中" }
                scripts
            }
            require(combined.size <= MAX_SCRIPT_ENTRIES) { "脚本数量过多" }
            return base.copy(scripts = combined)
        } catch (e: IllegalArgumentException) { throw e }
        catch (e: Exception) { throw IllegalArgumentException("tgz 备份格式无效", e) }
    }

    private fun readEntry(tar: TarArchiveInputStream, expected: Int): ByteArray {
        val output = ByteArrayOutputStream(expected.coerceAtMost(64 * 1024))
        val buffer = ByteArray(16 * 1024)
        var remaining = expected
        while (remaining > 0) {
            val count = tar.read(buffer, 0, minOf(buffer.size, remaining))
            if (count < 0) throw IllegalArgumentException("归档 entry 被截断")
            output.write(buffer, 0, count)
            remaining -= count
        }
        return output.toByteArray()
    }

    private fun normalizedArchivePath(raw: String): String {
        require(raw.isNotBlank() && !raw.startsWith('/') && !raw.startsWith('\\')) { "非法归档路径" }
        require(!Regex("^[A-Za-z]:").containsMatchIn(raw)) { "非法归档路径" }
        val parts = raw.replace('\\', '/').trimEnd('/').split('/')
        require(parts.isNotEmpty() && parts.none { it.isBlank() || it == "." || it == ".." }) { "归档路径越界" }
        return parts.joinToString("/")
    }

    private fun looksLikeGzip(bytes: ByteArray) = bytes.size >= 2 && bytes[0] == 0x1f.toByte() && bytes[1] == 0x8b.toByte()

    /** Parse and completely validate before restore starts or an upload is accepted. */
    private fun validate(manifest: JSONObject): Prepared {
        require(manifest.optString("format") == "daidai-panel-backup") { "不支持的备份格式" }
        require(manifest.optString("version").isNotBlank()) { "备份缺少 version" }
        val data = manifest.optJSONObject("data") ?: JSONObject()
        val configBundle = data.optJSONObject("configs")
        fun array(name: String, nested: JSONArray? = null): JSONArray = data.optJSONArray(name) ?: nested ?: manifest.optJSONArray(name) ?: JSONArray()
        val tasks = array("tasks")
        val envs = array("env_vars")
        val configs = array("system_configs", configBundle?.optJSONArray("system_configs"))
        val subscriptions = array("subscriptions")
        val notifications = array("notify_channels", configBundle?.optJSONArray("notify_channels"))
        val dependencies = array("dependencies")
        val scriptArray = array("scripts")
        requireObjects(tasks, "tasks", "name")
        requireObjects(envs, "env_vars", "name")
        requireObjects(configs, "system_configs", "key")
        requireObjects(subscriptions, "subscriptions", "url")
        requireObjects(notifications, "notify_channels", "name", "type")
        requireObjects(dependencies, "dependencies", "name", "type")
        val scripts = mutableListOf<Pair<String, ByteArray>>()
        val seen = hashSetOf<String>()
        for (i in 0 until scriptArray.length()) {
            val item = scriptArray.optJSONObject(i) ?: throw IllegalArgumentException("scripts[$i] 不是对象")
            val path = normalizeScriptPath(item.optString("path"))
            require(path.isNotBlank() && seen.add(path)) { "脚本路径为空或重复" }
            val encoded = item.optString("content_base64", item.optString("content"))
            val bytes = try { Base64.decode(encoded, Base64.DEFAULT) } catch (_: Exception) { throw IllegalArgumentException("脚本 $path 的 base64 无效") }
            require(bytes.size <= MAX_ENTRY_BYTES) { "脚本 $path 过大" }
            scripts += path to bytes
            require(scripts.size <= MAX_SCRIPT_ENTRIES && scripts.sumOf { it.second.size.toLong() } <= MAX_EXPANDED_BYTES) { "脚本内容过多" }
        }
        val selection = selectionFromManifest(manifest, tasks, envs, configs, subscriptions, notifications, dependencies, scriptArray)
        return Prepared(selection, tasks, envs, configs, subscriptions, notifications, dependencies, scripts)
    }

    private fun selectionFromManifest(manifest: JSONObject, vararg arrays: JSONArray): JSONObject {
        val supplied = manifest.optJSONObject("selection")
        if (supplied != null) return normalizedSelection(supplied)
        val keys = listOf("tasks", "env_vars", "configs", "subscriptions", "notifications", "dependencies", "scripts")
        return JSONObject().also { out -> keys.forEachIndexed { index, key -> out.put(key, arrays[index].length() > 0) } }
    }

    private fun normalizedSelection(input: JSONObject?): JSONObject {
        val explicit = input != null && input.keys().hasNext()
        fun flag(vararg names: String): Boolean = names.any { input?.optBoolean(it, false) == true }
        return JSONObject()
            .put("tasks", if (explicit) flag("tasks") else true)
            .put("env_vars", if (explicit) flag("env_vars", "envs") else true)
            .put("configs", if (explicit) flag("configs", "system_configs") else true)
            .put("subscriptions", if (explicit) flag("subscriptions") else true)
            .put("notifications", if (explicit) flag("notifications", "notify_channels") || flag("configs") else true)
            .put("dependencies", if (explicit) flag("dependencies") else true)
            .put("scripts", if (explicit) flag("scripts") else true)
    }

    private fun intersectSelection(available: JSONObject, requested: JSONObject): JSONObject = JSONObject().also { out ->
        available.keys().forEach { key -> out.put(key, available.optBoolean(key) && requested.optBoolean(key)) }
    }

    private fun rejectPassword(json: JSONObject) {
        require(json.optString("password").isBlank()) { "Kotlin fallback 暂不支持加密备份，请将 password 留空" }
    }

    private fun requireObjects(array: JSONArray, label: String, vararg required: String) {
        for (i in 0 until array.length()) {
            val row = array.optJSONObject(i) ?: throw IllegalArgumentException("$label[$i] 不是对象")
            required.forEach { key -> require(row.has(key) && row.optString(key).isNotBlank()) { "$label[$i] 缺少 $key" } }
        }
    }

    private fun rows(db: SQLiteDatabase, table: String): JSONArray = JSONArray().also { output ->
        db.query(table, null, null, null, null, null, "rowid ASC").use { cursor ->
            while (cursor.moveToNext()) output.put(cursorJson(cursor))
        }
    }

    private fun cursorJson(cursor: Cursor): JSONObject = JSONObject().also { json ->
        for (i in 0 until cursor.columnCount) {
            when (cursor.getType(i)) {
                Cursor.FIELD_TYPE_NULL -> json.put(cursor.getColumnName(i), JSONObject.NULL)
                Cursor.FIELD_TYPE_INTEGER -> json.put(cursor.getColumnName(i), cursor.getLong(i))
                Cursor.FIELD_TYPE_FLOAT -> json.put(cursor.getColumnName(i), cursor.getDouble(i))
                Cursor.FIELD_TYPE_BLOB -> json.put(cursor.getColumnName(i), Base64.encodeToString(cursor.getBlob(i), Base64.NO_WRAP))
                else -> json.put(cursor.getColumnName(i), cursor.getString(i))
            }
        }
    }

    private fun scriptSnapshot(): JSONArray = JSONArray().also { output ->
        scriptsDir.walkTopDown().filter(File::isFile).sortedBy { it.relativeTo(scriptsDir).path }.forEach { file ->
            output.put(JSONObject()
                .put("path", file.relativeTo(scriptsDir).path.replace(File.separatorChar, '/'))
                .put("content_base64", Base64.encodeToString(file.readBytes(), Base64.NO_WRAP)))
        }
    }

    private fun replace(db: SQLiteDatabase, table: String, source: JSONArray, columns: Set<String>, defaults: Map<String, String>) {
        db.delete(table, null, null)
        for (i in 0 until source.length()) {
            val row = source.getJSONObject(i)
            val values = ContentValues()
            defaults.forEach { (key, value) -> values.put(key, value) }
            // Translate Go legacy names into Kotlin fallback columns without losing grouping/order.
            if (table == "envs") {
                if (!row.has("groups_json") && row.has("group")) {
                    val groups = row.optString("group").split(',').map(String::trim).filter(String::isNotBlank)
                    values.put("groups_json", JSONArray(groups).toString())
                }
                if (!row.has("sort_order") && row.has("position")) values.put("sort_order", row.optInt("position"))
            }
            columns.forEach { column ->
                if (!row.has(column) || row.isNull(column)) return@forEach
                when (val value = row.get(column)) {
                    is Boolean -> values.put(column, if (value) 1 else 0)
                    is Int -> values.put(column, value)
                    is Long -> values.put(column, value)
                    is Double -> values.put(column, value)
                    is Number -> values.put(column, value.toDouble())
                    is JSONObject, is JSONArray -> values.put(column, value.toString())
                    else -> values.put(column, value.toString())
                }
            }
            val now = Instant.now().toString()
            if ("created_at" in columns && !values.containsKey("created_at")) values.put("created_at", now)
            if ("updated_at" in columns && !values.containsKey("updated_at")) values.put("updated_at", now)
            if (db.insertOrThrow(table, null, values) < 0) throw IllegalStateException("恢复 $table 失败")
        }
    }

    private fun normalizeScriptPath(raw: String): String {
        val path = raw.replace('\\', '/').trim('/')
        require(path.isNotBlank() && !path.split('/').any { it.isBlank() || it == "." || it == ".." }) { "非法脚本路径" }
        return path
    }

    private fun safeFile(name: String, mustExist: Boolean): File? {
        val clean = name.substringAfterLast('/').substringAfterLast('\\')
        if (clean.isBlank() || !supportedName(clean)) return null
        val file = File(backupDir, clean).canonicalFile
        if (file.parentFile != backupDir.canonicalFile) return null
        return file.takeIf { !mustExist || it.isFile }
    }

    companion object {
        private const val MAX_BACKUP_BYTES = 64 * 1024 * 1024
        private const val MAX_EXPANDED_BYTES = 128 * 1024 * 1024
        private const val MAX_ENTRY_BYTES = 32 * 1024 * 1024
        private const val MAX_MANIFEST_BYTES = 8 * 1024 * 1024
        private const val MAX_ENTRIES = 4096
        private const val MAX_SCRIPT_ENTRIES = 2048
        private fun supportedName(name: String): Boolean {
            val lower = name.lowercase()
            return lower.endsWith(".json") || lower.endsWith(".tgz") || lower.endsWith(".tar.gz") || lower.endsWith(".enc")
        }

        private val taskColumns = setOf("id", "name", "command", "cron_expression", "task_type", "python_version", "task_before", "task_after", "notify_on_failure", "notify_on_success", "notify_on_abort", "notification_channel_id", "status", "labels", "last_run_status", "last_run_logs", "last_log_id", "created_at", "updated_at")
        private val envColumns = setOf("id", "name", "value", "remarks", "enabled", "groups_json", "sort_order", "created_at", "updated_at")
        private val configColumns = setOf("key", "value", "updated_at")
        private val subscriptionColumns = setOf("id", "name", "url", "enabled", "type", "last_sync", "created_at", "updated_at")
        private val notificationColumns = setOf("id", "name", "type", "config", "push_scope", "enabled", "today_send_count", "today_send_date", "last_test_at", "last_test_status", "created_at", "updated_at")
        private val dependencyColumns = setOf("id", "name", "type", "python_version", "version", "status", "log", "created_at", "updated_at")
    }
}
