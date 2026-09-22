package com.daidai.daidai_app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TotpTest {
    @Test
    fun `uri encodes reserved username characters`() {
        val uri = Totp.uri("admin@local", "MFRGGZDFMZTWQ2LK")
        assertTrue(uri.startsWith("otpauth://totp/DaiDaiPanel:admin%40local?"))
        assertTrue(uri.contains("secret=MFRGGZDFMZTWQ2LK"))
        assertTrue(uri.contains("issuer=DaiDaiPanel"))
    }

    @Test
    fun `validate accepts the current step and rejects garbage`() {
        val secret = Totp.generateSecret()
        val now = System.currentTimeMillis() / 1000L
        val current = Totp.codeAt(Base32.decode(secret), now / Totp.PERIOD_SECONDS)
        assertTrue(Totp.validate(secret, current, now))
        assertFalse(Totp.validate(secret, "000000", now))
        assertTrue(current.matches(Regex("^\\d{6}$")))
    }
}
