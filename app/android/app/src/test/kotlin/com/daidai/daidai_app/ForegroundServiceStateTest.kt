package com.daidai.daidai_app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForegroundServiceStateTest {
    @Test
    fun `state changes only when foreground value changes`() {
        val state = ForegroundServiceState()

        assertTrue(state.update(true))
        assertTrue(state.enabled)
        assertFalse(state.update(true))
        assertTrue(state.update(false))
        assertFalse(state.enabled)
    }
}
