package com.daidai.daidai_app.ui.screens.notifications

import androidx.lifecycle.ViewModel
import com.daidai.daidai_app.data.prefs.LocalNotificationPrefs
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** 本机通知渠道展示定义：渠道键 + 标题 + 描述。 */
data class LocalNotificationChannelDef(
    val key: String,
    val title: String,
    val subtitle: String,
)

/**
 * 本机通知设置页 UI 状态。
 *
 * @param enabled 本机通知总开关
 * @param perChannel 渠道键 -> 该渠道开关状态
 * @param channels 渠道展示定义（用于列表渲染的元数据）
 */
data class LocalNotificationUiState(
    val enabled: Boolean = true,
    val perChannel: Map<String, Boolean> = emptyMap(),
    val channels: List<LocalNotificationChannelDef> = emptyList(),
)

/**
 * 本机通知设置页 ViewModel。
 *
 * 从 [LocalNotificationPrefs] 读取总开关与各渠道开关，并通过
 * [setEnabled] / [setChannelEnabled] 即时持久化，状态以
 * [StateFlow] 暴露给 Compose 屏幕。
 */
class LocalNotificationViewModel(
    private val prefs: LocalNotificationPrefs,
) : ViewModel() {

    private val _uiState = MutableStateFlow(LocalNotificationUiState())
    val uiState: StateFlow<LocalNotificationUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    private fun refresh() {
        val switches = CHANNELS.associate { it.key to prefs.getChannelEnabled(it.key) }
        _uiState.update {
            LocalNotificationUiState(
                enabled = prefs.enabled,
                perChannel = switches,
                channels = CHANNELS,
            )
        }
    }

    /** 切换本机通知总开关并即时持久化。 */
    fun setEnabled(value: Boolean) {
        prefs.enabled = value
        _uiState.update { it.copy(enabled = value) }
    }

    /** 切换单个渠道开关并即时持久化。 */
    fun setChannelEnabled(channel: String, value: Boolean) {
        prefs.setChannelEnabled(channel, value)
        _uiState.update { it.copy(perChannel = it.perChannel + (channel to value)) }
    }

    companion object {
        /** 本机通知渠道（与 Flutter 端 task / system 对齐）。 */
        private val CHANNELS = listOf(
            LocalNotificationChannelDef(
                key = "task",
                title = "任务通知",
                subtitle = "任务执行完成或失败时推送本地通知",
            ),
            LocalNotificationChannelDef(
                key = "system",
                title = "系统通知",
                subtitle = "面板系统事件和安全相关本地通知",
            ),
        )
    }
}
