package com.daidai.daidai_app

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AndroidRuntimeSmokeTest {
    @Test
    fun embeddedCoreAndEightRuntimeChecksProduceEvidence() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val evidenceDir = File(context.filesDir, "runtime-smoke").apply { mkdirs() }
        val evidenceFile = File(evidenceDir, "instrumentation.json")
        val records = JSONArray()
        var coreStatus = JSONObject()

        try {
            coreStatus = ensureCoreStarted(context)
            assertEquals("ready", coreStatus.optString("phase"))
            assertTrue("Go Core instance ID must be present", coreStatus.optString("instance_id").isNotBlank())
            assertTrue("Go Core version must be present", coreStatus.optString("core_version").isNotBlank())
            assertFalse("Kotlin fallback must fail the runtime gate", coreStatus.optString("instance_id").contains("fallback", ignoreCase = true))
            assertFalse("Kotlin fallback must fail the runtime gate", coreStatus.optString("core_version").contains("fallback", ignoreCase = true))
            assertTrue("Go Core endpoint must be loopback", coreStatus.optString("base_url").startsWith("http://127.0.0.1:"))

            val python = AndroidPythonRuntime.ensureReady(context)
            records.put(
                if (python == null) blockedRecord("python-3.14-android-arm64", "PY_OK_SSL_SQLITE_VENV_PIP", "python-runtime-unavailable")
                else runtimeRecord(
                    "python-3.14-android-arm64",
                    listOf(
                        runCheck(context, "PY_OK", listOf(python.executable, python.home, "-c", "print('PY_OK')"), "PY_OK", pythonEnv(context, python)),
                        runCheck(context, "SSL", listOf(python.executable, python.home, "-c", "import ssl;print('SSL')"), "SSL", pythonEnv(context, python)),
                        runCheck(context, "SQLite", listOf(python.executable, python.home, "-c", "import sqlite3;print('SQLite')"), "SQLite", pythonEnv(context, python)),
                        runCheck(context, "venv", listOf(python.executable, python.home, "-c", "import venv;print('venv')"), "venv", pythonEnv(context, python)),
                        runCheck(context, "pip", listOf(python.executable, python.home, "-c", "import ensurepip;print('pip')"), "pip", pythonEnv(context, python)),
                    ),
                ),
            )

            val node = AndroidNodeRuntime.ensureReady(context)
            records.put(
                if (node == null) blockedRecord("node-lts-android-arm64", "COMMONJS_ESM_HTTPS", "node-runtime-unavailable")
                else runtimeRecord(
                    "node-lts-android-arm64",
                    listOf(
                        runCheck(context, "CommonJS", listOf(node.executable, "-e", "require('path');console.log('CommonJS')"), "CommonJS", nodeEnv(context, node)),
                        runCheck(context, "ESM", listOf(node.executable, "--input-type=module", "-e", "import path from 'path';console.log('ESM')"), "ESM", nodeEnv(context, node)),
                        runCheck(context, "HTTPS", listOf(node.executable, "-e", "require('https');console.log('HTTPS')"), "HTTPS", nodeEnv(context, node)),
                    ),
                ),
            )
            records.put(
                if (node == null) blockedRecord("typescript-stable", "TS_OK", "node-runtime-unavailable")
                else runtimeRecord(
                    "typescript-stable",
                    listOf(runCheck(context, "TS_OK", listOf(node.executable, "-e", "require('typescript').transpileModule('const n:number=1',{});console.log('TS_OK')"), "TS_OK", nodeEnv(context, node))),
                ),
            )
            records.put(blockedRecord("shell-android-arm64", "SHELL_PIPE_EXIT_STOP", "runtime-placeholder-elf"))
            records.put(blockedRecord("git-android-arm64", "GIT_CLONE_FETCH_SPARSE", "runtime-placeholder-elf"))
            records.put(blockedRecord("ssh-android-arm64", "SSH_HOSTKEY", "runtime-placeholder-elf"))
            records.put(blockedRecord("yaegi-go", "GO_INTERPRET_OK", "runtime-placeholder-elf"))
            records.put(blockedRecord("go-builder-android-arm64", "GO_BUILD_EXPORT_ONLY", "runtime-placeholder-elf"))

            assertEquals(8, records.length())
        } finally {
            evidenceFile.writeText(
                JSONObject()
                    .put("schema_version", 1)
                    .put("core", coreStatus.apply { remove("local_token") })
                    .put("records", records)
                    .put("record_count", records.length())
                    .toString(2),
            )
        }
    }

    private fun ensureCoreStarted(context: Context): JSONObject {
        val latch = CountDownLatch(1)
        var result: Result<String>? = null
        val client = LocalPanelServiceClient(context)
        try {
            client.ensureStarted {
                result = it
                latch.countDown()
            }
            check(latch.await(90, TimeUnit.SECONDS)) { "Timed out waiting for LocalPanelHostService" }
            return JSONObject(requireNotNull(result).getOrThrow())
        } finally {
            client.close()
        }
    }

    private fun runCheck(
        context: Context,
        id: String,
        command: List<String>,
        marker: String,
        environment: Map<String, String>,
    ): JSONObject {
        return runCatching {
            val process = ProcessBuilder(command)
                .directory(context.cacheDir)
                .redirectErrorStream(true)
                .apply { environment().putAll(environment) }
                .start()
            val completed = process.waitFor(30, TimeUnit.SECONDS)
            if (!completed) process.destroyForcibly()
            val output = process.inputStream.bufferedReader().readText().trim().take(4000)
            val passed = completed && process.exitValue() == 0 && output.contains(marker)
            JSONObject()
                .put("id", id)
                .put("status", if (passed) "pass" else "blocked")
                .put(if (passed) "output" else "reason", if (passed) output else "runtime-execution-failed")
        }.getOrElse { blockedCheck(id, "runtime-execution-error:${it.javaClass.simpleName}") }
    }

    private fun runtimeRecord(runtimeId: String, checks: List<JSONObject>): JSONObject {
        val status = if (checks.all { it.optString("status") == "pass" }) "pass" else "blocked"
        return JSONObject().put("runtime_id", runtimeId).put("status", status).put("checks", JSONArray(checks))
    }

    private fun blockedRecord(runtimeId: String, checkId: String, reason: String): JSONObject =
        JSONObject().put("runtime_id", runtimeId).put("status", "blocked").put("checks", JSONArray().put(blockedCheck(checkId, reason)))

    private fun blockedCheck(id: String, reason: String): JSONObject =
        JSONObject().put("id", id).put("status", "blocked").put("reason", reason)

    private fun pythonEnv(context: Context, paths: PythonRuntimePaths): Map<String, String> = mapOf(
        "LD_LIBRARY_PATH" to context.applicationInfo.nativeLibraryDir,
        "PYTHONPATH" to paths.deps,
        "PIP_TARGET" to paths.deps,
        "HOME" to context.filesDir.absolutePath,
        "TMPDIR" to context.cacheDir.absolutePath,
    )

    private fun nodeEnv(context: Context, paths: NodeRuntimePaths): Map<String, String> = mapOf(
        "LD_LIBRARY_PATH" to context.applicationInfo.nativeLibraryDir,
        "NODE_PATH" to listOf(paths.modules, paths.deps).joinToString(File.pathSeparator),
        "HOME" to context.filesDir.absolutePath,
        "TMPDIR" to context.cacheDir.absolutePath,
    )
}
