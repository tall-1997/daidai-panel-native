package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.LOG_TEXT_LIMIT
import com.daidai.daidai_app.data.model.LogStreamEvent
import com.daidai.daidai_app.data.model.appendLogText
import com.daidai.daidai_app.data.remote.PanelRequests
import com.daidai.daidai_app.data.remote.PanelSession
import java.io.IOException
import java.io.Reader
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody

/** Owns the response until consumption finishes; cancellation interrupts blocked reads. */
internal class TaskLogTransport(
    private val client: OkHttpClient,
    private val accessToken: String?,
    private val localToken: String?,
) {
    suspend fun <T> consume(url: String, method: String = "GET", json: String? = null,
        stream: Boolean = false, block: (Response) -> T): T = suspendCancellableCoroutine { continuation ->
        val builder = Request.Builder().url(url)
        PanelRequests.originOf(url)?.let { builder.header("Origin", it) }
        (PanelSession.accessToken() ?: accessToken)?.takeIf(String::isNotBlank)?.let {
            builder.header("Authorization", "Bearer $it")
        }
        (PanelSession.localToken() ?: localToken)?.takeIf(String::isNotBlank)?.let {
            builder.header("X-Daidai-Local-Token", it)
        }
        val body = (json ?: "{}").toRequestBody("application/json; charset=utf-8".toMediaType())
        builder.method(method, if (method in setOf("POST", "PUT", "PATCH")) body else null)
        val activeClient = if (stream) client.newBuilder().readTimeout(65, TimeUnit.SECONDS).build() else client
        val call = activeClient.newCall(builder.build())
        continuation.invokeOnCancellation { call.cancel() }
        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                if (continuation.isActive) continuation.resumeWithException(e)
            }
            override fun onResponse(call: Call, response: Response) {
                try {
                    val result = response.use {
                        if (!it.isSuccessful) throw IOException("请求失败（HTTP ${it.code}）")
                        block(it)
                    }
                    if (continuation.isActive) continuation.resume(result)
                } catch (error: Exception) {
                    if (continuation.isActive) continuation.resumeWithException(error)
                }
            }
        })
    }

    suspend fun json(url: String, method: String = "GET", body: String? = null): String =
        consume(url, method, body) { response ->
            val source = requireNotNull(response.body).source()
            // Bound even JSON responses before materializing their content.
            if (source.request(4L * 1024 * 1024 + 1)) throw IOException("响应超过 4 MiB，请使用原文导出")
            source.readUtf8()
        }
}

internal fun readLogEvents(reader: Reader, emit: (LogStreamEvent) -> Boolean) {
    var event = ""
    var cursor: Long? = null
    var data = ""
    var hasData = false
    val line = StringBuilder()
    fun dispatch(): Boolean {
        if (!hasData) return true
        val keepReading = emit(if (event == "done") LogStreamEvent(cursor = cursor, done = data)
            else LogStreamEvent(data, cursor))
        event = ""; cursor = null; data = ""; hasData = false
        return keepReading
    }
    fun acceptLine(): Boolean {
        val text = line.toString().removeSuffix("\r")
        line.setLength(0)
        if (text.isEmpty()) return dispatch()
        val field = text.substringBefore(':')
        val value = text.substringAfter(':', "").removePrefix(" ")
        when (field) {
            "event" -> event = value
            "id" -> cursor = value.toLongOrNull()
            "data" -> { data = appendLogText(data, (if (hasData) "\n" else "") + value); hasData = true }
        }
        return true
    }
    val buffer = CharArray(4096)
    while (true) {
        val count = reader.read(buffer)
        if (count < 0) break
        for (index in 0 until count) {
            if (buffer[index] == '\n') {
                if (!acceptLine()) return
            } else {
                if (line.length >= LOG_TEXT_LIMIT) throw IOException("单行日志超过预览上限，请导出原文")
                line.append(buffer[index])
            }
        }
    }
    if (line.isNotEmpty() && !acceptLine()) return
    dispatch()
}
