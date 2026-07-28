package com.daidai.daidai_app

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network

class LocalPanelNetworkRecovery(
    private val context: Context,
    private val onRestored: () -> Unit,
) {
    private val manager = context.getSystemService(ConnectivityManager::class.java)
    private var registered = false
    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            if (LocalPanelHostService.isPersistentSchedulingEnabled(context)) {
                onRestored()
            }
        }
    }

    @Synchronized
    fun start() {
        if (registered) return
        manager.registerDefaultNetworkCallback(callback)
        registered = true
    }

    @Synchronized
    fun stop() {
        if (!registered) return
        runCatching { manager.unregisterNetworkCallback(callback) }
        registered = false
    }
}
