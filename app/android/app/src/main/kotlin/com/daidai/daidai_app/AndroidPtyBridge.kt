package com.daidai.daidai_app

import java.io.InputStream

internal object AndroidPtyBridge {
    init {
        System.loadLibrary("daidai_pty")
    }

    data class Handle(val fd: Int, val pid: Int)

    fun start(command: List<String>, environment: Map<String, String>, workingDirectory: String, rows: Int, columns: Int): Handle {
        val packed = nativeStart(command.toTypedArray(), environment.map { "${it.key}=${it.value}" }.toTypedArray(), workingDirectory, rows, columns)
        return Handle(fd = packed.toInt(), pid = (packed ushr 32).toInt())
    }

    fun inputStream(handle: Handle): InputStream = object : InputStream() {
        private val single = ByteArray(1)
        override fun read(): Int = if (read(single) == 1) single[0].toInt() and 0xff else -1
        override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
            if (offset == 0 && length == buffer.size) return nativeRead(handle.fd, buffer)
            val temporary = ByteArray(length)
            val count = nativeRead(handle.fd, temporary)
            if (count > 0) temporary.copyInto(buffer, offset, 0, count)
            return count
        }
    }

    fun write(handle: Handle, data: ByteArray) = nativeWrite(handle.fd, data)
    fun resize(handle: Handle, rows: Int, columns: Int) = nativeResize(handle.fd, rows, columns)
    fun stop(handle: Handle): Int = nativeStop(handle.fd, handle.pid)

    private external fun nativeStart(command: Array<String>, environment: Array<String>, workingDirectory: String, rows: Int, columns: Int): Long
    private external fun nativeRead(fd: Int, buffer: ByteArray): Int
    private external fun nativeWrite(fd: Int, data: ByteArray)
    private external fun nativeResize(fd: Int, rows: Int, columns: Int)
    private external fun nativeStop(fd: Int, pid: Int): Int
}
