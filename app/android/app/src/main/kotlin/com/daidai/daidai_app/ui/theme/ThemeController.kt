package com.daidai.daidai_app.ui.theme

import android.content.Context
import android.content.SharedPreferences
import com.daidai.daidai_app.ui.screens.settings.ThemeMode
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 全局主题控制器：在 [com.daidai.daidai_app.ui.theme.AppTheme] 与设置模块之间共享主题模式。
 *
 * 用户选择经 [initialize]/[setMode] 持久化到 SharedPreferences，冷启动恢复；
 * 对齐 Flutter `theme_provider.dart` 的 theme_mode 持久化行为。
 */
object ThemeController {
    private const val FILE_NAME = "ui_theme"
    private const val KEY_MODE = "theme_mode"

    private val _mode = MutableStateFlow(ThemeMode.SYSTEM)
    val mode: StateFlow<ThemeMode> = _mode.asStateFlow()

    private var preferences: SharedPreferences? = null

    /** Application.onCreate 时调用：恢复持久化的主题选择。 */
    fun initialize(context: Context) {
        val prefs = context.applicationContext.getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)
        preferences = prefs
        _mode.value = prefs.getString(KEY_MODE, null)
            ?.let { stored -> ThemeMode.entries.firstOrNull { it.name == stored } }
            ?: ThemeMode.SYSTEM
    }

    /** 更新全局主题模式并持久化。 */
    fun setMode(mode: ThemeMode) {
        _mode.value = mode
        preferences?.edit()?.putString(KEY_MODE, mode.name)?.apply()
    }
}
