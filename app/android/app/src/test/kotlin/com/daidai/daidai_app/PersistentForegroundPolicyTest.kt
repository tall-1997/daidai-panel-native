package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PersistentForegroundPolicyTest {
    @Test
    fun `process recreation restores persisted foreground selection`() {
        val restored = PersistentForegroundPolicy(initiallyEnabled = true)

        assertTrue(restored.enabled)
        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, restored.recoveryAction())
        assertTrue(restored.foregroundActive)
    }

    @Test
    fun `disable persists and requests foreground removal`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = true)
        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())

        assertEquals(PersistentForegroundPolicy.Action.STOP_FOREGROUND, policy.update(false))
        assertFalse(policy.enabled)
        assertFalse(policy.foregroundActive)
        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.recoveryAction())
    }

    @Test
    fun `repeated recreation recovery is idempotent within process`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = true)

        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())
        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.recoveryAction())
    }
}
