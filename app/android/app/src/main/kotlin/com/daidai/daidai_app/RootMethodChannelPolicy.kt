package com.daidai.daidai_app

internal enum class RootMethodDisposition {
    NOT_IMPLEMENTED,
}

internal object RootMethodChannelPolicy {
    fun disposition(@Suppress("UNUSED_PARAMETER") method: String): RootMethodDisposition {
        return RootMethodDisposition.NOT_IMPLEMENTED
    }
}
