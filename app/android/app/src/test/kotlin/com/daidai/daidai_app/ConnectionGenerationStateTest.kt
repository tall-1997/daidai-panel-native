package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionGenerationStateTest {
    @Test
    fun `death invalidates generation and next call binds again`() {
        val state = ConnectionGenerationState()
        val first = state.beginBinding()
        assertTrue(state.connected(first))

        assertTrue(state.failed(first))
        assertFalse(state.failed(first))
        assertFalse(state.isCurrent(first))

        val second = state.beginBinding()
        assertTrue(second > first)
        assertTrue(state.binding)
        assertEquals(second, state.generation)
    }

    @Test
    fun `late disconnected callback cannot invalidate replacement binding`() {
        val state = ConnectionGenerationState()
        val first = state.beginBinding()
        state.failed(first)
        val second = state.beginBinding()

        assertFalse(state.failed(first))
        assertTrue(state.isCurrent(second))
        assertTrue(state.binding)
    }

    @Test
    fun `dead connected call invalidates only its own generation`() {
        val state = ConnectionGenerationState()
        val dead = state.beginBinding()
        assertTrue(state.connected(dead))
        assertTrue(state.failed(dead))

        val replacement = state.beginBinding()
        assertFalse(state.failed(dead))
        assertTrue(state.connected(replacement))
        assertTrue(state.isCurrent(replacement))
    }
}
