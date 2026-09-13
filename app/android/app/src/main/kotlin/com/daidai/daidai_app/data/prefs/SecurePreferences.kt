package com.daidai.daidai_app.data.prefs

import android.content.Context
import android.content.SharedPreferences
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.nio.ByteBuffer
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Keystore-backed encrypted values. Non-secret preferences remain in ordinary SharedPreferences. */
class SecurePreferences(context: Context) {
    private val preferences: SharedPreferences =
        context.applicationContext.getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)

    fun putSecret(key: String, value: String?) {
        preferences.edit().apply {
            if (value == null) remove(key) else putString(key, CryptoBox.encrypt(value, keyAlias))
        }.apply()
    }

    fun getSecret(key: String): String? = preferences.getString(key, null)?.let {
        runCatching { CryptoBox.decrypt(it, keyAlias) }.getOrNull()
    }

    fun remove(key: String) {
        preferences.edit().remove(key).apply()
    }

    fun putPlain(key: String, value: String?) {
        preferences.edit().apply { if (value == null) remove(key) else putString(key, value) }.apply()
    }

    fun getPlain(key: String): String? = preferences.getString(key, null)

    companion object {
        private const val FILE_NAME = "panel_config"
        private const val KEY_ALIAS = "daidai_panel_config_aes"
        private val keyAlias: String get() = KEY_ALIAS
    }
}

/** AES/GCM envelope: version byte, nonce length, nonce, ciphertext. */
object CryptoBox {
    private const val TRANSFORMATION = "AES/GCM/NoPadding"
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"
    private const val VERSION: Byte = 1

    /** Injectable Base64 codec (Android uses android.util.Base64; JVM unit tests inject java.util.Base64). */
    fun encoder(): (ByteArray) -> String = { Base64.encodeToString(it, Base64.NO_WRAP) }
    fun decoder(): (String) -> ByteArray = { Base64.decode(it, Base64.NO_WRAP) }

    fun encrypt(value: String, alias: String): String = encryptWithKey(value, key(alias))

    fun decrypt(encoded: String, alias: String): String = decryptWithKey(encoded, key(alias))

    /** Pure AES/GCM envelope round-trip, decoupled from AndroidKeyStore so it is JVM-unit-testable. */
    fun encryptWithKey(value: String, secretKey: SecretKey, encode: (ByteArray) -> String = encoder()): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey)
        val nonce = cipher.iv
        val encrypted = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        val payload = ByteBuffer.allocate(2 + nonce.size + encrypted.size)
            .put(VERSION).put(nonce.size.toByte()).put(nonce).put(encrypted).array()
        return encode(payload)
    }

    /** Pure AES/GCM envelope decode, decoupled from AndroidKeyStore so it is JVM-unit-testable. */
    fun decryptWithKey(encoded: String, secretKey: SecretKey, decode: (String) -> ByteArray = decoder()): String {
        val payload = decode(encoded)
        require(payload.size > 2 && payload[0] == VERSION) { "Unsupported encrypted value" }
        val nonceLength = payload[1].toInt() and 0xff
        require(nonceLength in 12..16 && payload.size > 2 + nonceLength) { "Invalid encrypted value" }
        val nonce = payload.copyOfRange(2, 2 + nonceLength)
        val encrypted = payload.copyOfRange(2 + nonceLength, payload.size)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, secretKey, GCMParameterSpec(128, nonce))
        return cipher.doFinal(encrypted).toString(Charsets.UTF_8)
    }

    private fun key(alias: String): SecretKey {
        val store = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (store.getKey(alias, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setRandomizedEncryptionRequired(true)
            .build())
        return generator.generateKey()
    }
}
