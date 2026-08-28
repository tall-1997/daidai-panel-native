package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalPanelHostStatusTest {
    @Test
    fun `cron waits for startup and bound clients keep the service anchored`() {
        assertFalse(cronCanStop(initializing = true, cronIdle = null))
        assertTrue(cronCanStop(initializing = false, cronIdle = null))
        assertFalse(canStopIdleService(boundClients = 1, cronIdle = true))
        assertFalse(canStopIdleService(boundClients = 0, cronIdle = true, pendingCronTicks = 1))
        assertFalse(canStopIdleService(boundClients = 0, cronIdle = true, persistentForeground = true))
        assertFalse(canStopIdleService(boundClients = 0, cronIdle = true, browserSession = true))
        assertTrue(canStopIdleService(boundClients = 0, cronIdle = true))
    }

    @Test
    fun `persistent recovery failure preserves core phase and endpoint`() {
        val status = mergeLocalPanelHostStatus(
            coreStatus = mapOf(
                "phase" to "ready",
                "base_url" to "http://127.0.0.1:43211",
                "failure_stage" to "",
                "message" to "",
            ),
            foregroundServiceEnabled = true,
            schedulerStatus = mapOf("scheduler_host_state" to "active"),
            recoveryFailure = mapOf(
                "phase" to "failed",
                "base_url" to "",
                "recovery_phase" to "failed",
                "recovery_failure_stage" to "persistent_recovery",
                "recovery_message" to "Embedded core recovery failed",
            ),
        )

        assertEquals("ready", status["phase"])
        assertEquals("http://127.0.0.1:43211", status["base_url"])
        assertEquals("failed", status["recovery_phase"])
        assertEquals("persistent_recovery", status["recovery_failure_stage"])
    }
}
