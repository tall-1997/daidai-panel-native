package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ScriptPathContractTest {
    @Test
    fun `normal relative UTF-8 paths are preserved`() {
        assertEquals("目录/脚本.py", LocalPanelStore.normalizeScriptPath("目录/脚本.py"))
        assertEquals("目录/脚本.py", LocalPanelStore.normalizeScriptPath("/目录/脚本.py"))
        assertEquals("签到+通知.py", LocalPanelStore.normalizeScriptPath("签到+通知.py"))
        assertEquals("折扣%25+通知.py", LocalPanelStore.normalizeScriptPath("折扣%25+通知.py"))
        assertEquals("", LocalPanelStore.normalizeScriptPath("/", allowRootAlias = true))
    }

    @Test
    fun `traversal absolute and backslash paths are rejected`() {
        listOf("../escape.py", "safe/../escape.py", "//absolute.py", "/data/escape.py", "C:/escape.py", "safe\\..\\escape.py", "safe\\file.py").forEach { path ->
            assertThrows(path, IllegalArgumentException::class.java) {
                LocalPanelStore.normalizeScriptPath(path)
            }
        }
    }
}
