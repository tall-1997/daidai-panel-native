package com.daidai.daidai_app

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.DeadObjectException
import android.os.IBinder
import android.os.RemoteException
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.atomic.AtomicBoolean

class LocalPanelServiceClient(context: Context) {
    private val appContext = context.applicationContext
    private val executor: ExecutorService = Executors.newSingleThreadExecutor()
    private data class ConnectedService(
        val service: ILocalPanelService,
        val binder: IBinder,
        val generation: Long,
    )

    private var service: ConnectedService? = null
    private val connectionState = ConnectionGenerationState()
    private var activeConnection: ServiceConnection? = null
    private var activeBinder: IBinder? = null
    private var activeDeathRecipient: IBinder.DeathRecipient? = null
    @Volatile
    private var closed = false
    private val pending = PendingCallQueue<ConnectedService>()

    fun ensureStarted(callback: (Result<String>) -> Unit) = call(retrySafe = true, { it.ensureStarted() }, callback)

    fun status(callback: (Result<String>) -> Unit) = call(retrySafe = true, { it.status() }, callback)

    fun restart(callback: (Result<String>) -> Unit) = call(retrySafe = false, { it.restart() }, callback)

    fun stop(callback: (Result<String>) -> Unit) = call(retrySafe = false, { it.stop() }, callback)

    fun setPersistentSchedulingEnabled(enabled: Boolean, callback: (Result<String>) -> Unit) =
        call(retrySafe = false, { it.setPersistentSchedulingEnabled(enabled) }, callback)

    fun createBrowserUrl(callback: (Result<String>) -> Unit) =
        call(retrySafe = false, { it.createBrowserUrl() }, callback)

    @Synchronized
    fun close() {
        if (closed) return
        closed = true
        connectionState.close()
        releaseActiveConnection()
        service = null
        pending.close(IllegalStateException("Local panel client is closed"))
        executor.shutdown()
    }

    private fun call(
        retrySafe: Boolean,
        operation: (ILocalPanelService) -> String,
        callback: (Result<String>) -> Unit,
    ) {
        val completion = OnceResultCallback(callback)

        fun dispatch(attempt: Int) {
            withService(
                success = { connected ->
                    try {
                        executor.execute {
                            if (closed) {
                                completion.complete(Result.failure(IllegalStateException("Local panel client is closed")))
                            } else {
                                val binderAlive = connected.binder.isBinderAlive && connected.binder.pingBinder()
                                val result = if (binderAlive) {
                                    runCatching { operation(connected.service) }
                                } else {
                                    Result.failure(DeadObjectException())
                                }
                                val error = result.exceptionOrNull()
                                if (isDeadRemoteCall(binderAlive, error)) {
                                    invalidateConnectedGeneration(connected)
                                    if (shouldRetryRemoteCall(retrySafe, attempt, binderAlive, error)) {
                                        dispatch(attempt + 1)
                                    } else {
                                        completion.complete(result)
                                    }
                                } else {
                                    completion.complete(result)
                                }
                            }
                        }
                    } catch (error: RejectedExecutionException) {
                        completion.complete(Result.failure(error))
                    }
                },
                failure = { completion.complete(Result.failure(it)) },
            )
        }
        dispatch(0)
    }

    @Synchronized
    private fun withService(
        success: (ConnectedService) -> Unit,
        failure: (Throwable) -> Unit,
    ) {
        if (closed) {
            failure(IllegalStateException("Local panel client is closed"))
            return
        }
        service?.let { connected ->
            success(connected)
            return
        }
        pending.add(success, failure)
        if (connectionState.binding) return
        bindNewGeneration()
    }

    private fun bindNewGeneration() {
        val generation = connectionState.beginBinding()
        val connection = createConnection(generation)
        activeConnection = connection
        val registered = runCatching {
            appContext.bindService(
                Intent(appContext, LocalPanelHostService::class.java),
                connection,
                Context.BIND_AUTO_CREATE,
            )
        }.getOrElse {
            failGeneration(generation, it)
            return
        }
        if (!registered) {
            failGeneration(generation, IllegalStateException("Local panel service binding failed"))
        }
    }

    private fun createConnection(generation: Long): ServiceConnection = object : ServiceConnection {
        private val deathRecipient = IBinder.DeathRecipient {
            failGeneration(generation, IllegalStateException("Local panel binder died"))
        }

        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            if (binder == null) {
                failGeneration(generation, IllegalStateException("Local panel service returned a null binder"))
                return
            }
            try {
                binder.linkToDeath(deathRecipient, 0)
            } catch (error: RemoteException) {
                failGeneration(generation, error)
                return
            }
            val connected = ILocalPanelService.Stub.asInterface(binder)
            val connectedService = ConnectedService(connected, binder, generation)
            synchronized(this@LocalPanelServiceClient) {
                if (closed || !connectionState.connected(generation)) {
                    runCatching { binder.unlinkToDeath(deathRecipient, 0) }
                    return
                }
                activeBinder = binder
                activeDeathRecipient = deathRecipient
                service = connectedService
            }
            pending.succeedAll(connectedService)
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            failGeneration(generation, IllegalStateException("Local panel service disconnected"))
        }

        override fun onNullBinding(name: ComponentName?) {
            failGeneration(generation, IllegalStateException("Local panel service returned a null binding"))
        }

        override fun onBindingDied(name: ComponentName?) {
            failGeneration(generation, IllegalStateException("Local panel service binding died"))
        }
    }

    @Synchronized
    private fun failGeneration(generation: Long, error: Throwable) {
        if (!connectionState.failed(generation)) return
        releaseActiveConnection()
        service = null
        pending.failAll(error)
    }

    @Synchronized
    private fun invalidateConnectedGeneration(connected: ConnectedService) {
        if (service !== connected || !connectionState.failed(connected.generation)) return
        releaseActiveConnection()
        service = null
    }

    private fun releaseActiveConnection() {
        val binder = activeBinder
        val deathRecipient = activeDeathRecipient
        if (binder != null && deathRecipient != null) {
            runCatching { binder.unlinkToDeath(deathRecipient, 0) }
        }
        activeBinder = null
        activeDeathRecipient = null
        activeConnection?.let { runCatching { appContext.unbindService(it) } }
        activeConnection = null
    }
}

internal fun isDeadRemoteCall(binderAlive: Boolean, error: Throwable?): Boolean =
    !binderAlive || error is RemoteException

internal fun shouldRetryRemoteCall(
    retrySafe: Boolean,
    attempt: Int,
    binderAlive: Boolean,
    error: Throwable?,
): Boolean = retrySafe && attempt == 0 && isDeadRemoteCall(binderAlive, error)

internal class OnceResultCallback<T>(private val callback: (Result<T>) -> Unit) {
    private val completed = AtomicBoolean(false)

    fun complete(result: Result<T>) {
        if (completed.compareAndSet(false, true)) callback(result)
    }
}
