package com.daidai.daidai_app

class ForegroundServiceState {
    @Volatile
    var enabled: Boolean = false
        private set

    fun update(value: Boolean): Boolean {
        if (value == enabled) return false
        enabled = value
        return true
    }
}
