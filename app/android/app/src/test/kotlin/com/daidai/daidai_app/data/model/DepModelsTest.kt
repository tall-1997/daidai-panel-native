package com.daidai.daidai_app.data.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.json.JSONObject
import org.json.JSONArray

/**
 * 依赖模型契约测试（阶段 3-4）。
 *
 * 对齐 Go 后端 handler/deps.go + 本地 LocalPanelStore 字段约定。
 * 涵盖 DepItem / DepStatus / DepMirrors / 解析与边界语义。
 */
class DepModelsTest {

    // ---------------------------------------------------------------- DepItem.fromJson

    @Test
    fun `type normalized python from backend raw`() {
        assertEquals("python", DepItem.fromJson(JSONObject().put("type", "python").put("name", "pip").put("status", "installed")).type)
    }

    @Test
    fun `type normalized linux preserved`() {
        assertEquals("linux", DepItem.fromJson(JSONObject().put("type", "linux").put("name", "apk").put("status", "installed")).type)
    }

    @Test
    fun `unknown type falls back to node`() {
        assertEquals("node", DepItem.fromJson(JSONObject().put("type", "unknown").put("name", "x").put("status", "installed")).type)
    }

    @Test
    fun `python runtime used as version`() {
        val dep = DepItem.fromJson(JSONObject().put("type", "python").put("name", "requests").put("python_version", "2.31.0").put("status", "installed"))
        assertEquals("2.31.0", dep.version)
        assertEquals("2.31.0", dep.runtime)
    }

    @Test
    fun `non python shows status label as version`() {
        val dep = DepItem.fromJson(JSONObject().put("type", "nodejs").put("name", "lodash").put("status", "installed"))
        assertEquals("已安装", dep.version)
    }

    @Test
    fun `unknown status falls back to raw or dash`() {
        val unknown = DepItem.fromJson(JSONObject().put("type", "python").put("name", "x").put("status", ""))
        assertEquals("-", unknown.version)
    }

    @Test
    fun `active true for queued installing removing`() {
        assertTrue(DepItem.fromJson(JSONObject().put("type", "python").put("name", "x").put("status", "queued")).active)
        assertTrue(DepItem.fromJson(JSONObject().put("type", "python").put("name", "x").put("status", "installing")).active)
        assertTrue(DepItem.fromJson(JSONObject().put("type", "python").put("name", "x").put("status", "removing")).active)
        assertFalse(DepItem.fromJson(JSONObject().put("type", "python").put("name", "x").put("status", "installed")).active)
    }

    @Test
    fun `parseDepItems unwraps data array`() {
        val json = JSONObject().put("data", JSONArray().put(JSONObject().put("id", 1).put("type", "python").put("name", "requests").put("status", "installed")))
        val list = parseDepItems(json.toString())
        assertEquals(1, list.size)
        assertEquals("requests", list[0].name)
    }

    // ---------------------------------------------------------------- DepStatus.parse

    @Test
    fun `status parse reads data wrapper`() {
        val raw = JSONObject().put("data", JSONObject().put("id", 5L).put("status", "installing").put("log", "hello\nworld")).toString()
        val status = DepStatus.parse(raw)
        assertEquals(5L, status.id)
        assertEquals("installing", status.status)
        assertTrue(status.log.contains("hello\nworld"))
    }

    @Test
    fun `status parse truncates log to 128 KiB`() {
        val big = "x".repeat(129 * 1024)
        val raw = JSONObject().put("data", JSONObject().put("id", 1L).put("status", "failed").put("log", big)).toString()
        val status = DepStatus.parse(raw)
        assertEquals(128 * 1024, status.log.length)
    }

    @Test
    fun `status parse falls back to raw object`() {
        val raw = JSONObject().put("id", 7L).put("status", "cancelled").put("log", "cancelled").toString()
        val status = DepStatus.parse(raw)
        assertEquals(7L, status.id)
        assertEquals("cancelled", status.status)
    }

    @Test
    fun `dep status active matches deps contract`() {
        assertTrue(DepStatus(1L, "queued", "").active)
        assertTrue(DepStatus(1L, "installing", "").active)
        assertTrue(DepStatus(1L, "removing", "").active)
        assertFalse(DepStatus(1L, "installed", "").active)
        assertFalse(DepStatus(1L, "", "").active)
    }

    // ---------------------------------------------------------------- DepMirrors.parse + payload

    @Test
    fun `mirrors parse flat go shape`() {
        val raw = JSONObject()
            .put("pip_mirror", "https://pypi.tuna.tsinghua.edu.cn/simple")
            .put("npm_mirror", "https://registry.npmmirror.com")
            .put("linux_mirror", "")
            .put("linux_mirror_supported", true)
            .put("linux_mirror_label", "Ubuntu")
            .put("linux_mirror_message", "已切换为清华源")
            .toString()
        val mirrors = DepMirrors.parse(raw)
        assertEquals("https://pypi.tuna.tsinghua.edu.cn/simple", mirrors.pip)
        assertEquals("https://registry.npmmirror.com", mirrors.npm)
        assertEquals("", mirrors.linux)
        assertTrue(mirrors.linuxSupported)
        assertEquals("Ubuntu", mirrors.linuxLabel)
        assertEquals("已切换为清华源", mirrors.message)
    }

    @Test
    fun `mirrors parse data wrapper`() {
        val raw = JSONObject().put("data", JSONObject().put("pip_mirror", "").put("npm_mirror", "").put("linux_mirror_supported", false).put("linux_mirror_label", "Linux").put("linux_mirror_message", "")).toString()
        val mirrors = DepMirrors.parse(raw)
        assertEquals("", mirrors.pip)
        assertEquals("", mirrors.npm)
        assertFalse(mirrors.linuxSupported)
        assertEquals("Linux", mirrors.linuxLabel)
    }

    @Test
    fun `mirrors payload omits linux when unsupported`() {
        val mirrors = DepMirrors("a", "b", "c", false, "Linux", "")
        val payload = JSONObject(mirrors.payload())
        assertEquals("a", payload.getString("pip_mirror"))
        assertEquals("b", payload.getString("npm_mirror"))
        assertFalse(payload.has("linux_mirror"))
    }

    @Test
    fun `mirrors payload trims values`() {
        val mirrors = DepMirrors(" https://a ", " https://b ", " https://c ", true, "Linux", "")
        val payload = JSONObject(mirrors.payload())
        assertEquals("https://a", payload.getString("pip_mirror"))
        assertEquals("https://b", payload.getString("npm_mirror"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun `mirrors payload rejects non http pip`() {
        DepMirrors("ftp://bad", "", "", false, "Linux", "").payload()
    }

    // ---------------------------------------------------------------- DepManager

    @Test
    fun `manager fromId case insensitive`() {
        assertEquals(DepManager.Pip, DepManager.fromId("PIP"))
        assertEquals(DepManager.Npm, DepManager.fromId("npm"))
        assertEquals(DepManager.Npm, DepManager.fromId(null))
    }

    @Test
    fun `install request valid only when package name non blank`() {
        assertTrue(DepInstallRequest(packageName = "requests").isValid)
        assertFalse(DepInstallRequest(packageName = "").isValid)
        assertFalse(DepInstallRequest(packageName = "   ").isValid)
    }

    // ---------------------------------------------------------------- statusLabel

    @Test
    fun `statusLabel covers all known states`() {
        assertEquals("已安装", DepItem.statusLabel("installed"))
        assertEquals("安装中", DepItem.statusLabel("installing"))
        assertEquals("排队中", DepItem.statusLabel("queued"))
        assertEquals("失败", DepItem.statusLabel("failed"))
        assertEquals("卸载中", DepItem.statusLabel("removing"))
        assertEquals("已取消", DepItem.statusLabel("cancelled"))
        assertEquals("-", DepItem.statusLabel(""))
    }
}
