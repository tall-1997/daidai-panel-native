package com.daidai.daidai_app

import android.content.Context

object LocalPanelRuntime {
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> =
        GoCoreBridge.ensureStarted(context.applicationContext, localToken)

    fun stop(localToken: String): Map<String, Any> = GoCoreBridge.stop(localToken)

    fun restart(context: Context, localToken: String): Map<String, Any> =
        GoCoreBridge.restart(context.applicationContext, localToken)

    fun status(localToken: String): Map<String, Any> = GoCoreBridge.status(localToken)
}
