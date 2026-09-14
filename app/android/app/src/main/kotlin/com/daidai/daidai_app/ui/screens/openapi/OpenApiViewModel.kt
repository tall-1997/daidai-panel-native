package com.daidai.daidai_app.ui.screens.openapi

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.data.repository.OpenApiRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Open API 应用列表页的可观察状态。 */
data class OpenApiUiState(
    val apps: List<OpenApiApp> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * Open API 应用列表的只读 ViewModel。
 *
 * 通过 [OpenApiRepository] 拉取应用列表并暴露为 [StateFlow]。repository
 * 缺省为 null 时有意报「Open API 后端未装配」，绝不伪造空列表，以便
 * 布局可预览、行为不谎报（与 LoginViewModel 的约定一致）。
 */
class OpenApiViewModel(
    private val repository: OpenApiRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(OpenApiUiState())
    val uiState: StateFlow<OpenApiUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = OpenApiUiState.Phase.Error, errorMessage = "Open API 后端未装配")
            }
            return
        }
        _uiState.update { it.copy(phase = OpenApiUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val apps = repo.getApps()
                _uiState.update {
                    it.copy(apps = apps, phase = OpenApiUiState.Phase.Loaded, errorMessage = null)
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = OpenApiUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "Open API 应用加载失败，请重试",
                    )
                }
            }
        }
    }
}