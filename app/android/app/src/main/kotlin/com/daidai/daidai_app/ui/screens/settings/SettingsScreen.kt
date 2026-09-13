package com.daidai.daidai_app.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 设置总页：展示「关于 / 系统设置 / 主题 / 面板设置 / 日志」等入口列表，
 * 每个入口用 [CardView] 承载并暴露 [onNavigate] 点击回调。
 *
 * 本阶段不自建导航；点击回调由调用方（后续 NavHost 集成）传入，默认空实现。
 * 复用 [com.daidai.daidai_app.ui.components.CardView] 与 AppColors 令牌色。
 */
@Composable
fun SettingsScreen(
    modifier: Modifier = Modifier,
    viewModel: SettingsViewModel? = null,
    onNavigate: (SettingsEntry) -> Unit = {},
) {
    val vm = viewModel ?: remember { SettingsViewModel() }
    val state by vm.uiState.collectAsStateWithLifecycle()

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = "设置",
            style = MaterialTheme.typography.headlineMedium,
            color = AppColors.primary,
        )
        Text(
            text = "应用与面板的各项设置入口。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.size(4.dp))

        state.entries.forEach { entry ->
            SettingsEntryCard(
                entry = entry,
                onClick = { onNavigate(entry) },
            )
        }
    }
}

/**
 * 单个设置入口卡片：复用 [CardView]，右侧带「>」指示箭头。
 */
@Composable
private fun SettingsEntryCard(
    entry: SettingsEntry,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    CardView(
        modifier = modifier.fillMaxWidth(),
        onClick = onClick,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Filled.Settings,
                contentDescription = null,
                tint = AppColors.primary,
                modifier = Modifier.size(22.dp),
            )
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = entry.title,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = entry.subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Icon(
                imageVector = Icons.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = AppColors.slate400,
            )
        }
    }
}
