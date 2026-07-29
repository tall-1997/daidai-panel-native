package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Test

class RootMethodChannelPolicyTest {
    @Test
    fun `legacy root methods are not implemented`() {
        listOf(
            "executeAsRoot",
            "readFileAsRoot",
            "listDirectoryAsRoot",
            "isRooted",
        ).forEach { method ->
            assertEquals(
                RootMethodDisposition.NOT_IMPLEMENTED,
                RootMethodChannelPolicy.disposition(method),
            )
        }
    }

    @Test
    fun `unknown root methods are not implemented`() {
        assertEquals(
            RootMethodDisposition.NOT_IMPLEMENTED,
            RootMethodChannelPolicy.disposition("unknown"),
        )
    }
}
