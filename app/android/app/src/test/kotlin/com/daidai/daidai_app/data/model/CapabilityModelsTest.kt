package com.daidai.daidai_app.data.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.json.JSONObject
import org.json.JSONArray

/**
 * 流式解析与有界缓存契约测试（阶段 3-4 / 3-5）。
 *
 * 对齐 Go backend：
 *   - subscription handler: PullStream / LogStream SSE 帧格式
 *   - subscription model: SubLog.ToDict 字段名
 *   - dep handler: LogStream SSE 帧格式
 *   - localPanelStore: local_subscriptions / dependencies 本地 JSON 形状
 */
class CapabilityModelsTest {

    // ---------------------------------------------------------------- CapabilitySseParser

    @Test
    fun `parser emits simple event`() {
        val parser = CapabilitySseParser()
        parser.line("data: hello")
        val event = parser.line("")
        assertEquals("message", event!!.type)
        assertEquals("hello", event.data)
    }

    @Test
    fun `parser handles multi line data`() {
        val parser = CapabilitySseParser()
        parser.line("data: line 1")
        parser.line("data: line 2")
        val event = parser.line("")
        assertEquals("line 1\nline 2", event!!.data)
    }

    @Test
    fun `parser reads id and event fields`() {
        val parser = CapabilitySseParser()
        parser.line("id: 42")
        parser.line("event: done")
        parser.line("data: finished")
        val event = parser.line("")
        assertEquals("done", event!!.type)
        assertEquals("42", event.id)
        assertEquals("finished", event.data)
    }

    @Test
    fun `parser ignores comment lines`() {
        val parser = CapabilitySseParser()
        parser.line(": stream-open")
        parser.line("data: after comment")
        val event = parser.line("")
        assertEquals("message", event!!.type)
        assertEquals("after comment", event.data)
    }

    @Test
    fun `parser strips BOM on first line`() {
        val parser = CapabilitySseParser()
        parser.line("\uFEFFdata: bom")
        val event = parser.line("")
        assertEquals("message", event!!.type)
        assertEquals("bom", event.data)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `parser rejects event exceeding 256 KiB`() {
        val parser = CapabilitySseParser()
        val big = "x".repeat(300 * 1024)
        parser.line("data: $big")
        parser.line("")
    }

    @Test
    fun `parser ignores null id per spec`() {
        val parser = CapabilitySseParser()
        parser.line("id: a\x00b")
        parser.line("data: x")
        val event = parser.line("")
        assertEquals("", event!!.id)
    }

    @Test
    fun `parser default event type is message`() {
        val parser = CapabilitySseParser()
        parser.line("data: hi")
        val event = parser.line("")
        assertEquals("message", event!!.type)
    }

    // ---------------------------------------------------------------- boundedCapabilityLog

    @Test
    fun `bounded cap truncates to 128 KiB`() {
        val prev = "x".repeat(128 * 1024)
        val addition = "hello"
        val result = boundedCapabilityLog(prev, addition)
        assertEquals(128 * 1024, result.length)
        assertTrue(result.endsWith("hello"))
    }

    @Test
    fun `bounded cap keeps all under limit`() {
        val result = boundedCapabilityLog("previous", " small")
        assertEquals("previous small", result)
    }

    // ---------------------------------------------------------------- SubscriptionLogPage.parse

    @Test
    fun `parse paginated response`() {
        val data = JSONArray()
            .put(JSONObject().put("id", 1L).put("content", "line A").put("created_at", "2026-09-15T00:00:00Z").put("level", "info").put("operation_id", "op-1"))
            .put(JSONObject().put("id", 2L).put("content", "line B").put("created_at", "2026-09-15T00:01:00Z").put("status", "error").put("operation_id", "op-2"))
        val raw = JSONObject().put("data", data).put("total", 20).put("page", 1).put("page_size", 2).toString()
        val page = SubscriptionLogPage.parse(raw, 1)
        assertEquals(2, page.entries.size)
        assertEquals(20, page.total)
        assertEquals(1, page.page)
        assertEquals("line A", page.entries[0].content)
        assertEquals("line B", page.entries[1].content)
        assertEquals("2026-09-15T00:00:00Z", page.entries[0].createdAt)
        assertEquals("info", page.entries[0].status)
        assertEquals("error", page.entries[1].status)
        assertTrue(page.hasNext)
    }

    @Test
    fun `parse local shape uses message fallback and no total`() {
        val data = JSONArray()
            .put(JSONObject().put("id", 1L).put("message", "local msg").put("created_at", "t").put("level", "debug").put("operation_id", ""))
        val raw = JSONObject().put("data", data).toString()
        val page = SubscriptionLogPage.parse(raw, 1)
        assertEquals(1, page.entries.size)
        assertEquals("local msg", page.entries[0].content)
        assertEquals(1, page.total)
        assertFalse(page.hasNext)
    }

    @Test
    fun `parse non paginated local array client side slices`() {
        val data = JSONArray()
        for (i in 1..25) data.put(JSONObject().put("id", i.toLong()).put("content", "msg $i").put("created_at", "t").put("status", "").put("operation_id", ""))
        val raw = JSONObject().put("data", data).toString()
        val page1 = SubscriptionLogPage.parse(raw, 1)
        val page2 = SubscriptionLogPage.parse(raw, 2)
        assertEquals(20, page1.entries.size)
        assertEquals(5, page2.entries.size)
        assertTrue(page1.hasNext)
        assertFalse(page2.hasNext)
    }

    // ---------------------------------------------------------------- Subscription.fromJson

    @Test
    fun `subscription fromJson parses save_dir and target_path fallback`() {
        val json = JSONObject().put("save_dir", "/data/a").put("target_path", "/data/b")
        val sub = Subscription.fromJson(json)
        assertEquals("/data/a", sub.targetPath)
    }

    @Test
    fun `subscription fromJson lastPullAt prefers last_pull_at then last_sync`() {
        val json = JSONObject().put("last_pull_at", "2026-09-15T00:00:00Z").put("last_sync", "2026-09-14T00:00:00Z")
        val sub = Subscription.fromJson(json)
        assertEquals("2026-09-15T00:00:00Z", sub.lastPullAt)
    }

    @Test
    fun `subscription fromJson uses last_sync when last_pull_at absent`() {
        val json = JSONObject().put("last_sync", "2026-09-14T00:00:00Z")
        val sub = Subscription.fromJson(json)
        assertEquals("2026-09-14T00:00:00Z", sub.lastPullAt)
    }

    @Test
    fun `subscription fromJson ssh_key_id null preserves null`() {
        val json = JSONObject().put("ssh_key_id", JSONObject.NULL)
        val sub = Subscription.fromJson(json)
        assertEquals(null, sub.sshKeyId)
    }

    @Test
    fun `subscription fromJson ssh_key_id zero drops`() {
        val json = JSONObject().put("ssh_key_id", 0L)
        val sub = Subscription.fromJson(json)
        assertEquals(null, sub.sshKeyId)
    }

    @Test
    fun `subscription fromJson hasAuthToken requires field presence`() {
        val json = JSONObject().put("has_auth_token", true)
        val sub = Subscription.fromJson(json)
        assertTrue(sub.hasAuthToken)
        val json2 = JSONObject()
        val sub2 = Subscription.fromJson(json2)
        assertFalse(sub2.hasAuthToken)
    }
}
