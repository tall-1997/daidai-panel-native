package com.daidai.daidai_app.ui.screens

import androidx.lifecycle.ViewModel
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 配置来源。Remote 需要填写服务地址，Managed Local 使用应用托管的本地服务。 */
enum class ServerMode {
    Remote,
    ManagedLocal,
}

/**
 * 服务器配置的最小写入契约。
 * 具体实现由应用装配层提供，避免配置页依赖尚未完成的网络或存储模块。
 */
interface ServerConfigRepository {
    suspend fun save(mode: ServerMode, remoteUrl: String?)
}

data class ServerConfigUiState(
    val mode: ServerMode = ServerMode.Remote,
    val remoteUrl: String = "",
    val isSaving: Boolean = false,
    val errorMessage: String? = null,
)

class ServerConfigViewModel(
    private val repository: ServerConfigRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(ServerConfigUiState())
    val uiState: StateFlow<ServerConfigUiState> = _uiState.asStateFlow()

    fun selectMode(mode: ServerMode) {
        _uiState.update { it.copy(mode = mode, errorMessage = null) }
    }

    fun setRemoteUrl(url: String) {
        _uiState.update { it.copy(remoteUrl = url, errorMessage = null) }
    }

    fun save(onSuccess: () -> Unit) {
        val state = _uiState.value
        val url = state.remoteUrl.trim()
        if (state.mode == ServerMode.Remote && !isValidUrl(url)) {
            _uiState.update { it.copy(errorMessage = "请输入有效的远程服务 URL（例如 https://example.com）") }
            return
        }

        viewModelScope.launch {
            _uiState.update { it.copy(isSaving = true, errorMessage = null) }
            try {
                repository?.save(state.mode, url.takeIf { state.mode == ServerMode.Remote })
                _uiState.update { it.copy(isSaving = false) }
                onSuccess()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        isSaving = false,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "保存服务器配置失败，请重试",
                    )
                }
            }
        }
    }

    private fun isValidUrl(value: String): Boolean = runCatching {
        val url = value.toHttpUrlOrNull() ?: return false
        (url.scheme == "https" || url.scheme == "http") && url.host.isNotBlank()
    }.getOrDefault(false)
}
