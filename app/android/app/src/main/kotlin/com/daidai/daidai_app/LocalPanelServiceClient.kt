package com.daidai.daidai_app

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.os.RemoteException
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException

class LocalPanelServiceClient(context: Context) {
    private val appContext = context.applicationContext
    private val executor: ExecutorService = Executors.newSingleThreadExecutor()
    private var service: ILocalPanelService? = null
    private val connectionState = ConnectionGenerationState()
    private var activeConnection: ServiceConnection? = null
    private var activeBinder: IBinder? = null
    private var activeDeathRecipient: IBinder.DeathRecipient? = null
    @Volatile
    private var closed = false
    private val pending = PendingCallQueue<ILocalPanelService>()

    fun ensureStarted(callback: (Result<String>) -> Unit) = call({ it.ensureStarted() }, callback)

    fun status(callback: (Result<String>) -> Unit) = call({ it.status() }, callback)

    fun restart(callback: (Result<String>) -> Unit) = call({ it.restart() }, callback)

    fun stop(callback: (Result<String>) -> Unit) = call({ it.stop() }, callback)

    fun setPersistentSchedulingEnabled(enabled: Boolean, callback: (Result<String>) -> Unit) =
        call({ it.setPersistentSchedulingEnabled(enabled) }, callback)

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

    private fun call(operation: (ILocalPanelService) -> String, callback: (Result<String>) -> Unit) {
        withService(
            success = { connected ->
                try {
                    executor.execute {
                        if (closed) {
                            callback(Result.failure(IllegalStateException("Local panel client is closed")))
                        } else {
                            callback(runCatching { operation(connected) })
                        }
                    }
                } catch (error: RejectedExecutionException) {
                    callback(Result.failure(error))
                }
            },
            failure = { callback(Result.failure(it)) },
        )
    }

    @Synchronized
    private fun withService(
        success: (ILocalPanelService) -> Unit,
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
            synchronized(this@LocalPanelServiceClient) {
                if (closed || !connectionState.connected(generation)) {
                    runCatching { binder.unlinkToDeath(deathRecipient, 0) }
                    return
                }
                activeBinder = binder
                activeDeathRecipient = deathRecipient
                service = connected
            }
            pending.succeedAll(connected)
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
