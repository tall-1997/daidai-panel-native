package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Test

class AndroidSchedulerHostStatusTest {
    @Test
    fun `platform capabilities expose scheduler host adapter`() {
        val capabilities = AndroidSchedulerHostStatus.platformCapabilities(
            AndroidResourceGuarantee(
                state = "foreground_continuous",
                reasonCode = "ok",
                intervention = "",
            ),
        )
        val taskExecution = capabilities
            .getJSONObject("capabilities")
            .getJSONObject("task_execution")

        assertEquals(1, capabilities.getInt("version"))
        assertEquals("android.scheduler-host", taskExecution.getString("adapterId"))
        assertEquals(
            "enabled",
            capabilities.getJSONObject("capabilities").getJSONObject("system_restart").getString("state"),
        )
        assertEquals(
            "unsupported",
            capabilities.getJSONObject("capabilities").getJSONObject("system_update").getString("state"),
        )
        assertEquals(
            "unsupported",
            capabilities.getJSONObject("capabilities").getJSONObject("runtime_mutation").getString("state"),
        )
    }
}
