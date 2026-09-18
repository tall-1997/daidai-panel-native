package com.daidai.daidai_app

import java.security.SecureRandom
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/** RFC 6238 TOTP (HMAC-SHA1, 6 digits, 30s) matching the Go panel service. */
internal object Totp {
    const val DIGITS = 6
    const val PERIOD_SECONDS = 30L
    const val ISSUER = "DaiDaiPanel"

    fun generateSecret(): String {
        val bytes = ByteArray(20)
        SecureRandom().nextBytes(bytes)
        return Base32.encode(bytes)
    }

    fun uri(username: String, secret: String): String =
        "otpauth://totp/$ISSUER:$username?secret=$secret&issuer=$ISSUER&digits=$DIGITS&period=$PERIOD_SECONDS"

    fun validate(secret: String, code: String, nowSeconds: Long = System.currentTimeMillis() / 1000): Boolean {
        val trimmed = code.trim()
        if (!trimmed.matches(Regex("^\\d{$DIGITS}$"))) return false
        val key = runCatching { Base32.decode(secret) }.getOrNull() ?: return false
        val step = nowSeconds / PERIOD_SECONDS
        return (-1L..1L).any { codeAt(key, step + it) == trimmed }
    }

    internal fun codeAt(key: ByteArray, timeStep: Long): String {
        val data = ByteArray(8)
        var value = timeStep
        for (index in 7 downTo 0) {
            data[index] = (value and 0xff).toByte()
            value = value ushr 8
        }
        val mac = Mac.getInstance("HmacSHA1")
        mac.init(SecretKeySpec(key, "HmacSHA1"))
        val hash = mac.doFinal(data)
        val offset = hash.last().toInt() and 0x0f
        val binary = ((hash[offset].toInt() and 0x7f) shl 24) or
            ((hash[offset + 1].toInt() and 0xff) shl 16) or
            ((hash[offset + 2].toInt() and 0xff) shl 8) or
            (hash[offset + 3].toInt() and 0xff)
        return (binary % 1_000_000).toString().padStart(DIGITS, '0')
    }
}

internal object Base32 {
    private const val ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

    fun encode(data: ByteArray): String {
        if (data.isEmpty()) return ""
        val output = StringBuilder((data.size * 8 + 4) / 5)
        var buffer = 0
        var bits = 0
        for (byte in data) {
            buffer = (buffer shl 8) or (byte.toInt() and 0xff)
            bits += 8
            while (bits >= 5) {
                bits -= 5
                output.append(ALPHABET[(buffer shr bits) and 0x1f])
            }
        }
        if (bits > 0) output.append(ALPHABET[(buffer shl (5 - bits)) and 0x1f])
        return output.toString()
    }

    fun decode(encoded: String): ByteArray {
        val cleaned = encoded.trim().uppercase().replace("=", "").replace(" ", "")
        if (cleaned.isEmpty()) return ByteArray(0)
        val output = ArrayList<Byte>((cleaned.length * 5) / 8)
        var buffer = 0
        var bits = 0
        for (char in cleaned) {
            val value = ALPHABET.indexOf(char)
            require(value >= 0) { "invalid base32" }
            buffer = (buffer shl 5) or value
            bits += 5
            if (bits >= 8) {
                bits -= 8
                output += ((buffer shr bits) and 0xff).toByte()
            }
        }
        return output.toByteArray()
    }
}
