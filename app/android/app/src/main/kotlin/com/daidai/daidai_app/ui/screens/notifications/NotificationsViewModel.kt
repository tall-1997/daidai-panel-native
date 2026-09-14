package com.daidai.daidai_app.ui.screens.notifications

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.NotificationChannel
import com.daidai.daidai_app.data.repository.NotificationsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 通知列表页 UI 状态：加载 / 渠道列表 / 错误。 */
data class NotificationsUiState(
    val isLoading: Boolean = false,
    val channels: List<NotificationChannel> = emptyList(),
    val errorMessage: String? = null,
)

/**
 * 通知列表页 ViewModel。
 *
 * 初始化时自动加载一次渠道列表；加载失败可通过 [load] 重试。
 */
class NotificationsViewModel(
    private val repository: NotificationsRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(NotificationsUiState())
    val uiState: StateFlow<NotificationsUiState> = _uiState.asStateFlow()

    init {
        load()
    }

    /** 拉取渠道列表（重试/刷新入口）。成功时填充 channels，失败时写 errorMessage。 */
    fun load() {
        if (_uiState.value.isLoading) return
        _uiState.update { it.copy(isLoading = true, errorMessage = null) }
        viewModelScope.launch {
            try {
                val channels = repository.getChannels()
                _uiState.update {
                    it.copy(isLoading = false, channels = channels, errorMessage = null)
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        isLoading = false,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "加载通知渠道失败，请重试",
                    )
                }
            }
        }
    }
}
