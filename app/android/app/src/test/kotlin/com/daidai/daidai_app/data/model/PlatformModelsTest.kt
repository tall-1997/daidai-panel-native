package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PlatformModelsTest {

    @Test fun `mask is identified as masked token`() {
        assertTrue(isMaskedPlatformToken(PLATFORM_TOKEN_MASK))
        assertFalse(isMaskedPlatformToken("ghp_ABC"))
        assertFalse(isMaskedPlatformToken(""))
        assertFalse(isMaskedPlatformToken("***"))
    }

    @Test fun `platform info serialize with backend keys`() {
        val obj = JSONObject().apply {
            put("id", 1L)
            put("name", "github")
            put("label", "GitHub")
            put("icon", "icon_github")
        }
        val info = PlatformInfo.fromJson(obj)
        assertEquals("github", info.name)
        assertEquals("GitHub", info.displayName)
        assertEquals("icon_github", info.icon)
    }

    @Test fun `platform token uses mask when token hidden`() {
        val obj = JSONObject().apply {
            put("id", 1L)
            put("platform_id", 1L)
            put("name", "bot")
            put("token", PLATFORM_TOKEN_MASK)
            put("service", "")
            put("service_user", "bot")
            put("remarks", "")
            put("enabled", 1)
            put("created_at", "2026-01-01T00:00:00Z")
        }
        val token = PlatformTokenInfo.fromJson(obj)
        assertEquals(PLATFORM_TOKEN_MASK, token.displayToken)
        assertTrue(token.sealed)
        assertTrue(token.enabled)
    }

    @Test fun `token list from response handles Go payload`() {
        val payload = JSONObject().put(
            "data", JSONArray().apply {
                put(
                    JSONObject().apply {
                        put("id", 1L)
                        put("platform_id", 2L)
                        put("name", "A")
                        put("token", "secret-xxx")
                        put("service", "https://")
                        put("service_user", "alice")
                        put("remarks", "note")
                        put("enabled", 1)
                        put("created_at", "2026-01-01T00:00:00Z")
                    },
                )
            },
        )
        val list = PlatformTokenInfo.listFromResponse(payload.toString())
        assertEquals(1, list.size)
        assertEquals("secret-xxx", list[0].displayToken)
        assertFalse(list[0].sealed)
    }

    @Test fun `token list from response handles local payload`() {
        val payload = JSONObject().put(
            "data", JSONArray().apply {
                put(
                    JSONObject().apply {
                        put("id", 1L)
                        put("platform_id", 2L)
                        put("name", "A")
                        put("token", "********")
                        put("service", "")
                        put("service_user", "")
                        put("remarks", "")
                        put("enabled", 0)
                        put("created_at", "2026-01-01T00:00:00Z")
                    },
                )
            },
        )
        val list = PlatformTokenInfo.listFromResponse(payload.toString())
        assertEquals(1, list.size)
        assertEquals(PLATFORM_TOKEN_MASK, list[0].displayToken)
        assertTrue(list[0].sealed)
    }
}
