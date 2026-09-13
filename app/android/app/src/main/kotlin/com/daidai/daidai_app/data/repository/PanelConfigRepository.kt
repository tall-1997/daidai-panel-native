package com.daidai.daidai_app.data.repository

import android.content.Context
import com.daidai.daidai_app.data.prefs.SecurePreferences
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext

/** The panel endpoint selected by the user. */
enum class PanelConnectionMode(val persistedValue: String) {
    REMOTE("remote"),
    MANAGED_LOCAL("managedLocal");

    companion object {
        fun fromPersistedValue(value: String?): PanelConnectionMode =
            values().firstOrNull { it.persistedValue == value }
                ?: REMOTE
    }
}

data class PanelConfig(
    val serverUrl: String = "",
    val mode: PanelConnectionMode = PanelConnectionMode.REMOTE,
    val accessToken: String? = null,
    val localToken: String? = null,
)

/**
 * Persists endpoint selection and credentials. Tokens are encrypted with Android Keystore;
 * serverUrl and mode are intentionally stored as non-secret preferences.
 */
class PanelConfigRepository(context: Context) {
    private val storage = SecurePreferences(context)
    private val _config = MutableStateFlow(readConfig())

    val config: StateFlow<PanelConfig> = _config.asStateFlow()

    suspend fun getConfig(): PanelConfig = withContext(Dispatchers.IO) {
        refresh()
        _config.value
    }

    suspend fun setServerUrl(serverUrl: String) = update { copy(serverUrl = normalizeServerUrl(serverUrl)) }

    suspend fun setMode(mode: PanelConnectionMode) = update { copy(mode = mode) }

    suspend fun setAccessToken(token: String?) = update { copy(accessToken = token.cleanToken()) }

    suspend fun setLocalToken(token: String?) = update { copy(localToken = token.cleanToken()) }

    suspend fun setConfig(config: PanelConfig) = withContext(Dispatchers.IO) {
        val normalized = config.normalized()
        storage.putPlain(KEY_SERVER_URL, normalized.serverUrl)
        storage.putPlain(KEY_MODE, normalized.mode.persistedValue)
        storage.putSecret(KEY_ACCESS_TOKEN, normalized.accessToken)
        storage.putSecret(KEY_LOCAL_TOKEN, normalized.localToken)
        _config.value = normalized
    }

    suspend fun clearAccessToken() = setAccessToken(null)

    suspend fun clearLocalToken() = setLocalToken(null)

    suspend fun clearTokens() = withContext(Dispatchers.IO) {
        storage.remove(KEY_ACCESS_TOKEN)
        storage.remove(KEY_LOCAL_TOKEN)
        _config.value = _config.value.copy(accessToken = null, localToken = null)
    }

    private suspend fun update(transform: PanelConfig.() -> PanelConfig) = withContext(Dispatchers.IO) {
        setConfig(_config.value.transform())
    }

    private suspend fun refresh() {
        _config.value = readConfig()
    }

    private fun readConfig() = PanelConfig(
        serverUrl = normalizeServerUrl(storage.getPlain(KEY_SERVER_URL).orEmpty()),
        mode = PanelConnectionMode.fromPersistedValue(storage.getPlain(KEY_MODE)),
        accessToken = storage.getSecret(KEY_ACCESS_TOKEN).cleanToken(),
        localToken = storage.getSecret(KEY_LOCAL_TOKEN).cleanToken(),
    )

    companion object {
        private const val KEY_SERVER_URL = "server_url"
        private const val KEY_MODE = "mode"
        private const val KEY_ACCESS_TOKEN = "access_token"
        private const val KEY_LOCAL_TOKEN = "local_token"

        internal fun normalizeServerUrl(value: String): String {
            var v = value.trim()
            while (v.endsWith("/")) v = v.dropLast(1)
            return v
        }

        private fun String?.cleanToken(): String? = this?.trim()?.takeIf { it.isNotEmpty() }

        private fun PanelConfig.normalized() = copy(
            serverUrl = normalizeServerUrl(serverUrl),
            accessToken = accessToken.cleanToken(),
            localToken = localToken.cleanToken(),
        )
    }
}
