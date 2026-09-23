package com.daidai.daidai_app

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.util.Base64
import android.database.sqlite.SQLiteDatabase
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
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
                val started = invokeLocalHost("ensure-started")
                core = if (started.optString("phase") == "ready") started else waitForMethodChannelCore()
                assertCoreReady(core)
                token = started.getString("local_token")
                JSONObject().put("transport", "com.daidai.panel/local_host").put("core", sanitizedCore(core))
            }
            step("auth.initialize_and_login") {
                val credentials = JSONObject().put("username", "runtime_smoke_admin").put("password", "runtime-smoke-password")
                val init = request("GET", "/api/auth/check-init")
                val initializedNow = init.body.optBoolean("need_init")
                if (initializedNow) request("POST", "/api/auth/init", credentials, expected = setOf(200))
                val login = request("POST", "/api/auth/login", credentials, expected = setOf(200))
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
                val output = readAllWithTimeout(process)
                assertEquals("Rootfs /usr/bin/env failed: ${command.joinToString(" ")}\n$output", 0, process.exitValue())
                assertTrue(output.contains("ROOTFS_ENV_OK"))
                JSONObject().put("command", "/usr/bin/env").put("output", "ROOTFS_ENV_OK")
            }

            step("rootfs.node_file_read_diagnostics") {
                // npm-cli.js 的绝对路径随发行版不同（Ubuntu: /usr/share/nodejs/npm/bin/npm-cli.js;
                // Alpine: /usr/lib/node_modules/npm/bin/npm-cli.js），但 /usr/bin/npm 在两种
                // rootfs 中都指向真实的 npm-cli.js。先用 node 解析真实路径，避免硬编码发行版路径。
                val npmCli = executeGuestEvidence(
                    listOf("/usr/bin/node", "-e", "console.log(require('fs').realpathSync('/usr/bin/npm'))"),
                    "node realpath /usr/bin/npm",
                ).getString("output").trim()
                // /usr/bin/stat 在 Alpine/busybox 中不存在；python3 在两种 rootfs 中都有，用它做独立 size 校验。
                val guestStat = executeGuestEvidence(
                    listOf("/usr/bin/python3", "-c", "import os,sys; print(os.path.getsize(sys.argv[1]))", npmCli),
                    "python3 -c getsize $npmCli",
                )
                val nodeScript = """
                    const fs=require('fs'),path=process.argv[1],result={path};let buffer,bytesRead;
                    const capture=(name,fn)=>{try{result[name]={ok:true,...fn()}}catch(error){result[name]={ok:false,name:error.name,code:error.code||'',message:error.message}}};
                    capture('statSync',()=>({size:fs.statSync(path).size}));
                    capture('alloc',()=>{buffer=Buffer.alloc(fs.statSync(path).size);return{length:buffer.length}});
                    capture('readSync',()=>{const fd=fs.openSync(path,'r');try{bytesRead=fs.readSync(fd,buffer,0,buffer.length,0);return{bytesRead}}finally{fs.closeSync(fd)}});
                    capture('decodeReadBuffer',()=>({length:buffer.subarray(0,bytesRead).toString('utf8').length}));
                    capture('readFileBuffer',()=>({length:fs.readFileSync(path).length}));
                    capture('readFileUtf8',()=>({length:fs.readFileSync(path,'utf8').length}));
                    console.log(JSON.stringify(result));
                """.trimIndent().replace("\n", "")
                val node = executeGuestEvidence(
                    listOf("/usr/bin/node", "-e", nodeScript, npmCli),
                    "node npm CLI file read diagnostics",
                )
                val nodeResult = JSONObject(node.getString("output").lineSequence().last { it.startsWith("{") })
                val size = guestStat.getString("output").toLong()
                val stat = nodeResult.getJSONObject("statSync")
                val decoded = nodeResult.getJSONObject("decodeReadBuffer")
                val utf8 = nodeResult.getJSONObject("readFileUtf8")
                assertTrue("guest stat mismatch: ${stat.toString()}", stat.getLong("size") == size)
                assertTrue("utf8 decode corrupt: ${decoded.toString()}", decoded.getLong("length") == size)
                assertTrue("readFile utf8 corrupt: ${utf8.toString()}", utf8.getLong("length") == size)
                JSONObject()
                    .put("guest_stat_size", size)
                    .put("node", nodeResult)
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
                val finished = process.waitFor(180, TimeUnit.SECONDS)
                if (!finished) process.destroyForcibly()
                check(finished) { "Rootfs npm env node smoke timed out" }
                val output = readAllWithTimeout(process)
                assertEquals("Rootfs npm env node failed: ${command.joinToString(" ")}\n$output", 0, process.exitValue())
                assertTrue("Rootfs npm env node output was not a version: ${output.take(512)}", output.trim().matches(Regex("[0-9]+(?:\\.[0-9]+)+")))
                JSONObject().put("command", "/usr/bin/npm --version").put("version", output.trim())
            }
            step("runtime.python.version") {
                executeGuestEvidence(listOf("/usr/bin/python3", "--version"), "python3 --version")
            }
            step("runtime.node.version") {
                executeGuestEvidence(listOf("/usr/bin/node", "--version"), "node --version")
            }
            step("runtime.shell.version") {
                executeGuestEvidence(listOf("/bin/bash", "-c", "printf '%s' \"\$BASH_VERSION\""), "bash --version")
            }
            step("runtime.dns.managed_resolver") {
                val status = AndroidLinuxRuntime.statusJson(context).getJSONObject("rootfs")
                    .getJSONObject("compatibility").getJSONObject("dns")
                val expected = (0 until status.getJSONArray("servers").length())
                    .map { status.getJSONArray("servers").getString(it) }
                assertTrue(status.getBoolean("write_success"))
                assertTrue(status.getString("updated_at").isNotBlank())
                assertEquals("", status.getString("error"))
                val resolv = executeGuestEvidence(listOf("/bin/cat", "/etc/resolv.conf"), "cat /etc/resolv.conf")
                val python = executeGuestEvidence(
                    listOf("/usr/bin/python3", "-c", "import ipaddress; p=[x.split()[1] for x in open('/etc/resolv.conf') if x.startswith('nameserver ')]; [ipaddress.ip_address(x) for x in p]; print(','.join(p))"),
                    "python3 validate resolv.conf",
                )
                val node = executeGuestEvidence(
                    listOf("/usr/bin/node", "-e", "const fs=require('fs'),net=require('net');const p=fs.readFileSync('/etc/resolv.conf','utf8').split(/\\n/).filter(x=>x.startsWith('nameserver ')).map(x=>x.split(/\\s+/)[1]);if(!p.length||p.some(x=>!net.isIP(x)))process.exit(1);console.log(p.join(','))"),
                    "node validate resolv.conf",
                )
                assertEquals(expected, resolv.getString("output").lineSequence().mapNotNull { line ->
                    line.removePrefix("nameserver ").takeIf { line.startsWith("nameserver ") }
                }.toList())
                assertEquals(expected.joinToString(","), python.getString("output"))
                assertEquals(expected.joinToString(","), node.getString("output"))
                JSONObject().put("source", status.getString("source")).put("servers", status.getJSONArray("servers"))
                    .put("write_success", status.getBoolean("write_success")).put("updated_at", status.getString("updated_at"))
                    .put("error", status.getString("error"))
                    .put("resolv_conf", resolv).put("python", python).put("node", node)
            }
            step("runtime.dns.real_resolution") {
                val mirrors = AndroidLinuxRuntime.mirrorConfig(context)
                val hosts = linkedMapOf(
                    "pip" to requireNotNull(URI(mirrors.pipMirror).host) { "Pip mirror host is missing" },
                    "npm" to requireNotNull(URI(mirrors.npmMirror).host) { "Npm mirror host is missing" },
                )
                val resolutions = JSONObject()
                hosts.forEach { (mirror, host) ->
                    val python = executeGuestEvidence(
                        listOf(
                            "/usr/bin/python3", "-c",
                            "import json,socket,sys; h=sys.argv[1]; a=sorted({x[4][0] for x in socket.getaddrinfo(h,443,type=socket.SOCK_STREAM)}); assert a; print(json.dumps({'host':h,'addresses':a},separators=(',',':')))",
                            host,
                        ),
                        "python3 socket.getaddrinfo $host",
                        timeoutSeconds = 30,
                    )
                    val node = executeGuestEvidence(
                        listOf(
                            "/usr/bin/node", "-e",
                            "const dns=require('dns'),h=process.argv[1];dns.lookup(h,{all:true},(e,a)=>{if(e||!a.length){console.error(e||'empty DNS result');process.exit(1)}console.log(JSON.stringify({host:h,addresses:a.map(x=>x.address).sort()}))})",
                            host,
                        ),
                        "node dns.lookup $host",
                        timeoutSeconds = 30,
                    )
                    assertResolutionEvidence(python, host)
                    assertResolutionEvidence(node, host)
                    resolutions.put(mirror, JSONObject().put("host", host).put("python", python).put("node", node))
                }
                JSONObject().put("hosts", JSONObject(hosts as Map<*, *>)).put("resolutions", resolutions)
                    .put("all_resolved", true)
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
            installAndRemoveDependency("python", wheel.absolutePath, DependencyStorage.PYTHON_VERSION)
            installAndRemoveDependency("nodejs", tarball.absolutePath, "")

            step("core.method_channel.restart") {
                val before = core.getString("instance_id")
                core = invokeLocalHost("restart")
                assertCoreReady(core)
                assertEquals("kotlin-local-fallback", before)
                assertEquals(before, core.getString("instance_id"))
                token = core.getString("local_token")
                JSONObject().put("transport", "com.daidai.panel/local_host").put("previous_instance_id", before)
                    .put("core", sanitizedCore(core))
            }
            step("persistence.after_restart") {
                val login = request("POST", "/api/auth/login", JSONObject()
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
            val evidence = JSONObject()
                .put("schema_version", 2)
                .put("status", if (failure == null) "pass" else "failed")
                .put("generated_at", Instant.now().toString())
                .put("core", sanitizedCore(core))
                .put("steps", steps)
                .put("failure", failure?.let { throwable(it) } ?: JSONObject.NULL)
                .toString(2)
            evidenceFile.writeText(evidence)
            publishInstrumentationEvidence(evidence)
        }
    }

    private fun publishInstrumentationEvidence(evidence: String) {
        val prefix = "daidai.runtime_smoke.evidence"
        val bytes = evidence.toByteArray(StandardCharsets.UTF_8)
        val encoded = Base64.encodeToString(bytes, Base64.NO_WRAP)
        val chunks = encoded.chunked(3000)
        val results = Bundle().apply {
            putString("$prefix.encoding", "base64")
            putString("$prefix.chunk_count", chunks.size.toString())
            putString("$prefix.sha256", sha256(bytes))
            chunks.forEachIndexed { index, chunk ->
                putString("$prefix.chunk_${index.toString().padStart(4, '0')}", chunk)
            }
        }
        InstrumentationRegistry.getInstrumentation().addResults(results)
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
        assertEquals("kotlin-local-fallback", status.optString("instance_id"))
        assertEquals("kotlin-local-fallback", status.optString("core_version"))
        assertEquals("full", status.optString("fallback_mode"))
        assertTrue(status.optString("scheduler_host_state") in setOf("active", "foreground_continuous", "system_compensation"))
        assertTrue(status.optString("scheduler_guarantee_state") in setOf("active", "foreground_continuous", "system_compensation"))
        assertTrue(status.optString("base_url").startsWith("http://127.0.0.1:"))
    }

    private fun executeGuestEvidence(command: List<String>, displayCommand: String, timeoutSeconds: Long = 60): JSONObject {
        val invocation = requireNotNull(AndroidLinuxRuntime.guestCommand(context, context.filesDir, command)) {
            "Packaged rootfs command is unavailable: $displayCommand"
        }
        val builder = ProcessBuilder(invocation).directory(context.filesDir).redirectErrorStream(true)
        builder.environment().putAll(AndroidLinuxRuntime.baseEnvironment(context, context.filesDir))
        AndroidLinuxRuntime.applyGuestEnvironment(invocation, builder.environment())
        val process = builder.start()
        process.outputStream.close()
        val finished = process.waitFor(timeoutSeconds, TimeUnit.SECONDS)
        if (!finished) process.destroyForcibly()
        check(finished) { "Rootfs command timed out: $displayCommand" }
        val output = readAllWithTimeout(process).trim()
        assertEquals("Rootfs command failed: $displayCommand\n$output", 0, process.exitValue())
        assertTrue("Rootfs command returned empty evidence: $displayCommand", output.isNotBlank())
        return JSONObject().put("command", displayCommand).put("output", output).put("exit_code", process.exitValue())
    }

    private fun readAllWithTimeout(process: Process, timeoutSeconds: Long = 30): String {
        val reader = process.inputStream.bufferedReader()
        val executor = Executors.newSingleThreadExecutor()
        return try {
            val future = executor.submit(Callable<String> { reader.readText() })
            try {
                future.get(timeoutSeconds, TimeUnit.SECONDS)
            } catch (error: TimeoutException) {
                try { reader.close() } catch (_: Exception) { }
                ""
            }
        } finally {
            executor.shutdownNow()
        }
    }

    private fun assertResolutionEvidence(evidence: JSONObject, expectedHost: String) {
        val resolved = JSONObject(evidence.getString("output"))
        assertEquals(expectedHost, resolved.getString("host"))
        assertTrue(resolved.getJSONArray("addresses").length() > 0)
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
                    if ("success" == last.getString("state")) return last
                    val detail = JSONObject().put("operation", last)
                    val segments = id.split("_")
                    if (segments.size >= 2 && segments[0] == "task") {
                        collectTaskDiagnostics(segments[1].toLong(), detail)
                    }
                    error("Operation $id terminal state ${last.optString("state")}: ${detail.toString()}")
                }
            }
            Thread.sleep(500)
        }
        error("Operation $id did not reach terminal state: $last")
    }

    private fun collectTaskDiagnostics(taskId: Long, detail: JSONObject) {
        val candidates = linkedSetOf<File>()
        context.getDatabasePath("daidai-local.db").parentFile?.listFiles()?.filter {
            it.isFile && it.name.endsWith(".db")
        }?.forEach { candidates.add(it) }
        File(context.filesDir, "local-panel").listFiles()?.filter {
            it.isFile && it.name.endsWith(".db")
        }?.forEach { candidates.add(it) }
        context.filesDir.listFiles()?.filter {
            it.isFile && it.name.endsWith(".db")
        }?.forEach { candidates.add(it) }
        val summaries = JSONArray()
        for (file in candidates) {
            val info = JSONObject().put("path", file.path).put("size", file.length())
            try {
                SQLiteDatabase.openDatabase(file.path, null, SQLiteDatabase.OPEN_READONLY).use { database ->
                    val tables = ArrayList<String>()
                    database.rawQuery("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name", null)
                        .use { c -> while (c.moveToNext()) tables.add(c.getString(0)) }
                    info.put("tables", JSONArray(tables))
                    if (tables.contains("tasks")) {
                        database.query(
                            "tasks", arrayOf("last_run_status", "last_run_logs", "last_log_id"),
                            "id = ?", arrayOf(taskId.toString()), null, null, null,
                        ).use { c ->
                            if (c.moveToFirst()) {
                                if (!c.isNull(0)) info.put("last_run_status", c.getString(0))
                                if (!c.isNull(1)) {
                                    val logs = c.getString(1)
                                    if (logs.isNotBlank()) info.put("run_logs", logs.takeLast(4000))
                                }
                                if (!c.isNull(2)) info.put("last_log_id", c.getLong(2))
                            }
                        }
                        database.query(
                            "task_logs_local", arrayOf("id", "status", "content", "ended_at"),
                            "task_id = ?", arrayOf(taskId.toString()), null, null, "id DESC", "1",
                        ).use { c ->
                            if (c.moveToFirst()) {
                                val content = if (c.isNull(2)) null else c.getString(2)
                                info.put("log_id", c.getLong(0)).put("log_status", c.getInt(1))
                                    .put("log_ended_at", if (c.isNull(3)) null else c.getString(3))
                                if (content != null) info.put("log_tail", content.takeLast(4000))
                            }
                        }
                    }
                }
            } catch (e: Exception) {
                info.put("open_error", e.toString())
            }
            summaries.put(info)
        }
        detail.put("databases", summaries)
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
