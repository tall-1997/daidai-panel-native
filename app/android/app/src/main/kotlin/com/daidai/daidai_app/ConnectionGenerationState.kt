package com.daidai.daidai_app

class ConnectionGenerationState {
    var generation: Long = 0
        private set
    var binding: Boolean = false
        private set

    fun beginBinding(): Long {
        generation++
        binding = true
        return generation
    }

    fun connected(value: Long): Boolean {
        if (!isCurrent(value) || !binding) return false
        binding = false
        return true
    }

    fun failed(value: Long): Boolean {
        if (!isCurrent(value)) return false
        generation++
        binding = false
        return true
    }

    fun isCurrent(value: Long): Boolean = value == generation

    fun close() {
        generation++
        binding = false
    }
}
