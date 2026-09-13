package com.daidai.daidai_app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * Compose 原生工程主题。
 *
 * 阶段 0 以 Material3 色彩方案作为基础（规划 §1.5 / 任务 §3 允许的兜底方案），
 * 主色覆盖为品牌主色 `primary #10B981`。
 *
 * 阶段 1 升级到 compose-miuix：在 [AppTheme] 内部用 `MiuixTheme` 包裹 [content]，
 * 将调色板迁移到 `top.yukonga.miuix.kmp` 的色彩方案（见 AppThemeMiuix.kt 扩展点）。
 * 届时保持 `AppTheme(content)` 签名不变，UI 无需改动。
 */
object AppThemes {
    fun lightColors() = lightColorScheme(
        primary = AppColors.primary,
        onPrimary = Color.White,
        primaryContainer = AppColors.primaryLight,
        onPrimaryContainer = AppColors.primaryDark,
        secondary = AppColors.miuixBlue,
        background = AppColors.lightPage,
        surface = AppColors.glassCard,
        surfaceVariant = AppColors.slate100,
        onBackground = AppColors.slate900,
        onSurface = AppColors.slate900,
        error = AppColors.red500,
        outline = AppColors.slate300,
    )

    fun darkColors() = darkColorScheme(
        primary = AppColors.primary,
        onPrimary = Color.White,
        primaryContainer = AppColors.primaryDark,
        onPrimaryContainer = AppColors.primaryLight,
        secondary = AppColors.miuixBlue,
        background = AppColors.darkPage,
        surface = AppColors.darkSurface,
        surfaceVariant = AppColors.slate800,
        onBackground = AppColors.slate100,
        onSurface = AppColors.slate100,
        error = AppColors.red500,
        outline = AppColors.slate500,
    )

    fun shapes() = Shapes(
        extraSmall = RoundedCornerShape(6.dp),
        small = RoundedCornerShape(10.dp),
        medium = RoundedCornerShape(16.dp),
        large = RoundedCornerShape(24.dp),
    )
}

@Composable
fun AppTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val colorScheme = if (darkTheme) AppThemes.darkColors() else AppThemes.lightColors()
    MaterialTheme(
        colorScheme = colorScheme,
        shapes = AppThemes.shapes(),
        content = content,
    )
}
