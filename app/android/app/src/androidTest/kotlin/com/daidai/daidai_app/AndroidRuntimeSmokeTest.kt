package com.daidai.daidai_app

import android.content.Context
import android.content.Intent
import android.database.sqlite.SQLiteDatabase
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.zip.GZIPOutputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AndroidRuntimeSmokeTest {
    private lateinit var context: Context
    private lateinit var evidenceFile: File
    private val steps = JSONArray()
    private var core = JSONObject()
    private var token = ""
    private var accessToken = ""

    @Test
    fun methodChannelCoreLifecyclePersistsRuntimeState() {
        context = ApplicationProvider.getApplicationContext()
        evidenceFile = File(context.filesDir, "runtime-smoke/instrumentation.json").apply {
            parentFile?.mkdirs()
        }
        var failure: Throwable? = null
        try {
            step("core.method_channel.ensure_started") {
                val serviceIntent = Intent(context, LocalPanelHostService::class.java)
                context.startService(serviceIntent)
                invokeLocalHost("ensure-started")
                core = waitForMethodChannelCore()
                assertCoreReady(core)
                token = core.getString("local_token")
                JSONObject().put("transport", "com.daidai.panel/local_host").put("core", sanitizedCore(core))
            }
            step("auth.initialize_and_login") {
                val credentials = JSONObject().put("username", "runtime_smoke_admin").put("password", "runtime-smoke-password")
                val init = request("GET", "/api/v1/auth/check-init")
                val initializedNow = init.body.optBoolean("need_init")
                if (initializedNow) request("POST", "/api/v1/auth/init", credentials, expected = setOf(200))
                val login = request("POST", "/api/v1/auth/login", credentials, expected = setOf(200))
                accessToken = login.body.getString("access_token")
                assertEquals("runtime_smoke_admin", login.body.getJSONObject("user").getString("username"))
                JSONObject().put("initialized_now", initializedNow).put("admin_authenticated", true)
                    .put("username", "runtime_smoke_admin").put("login_status", login.status)
            }

            step("rootfs.exec_env") {
                val command = requireNotNull(AndroidLinuxRuntime.guestCommand(
                    context,
                    context.filesDir,
                    listOf("/usr/bin/env", "/bin/sh", "-lc", "printf ROOTFS_ENV_OK"),
                )) { "Packaged rootfs runner is unavailable" }
                val builder = ProcessBuilder(command).directory(context.filesDir).redirectErrorStream(true)
                builder.environment().putAll(AndroidLinuxRuntime.baseEnvironment(context, context.filesDir))
                AndroidLinuxRuntime.applyGuestEnvironment(command, builder.environment())
                val process = builder.start()
                process.outputStream.close()
                val finished = process.waitFor(60, TimeUnit.SECONDS)
                if (!finished) process.destroyForcibly()
                check(finished) { "Rootfs /usr/bin/env timed out" }
                val output = process.inputStream.bufferedReader().use { it.readText() }
                assertEquals(0, process.exitValue())
                assertTrue(output.contains("ROOTFS_ENV_OK"))
                JSONObject().put("command", "/usr/bin/env").put("output", "ROOTFS_ENV_OK")
            }

            step("rootfs.python_crypto") {
                val command = requireNotNull(AndroidLinuxRuntime.guestCommand(
                    context,
                    context.filesDir,
                    listOf(
                        "/usr/bin/python3",
                        "-c",
                        "from Crypto.Cipher import AES, PKCS1_v1_5; print('PYCRYPTODOME_OK')",
                    ),
                )) { "Packaged rootfs Python is unavailable" }
                val builder = ProcessBuilder(command).directory(context.filesDir).redirectErrorStream(true)
                builder.environment().putAll(AndroidLinuxRuntime.baseEnvironment(context, context.filesDir))
                AndroidLinuxRuntime.applyGuestEnvironment(command, builder.environment())
                val process = builder.start()
                process.outputStream.close()
                val finished = process.waitFor(60, TimeUnit.SECONDS)
                if (!finished) process.destroyForcibly()
                check(finished) { "Rootfs PyCryptodome import timed out" }
                val output = process.inputStream.bufferedReader().use { it.readText() }
                assertEquals(0, process.exitValue())
                assertTrue(output.contains("PYCRYPTODOME_OK"))
                JSONObject().put("imports", "AES,PKCS1_v1_5").put("output", "PYCRYPTODOME_OK")
            }

            step("rootfs.npm_env_node") {
                val command = requireNotNull(AndroidLinuxRuntime.guestCommand(
                    context,
                    context.filesDir,
                    listOf("/usr/bin/npm", "--version"),
                )) { "Packaged rootfs npm is unavailable" }
                val builder = ProcessBuilder(command).directory(context.filesDir).redirectErrorStream(true)
                builder.environment().putAll(AndroidLinuxRuntime.baseEnvironment(context, context.filesDir))
                AndroidLinuxRuntime.applyGuestEnvironment(command, builder.environment())
                val process = builder.start()
                process.outputStream.close()
                val finished = process.waitFor(60, TimeUnit.SECONDS)
                if (!finished) process.destroyForcibly()
                check(finished) { "Rootfs npm env node smoke timed out" }
                val output = process.inputStream.bufferedReader().use { it.readText() }
                assertEquals(0, process.exitValue())
                assertTrue(output.trim().matches(Regex("[0-9]+(?:\\.[0-9]+)+")))
                JSONObject().put("command", "/usr/bin/npm --version").put("version", output.trim())
            }

            val envId = step("env.create") {
                val response = request("POST", "/api/v1/envs", JSONObject()
                    .put("name", "ANDROID_RUNTIME_SMOKE")
                    .put("value", "initial-value")
                    .put("remarks", "instrumentation"), expected = setOf(201))
                val id = response.body.getJSONObject("data").getLong("id")
                JSONObject().put("env_id", id).put("created", true)
            }.getLong("env_id")
            step("env.read_update") {
                val read = request("GET", "/api/v1/envs/$envId").body.getJSONObject("data")
                assertEquals("ANDROID_RUNTIME_SMOKE", read.getString("name"))
                val updated = request("PUT", "/api/v1/envs/$envId", JSONObject()
                    .put("value", "persisted-value").put("remarks", "updated")).body.getJSONObject("data")
                assertEquals("persisted-value", updated.getString("value"))
                assertEquals("updated", updated.getString("remarks"))
                JSONObject().put("env_id", envId).put("updated", true).put("expected_value", "persisted-value")
            }

            val scripts = File(context.filesDir, "local-panel/scripts").apply { mkdirs() }
            val marker = "ENV_VALUE=persisted-value"
            val taskResults = JSONArray()
            listOf(
                Triple("shell", "runtime-smoke.sh", "printf 'ENV_VALUE=%s\\n' \"\$ANDROID_RUNTIME_SMOKE\""),
                Triple("python", "runtime-smoke.py", "import os; print('ENV_VALUE=' + os.environ['ANDROID_RUNTIME_SMOKE'])"),
                Triple("node", "runtime-smoke.js", "console.log('ENV_VALUE=' + process.env.ANDROID_RUNTIME_SMOKE)"),
            ).forEach { (runtime, filename, source) ->
                File(scripts, filename).writeText(source + "\n")
                taskResults.put(runTask(runtime, filename, marker))
            }
            step("tasks.shell_python_node") {
                assertEquals(3, taskResults.length())
                JSONObject().put("tasks", taskResults).put("environment_marker", marker)
            }

            val wheel = writeWheelFixture(File(context.cacheDir, "runtime_smoke_pkg-1.0.0-py3-none-any.whl"))
            val tarball = writeNodeTarballFixture(File(context.cacheDir, "runtime-smoke-node-1.0.0.tgz"))
            installAndRemoveDependency("python", wheel.absolutePath, "3.14")
            installAndRemoveDependency("nodejs", tarball.absolutePath, "")

            step("core.method_channel.restart") {
                val before = core.getString("instance_id")
                core = invokeLocalHost("restart")
                assertCoreReady(core)
                assertTrue("Core restart must create a new instance", before != core.getString("instance_id"))
                token = core.getString("local_token")
                JSONObject().put("transport", "com.daidai.panel/local_host").put("previous_instance_id", before)
                    .put("core", sanitizedCore(core))
            }
            step("persistence.after_restart") {
                val login = request("POST", "/api/v1/auth/login", JSONObject()
                    .put("username", "runtime_smoke_admin").put("password", "runtime-smoke-password"))
                accessToken = login.body.getString("access_token")
                assertEquals("runtime_smoke_admin", login.body.getJSONObject("user").getString("username"))
                val env = request("GET", "/api/v1/envs/$envId").body.getJSONObject("data")
                assertEquals("persisted-value", env.getString("value"))
                request("DELETE", "/api/v1/envs/$envId")
                request("GET", "/api/v1/envs/$envId", expected = setOf(404))
                JSONObject().put("admin_persisted", true).put("env_persisted", true).put("env_deleted", true)
            }
        } catch (error: Throwable) {
            failure = error
            throw error
        } finally {
            evidenceFile.writeText(JSONObject()
                .put("schema_version", 2)
                .put("status", if (failure == null) "pass" else "failed")
                .put("generated_at", Instant.now().toString())
                .put("core", sanitizedCore(core))
                .put("steps", steps)
                .put("failure", failure?.let { throwable(it) } ?: JSONObject.NULL)
                .toString(2))
        }
    }

    private fun waitForMethodChannelCore(): JSONObject {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(120)
        var last = JSONObject()
        while (System.nanoTime() < deadline) {
            last = invokeLocalHost("status")
            if (last.optString("phase") == "ready") return last
            Thread.sleep(500)
        }
        error("Flutter local_host MethodChannel did not start Core: $last")
    }

    private fun invokeLocalHost(method: String): JSONObject {
        val latch = CountDownLatch(1)
        var result: Result<String>? = null
        val client = LocalPanelServiceClient(context)
        try {
            val callback: (Result<String>) -> Unit = { result = it; latch.countDown() }
            when (method) {
                "ensure-started" -> client.ensureStarted(callback)
                "status" -> client.status(callback)
                "restart" -> client.restart(callback)
                else -> error("Unsupported local host method: $method")
            }
            check(latch.await(120, TimeUnit.SECONDS)) { "Timed out invoking local_host/$method" }
            return JSONObject(requireNotNull(result).getOrThrow())
        } finally {
            client.close()
        }
    }

    private fun assertCoreReady(status: JSONObject) {
        assertEquals("ready", status.optString("phase"))
        assertTrue(status.optString("instance_id").isNotBlank())
        assertTrue(status.optString("core_version").isNotBlank())
        assertEquals("kotlin-local-fallback", status.optString("core_version"))
        assertEquals("full", status.optString("fallback_mode"))
        assertTrue(status.optString("base_url").startsWith("http://127.0.0.1:"))
    }

    private fun runTask(runtime: String, filename: String, marker: String): JSONObject {
        val created = step("task.$runtime.create") {
            val response = request("POST", "/api/v1/tasks", JSONObject()
                .put("name", "Android runtime smoke $runtime")
                .put("command", "task $filename")
                .put("task_type", "manual")
                .put("timeout", 60), expected = setOf(201))
            JSONObject().put("task_id", response.body.getJSONObject("data").getLong("id"))
        }
        val taskId = created.getLong("task_id")
        val operationId = step("task.$runtime.run") {
            val response = request("PUT", "/api/v1/tasks/$taskId/run")
            JSONObject().put("task_id", taskId).put("operation_id", response.body.getString("operation_id"))
        }.getString("operation_id")
        val log = step("task.$runtime.wait_terminal_and_read_log") {
            val operation = waitForOperation(operationId)
            val terminal = waitForTaskLog(taskId, marker)
            assertEquals(0, terminal.getInt("status"))
            JSONObject().put("task_id", taskId).put("operation_id", operationId)
                .put("operation", operation)
                .put("log_id", terminal.getLong("id")).put("terminal_status", terminal.get("status"))
                .put("content_sha256", sha256(terminal.getString("content"))).put("marker_found", true)
        }
        request("DELETE", "/api/v1/tasks/$taskId")
        return log.put("runtime", runtime)
    }

    private fun waitForTaskLog(taskId: Long, marker: String): JSONObject {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(90)
        var last = JSONObject()
        while (System.nanoTime() < deadline) {
            val list = request("GET", "/api/v1/logs?task_id=$taskId&page_size=1").body.optJSONArray("data") ?: JSONArray()
            if (list.length() > 0) {
                val id = list.getJSONObject(0).getLong("id")
                last = request("GET", "/api/v1/logs/$id").body
                val content = last.optString("content")
                if (content.contains(marker) && last.opt("ended_at") != null && last.opt("ended_at") != JSONObject.NULL) return last
            }
            Thread.sleep(500)
        }
        error("Task $taskId did not reach log terminal state: $last")
    }

    private fun installAndRemoveDependency(type: String, spec: String, pythonVersion: String) {
        val key = if (type == "python") "python_wheel" else "node_tarball"
        val dep = step("dependency.$key.install") {
            val body = JSONObject().put("type", type).put("names", JSONArray().put(spec))
            if (pythonVersion.isNotBlank()) body.put("python_version", pythonVersion)
            val response = request("POST", "/api/v1/deps", body, expected = setOf(201))
            val item = response.body.getJSONArray("data").getJSONObject(0)
            val terminal = waitForDependency(item.getLong("id"), setOf("installed", "failed"))
            assertEquals("installed", terminal.getString("status"))
            val operation = waitForOperation(item.getString("operation_id"))
            JSONObject().put("dependency_id", item.getLong("id")).put("operation_id", item.optString("operation_id"))
                .put("operation", operation).put("terminal_status", terminal.getString("status"))
                .put("fixture_sha256", sha256(File(spec).readBytes()))
        }
        val id = dep.getLong("dependency_id")
        step("dependency.$key.uninstall") {
            val installOperationId = dep.getString("operation_id")
            request("DELETE", "/api/v1/deps/$id")
            val uninstallOperationId = waitForDependencyOperation(id, installOperationId)
            val operation = waitForOperation(uninstallOperationId)
            val terminal = waitForDependency(id, setOf("failed", "cancelled"), allowMissing = true)
            assertTrue(terminal.optBoolean("missing"))
            JSONObject().put("dependency_id", id).put("operation_id", uninstallOperationId)
                .put("operation", operation).put("dependency_missing", true)
        }
    }

    private fun waitForDependencyOperation(id: Long, previous: String): String {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30)
        val prefix = "dep_${id}_%"
        while (System.nanoTime() < deadline) {
            val databaseFile = findDatabaseFile()
            if (databaseFile != null) {
                SQLiteDatabase.openDatabase(databaseFile.path, null, SQLiteDatabase.OPEN_READONLY).use { database ->
                    database.query("operations", arrayOf("id"), "id LIKE ? AND id != ?", arrayOf(prefix, previous), null, null, "sequence DESC", "1").use { cursor ->
                        if (cursor.moveToFirst()) return cursor.getString(0)
                    }
                }
            }
            Thread.sleep(100)
        }
        error("Dependency $id did not create an uninstall operation")
    }

    private fun waitForDependency(id: Long, terminal: Set<String>, allowMissing: Boolean = false): JSONObject {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(120)
        var last = JSONObject()
        while (System.nanoTime() < deadline) {
            val response = request("GET", "/api/v1/deps/$id/status", expected = if (allowMissing) setOf(200, 404) else setOf(200))
            if (response.status == 404) return JSONObject().put("missing", true)
            last = response.body.getJSONObject("data")
            if (last.optString("status") in terminal) return last
            Thread.sleep(500)
        }
        error("Dependency $id did not reach terminal state: $last")
    }

    private fun waitForOperation(id: String): JSONObject {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(120)
        val terminal = setOf("success", "failed", "aborted", "unknown", "canceled")
        var last = JSONObject()
        while (System.nanoTime() < deadline) {
            val databaseFile = findDatabaseFile()
            if (databaseFile != null) {
                runCatching {
                    SQLiteDatabase.openDatabase(databaseFile.path, null, SQLiteDatabase.OPEN_READONLY).use { database ->
                        database.query(
                            "operations",
                            arrayOf("id", "kind", "state", "phase", "progress", "exit_code", "error_code", "started_at", "ended_at", "log_cursor"),
                            "id = ?",
                            arrayOf(id),
                            null,
                            null,
                            null,
                        ).use { cursor ->
                            if (cursor.moveToFirst()) {
                                last = JSONObject().put("id", cursor.getString(0)).put("kind", cursor.getString(1))
                                    .put("state", cursor.getString(2)).put("phase", cursor.getString(3))
                                    .put("progress", cursor.getDouble(4))
                                    .put("exit_code", if (cursor.isNull(5)) JSONObject.NULL else cursor.getInt(5))
                                    .put("error_code", cursor.getString(6))
                                    .put("started_at", if (cursor.isNull(7)) JSONObject.NULL else cursor.getString(7))
                                    .put("ended_at", if (cursor.isNull(8)) JSONObject.NULL else cursor.getString(8))
                                    .put("log_cursor", cursor.getLong(9))
                            }
                        }
                    }
                }
                if (last.optString("state") in terminal) {
                    assertEquals("success", last.getString("state"))
                    return last
                }
            }
            Thread.sleep(500)
        }
        error("Operation $id did not reach terminal state: $last")
    }

    private fun findDatabaseFile(): File? = File(context.filesDir, "local-panel").walkTopDown()
        .filter { it.isFile && it.name == "daidai.db" }
        .maxByOrNull { it.lastModified() }

    private fun request(method: String, path: String, body: JSONObject? = null, expected: Set<Int> = setOf(200)): HttpResult {
        val connection = URL(core.getString("base_url") + path).openConnection() as HttpURLConnection
        connection.requestMethod = method
        connection.connectTimeout = 10_000
        connection.readTimeout = 30_000
        connection.setRequestProperty("X-Daidai-Local-Token", token)
        connection.setRequestProperty("Origin", core.getString("base_url"))
        if (accessToken.isNotBlank()) connection.setRequestProperty("Authorization", "Bearer $accessToken")
        if (body != null) {
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", "application/json")
            connection.outputStream.use { it.write(body.toString().toByteArray()) }
        }
        val status = connection.responseCode
        val raw = (if (status >= 400) connection.errorStream else connection.inputStream)?.bufferedReader()?.use { it.readText() }.orEmpty()
        val parsed = if (raw.isBlank()) JSONObject() else JSONObject(raw)
        check(status in expected) { "$method $path returned $status: $raw" }
        return HttpResult(status, parsed)
    }

    private fun step(id: String, action: () -> JSONObject): JSONObject {
        val started = Instant.now().toString()
        return try {
            val evidence = action()
            steps.put(JSONObject().put("id", id).put("status", "pass").put("started_at", started)
                .put("ended_at", Instant.now().toString()).put("evidence", evidence))
            evidence
        } catch (error: Throwable) {
            steps.put(JSONObject().put("id", id).put("status", "failed").put("started_at", started)
                .put("ended_at", Instant.now().toString()).put("error", throwable(error)))
            throw error
        }
    }

    private fun writeWheelFixture(file: File): File {
        ZipOutputStream(file.outputStream()).use { zip ->
            mapOf(
                "runtime_smoke_pkg/__init__.py" to "VALUE = 'offline'\n",
                "runtime_smoke_pkg-1.0.0.dist-info/METADATA" to "Metadata-Version: 2.1\nName: runtime-smoke-pkg\nVersion: 1.0.0\n",
                "runtime_smoke_pkg-1.0.0.dist-info/WHEEL" to "Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
                "runtime_smoke_pkg-1.0.0.dist-info/RECORD" to "",
            ).forEach { (name, value) -> zip.putNextEntry(ZipEntry(name)); zip.write(value.toByteArray()); zip.closeEntry() }
        }
        return file
    }

    private fun writeNodeTarballFixture(file: File): File {
        GZIPOutputStream(file.outputStream()).use { gzip ->
            writeTarEntry(gzip, "package/package.json", "{\"name\":\"runtime-smoke-node\",\"version\":\"1.0.0\",\"main\":\"index.js\"}\n".toByteArray())
            writeTarEntry(gzip, "package/index.js", "module.exports = 'offline';\n".toByteArray())
            gzip.write(ByteArray(1024))
        }
        return file
    }

    private fun writeTarEntry(output: GZIPOutputStream, name: String, data: ByteArray) {
        val header = ByteArray(512)
        fun field(offset: Int, length: Int, value: String) {
            value.toByteArray(StandardCharsets.US_ASCII).copyInto(header, offset, endIndex = minOf(value.length, length))
        }
        field(0, 100, name); field(100, 8, "0000644\u0000"); field(108, 8, "0000000\u0000")
        field(116, 8, "0000000\u0000"); field(124, 12, data.size.toString(8).padStart(11, '0') + "\u0000")
        field(136, 12, "00000000000\u0000"); for (index in 148 until 156) header[index] = 0x20
        header[156] = '0'.code.toByte(); field(257, 6, "ustar\u0000"); field(263, 2, "00")
        field(148, 8, header.sumOf { it.toUByte().toInt() }.toString(8).padStart(6, '0') + "\u0000 ")
        output.write(header); output.write(data)
        val padding = (512 - data.size % 512) % 512
        if (padding > 0) output.write(ByteArray(padding))
    }

    private fun sanitizedCore(value: JSONObject): JSONObject = JSONObject(value.toString()).apply { remove("local_token") }
    private fun throwable(error: Throwable): JSONObject = JSONObject().put("type", error.javaClass.name).put("message", error.message ?: "")
    private fun sha256(value: String): String = MessageDigest.getInstance("SHA-256").digest(value.toByteArray()).joinToString("") { "%02x".format(it) }
    private fun sha256(value: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(value).joinToString("") { "%02x".format(it) }
    private data class HttpResult(val status: Int, val body: JSONObject)
}
