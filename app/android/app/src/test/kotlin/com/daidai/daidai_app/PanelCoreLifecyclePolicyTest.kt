package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Test

class PanelCoreLifecyclePolicyTest {
    @Test
    fun `service destruction keeps process-owned core running`() {
        assertEquals(
            PanelCoreLifecyclePolicy.Action.KEEP_RUNNING,
            PanelCoreLifecyclePolicy.onServiceDestroyed(),
        )
    }

    @Test
    fun `explicit stop owns core shutdown`() {
        assertEquals(
            PanelCoreLifecyclePolicy.Action.STOP_CORE,
            PanelCoreLifecyclePolicy.onExplicitStop(),
        )
    }

    @Test
    fun `persistent disable keeps bound core running`() {
        assertEquals(
            PanelCoreLifecyclePolicy.Action.KEEP_RUNNING,
            PanelCoreLifecyclePolicy.onPersistentDisabled(),
        )
    }
}
