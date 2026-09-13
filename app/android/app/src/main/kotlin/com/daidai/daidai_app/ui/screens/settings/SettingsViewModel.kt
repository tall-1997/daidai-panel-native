package com.daidai.daidai_app.ui.screens.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * 主题模式：沿用 Flutter `NetworkThemeMode` / Material `ThemeMode` 的三态语义
 * （system / light / dark）。本阶段仅在 ViewModel 层维护该状态，供
 * [ThemeSettingsScreen] 展示与切换，尚未真正写入 [com.daidai.daidai_app.ui.theme.AppTheme]
 * —— 待集成阶段再接 Theme.kt。
 */
enum class ThemeMode(val label: String) {
    SYSTEM("跟随系统"),
    LIGHT("浅色"),
    DARK("深色"),
}

/**
 * 设置入口列表项（对应 Flutter 设置页「关于 / 系统设置 / 主题 / 面板设置 / 日志等」）。
 * [title]、[subtitle] 为该入口在总设置页的展示文案；[route] 预留给后续导航集成。
 */
enum class SettingsEntry(
    val title: String,
    val subtitle: String,
    val route: String,
) {
    ABOUT("关于", "版本、开源许可与项目信息", "about"),
    SYSTEM_SETTINGS("系统设置", "时区、语言、代理与更新设置", "system_settings"),
    THEME("主题", "视觉风格、主题模式与色板预览", "theme"),
    PANEL_SETTINGS("面板设置", "面板标题、图标与外观", "panel_settings"),
    LOGS("日志", "运行日志与诊断信息", "logs"),
}

/**
 * 设置总页的可观察状态：当前主题模式 + 设置入口列表。
 */
data class SettingsUiState(
    val themeMode: ThemeMode = ThemeMode.SYSTEM,
    val entries: List<SettingsEntry> = SettingsEntry.entries,
)

/**
 * 阶段 4-3 设置模块的 ViewModel：
 * - 维护 [ThemeMode] 的 [StateFlow]，供主题切换页展示与回显；
 * - 提供设置入口列表 [SettingsEntry]（导航目标），点击回调由 Screen 层接出，
 *   暂未接入 NavHost。
 *
 * 集成点：后续将 [themeMode] 的订阅接到 [com.daidai.daidai_app.ui.theme.AppTheme]
 * （darkTheme 参数），并用 [PanelConfigRepository] 持久化用户选择。
 */
class SettingsViewModel : ViewModel() {
    private val _uiState = MutableStateFlow(SettingsUiState())
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    /** 切换主题模式（纯状态变更，不作用于 Theme.kt）。 */
    fun setThemeMode(mode: ThemeMode) {
        _uiState.update { it.copy(themeMode = mode) }
    }
}
