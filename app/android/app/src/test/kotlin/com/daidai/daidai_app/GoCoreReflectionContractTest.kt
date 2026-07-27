package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Test

class GoCoreReflectionContractTest {
    @Test
    fun `gomobile reflection names match generated Java API`() {
        assertEquals("mobilecore.mobilecore.Mobilecore", GoCoreReflectionContract.CLASS_NAME)
        assertEquals("startCore", GoCoreReflectionContract.START_CORE)
        assertEquals("stopCore", GoCoreReflectionContract.STOP_CORE)
        assertEquals("coreStatus", GoCoreReflectionContract.CORE_STATUS)
        assertEquals("coreEndpoint", GoCoreReflectionContract.CORE_ENDPOINT)
    }
}
