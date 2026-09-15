package com.daidai.daidai_app.data.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 青龙 ql 环境变量格式解析 / 序列化测试（F5 批量导入导出）。
 *
 * 覆盖三行式：
 *  1. `名称 值 备注`            —— 空白分隔
 *  2. `名称="值" 备注` / `name="值" 备注` —— 等号 + 引号
 *  3. `名称=值 备注`            —— 等号分隔（与导出格式互通）
 */
class EnvQlFormatTest {

    // ---------- 解析 ----------

    @Test
    fun `space separated name value remark`() {
        val result = parseQlEnvText("JD_COOKIE 123abc 我的京东")
        assertEquals(1, result.items.size)
        assertEquals("JD_COOKIE", result.items[0].name)
        assertEquals("123abc", result.items[0].value)
        assertEquals("我的京东", result.items[0].remark)
    }

    @Test
    fun `name equals quoted value with remark`() {
        val result = parseQlEnvText("API_KEY=\"a b c\" 备注内容")
        assertEquals(1, result.items.size)
        assertEquals("API_KEY", result.items[0].name)
        assertEquals("a b c", result.items[0].value)
        assertEquals("备注内容", result.items[0].remark)
    }

    @Test
    fun `name equals plain value with remark`() {
        val result = parseQlEnvText("TOKEN=xyz789 空格备注")
        assertEquals(1, result.items.size)
        assertEquals("TOKEN", result.items[0].name)
        assertEquals("xyz789", result.items[0].value)
        assertEquals("空格备注", result.items[0].remark)
    }

    @Test
    fun `value containing equals sign is kept intact`() {
        val result = parseQlEnvText("SECRET=\"a=b=c\" 备注")
        assertEquals("a=b=c", result.items[0].value)
    }

    @Test
    fun `chinese variable names are supported`() {
        val result = parseQlEnvText("京东账号=123 测试")
        assertEquals("京东账号", result.items[0].name)
        assertEquals("123", result.items[0].value)
    }

    @Test
    fun `multiple lines parse with remarks optional`() {
        val text = """
            JD_COOKIE="pt_key=AAJk" 京东1
            API_KEY abc123
            备注行
        """.trimIndent()
        val result = parseQlEnvText(text)
        assertEquals(2, result.items.size)
        assertEquals("JD_COOKIE", result.items[0].name)
        assertEquals("pt_key=AAJk", result.items[0].value)
        assertEquals("京东1", result.items[0].remark)
        assertEquals("API_KEY", result.items[1].name)
        assertEquals("abc123", result.items[1].value)
        assertEquals("", result.items[1].remark)
    }

    @Test
    fun `blank lines and comments are skipped`() {
        val result = parseQlEnvText("\n  \n# comment\nA=B 备注\n")
        assertEquals(1, result.items.size)
        assertTrue(result.errors.isEmpty())
    }

    @Test
    fun `invalid lines are reported with line numbers`() {
        val result = parseQlEnvText("A=B\n=onlyvalue\nC=D")
        assertEquals(2, result.items.size)
        assertEquals(1, result.errors.size)
        assertTrue(result.errors[0].startsWith("第 2 行"))
    }

    @Test
    fun `empty value line is invalid`() {
        val result = parseQlEnvText("EMPTY=\nOK=1")
        assertEquals(1, result.items.size)
        assertEquals("OK", result.items[0].name)
        assertEquals(1, result.errors.size)
    }

    // ---------- 导出 ----------

    @Test
    fun `export produces name equals value remark lines`() {
        val envs = listOf(
            EnvVar(name = "A", value = "1", remark = "备注一"),
            EnvVar(name = "B", value = "two", remark = ""),
        )
        val result = toQlExportText(envs)
        assertEquals("A=1 备注一\nB=two", result.text)
        assertTrue(result.skippedNames.isEmpty())
    }

    @Test
    fun `export quotes remarks containing spaces`() {
        val envs = listOf(EnvVar(name = "A", value = "1", remark = "含 空格"))
        val result = toQlExportText(envs)
        assertEquals("A=1 \"含 空格\"", result.text)
    }

    @Test
    fun `export escapes newlines in values`() {
        val envs = listOf(EnvVar(name = "A", value = "line1\nline2", remark = ""))
        val result = toQlExportText(envs)
        assertEquals("A=line1\\nline2", result.text)
    }

    @Test
    fun `export skips invalid names and reports them`() {
        val envs = listOf(
            EnvVar(name = "OK_NAME", value = "1", remark = ""),
            EnvVar(name = "123BAD", value = "2", remark = ""),
            EnvVar(name = "BAD-NAME", value = "3", remark = ""),
        )
        val result = toQlExportText(envs)
        assertEquals("OK_NAME=1", result.text)
        assertEquals(listOf("123BAD", "BAD-NAME"), result.skippedNames)
    }

    @Test
    fun `isQlEnvName validates ql name rules`() {
        assertTrue(isQlEnvName("JD_COOKIE"))
        assertTrue(isQlEnvName("_underscore"))
        assertTrue(isQlEnvName("A1B2"))
        assertFalse(isQlEnvName("123BAD"))
        assertFalse(isQlEnvName("BAD-NAME"))
        assertFalse(isQlEnvName(""))
        assertFalse(isQlEnvName("含中文"))
    }

    // ---------- 模型 ----------

    @Test
    fun `env var parses groups from groups array`() {
        val json = org.json.JSONObject("""{"id":1,"name":"A","groups":["g1","g2"],"group":"g1,g2"}""")
        val env = EnvVar.fromJson(json)
        assertEquals(listOf("g1", "g2"), env.groups)
        assertEquals("g1", env.primaryGroup())
    }

    @Test
    fun `env var parses group from comma string fallback`() {
        val json = org.json.JSONObject("""{"id":1,"name":"A","group":"g1, g2"}""")
        val env = EnvVar.fromJson(json)
        assertEquals(listOf("g1", "g2"), env.groups)
    }
}
