package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream

class LocalTaskFallbackSemanticsTest {
    @Test
    fun `detects Python and Node missing dependencies`() {
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("python", "Crypto"),
            LocalTaskFallbackSemantics.detectMissingDependency("python", "ModuleNotFoundError: No module named 'Crypto.Hash'"),
        )
        assertEquals(
            LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "axios"),
            LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "Error: Cannot find module 'axios'"),
        )
    }

    @Test
    fun `rejects local Node module and unrelated output`() {
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("nodejs", "Error: Cannot find module './helper'"))
        assertNull(LocalTaskFallbackSemantics.detectMissingDependency("python", "SyntaxError: invalid syntax"))
    }

    @Test
    fun `cursor resumes after acknowledged line`() {
        assertEquals(2L, LocalTaskFallbackSemantics.cursor("2", "1"))
        assertEquals(3L, LocalTaskFallbackSemantics.cursor(null, "3"))
        assertEquals(0L, LocalTaskFallbackSemantics.cursor("invalid", null))
        assertEquals(listOf(3L to "c", 4L to "d"), LocalTaskFallbackSemantics.linesAfterCursor(listOf("a", "b", "c", "d"), 2))
        assertEquals(emptyList<Pair<Long, String>>(), LocalTaskFallbackSemantics.linesAfterCursor(listOf("a"), 9))
        assertEquals(listOf(9L to "c", 10L to "d"), LocalTaskFallbackSemantics.linesAfterCursor(listOf("c", "d"), 8, 10))
    }

    @Test
    fun `dependency retries are bounded like Go backend`() {
        assertEquals(5, LocalTaskFallbackSemantics.MAX_DEPENDENCY_INSTALLS)
        val candidate = LocalTaskFallbackSemantics.DependencyCandidate("nodejs", "axios")
        assertEquals(candidate, LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", emptySet(), 4))
        assertNull(LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", emptySet(), 5))
        assertNull(LocalTaskFallbackSemantics.nextDependency("nodejs", "Cannot find module 'axios'", setOf(candidate), 1))
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
