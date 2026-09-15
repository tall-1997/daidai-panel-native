package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import com.daidai.daidai_app.data.remote.PanelSession
import java.io.IOException
import java.io.OutputStream
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.channels.trySendBlocking
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okio.BufferedSource

internal data class CapabilityEvent(val type: String, val data: String, val id: String = "")

/** SSE framing is independent of the JSON/plain-text payload used by each backend. */
internal class CapabilitySseParser {
    private var type = "message"
    private var id = ""
    private val data = StringBuilder()
    private var hasData = false

    fun line(raw: String): CapabilityEvent? {
        val line = raw.removeSuffix("\r")
        if (line.isEmpty()) {
            val event = if (hasData) CapabilityEvent(type, data.toString().removeSuffix("\n"), id) else null
            type = "message"
            data.setLength(0)
            hasData = false
            return event
        }
        val field = line.substringBefore(':')
        val value = line.substringAfter(':', "").removePrefix(" ")
        when (field) {
            "event" -> type = value
            "id" -> if ('\u0000' !in value) id = value
            "data" -> {
                require(data.length + value.length <= 262144) { "单条日志事件超过 256 KiB" }
                data.append(value).append('\n')
                hasData = true
            }
        }
        return null
    }
}

internal fun boundedCapabilityLog(previous: String, addition: String): String =
    (previous + addition).takeLast(128 * 1024)

/** Owns response bodies through cancellation, including a blocked socket read. */
internal class CapabilityRequests(
    private val client: OkHttpClient,
    private val accessToken: String?,
    private val localToken: String?,
) {
    private fun <T> read(
        method: String,
        url: String,
        json: String? = null,
        stream: Boolean = false,
        consume: (BufferedSource, (T) -> Boolean) -> Unit,
    ): Flow<T> = callbackFlow {
        val active = AtomicReference<Call?>()
        val responseRef = AtomicReference<Response?>()
        val transport = if (stream) client.newBuilder().readTimeout(6, TimeUnit.MINUTES)
            .callTimeout(0, TimeUnit.MILLISECONDS).build() else client
        fun start(token: String?, retry: Boolean) {
            if (isClosedForSend) return
            val builder = Request.Builder().url(url)
            PanelRequests.originOf(url)?.let { builder.header("Origin", it) }
            token?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
            localToken?.takeIf { it.isNotBlank() }?.let { builder.header("x-daidai-local-token", it) }
            if (stream) builder.header("Accept", "text/event-stream")
            builder.method(method, if (method in setOf("GET", "DELETE")) null else
                (json ?: "{}").toRequestBody("application/json; charset=utf-8".toMediaType()))
            val call = transport.newCall(builder.build())
            active.set(call)
            if (isClosedForSend) { call.cancel(); return }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) { close(e) }
                override fun onResponse(call: Call, response: Response) {
                    responseRef.set(response)
                    try {
                        response.use {
                            if (isClosedForSend) return
                            if (response.code == 401 && retry) {
                                response.close()
                                if (PanelSession.tryRefresh()) {
                                    start(PanelSession.accessToken(), false)
                                    return
                                }
                                throw PanelApiException(401, "认证已过期")
                            }
                            if (!response.isSuccessful) throw PanelApiException(response.code,
                                response.peekBody(8192).string())
                            if (stream && response.body?.contentType()?.subtype != "event-stream") {
                                throw IOException("服务器未返回 SSE 日志流")
                            }
                            val source = response.body?.source() ?: throw IOException("响应体为空")
                            consume(source) { trySendBlocking(it).isSuccess }
                        }
                        close()
                    } catch (error: Exception) { close(error) }
                    finally { responseRef.compareAndSet(response, null) }
                }
            })
        }
        start(PanelSession.accessToken() ?: accessToken, true)
        awaitClose {
            active.getAndSet(null)?.cancel()
            responseRef.getAndSet(null)?.close()
        }
    }.buffer(16)

    fun events(url: String): Flow<CapabilityEvent> = read("GET", url, stream = true) { source, send ->
        val parser = CapabilitySseParser()
        var first = true
        while (!source.exhausted()) {
            val raw = source.readUtf8LineStrict(65536)
            val event = parser.line(if (first) raw.removePrefix("\uFEFF") else raw)
            first = false
            if (event != null && (!send(event) || event.type == "done")) break
        }
    }

    suspend fun text(method: String, url: String, json: String? = null): String {
        var result = ""
        read<String>(method, url, json) { source, send ->
            val buffer = okio.Buffer()
            while (source.read(buffer, 8192) != -1L) {
                require(buffer.size <= 2 * 1024 * 1024) { "响应超过 2 MiB，请缩小查询范围" }
            }
            send(buffer.readUtf8())
        }.collect { result = it }
        return result
    }

    suspend fun download(url: String, output: OutputStream) = withContext(Dispatchers.IO) {
        read<Unit>("GET", url) { source, _ ->
            val buffer = ByteArray(8192)
            while (true) {
                val count = source.read(buffer)
                if (count == -1) break
                output.write(buffer, 0, count)
            }
            output.flush()
        }.collect { }
    }
}
