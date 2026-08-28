package com.daidai.daidai_app

import fi.iki.elonen.NanoHTTPD
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.json.JSONArray
import org.json.JSONObject

class LocalPanelFallbackContractTest {
    @Test
    fun `full Kotlin fallback exposes a ready local endpoint`() {
        val status = LocalPanelRuntime.fallbackStatus(
            endpoint = "http://127.0.0.1:5700",
            localToken = "process-token",
            reason = "go_core_start:LinkageError",
        )

        assertEquals("ready", status["phase"])
        assertEquals("full", status["fallback_mode"])
        assertEquals("http://127.0.0.1:5700", status["base_url"])
        assertEquals("process-token", status["local_token"])
        assertEquals("go_core_start:LinkageError", status["failure_stage"])
        assertTrue(status["message"].toString().isNotBlank())
    }

    @Test
    fun `request boundary splits app internal, local browser and lan browser sources`() {
        val expected = LocalPanelHttpServer.RequestBoundary(
            authority = "127.0.0.1:5700",
            origin = "http://127.0.0.1:5700",
            localToken = "process-token",
        )
        val valid = mapOf(
            "host" to "127.0.0.1:5700",
            "origin" to "http://127.0.0.1:5700",
            "x-daidai-local-token" to "process-token",
        )

        // App 内部（带 token）：token 校验 + Origin 若存在须匹配
        assertEquals(null, expected.rejection(valid))
        assertEquals(null, expected.rejection(valid.mapKeys { it.key.uppercase() }))
        assertEquals(NanoHTTPD.Response.Status.UNAUTHORIZED, expected.rejection(valid + ("x-daidai-local-token" to "wrong")))
        assertEquals(NanoHTTPD.Response.Status.FORBIDDEN, expected.rejection(valid + ("origin" to "http://localhost:5700")))
        assertEquals(null, expected.rejection(valid - "origin"))

        // 非本机/局域网 host 一律拒绝
        assertEquals(NanoHTTPD.Response.Status.BAD_REQUEST, expected.rejection(valid + ("host" to "evil.example.com:5700")))
        assertEquals(
            NanoHTTPD.Response.Status.BAD_REQUEST,
            expected.rejection(valid + ("Host" to "127.0.0.1:5700")),
        )

        // LAN 浏览器（无 token 无 session）：放行，后续由 serve 强制 JWT
        assertEquals(null, expected.rejection(valid - "x-daidai-local-token"))
        assertEquals(null, expected.rejection(valid + ("host" to "192.168.1.10:5700") - "x-daidai-local-token"))

        // 本机浏览器（browserSession）：Origin 若存在须匹配
        assertEquals(null, expected.rejection(valid - "x-daidai-local-token" - "origin", browserSession = true))
        assertEquals(
            NanoHTTPD.Response.Status.FORBIDDEN,
            expected.rejection(valid - "x-daidai-local-token" + ("origin" to "http://evil.invalid"), browserSession = true),
        )
    }

    @Test
    fun `full Kotlin fallback allows store backed API routes`() {
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.GET, "/api/v1/health"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.GET, "/api/local/capabilities"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/auth/init"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/system/restore"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.GET, "/api/android/recovery-metadata"))

        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/tasks"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/scripts"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/envs"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/deps"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/configs"))
        assertTrue(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.POST, "/api/system/update"))
        assertFalse(LocalPanelHttpServer.isFallbackRouteAllowed(NanoHTTPD.Method.GET, "/not-api"))
        assertTrue(LocalPanelHttpServer.isOpenApiTokenCapabilityEnabled())
    }

    @Test
    fun `only bootstrap auth routes are public`() {
        assertTrue(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.GET, "/api/auth/check-init"))
        assertTrue(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.POST, "/api/auth/init"))
        assertTrue(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.POST, "/api/auth/login"))
        assertTrue(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.POST, "/api/auth/refresh"))
        assertTrue(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.GET, "/api/auth/captcha-config"))
        assertFalse(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.GET, "/api/auth/user"))
        assertFalse(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.GET, "/api/auth/user-list"))
        assertFalse(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.POST, "/api/auth/users"))
        assertFalse(LocalPanelHttpServer.isPublicAuthRoute(NanoHTTPD.Method.GET, "/api/auth/login"))
    }

    @Test
    fun `public API routes use an exact method and path allowlist`() {
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/health"))
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/v1/health?probe=ready"))
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/local/capabilities"))
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/system/public-version"))
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.POST, "/api/system/health-check"))
        assertTrue(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/android/recovery-metadata"))
        assertFalse(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.POST, "/api/health"))
        assertFalse(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/system/version"))
        assertFalse(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/system/public-version/details"))
        assertFalse(LocalPanelHttpServer.isPublicApiRoute(NanoHTTPD.Method.GET, "/api/android/recovery-metadata/private"))
    }

    @Test
    fun `ordinary users only receive self service security session routes`() {
        assertTrue(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.GET, "/api/security/sessions"))
        assertTrue(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.DELETE, "/api/security/sessions/others"))
        assertTrue(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.DELETE, "/api/security/sessions/42"))
        assertFalse(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.GET, "/api/security/login-logs"))
        assertFalse(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.GET, "/api/security/audit-logs"))
        assertFalse(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.POST, "/api/security/ip-whitelist"))
        assertFalse(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.DELETE, "/api/security/sessions/not-an-id"))
        assertTrue(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.GET, "/api/v1/security/sessions"))
        assertTrue(LocalPanelStore.isSelfServiceSecurityRoute(NanoHTTPD.Method.DELETE, "/api/v1/security/sessions/42"))
    }

    @Test
    fun `v1 security paths normalize to the existing security handlers`() {
        assertEquals("/api/security/sessions", LocalPanelStore.normalizeApiPath("/api/v1/security/sessions"))
        assertEquals("/api/security/sessions", LocalPanelStore.normalizeApiPath("/api/security/sessions?active=true"))
    }

    @Test
    fun `OpenAPI backup scope covers backup and restore without granting system`() {
        assertEquals("backup", LocalPanelStore.openApiResource("/api/system/backups"))
        assertEquals("backup", LocalPanelStore.openApiResource("/api/v1/system/backup/download"))
        assertEquals("backup", LocalPanelStore.openApiResource("/api/system/restore"))
        assertEquals("backup", LocalPanelStore.openApiResource("/api/system/restore/progress"))
        assertEquals("system", LocalPanelStore.openApiResource("/api/system/info"))
        assertTrue(LocalPanelStore.isOpenApiScopeAllowed("backup", "/api/system/backups"))
        assertTrue(LocalPanelStore.isOpenApiScopeAllowed("backup", "/api/system/restore"))
        assertFalse(LocalPanelStore.isOpenApiScopeAllowed("system", "/api/system/backups"))
        assertFalse(LocalPanelStore.isOpenApiScopeAllowed("system", "/api/system/restore"))
        assertTrue(LocalPanelStore.isOpenApiScopeAllowed("system", "/api/system/info"))
    }

    @Test
    fun `batch mutation response keeps web and flutter counters aligned to affected rows`() {
        val payload = LocalPanelStore.batchMutationPayload(listOf(2, 9), affected = 1)
        assertEquals(1, payload.getInt("count"))
        assertEquals(1, payload.getInt("success_count"))
        assertEquals(1, payload.getInt("affected"))
        assertEquals(1, payload.getJSONObject("data").getInt("count"))
        assertEquals(2, payload.getJSONObject("data").getJSONArray("ids").length())
    }

    @Test
    fun `host parser accepts bracketed and bare private IPv6`() {
        assertTrue(LocalPanelHttpServer.isLocalOrLanHost("[::1]:5700"))
        assertTrue(LocalPanelHttpServer.isLocalOrLanHost("::1"))
        assertTrue(LocalPanelHttpServer.isLocalOrLanHost("[fd12:3456::1]:5700"))
        assertTrue(LocalPanelHttpServer.isLocalOrLanHost("fe80::1"))
        assertFalse(LocalPanelHttpServer.isLocalOrLanHost("[::1]invalid"))
        assertFalse(LocalPanelHttpServer.isLocalOrLanHost("[2001:db8::1]:5700"))
    }

    @Test
    fun `identity session migration advances schema`() {
        assertEquals(19, LocalPanelStore.SCHEMA_VERSION)
        assertTrue(LocalPanelStore.requiresIdentitySessionMigration(18))
        assertFalse(LocalPanelStore.requiresIdentitySessionMigration(19))
        assertTrue(LocalPanelStore.needsInitialization(0))
        assertFalse(LocalPanelStore.needsInitialization(1))
        assertTrue(LocalPanelStore.isSupportedUserRole("admin"))
        assertTrue(LocalPanelStore.isSupportedUserRole("operator"))
        assertFalse(LocalPanelStore.isSupportedUserRole("owner"))
    }

    @Test
    fun `v19 identity schema and migration preserve only determinable ownership`() {
        val usersSql = LocalPanelStore.LOCAL_USERS_CREATE_SQL
        assertTrue(usersSql.contains("role TEXT NOT NULL DEFAULT 'admin'"))
        assertTrue(usersSql.contains("enabled INTEGER NOT NULL DEFAULT 1"))
        assertTrue(usersSql.contains("avatar_url TEXT NOT NULL DEFAULT ''"))

        val sessionSql = LocalPanelStore.IDENTITY_SESSION_COPY_SQL
        assertTrue(sessionSql.contains("JOIN security_sessions ON security_sessions.access_token=local_sessions_legacy.access_token"))
        assertTrue(sessionSql.contains("WHERE security_sessions.user_id>0"))
        assertFalse(sessionSql.contains("ORDER BY id LIMIT 1"))
        assertFalse(sessionSql.contains("COALESCE"))
    }

    @Test
    fun `v18 refresh migration grants a thirty day compatibility period`() {
        val upgradedAt = Instant.parse("2026-08-27T10:15:30Z")
        assertEquals("2026-09-26T10:15:30Z", LocalPanelStore.legacyRefreshExpiry(upgradedAt))
        assertTrue(LocalPanelStore.needsLegacyRefreshExpiry("", "2026-08-28T10:15:30Z"))
        assertTrue(LocalPanelStore.needsLegacyRefreshExpiry("2026-08-28T10:15:30Z", "2026-08-28T10:15:30Z"))
        assertFalse(LocalPanelStore.needsLegacyRefreshExpiry("2026-09-26T10:15:30Z", "2026-08-28T10:15:30Z"))
    }

    @Test
    fun `dashboard aborted contract and success denominator match Go core`() {
        val daily = LocalPanelStore.dashboardDailyStat("08-27", 3, 1, 2)
        assertEquals(3L, daily.getLong("success"))
        assertEquals(1L, daily.getLong("failed"))
        assertEquals(2L, daily.getLong("aborted"))
        assertEquals(75.0, LocalPanelStore.dashboardSuccessRate(3, 1), 0.0)
        assertEquals(0.0, LocalPanelStore.dashboardSuccessRate(0, 0), 0.0)
    }

    @Test
    fun `batch task IDs require unique positive integral values`() {
        assertEquals(listOf(1L, 2L), LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray(listOf(1, 2)))))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject()))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray())))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray(listOf(1, 1)))))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray(listOf(0, 2)))))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray(listOf(1.5)))))
        assertEquals(null, LocalPanelStore.validatedBatchTaskIds(JSONObject().put("task_ids", JSONArray(listOf(1, 2))), maximum = 1))
    }

    @Test
    fun `batch task routes require exact action and method`() {
        assertTrue(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.POST, "/tasks/batch/run", "run"))
        assertTrue(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.PUT, "/tasks/batch/disable", "disable"))
        assertTrue(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.DELETE, "/tasks/batch/delete", "delete"))
        assertFalse(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.PUT, "/tasks/batch/run", "run"))
        assertFalse(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.POST, "/tasks/batch/run/extra", "run"))
        assertFalse(LocalPanelStore.isValidTaskBatchRequest(NanoHTTPD.Method.POST, "/tasks/batch/retry", "retry"))
    }
}
