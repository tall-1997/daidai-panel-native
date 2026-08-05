package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ScriptRunLogPresentationTest {
    @Test
    fun `extracts final traceback exception instead of generic footer`() {
        val logs = listOf(
            "Traceback (most recent call last):",
            "  File \"测试.py\", line 1, in <module>",
            "ValueError: 原始错误文本",
            "Script failed with exit code 1",
        )
        assertEquals("ValueError: 原始错误文本", ScriptRunLogPresentation.errorSummary(logs, true))
    }

    @Test
    fun `does not invent an error for successful output`() {
        assertNull(ScriptRunLogPresentation.errorSummary(listOf("TOKEN=process printed value"), false))
    }
}
