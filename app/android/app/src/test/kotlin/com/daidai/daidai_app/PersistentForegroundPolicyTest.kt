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
    fun `each recovery request idempotently retriggers foreground recovery`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = true)

        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())
        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())
        assertTrue(policy.foregroundActive)
    }

    @Test
    fun `transient session starts foreground when not persisted`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)

        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.beginTransientSession())
        assertTrue(policy.transientSessionActive)
        assertTrue(policy.foregroundActive)
    }

    @Test
    fun `transient session ends and stops foreground`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)
        policy.beginTransientSession()

        assertEquals(PersistentForegroundPolicy.Action.STOP_FOREGROUND, policy.endTransientSession())
        assertFalse(policy.transientSessionActive)
        assertFalse(policy.foregroundActive)
    }

    @Test
    fun `transient session is a no-op when persistence enabled`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = true)
        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())

        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.beginTransientSession())
        assertTrue(policy.foregroundActive)
        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.endTransientSession())
        assertTrue(policy.foregroundActive)
    }

    @Test
    fun `ending without a transient session is a no-op`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)

        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.endTransientSession())
        assertFalse(policy.transientSessionActive)
        assertFalse(policy.foregroundActive)
    }

    @Test
    fun `reconcile does not stop foreground during transient session`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)
        policy.beginTransientSession()

        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.update(false))
        assertTrue(policy.foregroundActive)
        assertTrue(policy.transientSessionActive)
    }

    @Test
    fun `repeated begin is idempotent while already in foreground`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)

        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.beginTransientSession())
        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.beginTransientSession())
        assertTrue(policy.foregroundActive)
    }

    @Test
    fun `disabling persistence then ending session stops foreground`() {
        val policy = PersistentForegroundPolicy(initiallyEnabled = false)
        policy.beginTransientSession()

        assertEquals(PersistentForegroundPolicy.Action.NONE, policy.update(false))
        assertEquals(PersistentForegroundPolicy.Action.STOP_FOREGROUND, policy.endTransientSession())
        assertFalse(policy.foregroundActive)
        assertFalse(policy.transientSessionActive)
    }
}
