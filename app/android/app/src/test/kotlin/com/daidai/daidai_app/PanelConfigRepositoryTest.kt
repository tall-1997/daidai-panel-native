package com.daidai.daidai_app

import com.daidai.daidai_app.data.prefs.CryptoBox
import com.daidai.daidai_app.data.repository.PanelConfigRepository
import com.daidai.daidai_app.data.repository.PanelConnectionMode
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

class PanelConfigRepositoryTest {

    private fun aesKey(): SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()

    private fun b64encode(bytes: ByteArray): String = java.util.Base64.getEncoder().encodeToString(bytes)
    private fun b64decode(s: String): ByteArray = java.util.Base64.getDecoder().decode(s)

    private fun encrypt(value: String, key: SecretKey): String =
        CryptoBox.encryptWithKey(value, key, ::b64encode)

    private fun decrypt(encoded: String, key: SecretKey): String =
        CryptoBox.decryptWithKey(encoded, key, ::b64decode)

    @Test
    fun `mode persistence values are stable and unknown values fall back to remote`() {
        assertEquals("remote", PanelConnectionMode.REMOTE.persistedValue)
        assertEquals("managedLocal", PanelConnectionMode.MANAGED_LOCAL.persistedValue)
        assertEquals(PanelConnectionMode.REMOTE, PanelConnectionMode.fromPersistedValue("unknown"))
        assertEquals(PanelConnectionMode.REMOTE, PanelConnectionMode.fromPersistedValue(null))
        assertEquals(PanelConnectionMode.MANAGED_LOCAL, PanelConnectionMode.fromPersistedValue("managedLocal"))
    }

    @Test
    fun `normalizeServerUrl trims whitespace and trailing slash`() {
        assertEquals("", PanelConfigRepository.normalizeServerUrl(""))
        assertEquals("https://panel.example.com", PanelConfigRepository.normalizeServerUrl("  https://panel.example.com/  "))
        assertEquals("http://127.0.0.1:8080", PanelConfigRepository.normalizeServerUrl("http://127.0.0.1:8080///"))
    }

    @Test
    fun `AES GCM envelope round-trips secret value`() {
        val key = aesKey()
        val secret = "eyJhbGciOiJIUzI1NiJ9.some-token.payload"
        for (value in listOf("", secret, "含中文的令牌值 🔑")) {
            val encoded = encrypt(value, key)
            assertTrue("ciphertext must not equal plaintext", encoded != value || value.isEmpty())
            assertEquals(value, decrypt(encoded, key))
        }
    }

    @Test
    fun `AES GCM envelope is non-deterministic for same plaintext`() {
        val key = aesKey()
        val a = encrypt("same", key)
        val b = encrypt("same", key)
        assertTrue("nonce must randomize ciphertext (GCM)", a != b)
        assertEquals("same", decrypt(a, key))
        assertEquals("same", decrypt(b, key))
    }

    @Test(expected = IllegalArgumentException::class)
    fun `AES GCM envelope rejects corrupted or unknown-version payload`() {
        val key = aesKey()
        val encoded = encrypt("token", key)
        val bytes = b64decode(encoded)
        bytes[0] = 99 // corrupt version byte
        CryptoBox.decryptWithKey(b64encode(bytes), key, ::b64decode)
    }

    @Test
    fun `envelope layout is version, nonce len, nonce, ciphertext`() {
        val key = aesKey()
        val encoded = encrypt("value", key)
        val payload = b64decode(encoded)
        assertTrue(payload[0] == 1.toByte())
        val nonceLen = payload[1].toInt() and 0xff
        assertTrue(nonceLen in 12..16)
        // payload = 1 version + 1 nonceLen + nonce(nonceLen) + AESGCM ciphertext.
        // ciphertext = plaintext(5) + 16-byte GCM auth tag.
        assertTrue(payload.size == 2 + nonceLen + 5 + 16)
    }

    @Test
    fun `AES GCM ciphertext is strictly longer than an empty envelope`() {
        // empty plaintext still yields an auth tag in the payload
        val empty = encrypt("", aesKey())
        assertTrue(empty.isNotEmpty())
    }
}