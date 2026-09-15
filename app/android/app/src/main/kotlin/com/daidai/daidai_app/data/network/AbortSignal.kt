package com.daidai.daidai_app.data.network

/** 请求取消信号，用于 SSE 流的取消控制。 */
class AbortSignal {
    private var cancelled = false
    private val listeners = mutableListOf<() -> Unit>()

    fun cancel() {
        if (!cancelled) {
            cancelled = true
            listeners.forEach { it() }
        }
    }

    fun onCancel(listener: () -> Unit) {
        if (cancelled) {
            listener()
        } else {
            listeners.add(listener)
        }
    }

    val isCancelled: Boolean get() = cancelled
}
