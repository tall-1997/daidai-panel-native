package com.daidai.daidai_app

import java.io.ByteArrayOutputStream
import java.io.File
import java.nio.file.Files
import org.apache.commons.compress.compressors.bzip2.BZip2CompressorOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class AndroidBsPatchTest {
    @Test
    fun `applies BSDIFF40 patch without native libraries`() {
        val directory = Files.createTempDirectory("bsdiff-test").toFile()
        val oldFile = File(directory, "old.apk").apply { writeBytes(byteArrayOf(1, 2, 3)) }
        val patchFile = File(directory, "update.patch")
        val outputFile = File(directory, "new.apk")
        val control = compressed(offset(3) + offset(0) + offset(0))
        val diff = compressed(byteArrayOf(0, 0, 1))
        val extra = compressed(byteArrayOf())
        patchFile.writeBytes("BSDIFF40".toByteArray() + offset(control.size.toLong()) + offset(diff.size.toLong()) + offset(3) + control + diff + extra)

        assertEquals(0, AndroidBsPatch.patch(oldFile, patchFile, outputFile))
        assertArrayEquals(byteArrayOf(1, 2, 4), outputFile.readBytes())
    }

    private fun compressed(value: ByteArray): ByteArray = ByteArrayOutputStream().use { output ->
        BZip2CompressorOutputStream(output).use { it.write(value) }
        output.toByteArray()
    }

    private fun offset(value: Long): ByteArray {
        var remaining = kotlin.math.abs(value)
        return ByteArray(8) { index ->
            val byte = (remaining and 0xff).toInt()
            remaining = remaining shr 8
            if (index == 7 && value < 0) (byte or 0x80).toByte() else byte.toByte()
        }
    }
}
