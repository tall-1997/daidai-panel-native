package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Test

class AndroidResourceProtectionTest {
    @Test
    fun `storage pressure limits low priority guarantee first`() {
        val guarantee = AndroidResourceProtection.evaluate(
            AndroidResourceSnapshot(
                batteryPercent = 80,
                batteryCharging = true,
                thermalStatus = "none",
                lowMemory = false,
                availableMemoryBytes = 2_000_000_000,
                availableStorageBytes = 128_000_000,
            ),
        )

        assertEquals("resource_limited", guarantee.state)
        assertEquals("storage_low", guarantee.reasonCode)
    }

    @Test
    fun `healthy resources produce foreground continuous guarantee`() {
        val guarantee = AndroidResourceProtection.evaluate(
            AndroidResourceSnapshot(
                batteryPercent = 80,
                batteryCharging = false,
                thermalStatus = "none",
                lowMemory = false,
                availableMemoryBytes = 2_000_000_000,
                availableStorageBytes = 2_000_000_000,
            ),
        )

        assertEquals("foreground_continuous", guarantee.state)
        assertEquals("ok", guarantee.reasonCode)
    }
}
