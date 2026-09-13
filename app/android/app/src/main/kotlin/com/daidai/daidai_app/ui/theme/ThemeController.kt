package com.daidai.daidai_app.ui.theme

import com.daidai.daidai_app.ui.screens.settings.ThemeMode
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 全局主题控制器：在 [com.daidai.daidai_app.ui.theme.AppTheme] 与设置模块
 * （[com.daidai.daidai_app.ui.screens.settings.SettingsViewModel]）之间共享主题模式。
 *
 * SettingsViewModel / 集成层调用 [setMode] 写入用户选择，[AppTheme] 在组合期订阅
 * [mode] 驱动 light/dark 色彩方案。默认 [ThemeMode.SYSTEM]（跟随系统）。
 */
object ThemeController {
    private val _mode = MutableStateFlow(ThemeMode.SYSTEM)
    val mode: StateFlow<ThemeMode> = _mode.asStateFlow()

    /** 更新全局主题模式。 */
    fun setMode(mode: ThemeMode) {
        _mode.value = mode
    }
}
