package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream

class LocalTaskFallbackSemanticsTest {
    @Test
    fun `python package names map to their import modules`() {
        assertEquals("Crypto", LocalTaskFallbackSemantics.pythonImportName("pycryptodome"))
        assertEquals("Cryptodome", LocalTaskFallbackSemantics.pythonImportName("pycryptodomex"))
        assertEquals("yaml", LocalTaskFallbackSemantics.pythonImportName("PyYAML"))
        assertEquals("requests", LocalTaskFallbackSemantics.pythonImportName("requests==2.32.0"))
        assertNull(LocalTaskFallbackSemantics.pythonImportName("../unsafe"))
    }

    @Test
    fun `detects Python and Node missing dependencies`() {
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("python", "pycryptodome"),
            LocalTaskFallbackSemantics.detectMissingDependency("python", "ModuleNotFoundError: No module named 'Crypto.Hash'"),
        )
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("python", "python-dotenv"),
            LocalTaskFallbackSemantics.detectMissingDependency("python", "ModuleNotFoundError: No module named 'dotenv'"),
        )
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "axios"),
            LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "Error: Cannot find module 'axios'"),
        )
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "got@11"),
            LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "依赖 got@11 安装失败，请手动安装: npm install got@11"),
        )
        assertEquals(
            listOf(
                LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "iconv-lite"),
                LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "tough-cookie"),
            ),
            LocalTaskFallbackSemantics.detectMissingDependencies("nodejs", "请手动安装: npm install iconv-lite tough-cookie"),
        )
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("python", "beautifulsoup4"),
            LocalTaskFallbackSemantics.detectMissingDependency("python", "请手动安装: pip install beautifulsoup4"),
        )
    }

    @Test
    fun `rejects local Node module and unrelated output`() {
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "Error: Cannot find module './helper'"))
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "Error: Cannot find module 'notify'"))
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("python", "ModuleNotFoundError: No module named 'notify'"))
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("python", "SyntaxError: invalid syntax"))
    }

    @Test
    fun `cursor resumes after acknowledged line`() {
        assertEquals(1L, LocalTaskFallbackSemantics.cursor("2", "1"))
        assertEquals(3L, LocalTaskFallbackSemantics.cursor(null, "3"))
        assertEquals(0L, LocalTaskFallbackSemantics.cursor("invalid", null))
        assertEquals(listOf(3L to "c", 4L to "d"), LocalTaskFallbackSemantics.linesAfterCursor(listOf("a", "b", "c", "d"), 2))
        assertEquals(emptyList<Pair<Long, String>>(), LocalTaskFallbackSemantics.linesAfterCursor(listOf("a"), 9))
        assertEquals(listOf(9L to "c", 10L to "d"), LocalTaskFallbackSemantics.linesAfterCursor(listOf("c", "d"), 8, 10))
    }

    @Test
    fun `log query contract supports filters and bounded pagination`() {
        val query = LocalLogQueryContract.build(
            mapOf("task_id" to "12", "status" to "1", "keyword" to "错误", "page" to "3", "page_size" to "500"),
        )
        assertEquals(" WHERE l.task_id = ? AND l.status = ? AND (t.name LIKE ? OR l.content LIKE ?)", query.where)
        assertEquals(listOf("12", "1", "%错误%", "%错误%"), query.args.toList())
        assertEquals(20, query.limit)
        assertEquals(40, query.offset)
    }

    @Test
    fun `fallback log file exposes unified fields`() {
        val file = LocalLogQueryContract.logFile(12, 34, 56, "2026-08-21T10:00:00Z")
        assertEquals("task-12-34.log", file["filename"])
        assertEquals("task_12/task-12-34.log", file["path"])
        assertEquals(34L, file["log_id"])
        assertEquals(56L, file["size"])
        assertEquals("2026-08-21T10:00:00Z", file["created_at"])
    }

    @Test
    fun `dependency retries are bounded like Go backend`() {
        assertEquals(5, LocalTaskFallbackSemantics.MAX_DEPENDENCY_INSTALLS)
        val candidate = LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "axios")
        assertEquals(candidate, LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", emptySet(), 4))
        assertNull(LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", emptySet(), 5))
        assertNull(LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", setOf(candidate), 1))
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "tough-cookie"),
            LocalTaskFallbackSemantics.nextDependency(
                "nodejs",
                "请手动安装: npm install iconv-lite tough-cookie",
                setOf(LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "iconv-lite")),
                1,
            ),
        )
    }

    @Test
    fun `runtime and enabled panel environment are injected into process environment`() {
        val target = mutableMapOf("PATH" to "/system/bin", "PANEL_ENV" to "old")
        LocalTaskFallbackSemantics.applyRuntimeEnvironment(
            target,
            mapOf("PATH" to "/runtime/bin", "PANEL_ENV" to "enabled", "DAIDAI_ANDROID_LOCAL" to "1"),
        )
        assertEquals("/runtime/bin", target["PATH"])
        assertEquals("enabled", target["PANEL_ENV"])
        assertEquals("1", target["DAIDAI_ANDROID_LOCAL"])
    }

    @Test
    fun `task notification switches match terminal status`() {
        assertTrue(LocalTaskFallbackSemantics.shouldNotify("success", false, true, false))
        assertTrue(LocalTaskFallbackSemantics.shouldNotify("failed", true, false, false))
        assertTrue(LocalTaskFallbackSemantics.shouldNotify("aborted", false, false, true))
        assertFalse(LocalTaskFallbackSemantics.shouldNotify("success", true, false, true))
    }

    @Test
    fun `task log status keeps aborted terminal and running distinct`() {
        assertEquals(0, LocalPanelStore.taskLogStatusCode("success"))
        assertEquals(1, LocalPanelStore.taskLogStatusCode("failed"))
        assertEquals(2, LocalPanelStore.taskLogStatusCode("running"))
        assertEquals(3, LocalPanelStore.taskLogStatusCode("aborted"))
        assertEquals(3, LocalPanelStore.taskLogStatusCode("stopped"))
        assertEquals("aborted", LocalPanelStore.taskLogRunStatus(3))
        assertTrue(LocalPanelStore.taskLogDone(3))
        assertFalse(LocalPanelStore.taskLogDone(2))
    }

    @Test
    fun `task tokenizer and parser preserve unicode paths and arguments`() {
        data class Case(val command: String, val path: String, val args: List<String>, val mode: String, val timeout: Long = 300)
        val cases = listOf(
            Case("task '中文 目录/签到+通知%脚本.py' now -- --名称 '张 三' + %", "中文 目录/签到+通知%脚本.py", listOf("--名称", "张 三", "+", "%"), "now"),
            Case("task -m 5m -l 中文\\ 目录/脚本.py -- --flag=value", "中文 目录/脚本.py", listOf("--flag=value"), "normal", 300),
            Case("task \"目录/脚本.sh\" conc JD_COOKIE 1-2", "目录/脚本.sh", emptyList(), "conc"),
            Case("task '目录/脚本.js' desi TOKEN 2", "目录/脚本.js", emptyList(), "desi"),
        )

        cases.forEach { case ->
            val plan = LocalTaskFallbackSemantics.parseTaskCommand(case.command) { it == case.path }
            assertEquals(case.command, case.path, plan.scriptPath)
            assertEquals(case.command, case.args, plan.scriptArgs)
            assertEquals(case.command, case.mode, plan.mode)
            assertEquals(case.command, case.timeout, plan.timeoutSeconds)
        }
    }

    @Test
    fun `script path normalization decodes percent encoded unicode and preserves plus`() {
        assertEquals(
            "中文 目录/签到+通知%脚本.js",
            LocalPanelStore.normalizeScriptPath("%E4%B8%AD%E6%96%87%20%E7%9B%AE%E5%BD%95/%E7%AD%BE%E5%88%B0+%E9%80%9A%E7%9F%A5%25%E8%84%9A%E6%9C%AC.js"),
        )
        assertEquals("目录/脚本.js", LocalPanelStore.normalizeScriptPath("%E7%9B%AE%E5%BD%95%2F%E8%84%9A%E6%9C%AC.js"))
        assertEquals("签到+通知.js", LocalPanelStore.normalizeScriptPath("签到+通知.js"))
        assertEquals("100%25.js", LocalPanelStore.normalizeScriptPath("100%25.js"))
        assertEquals("100%.js", LocalPanelStore.normalizeScriptPath("100%.js"))
    }

    @Test
    fun `script path normalization rejects encoded traversal after decode`() {
        assertThrows(IllegalArgumentException::class.java) {
            LocalPanelStore.normalizeScriptPath("%2E%2E/%E8%84%9A%E6%9C%AC.js")
        }
        assertThrows(IllegalArgumentException::class.java) {
            LocalPanelStore.normalizeScriptPath("%2Fdata%2Fsecret.js")
        }
    }

    @Test
    fun `task parser reports malformed quotes and missing unicode paths`() {
        assertThrows(IllegalArgumentException::class.java) {
            LocalTaskFallbackSemantics.parseTaskCommand("task '中文/脚本.py") { true }
        }
        val error = assertThrows(IllegalArgumentException::class.java) {
            LocalTaskFallbackSemantics.parseTaskCommand("task '不存在 目录/脚本.py' now") { false }
        }
        assertEquals("脚本不存在或路径无效: 不存在 目录/脚本.py", error.message)
    }

    @Test
    fun `tokenizer matches Go escaping semantics`() {
        val cases = listOf(
            "python '中文 目录/脚本.py' --name \"张 三\"" to listOf("python", "中文 目录/脚本.py", "--name", "张 三"),
            "script\\ path.py plus+ percent%" to listOf("script path.py", "plus+", "percent%"),
            "cmd \\\\server\\file" to listOf("cmd", "\\server\\file"),
            "cmd '' \"\"" to listOf("cmd", "", ""),
        )
        cases.forEach { (command, expected) -> assertEquals(command, expected, LocalTaskFallbackSemantics.tokenize(command)) }
    }

    @Test
    fun `task modes produce compatible environment selections`() {
        val conc = LocalTaskFallbackSemantics.taskEnvironments(
            LocalTaskFallbackSemantics.TaskCommandPlan("脚本.py", mode = "conc", envName = "账号", accountSpec = "1-2"),
            mapOf("账号" to "甲&乙&丙"),
        )
        assertEquals(listOf(1, 2), conc.map { it.index })
        assertEquals(listOf("甲", "乙"), conc.map { it.values["账号"] })

        val desi = LocalTaskFallbackSemantics.taskEnvironments(
            LocalTaskFallbackSemantics.TaskCommandPlan("脚本.py", mode = "desi", envName = "账号", accountSpec = "2,max"),
            mapOf("账号" to "甲&乙&丙"),
        ).single()
        assertEquals("乙&丙", desi.values["账号"])
        assertEquals("2 3", desi.values["numParam"])

        listOf(
            listOf("甲&一", "", "乙\\二"),
            listOf("a&", "b"),
            listOf("a", "&b"),
            listOf("a&", "&b"),
        ).forEach { special ->
            assertEquals(
                special,
                LocalTaskFallbackSemantics.splitEnvironmentValues(
                    LocalTaskFallbackSemantics.joinEnvironmentValues(special),
                ),
            )
        }
    }

    @Test
    fun `process termination escalates after graceful timeout`() {
        val process = RecordingProcess()
        LocalTaskProcessTerminator.terminate(process) { false }
        assertEquals(listOf("destroy", "destroyForcibly"), process.calls)
    }

    private class RecordingProcess : Process() {
        val calls = mutableListOf<String>()
        override fun destroy() { calls += "destroy" }
        override fun destroyForcibly(): Process { calls += "destroyForcibly"; return this }
        override fun getOutputStream() = ByteArrayOutputStream()
        override fun getInputStream() = ByteArrayInputStream(ByteArray(0))
        override fun getErrorStream() = ByteArrayInputStream(ByteArray(0))
        override fun waitFor() = 0
        override fun exitValue() = 0
    }
}
