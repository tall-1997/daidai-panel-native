package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PendingCallQueueTest {
    @Test
    fun `connection failure completes every pending call`() {
        val queue = PendingCallQueue<String>()
        val failures = mutableListOf<String>()
        queue.add({}, { failures += it.message.orEmpty() })
        queue.add({}, { failures += it.message.orEmpty() })

        queue.failAll(IllegalStateException("binding died"))

        assertEquals(listOf("binding died", "binding died"), failures)
        assertEquals(0, queue.size)
    }

    @Test
    fun `close rejects new calls and completes queued calls`() {
        val queue = PendingCallQueue<String>()
        var firstFailure = ""
        var secondFailure = ""
        queue.add({}, { firstFailure = it.message.orEmpty() })

        queue.close(IllegalStateException("client closed"))
        val accepted = queue.add({}, { secondFailure = it.message.orEmpty() })

        assertFalse(accepted)
        assertEquals("client closed", firstFailure)
        assertEquals("client closed", secondFailure)
        assertTrue(queue.closed)
    }
}
