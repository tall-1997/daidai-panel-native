package com.daidai.daidai_app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalPanelRemoteCallPolicyTest {
    @Test
    fun `callback completes exactly once across retry outcomes`() {
        var calls = 0
        val completion = OnceResultCallback<String> { calls++ }

        completion.complete(Result.failure(IllegalStateException("dead first generation")))
        completion.complete(Result.success("replacement generation"))

        assertTrue(calls == 1)
    }

    @Test
    fun `safe dead call retries exactly once`() {
        assertTrue(shouldRetryRemoteCall(true, 0, false, null))
        assertFalse(shouldRetryRemoteCall(true, 1, false, null))
    }

    @Test
    fun `dead binder retries safe calls and mutation calls remain single shot`() {
        assertTrue(shouldRetryRemoteCall(true, 0, false, null))
        assertFalse(shouldRetryRemoteCall(false, 0, false, null))
        assertFalse(shouldRetryRemoteCall(false, 0, true, null))
    }
}
