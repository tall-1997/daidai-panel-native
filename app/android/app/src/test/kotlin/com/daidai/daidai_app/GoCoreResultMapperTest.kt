package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class GoCoreResultMapperTest {
    @Test
    fun `ready result accepts strict loopback endpoint`() {
        val status = GoCoreResultMapper.toStatus(
            """{"ok":true,"id":7,"running":true,"status":"running","endpoint":"http://127.0.0.1:43210"}""",
            localToken = "local-token-value",
        )

        assertEquals("ready", status["phase"])
        assertEquals("http://127.0.0.1:43210", status["base_url"])
        assertEquals("local-token-value", status["local_token"])
    }

    @Test
    fun `ready result rejects non-loopback endpoint`() {
        val status = GoCoreResultMapper.toStatus(
            """{"ok":true,"running":true,"status":"running","endpoint":"http://192.168.1.2:43210"}""",
            localToken = "local-token-value",
        )

        assertEquals("failed", status["phase"])
        assertEquals("", status["base_url"])
        assertFalse(status.containsKey("local_token"))
    }

    @Test
    fun `failure does not expose Go error or token`() {
        val status = GoCoreResultMapper.toStatus(
            """{"ok":false,"status":"stopped","errorCode":"BOOTSTRAP_FAILED","error":"/private/path local-token-value"}""",
            localToken = "local-token-value",
        )

        assertEquals("failed", status["phase"])
        assertEquals("Embedded core failed", status["message"])
        assertFalse(status.values.any { it.toString().contains("local-token-value") })
        assertFalse(status.values.any { it.toString().contains("/private/path") })
    }

    @Test
    fun `failure exposes stable diagnostic stage before broad error code`() {
        val status = GoCoreResultMapper.toStatus(
            """{"ok":false,"status":"stopped","errorCode":"INVALID_DATA_DIR","failureStage":"recovery-converge"}""",
            localToken = "local-token-value",
        )

        assertEquals("failed", status["phase"])
        assertEquals("recovery-converge", status["failure_stage"])
        assertFalse(status.containsKey("local_token"))
    }
}
