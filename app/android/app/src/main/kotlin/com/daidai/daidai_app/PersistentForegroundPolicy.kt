package com.daidai.daidai_app

class PersistentForegroundPolicy(initiallyEnabled: Boolean) {
    enum class Action { NONE, START_FOREGROUND, STOP_FOREGROUND }

    var enabled: Boolean = initiallyEnabled
        private set
    var foregroundActive: Boolean = false
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

    private fun reconcile(): Action = when {
        enabled && !foregroundActive -> {
            foregroundActive = true
            Action.START_FOREGROUND
        }
        !enabled && foregroundActive -> {
            foregroundActive = false
            Action.STOP_FOREGROUND
        }
        else -> Action.NONE
    }
}
