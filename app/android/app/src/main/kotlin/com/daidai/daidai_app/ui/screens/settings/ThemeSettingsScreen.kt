package com.daidai.daidai_app.ui.screens.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.RadioButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 主题设置页：主题模式三选一（跟随系统 / 浅色 / 深色）+ 色板预览（用
 * [AppColors] 令牌画色块）+ 主题切换状态。
 *
 * 注意：本阶段主题切换仅改变 [SettingsViewModel] 的 [ThemeMode] 状态，
 * 不实际修改 [com.daidai.daidai_app.ui.theme.AppTheme] —— 该接线留待集成阶段。
 * 色板预览同时展示 Material3 风格（Emerald/Slate）与 MIUIX 令牌色如需求所述。
 */
@Composable
fun ThemeSettingsScreen(
    modifier: Modifier = Modifier,
    viewModel: SettingsViewModel? = null,
    onThemeModeChange: (ThemeMode) -> Unit = {},
) {
    val currentMode by com.daidai.daidai_app.ui.theme.ThemeController.mode.collectAsStateWithLifecycle()
    val onModeChange: (ThemeMode) -> Unit = { mode ->
        onThemeModeChange(mode)
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "主题设置",
            style = MaterialTheme.typography.headlineMedium,
            color = AppColors.primary,
        )
        Text(
            text = "选择视觉风格与主题模式，选择实时应用到全局主题。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        // 主题模式选择
        CardView(title = "主题模式") {
            ThemeMode.entries.forEach { mode ->
                ThemeModeRow(
                    mode = mode,
                    selected = currentMode == mode,
                    onSelect = { onModeChange(mode) },
                )
            }
        }

        // Material3 风格色板预览
        CardView(title = "Material3 · Emerald/Slate 令牌") {
            PaletteSwatchGroup(
                swatches = listOf(
                    Swatch(AppColors.primary, "Primary"),
                    Swatch(AppColors.primaryDark, "PrimaryDark"),
                    Swatch(AppColors.primaryLight, "PrimaryLight"),
                    Swatch(AppColors.slate900, "Slate900"),
                    Swatch(AppColors.slate500, "Slate500"),
                    Swatch(AppColors.slate200, "Slate200"),
                ),
            )
        }

        // MIUIX 双风格色板预览
        CardView(title = "MIUIX 令牌") {
            PaletteSwatchGroup(
                swatches = listOf(
                    Swatch(AppColors.miuixRed, "MiuixRed"),
                    Swatch(AppColors.miuixGreen, "MiuixGreen"),
                    Swatch(AppColors.miuixBlue, "MiuixBlue"),
                    Swatch(AppColors.miuixPurple, "MiuixPurple"),
                    Swatch(AppColors.miuixYellow, "MiuixYellow"),
                ),
            )
        }

        // 主题切换状态回显
        CardView(title = "当前主题模式") {
            Text(
                text = currentMode.label,
                style = MaterialTheme.typography.bodyLarge,
                color = when {
                    currentMode == ThemeMode.DARK -> AppColors.miuixBlue
                    currentMode == ThemeMode.LIGHT -> AppColors.primary
                    else -> AppColors.slate500
                },
            )
            Text(
                text = "主题选择已持久化，重启后保持。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** 单个主题模式单选行。 */
@Composable
private fun ThemeModeRow(
    mode: ThemeMode,
    selected: Boolean,
    onSelect: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(
            selected = selected,
            onClick = onSelect,
            colors = RadioButtonDefaults.colors(
                selectedColor = AppColors.primary,
            ),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = mode.label,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** 单个色板色块 + 命名。 */
private data class Swatch(
    val color: Color,
    val name: String,
)

/** 一组色块预览：横向平铺展示令牌颜色。 */
@Composable
private fun PaletteSwatchGroup(swatches: List<Swatch>) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        swatches.chunked(3).forEach { rowSwatches ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                rowSwatches.forEach { swatch ->
                    Column(
                        modifier = Modifier.weight(1f),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Spacer(
                            modifier = Modifier
                                .height(48.dp)
                                .fillMaxWidth()
                                .clip(RoundedCornerShape(8.dp))
                                .background(swatch.color),
                        )
                        Text(
                            text = swatch.name,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                }
            }
        }
    }
}
