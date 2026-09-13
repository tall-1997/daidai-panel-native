package com.daidai.daidai_app.ui.screens

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.DashboardStats
import com.daidai.daidai_app.data.model.SystemInfo
import com.daidai.daidai_app.data.repository.DashboardDataBundle
import com.daidai.daidai_app.data.repository.DashboardRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 仪表盘 overview tab 的可观察状态。 */
sealed interface DashboardUiState {
    object Loading : DashboardUiState

    data class Success(
        val systemInfo: SystemInfo,
        val stats: DashboardStats,
    ) : DashboardUiState

    data class Error(val message: String) : DashboardUiState
}

/**
 * 仪表盘（overview）的 ViewModel：通过 [DashboardRepository] 拉取
 * `/api/system/info` 与 `/api/system/dashboard`，产出 Loading/Success/Error 三态
 * [DashboardUiState]。
 */
class DashboardViewModel(
    private val repository: DashboardRepository,
) : ViewModel() {
    private val _uiState = MutableStateFlow<DashboardUiState>(DashboardUiState.Loading)
    val uiState: StateFlow<DashboardUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        _uiState.value = DashboardUiState.Loading
        viewModelScope.launch {
            try {
                val bundle: DashboardDataBundle = repository.loadDashboard()
                _uiState.value = DashboardUiState.Success(bundle.systemInfo, bundle.stats)
            } catch (error: Exception) {
                _uiState.update {
                    DashboardUiState.Error(
                        error.message?.takeIf { it.isNotBlank() } ?: "加载仪表盘数据失败，请重试",
                    )
                }
            }
        }
    }
}
