package com.daidai.daidai_app

class PendingCallQueue<T> {
    private data class Entry<T>(
        val success: (T) -> Unit,
        val failure: (Throwable) -> Unit,
    )

    private val entries = mutableListOf<Entry<T>>()
    private var closeError: Throwable? = null
    var closed: Boolean = false
        private set
    val size: Int
        @Synchronized get() = entries.size

    @Synchronized
    fun add(success: (T) -> Unit, failure: (Throwable) -> Unit): Boolean {
        if (closed) {
            failure(closeError ?: IllegalStateException("Local panel client is closed"))
            return false
        }
        entries += Entry(success, failure)
        return true
    }

    @Synchronized
    fun succeedAll(value: T) {
        entries.toList().also { entries.clear() }.forEach {
            runCatching { it.success(value) }
        }
    }

    @Synchronized
    fun failAll(error: Throwable) {
        entries.toList().also { entries.clear() }.forEach {
            runCatching { it.failure(error) }
        }
    }

    @Synchronized
    fun close(error: Throwable) {
        closed = true
        closeError = error
        failAll(error)
    }
}
