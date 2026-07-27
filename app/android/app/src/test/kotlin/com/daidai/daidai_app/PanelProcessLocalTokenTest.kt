package com.daidai.daidai_app

import java.util.Base64
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test

class PanelProcessLocalTokenTest {
    @Test
    fun `token is stable only for current process and contains 32 random bytes`() {
        val first = PanelProcessLocalToken.value
        val second = PanelProcessLocalToken.value

        assertSame(first, second)
        assertEquals(32, Base64.getUrlDecoder().decode(first).size)
    }
}
