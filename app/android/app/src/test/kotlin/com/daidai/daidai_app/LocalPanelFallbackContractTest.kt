package com.daidai.daidai_app

import fi.iki.elonen.NanoHTTPD
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalPanelFallbackContractTest {
    @Test
    fun `degraded ready Go core remains the primary endpoint`() {
        val status = GoCoreResultMapper.toStatus(
            """{"ok":true,"id":8,"running":true,"status":"degraded-ready","endpoint":"http://127.0.0.1:43211"}""",
            localToken = "process-token",
        )

        assertFalse(LocalPanelRuntime.requiresFallback(status))
        assertEquals("http://127.0.0.1:43211", status["base_url"])
    }

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
    fun `request boundary requires exact process token host and origin`() {
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

        assertEquals(null, expected.rejection(valid))
        assertEquals(null, expected.rejection(valid.mapKeys { it.key.uppercase() }))
        assertEquals(NanoHTTPD.Response.Status.UNAUTHORIZED, expected.rejection(valid - "x-daidai-local-token"))
        assertEquals(NanoHTTPD.Response.Status.UNAUTHORIZED, expected.rejection(valid + ("x-daidai-local-token" to "wrong")))
        assertEquals(NanoHTTPD.Response.Status.BAD_REQUEST, expected.rejection(valid + ("host" to "localhost:5700")))
        assertEquals(NanoHTTPD.Response.Status.FORBIDDEN, expected.rejection(valid + ("origin" to "http://localhost:5700")))
        assertEquals(NanoHTTPD.Response.Status.FORBIDDEN, expected.rejection(valid - "origin"))
        assertEquals(
            NanoHTTPD.Response.Status.BAD_REQUEST,
            expected.rejection(valid + ("Host" to "127.0.0.1:5700")),
        )
        assertEquals(
            NanoHTTPD.Response.Status.UNAUTHORIZED,
            expected.rejection(valid + ("X-Daidai-Local-Token" to "process-token")),
        )
        assertEquals(
            NanoHTTPD.Response.Status.UNAUTHORIZED,
            expected.copy(localToken = "").rejection(valid + ("x-daidai-local-token" to "")),
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
    }
}
