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
            _uiState.update { it.copy(errorMessage = "请输入有效的 HTTPS 远程服务 URL（本机调试可用 http://127.0.0.1）") }
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

    // 与 network_security_config.xml 对齐：明文 HTTP 仅限本机/模拟器回环地址。
    private val cleartextAllowedHosts = setOf("localhost", "127.0.0.1", "::1", "10.0.2.2", "10.0.3.2")

    private fun isValidUrl(value: String): Boolean = runCatching {
        val url = value.toHttpUrlOrNull() ?: return false
        when (url.scheme) {
            "https" -> url.host.isNotBlank()
            "http" -> url.host.isNotBlank() && url.host.lowercase() in cleartextAllowedHosts
            else -> false
        }
    }.getOrDefault(false)
}
