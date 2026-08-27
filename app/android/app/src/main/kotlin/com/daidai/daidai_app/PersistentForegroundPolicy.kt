package com.daidai.daidai_app

class PersistentForegroundPolicy(initiallyEnabled: Boolean) {
    enum class Action { NONE, START_FOREGROUND, STOP_FOREGROUND }

    var enabled: Boolean = initiallyEnabled
        private set
    var foregroundActive: Boolean = false
        private set
    var transientSessionActive: Boolean = false
        private set

    fun recoveryAction(): Action = if (enabled) {
        foregroundActive = true
        Action.START_FOREGROUND
    } else {
        reconcile()
    }

    fun update(value: Boolean): Action {
        enabled = value
        return reconcile()
    }

    fun beginTransientSession(): Action {
        transientSessionActive = true
        if (enabled) {
            foregroundActive = true
            return Action.NONE
        }
        if (!foregroundActive) {
            foregroundActive = true
            return Action.START_FOREGROUND
        }
        return Action.NONE
    }

    fun endTransientSession(): Action {
        if (!transientSessionActive) return Action.NONE
        transientSessionActive = false
        if (enabled) {
            foregroundActive = true
            return Action.NONE
        }
        if (foregroundActive) {
            foregroundActive = false
            return Action.STOP_FOREGROUND
        }
        return Action.NONE
    }

    private fun reconcile(): Action = when {
        enabled && !foregroundActive -> {
            foregroundActive = true
            Action.START_FOREGROUND
        }
        !enabled && foregroundActive && !transientSessionActive -> {
            foregroundActive = false
            Action.STOP_FOREGROUND
        }
        else -> Action.NONE
    }
}
