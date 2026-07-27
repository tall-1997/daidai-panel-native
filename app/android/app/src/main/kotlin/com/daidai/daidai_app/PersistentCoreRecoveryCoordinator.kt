package com.daidai.daidai_app

import java.util.concurrent.Executors

interface CoreRecoveryTaskRunner {
    fun execute(
        task: () -> Map<String, Any>,
        callback: (Result<Map<String, Any>>) -> Unit,
    )

    fun close()
}

class ExecutorCoreRecoveryTaskRunner : CoreRecoveryTaskRunner {
    private val worker = Executors.newSingleThreadExecutor()

    override fun execute(
        task: () -> Map<String, Any>,
        callback: (Result<Map<String, Any>>) -> Unit,
    ) {
        worker.execute { callback(runCatching(task)) }
    }

    override fun close() {
        worker.shutdown()
    }
}

class PersistentCoreRecoveryCoordinator(
    private val runner: CoreRecoveryTaskRunner,
    private val runtime: () -> Map<String, Any>,
    private val onResult: (Result<Map<String, Any>>) -> Unit,
    private val beforeResultCommit: () -> Unit = {},
) {
    private val lock = Any()
    private var closed = false
    private var recovering = false
    private var generation = 0L

    fun recoverAfterForegroundStarted(): Boolean {
        val currentGeneration = synchronized(lock) {
            if (closed || recovering) return false
            recovering = true
            ++generation
        }
        try {
            runner.execute(runtime) { result ->
                beforeResultCommit()
                synchronized(lock) {
                    if (closed || generation != currentGeneration) return@synchronized
                    recovering = false
                    onResult(result)
                }
            }
        } catch (error: RuntimeException) {
            synchronized(lock) {
                if (!closed && generation == currentGeneration) {
                    recovering = false
                    onResult(Result.failure(error))
                }
            }
        }
        return true
    }

    fun cancelPending() {
        synchronized(lock) {
            generation++
            recovering = false
        }
    }

    fun close() {
        val shouldClose = synchronized(lock) {
            if (closed) {
                false
            } else {
                closed = true
                generation++
                recovering = false
                true
            }
        }
        if (shouldClose) runner.close()
    }
}
