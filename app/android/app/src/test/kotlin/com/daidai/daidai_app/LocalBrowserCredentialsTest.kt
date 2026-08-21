package com.daidai.daidai_app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalBrowserCredentialsTest {
    @Test
    fun `ticket is single use and creates a browser session`() {
        var sequence = 0
        val credentials = LocalBrowserCredentials(
            nowMillis = { 1_000L },
            tokenFactory = { "token-${sequence++}" },
        )

        val ticket = credentials.issueTicket()
        val session = credentials.redeem(ticket)

        assertNotNull(session)
        assertTrue(credentials.hasSession(session!!))
        assertNull(credentials.redeem(ticket))
    }

    @Test
    fun `ticket expires after thirty seconds`() {
        var now = 1_000L
        val credentials = LocalBrowserCredentials(
            nowMillis = { now },
            tokenFactory = { "ticket" },
        )

        val ticket = credentials.issueTicket()
        now += 30_001L

        assertNull(credentials.redeem(ticket))
    }

    @Test
    fun `session expires after fifteen minutes and clear revokes it`() {
        var now = 1_000L
        var sequence = 0
        val credentials = LocalBrowserCredentials(
            nowMillis = { now },
            tokenFactory = { "token-${sequence++}" },
        )
        val firstSession = credentials.redeem(credentials.issueTicket())!!
        credentials.clear()
        assertFalse(credentials.hasSession(firstSession))

        val secondSession = credentials.redeem(credentials.issueTicket())!!
        now += 15 * 60_000L + 1L
        assertFalse(credentials.hasSession(secondSession))
    }
}
