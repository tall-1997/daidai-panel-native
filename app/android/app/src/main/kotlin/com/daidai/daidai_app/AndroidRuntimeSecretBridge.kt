package com.daidai.daidai_app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

object AndroidRuntimeSecretBridge {
    private const val KEY_ALIAS = "daidai_runtime_secret_envelope_v1"
    private const val PREFS_NAME = "runtime_secret_envelope"
    private const val PREF_CIPHER = "cipher"
    private const val PREF_IV = "iv"

    fun runtimeMasterKey(context: Context): String {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val cipherText = prefs.getString(PREF_CIPHER, null)
        val iv = prefs.getString(PREF_IV, null)
        if (!cipherText.isNullOrBlank() && !iv.isNullOrBlank()) {
            return Base64.encodeToString(
                decrypt(Base64.decode(cipherText, Base64.NO_WRAP), Base64.decode(iv, Base64.NO_WRAP)),
                Base64.NO_WRAP,
            )
        }
        val masterKey = ByteArray(32)
        SecureRandom().nextBytes(masterKey)
        val encrypted = encrypt(masterKey)
        check(
            prefs.edit()
                .putString(PREF_CIPHER, Base64.encodeToString(encrypted.first, Base64.NO_WRAP))
                .putString(PREF_IV, Base64.encodeToString(encrypted.second, Base64.NO_WRAP))
                .commit()
        )
        return Base64.encodeToString(masterKey, Base64.NO_WRAP)
    }

    private fun encrypt(payload: ByteArray): Pair<ByteArray, ByteArray> {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        return cipher.doFinal(payload) to cipher.iv
    }

    private fun decrypt(payload: ByteArray, iv: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, iv))
        return cipher.doFinal(payload)
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build()
        )
        return generator.generateKey()
    }
}
