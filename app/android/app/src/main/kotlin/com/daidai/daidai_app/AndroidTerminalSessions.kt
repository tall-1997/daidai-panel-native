package com.daidai.daidai_app

import android.util.Base64
import java.io.Closeable
import java.io.File
import java.time.Instant
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import org.json.JSONArray
import org.json.JSONObject

internal class AndroidTerminalSessions : Closeable {
    private data class OutputChunk(val cursor: Long, val data: ByteArray)

    private class Session(
        val id: String,
        val handle: AndroidPtyBridge.Handle,
        val shell: String,
        val createdAt: String,
    ) {
        val closed = AtomicBoolean(false)
        val output = ArrayDeque<OutputChunk>()
        var outputBytes = 0
        var cursor = 0L
        @Volatile var status = "running"
        @Volatile var exitCode: Int? = null
    }

    private val sessions = ConcurrentHashMap<String, Session>()
    private val createLock = Any()

    fun create(command: List<String>, environment: Map<String, String>, workingDirectory: File, shell: String, rows: Int, columns: Int): JSONObject {
        val session = synchronized(createLock) {
            cleanupFinished()
            require(sessions.values.count { it.status == "running" } < MAX_RUNNING_SESSIONS) { "TERMINAL_SESSION_LIMIT: too many running terminal sessions" }
            val id = UUID.randomUUID().toString()
            val handle = AndroidPtyBridge.start(command, environment, workingDirectory.absolutePath, rows.coerceIn(2, 200), columns.coerceIn(10, 400))
            Session(id, handle, shell, Instant.now().toString()).also { sessions[id] = it }
        }
        Thread({ readOutput(session) }, "terminal-${session.id}").apply { isDaemon = true }.start()
        return sessionJson(session)
    }

    fun get(id: String, after: Long): JSONObject {
        val session = sessions[id] ?: throw NoSuchElementException("终端会话不存在")
        val chunks = JSONArray()
        synchronized(session.output) {
            session.output.filter { it.cursor > after }.forEach { chunk ->
                chunks.put(JSONObject()
                    .put("cursor", chunk.cursor)
                    .put("encoding", "base64")
                    .put("data", Base64.encodeToString(chunk.data, Base64.NO_WRAP)))
            }
        }
        return sessionJson(session).put("output", chunks).put("cursor", session.cursor)
    }

    fun write(id: String, data: ByteArray) {
        require(data.size <= MAX_INPUT_BYTES) { "终端单次输入超过限制" }
        val session = running(id)
        AndroidPtyBridge.write(session.handle, data)
    }

    fun resize(id: String, rows: Int, columns: Int) {
        val session = running(id)
        AndroidPtyBridge.resize(session.handle, rows.coerceIn(2, 200), columns.coerceIn(10, 400))
    }

    fun stop(id: String): JSONObject {
        val session = sessions[id] ?: throw NoSuchElementException("终端会话不存在")
        finish(session)
        return sessionJson(session)
    }

    fun remove(id: String) {
        val session = sessions.remove(id) ?: throw NoSuchElementException("终端会话不存在")
        finish(session)
    }

    private fun running(id: String): Session {
        val session = sessions[id] ?: throw NoSuchElementException("终端会话不存在")
        check(session.status == "running" && !session.closed.get()) { "终端会话已经结束" }
        return session
    }

    private fun readOutput(session: Session) {
        val buffer = ByteArray(8192)
        try {
            AndroidPtyBridge.inputStream(session.handle).use { input ->
                while (!session.closed.get()) {
                    val count = input.read(buffer)
                    if (count < 0) break
                    if (count > 0) append(session, buffer.copyOf(count))
                }
            }
        } catch (_: Exception) {
            // Closing a PTY interrupts its blocking reader.
        } finally {
            finish(session)
        }
    }

    private fun append(session: Session, bytes: ByteArray) = synchronized(session.output) {
        session.output.addLast(OutputChunk(++session.cursor, bytes))
        session.outputBytes += bytes.size
        while (session.outputBytes > MAX_OUTPUT_BYTES && session.output.isNotEmpty()) {
            session.outputBytes -= session.output.removeFirst().data.size
        }
    }

    private fun finish(session: Session) {
        if (!session.closed.compareAndSet(false, true)) return
        session.exitCode = runCatching { AndroidPtyBridge.stop(session.handle) }.getOrDefault(1)
        session.status = if (session.exitCode == 0) "exited" else "stopped"
    }

    private fun sessionJson(session: Session): JSONObject = JSONObject()
        .put("id", session.id)
        .put("status", session.status)
        .put("shell", session.shell)
        .put("pid", session.handle.pid)
        .put("created_at", session.createdAt)
        .put("exit_code", session.exitCode ?: JSONObject.NULL)

    private fun cleanupFinished() {
        val removable = sessions.values.filter { it.status != "running" }.sortedByDescending { it.createdAt }.drop(MAX_RETAINED_SESSIONS)
        removable.forEach { sessions.remove(it.id, it) }
    }

    override fun close() {
        sessions.values.forEach(::finish)
        sessions.clear()
    }

    companion object {
        private const val MAX_RUNNING_SESSIONS = 3
        private const val MAX_RETAINED_SESSIONS = 20
        private const val MAX_INPUT_BYTES = 64 * 1024
        private const val MAX_OUTPUT_BYTES = 1024 * 1024
    }
}
