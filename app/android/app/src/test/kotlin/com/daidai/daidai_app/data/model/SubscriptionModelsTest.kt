package com.daidai.daidai_app.data.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.json.JSONObject

/**
 * 订阅模型与规则语义测试（F2）。
 *
 * 规则语义对齐 Go 后端 panel/server/service/subscription.go：
 *  - split：`,` / `|` 分隔、trim、空段丢弃、去重
 *  - 匹配为子串包含（strings.Contains），非正则
 *  - 白名单空 = 全部命中；含通配段 = 全部命中
 *  - 黑名单对白名单与依赖规则都生效
 *  - depend_on 参与检出但依赖文件不建任务（命中判定与 Go matchesSubscriptionDependency 对齐）
 */
class SubscriptionModelsTest {

    // ---------------------------------------------------------------- payload 序列化

    @Test
    fun `payload toJson carries rule fields`() {
        val json = SubscriptionWritePayload(
            name = "repo",
            url = "https://example.com/repo.git",
            targetPath = "/data/sub",
            enabled = true,
            branch = "main",
            schedule = "0 */6 * * *",
            whitelist = "scripts/utils, txt",
            blacklist = "node_modules|test",
            dependOn = "sendNotify",
            subPath = "scripts/",
            alias = "我的订阅",
            authType = "ssh",
            authUsername = "git",
            authToken = "",
            sshKeyId = 3L,
        ).toJson()

        assertEquals("repo", json.getString("name"))
        assertEquals("/data/sub", json.getString("save_dir"))
        assertEquals("main", json.getString("branch"))
        assertEquals("0 */6 * * *", json.getString("schedule"))
        assertEquals("scripts/utils, txt", json.getString("whitelist"))
        assertEquals("node_modules|test", json.getString("blacklist"))
        assertEquals("sendNotify", json.getString("depend_on"))
        assertEquals("scripts/", json.getString("sub_path"))
        assertEquals("我的订阅", json.getString("alias"))
        assertEquals("ssh", json.getString("auth_type"))
        assertEquals("git", json.getString("auth_username"))
        assertEquals(3L, json.getLong("ssh_key_id"))
        assertFalse(json.has("auth_token"))
    }

    @Test
    fun `payload toJson keeps blank ssh key id as explicit null`() {
        val json = SubscriptionWritePayload(
            name = "n", url = "u", sshKeyId = null,
        ).toJson()
        assertTrue(json.has("ssh_key_id"))
        assertTrue(json.isNull("ssh_key_id"))
    }

    @Test
    fun `payload toJson writes token only when non blank`() {
        val withToken = SubscriptionWritePayload(name = "n", url = "u", authToken = "tok").toJson()
        assertEquals("tok", withToken.getString("auth_token"))
        val blank = SubscriptionWritePayload(name = "n", url = "u", authToken = "  ").toJson()
        assertFalse(blank.has("auth_token"))
    }

    @Test
    fun `payload toJson always writes form-managed rule fields even when blank`() {
        val json = SubscriptionWritePayload(name = "n", url = "u").toJson()
        assertTrue(json.has("save_dir"))
        assertTrue(json.has("branch"))
        assertTrue(json.has("schedule"))
        assertTrue(json.has("whitelist"))
        assertTrue(json.has("blacklist"))
        assertTrue(json.has("depend_on"))
    }

    @Test
    fun `subscription fromJson parses rule fields from remote and local shapes`() {
        val remote = JSONObject()
            .put("id", 1L)
            .put("name", "n")
            .put("url", "u")
            .put("save_dir", "/s")
            .put("branch", "dev")
            .put("schedule", "0 0 * * *")
            .put("whitelist", "a,b")
            .put("blacklist", "c")
            .put("depend_on", "d")
            .put("sub_path", "sp/")
            .put("alias", "al")
            .put("auth_type", "token")
            .put("auth_username", "u2")
            .put("has_auth_token", true)
            .put("ssh_key_id", JSONObject.NULL)
            .put("force_overwrite", false)

        val sub = Subscription.fromJson(remote)
        assertEquals("dev", sub.branch)
        assertEquals("0 0 * * *", sub.schedule)
        assertEquals("a,b", sub.whitelist)
        assertEquals("c", sub.blacklist)
        assertEquals("d", sub.dependOn)
        assertEquals("sp/", sub.subPath)
        assertEquals("al", sub.alias)
        assertEquals("token", sub.authType)
        assertEquals("u2", sub.authUsername)
        assertTrue(sub.hasAuthToken)
        assertEquals(null, sub.sshKeyId)
        assertFalse(sub.forceOverwrite)
    }

    @Test
    fun `subscription fromJson reads local last_sync and ssh_key_id`() {
        val local = JSONObject()
            .put("id", 9L)
            .put("name", "n")
            .put("url", "u")
            .put("last_sync", "2026-09-15T00:00:00Z")
            .put("ssh_key_id", 7L)
        val sub = Subscription.fromJson(local)
        assertEquals("2026-09-15T00:00:00Z", sub.lastPullAt)
        assertEquals(7L, sub.sshKeyId)
    }

    // ---------------------------------------------------------------- 分隔符 / 子串语义

    @Test
    fun `split handles comma pipe blanks and duplicates`() {
        assertEquals(listOf("a", "b", "c"), SubscriptionRules.split("a,b|c"))
        assertEquals(listOf("a", "b"), SubscriptionRules.split(" a ,, b ||"))
        assertEquals(listOf("x"), SubscriptionRules.split("x,x,x"))
        assertEquals(emptyList<String>(), SubscriptionRules.split(""))
        assertEquals(emptyList<String>(), SubscriptionRules.split(null))
    }

    @Test
    fun `contains is substring not regex`() {
        assertTrue(SubscriptionRules.contains("scripts/utils/a.js", "utils"))
        assertTrue(SubscriptionRules.contains("foo/bar/baz.js", "bar"))
        assertFalse(SubscriptionRules.contains("foo/bar.js", "baz"))
        // 正则元字符按字面处理
        assertTrue(SubscriptionRules.contains("dir/2026.01/file.js", "2026.01"))
        assertFalse(SubscriptionRules.contains("dir/2026.01/file.js", "2026x01"))
    }

    @Test
    fun `normalize strips leading slashes and dot slash and normalizes backslash`() {
        assertEquals("scripts/a.js", SubscriptionRules.normalize("/scripts/a.js"))
        assertEquals("scripts/a.js", SubscriptionRules.normalize("./scripts/a.js"))
        assertEquals("scripts/a.js", SubscriptionRules.normalize("scripts\\a.js"))
    }

    @Test
    fun `whitelist empty matches everything`() {
        assertTrue(SubscriptionRules.matchesWhitelist("", "any/file.js"))
        assertTrue(SubscriptionRules.matchesWhitelist(null, "any/file.js"))
    }

    @Test
    fun `whitelist wildcard patterns match everything`() {
        assertTrue(SubscriptionRules.matchesWhitelist("*", "a/b.js"))
        assertTrue(SubscriptionRules.matchesWhitelist("all", "a/b.js"))
        assertTrue(SubscriptionRules.matchesWhitelist("全部", "a/b.js"))
    }

    @Test
    fun `whitelist matches by substring`() {
        val wl = "scripts/utils, txt"
        assertTrue(SubscriptionRules.matchesWhitelist(wl, "scripts/utils/tool.js"))
        assertTrue(SubscriptionRules.matchesWhitelist(wl, "scripts/other/note.txt"))
        assertFalse(SubscriptionRules.matchesWhitelist(wl, "scripts/other/file.js"))
    }

    @Test
    fun `blacklist excludes matches and applies regardless of whitelist`() {
        val bl = "node_modules|test"
        assertTrue(SubscriptionRules.matchesBlacklist(bl, "node_modules/x.js"))
        assertTrue(SubscriptionRules.matchesBlacklist(bl, "src/test/x.js"))
        assertFalse(SubscriptionRules.matchesBlacklist(bl, "src/app.js"))
        // 黑名单优先于白名单：白名单命中但黑名单也命中 → 不通过
        val whitelisted = SubscriptionRules.matchesWhitelist("src", "src/test/x.js")
        assertTrue(whitelisted)
        assertTrue(SubscriptionRules.matchesBlacklist("test", "src/test/x.js"))
    }

    @Test
    fun `dependOn matches by substring`() {
        assertTrue(SubscriptionRules.matchesDependOn("sendNotify, utils", "utils/helper.js"))
        assertTrue(SubscriptionRules.matchesDependOn("sendNotify, utils", "lib/sendNotify.js"))
        assertFalse(SubscriptionRules.matchesDependOn("sendNotify, utils", "main.js"))
    }

    @Test
    fun `rule semantics match go checkBlacklist composition`() {
        // Go matchesSubscriptionFilters = matchesWhitelist && checkBlacklist
        fun passes(wl: String?, bl: String?, path: String): Boolean =
            SubscriptionRules.matchesWhitelist(wl, path) && !SubscriptionRules.matchesBlacklist(bl, path)

        assertTrue(passes("utils", "test", "scripts/utils/main.js"))
        assertFalse(passes("utils", "test", "scripts/utils/test/x.js"))
        assertFalse(passes("utils", "test", "other/main.js"))
    }
}
