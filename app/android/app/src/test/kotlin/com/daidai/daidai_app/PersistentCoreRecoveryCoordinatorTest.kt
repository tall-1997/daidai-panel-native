package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class PersistentCoreRecoveryCoordinatorTest {
    @Test
    fun `long core start remains pending until it naturally completes`() {
        val runner = FakeCoreRecoveryTaskRunner()
        var starts = 0
        var status: Map<String, Any>? = null
        val coordinator = PersistentCoreRecoveryCoordinator(
            runner = runner,
            runtime = { starts++; mapOf("phase" to "ready") },
            onResult = { status = it.getOrThrow() },
        )
        val policy = PersistentForegroundPolicy(initiallyEnabled = true)

        assertEquals(PersistentForegroundPolicy.Action.START_FOREGROUND, policy.recoveryAction())
        assertTrue(coordinator.recoverAfterForegroundStarted())
        assertEquals(0, starts)
        assertEquals(null, status)

        runner.runPending()

        assertEquals(1, starts)
        assertEquals("ready", status?.get("phase"))
    }

    @Test
    fun `close suppresses late recovery result`() {
        val runner = FakeCoreRecoveryTaskRunner()
        var completed = false
        val coordinator = PersistentCoreRecoveryCoordinator(
            runner = runner,
            runtime = { mapOf("phase" to "ready") },
            onResult = { completed = true },
        )

        coordinator.recoverAfterForegroundStarted()
        coordinator.close()
        runner.runPending()

        assertFalse(completed)
        assertTrue(runner.closed)
    }

    @Test
    fun `disable cancels stale result and allows a new recovery`() {
        val runner = FakeCoreRecoveryTaskRunner()
        var completions = 0
        val coordinator = PersistentCoreRecoveryCoordinator(
            runner = runner,
            runtime = { mapOf("phase" to "ready") },
            onResult = { completions++ },
        )

        coordinator.recoverAfterForegroundStarted()
        coordinator.cancelPending()
        runner.runPending()
        assertEquals(0, completions)

        assertTrue(coordinator.recoverAfterForegroundStarted())
        runner.runPending()
        assertEquals(1, completions)
    }

    @Test
    fun `runtime failure is delivered as a queryable recovery result`() {
        val runner = FakeCoreRecoveryTaskRunner()
        var failureMessage = ""
        val coordinator = PersistentCoreRecoveryCoordinator(
            runner = runner,
            runtime = { error("core failed") },
            onResult = { failureMessage = it.exceptionOrNull()?.message.orEmpty() },
        )

        coordinator.recoverAfterForegroundStarted()
        runner.runPending()

        assertEquals("core failed", failureMessage)
    }

    @Test
    fun `cancel between result arrival and commit suppresses callback`() {
        val resultArrived = CountDownLatch(1)
        val allowCommit = CountDownLatch(1)
        var completed = false
        val runner = ThreadedCoreRecoveryTaskRunner()
        val coordinator = PersistentCoreRecoveryCoordinator(
            runner = runner,
            runtime = { mapOf("phase" to "ready") },
            onResult = { completed = true },
            beforeResultCommit = {
                resultArrived.countDown()
                allowCommit.await(2, TimeUnit.SECONDS)
            },
        )

        coordinator.recoverAfterForegroundStarted()
        assertTrue(resultArrived.await(2, TimeUnit.SECONDS))
        coordinator.cancelPending()
        allowCommit.countDown()
        runner.awaitCompletion()

        assertFalse(completed)
        coordinator.close()
    }

    private class FakeCoreRecoveryTaskRunner : CoreRecoveryTaskRunner {
        private var task: (() -> Map<String, Any>)? = null
        private var callback: ((Result<Map<String, Any>>) -> Unit)? = null
        var closed = false
            private set

        override fun execute(
            task: () -> Map<String, Any>,
            callback: (Result<Map<String, Any>>) -> Unit,
        ) {
            this.task = task
            this.callback = callback
        }

        fun runPending() {
            callback?.invoke(runCatching { checkNotNull(task).invoke() })
        }

        override fun close() {
            closed = true
        }
    }

    private class ThreadedCoreRecoveryTaskRunner : CoreRecoveryTaskRunner {
        private val completed = CountDownLatch(1)

        override fun execute(
            task: () -> Map<String, Any>,
            callback: (Result<Map<String, Any>>) -> Unit,
        ) {
            Thread {
                try {
                    callback(runCatching(task))
                } finally {
                    completed.countDown()
                }
            }.start()
        }

        fun awaitCompletion() {
            assertTrue(completed.await(2, TimeUnit.SECONDS))
        }

        override fun close() = Unit
    }
}
