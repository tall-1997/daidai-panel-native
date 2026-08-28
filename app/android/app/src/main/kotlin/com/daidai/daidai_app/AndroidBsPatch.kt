package com.daidai.daidai_app

import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream
import java.io.RandomAccessFile
import org.apache.commons.compress.compressors.bzip2.BZip2CompressorInputStream

object AndroidBsPatch {
    private const val HEADER_SIZE = 32L
    private const val MAX_OUTPUT_SIZE = 2L * 1024 * 1024 * 1024

    fun patch(oldFile: File, patchFile: File, outputFile: File): Int = runCatching {
        applyPatch(oldFile, patchFile, outputFile)
        0
    }.getOrElse { -1 }

    internal fun applyPatch(oldFile: File, patchFile: File, outputFile: File) {
        RandomAccessFile(patchFile, "r").use { patch ->
            val magic = ByteArray(8).also(patch::readFully)
            require(magic.contentEquals("BSDIFF40".toByteArray(Charsets.US_ASCII))) { "Invalid BSDIFF patch header" }
            val controlSize = readOffset(patch)
            val diffSize = readOffset(patch)
            val outputSize = readOffset(patch)
            require(controlSize >= 0 && diffSize >= 0 && outputSize in 0..MAX_OUTPUT_SIZE) { "Invalid BSDIFF patch sizes" }
            require(HEADER_SIZE + controlSize + diffSize <= patchFile.length()) { "Truncated BSDIFF patch" }

            compressedStream(patchFile, HEADER_SIZE).use { control ->
                compressedStream(patchFile, HEADER_SIZE + controlSize).use { diff ->
                    compressedStream(patchFile, HEADER_SIZE + controlSize + diffSize).use { extra ->
                        RandomAccessFile(oldFile, "r").use { old ->
                            BufferedOutputStream(FileOutputStream(outputFile)).use { output ->
                                var oldPosition = 0L
                                var newPosition = 0L
                                val diffBuffer = ByteArray(64 * 1024)
                                val oldBuffer = ByteArray(diffBuffer.size)
                                while (newPosition < outputSize) {
                                    val addLength = readOffset(control)
                                    val copyLength = readOffset(control)
                                    val seekLength = readOffset(control)
                                    require(addLength >= 0 && copyLength >= 0 && newPosition + addLength + copyLength <= outputSize) {
                                        "Invalid BSDIFF control tuple"
                                    }
                                    var remaining = addLength
                                    while (remaining > 0) {
                                        val count = minOf(remaining, diffBuffer.size.toLong()).toInt()
                                        diff.readFully(diffBuffer, count)
                                        oldBuffer.fill(0, 0, count)
                                        val readableStart = maxOf(oldPosition, 0L)
                                        val prefix = (readableStart - oldPosition).coerceAtMost(count.toLong()).toInt()
                                        if (prefix < count && readableStart < old.length()) {
                                            old.seek(readableStart)
                                            val available = minOf(count - prefix, old.length() - readableStart).toInt()
                                            old.readFully(oldBuffer, prefix, available)
                                        }
                                        repeat(count) { index ->
                                            diffBuffer[index] = (diffBuffer[index] + oldBuffer[index]).toByte()
                                        }
                                        output.write(diffBuffer, 0, count)
                                        oldPosition += count
                                        newPosition += count
                                        remaining -= count
                                    }
                                    copyStream(extra, output, copyLength)
                                    newPosition += copyLength
                                    oldPosition += seekLength
                                }
                            }
                        }
                    }
                }
            }
        }
        require(outputFile.length() <= MAX_OUTPUT_SIZE) { "BSDIFF output exceeds size limit" }
    }

    private fun compressedStream(file: File, offset: Long): InputStream {
        val stream = FileInputStream(file)
        stream.channel.position(offset)
        return BZip2CompressorInputStream(BufferedInputStream(stream), false)
    }

    private fun readOffset(file: RandomAccessFile): Long = readOffset { file.read() }

    private fun readOffset(stream: InputStream): Long = readOffset { stream.read() }

    private fun readOffset(readByte: () -> Int): Long {
        val bytes = ByteArray(8) {
            val value = readByte()
            require(value >= 0) { "Truncated BSDIFF offset" }
            value.toByte()
        }
        var value = (bytes[7].toInt() and 0x7f).toLong()
        for (index in 6 downTo 0) value = value * 256 + (bytes[index].toInt() and 0xff)
        return if (bytes[7].toInt() and 0x80 != 0) -value else value
    }

    private fun InputStream.readFully(buffer: ByteArray, count: Int) {
        var offset = 0
        while (offset < count) {
            val read = read(buffer, offset, count - offset)
            require(read > 0) { "Truncated BSDIFF data" }
            offset += read
        }
    }

    private fun copyStream(input: InputStream, output: BufferedOutputStream, length: Long) {
        val buffer = ByteArray(64 * 1024)
        var remaining = length
        while (remaining > 0) {
            val count = minOf(remaining, buffer.size.toLong()).toInt()
            input.readFully(buffer, count)
            output.write(buffer, 0, count)
            remaining -= count
        }
    }
}
