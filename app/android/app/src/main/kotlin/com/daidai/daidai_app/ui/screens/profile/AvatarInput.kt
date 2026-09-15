package com.daidai.daidai_app.ui.screens.profile

import java.io.InputStream
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive

internal const val MAX_AVATAR_BYTES = 5 * 1024 * 1024

internal fun readBoundedAvatar(input: InputStream): ByteArray {
    val output = java.io.ByteArrayOutputStream()
    val buffer = ByteArray(8192)
    while (true) {
        currentCoroutineContext().ensureActive()
        val count = input.read(buffer, 0, minOf(buffer.size, MAX_AVATAR_BYTES + 1 - output.size()))
        if (count < 0) break
        if (count == 0) continue
        output.write(buffer, 0, count)
        require(output.size() <= MAX_AVATAR_BYTES) { "头像不能超过 5MB" }
    }
    require(output.size() > 0) { "头像文件为空" }
    return output.toByteArray()
}
