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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 可编辑的系统设置项（展示态）：时区 / 语言 / 代理等。
 *
 * 每个可编辑项通过 [onEdit] 回调暴露给调用方去做真正的交互（弹窗/页面等），
 * 本阶段仅展示当前值与一个「编辑」占位按钮。标注的集成点见各字段。
 */
data class SystemSettingsUiState(
    val timezone: String = "Asia/Shanghai",
    val language: String = "中文（简体）",
    val proxyUrl: String = "未配置",
    val updateMirror: String = "默认",
)

/**
 * 系统设置页：展示时区、语言、代理与更新镜像等系统级设置项（当前为展示态）。
 *
 * 可编辑项通过 [onEditSystemSetting] 回调暴露，真正交互（弹窗 / 设置页）留作
 * 集成点。默认值由 [SystemSettingsUiState] 提供，后续可替换为读取真实系统配置。
 */
@Composable
fun SystemSettingsScreen(
    modifier: Modifier = Modifier,
    settings: SystemSettingsUiState = remember { SystemSettingsUiState() },
    onEditSystemSetting: (SystemSettingKey) -> Unit = {},
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "系统设置",
            style = MaterialTheme.typography.headlineMedium,
            color = AppColors.primary,
        )
        Text(
            text = "时区、语言、代理与更新等系统级设置项。编辑交互待集成阶段接入。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        CardView(title = "基础") {
            SettingsRow(
                label = "时区",
                value = settings.timezone,
                onClick = { onEditSystemSetting(SystemSettingKey.TIMEZONE) },
            )
            SettingsRow(
                label = "语言",
                value = settings.language,
                onClick = { onEditSystemSetting(SystemSettingKey.LANGUAGE) },
            )
        }

        CardView(title = "网络") {
            SettingsRow(
                label = "代理地址",
                value = settings.proxyUrl,
                onClick = { onEditSystemSetting(SystemSettingKey.PROXY) },
            )
            SettingsRow(
                label = "系统更新镜像源",
                value = settings.updateMirror,
                onClick = { onEditSystemSetting(SystemSettingKey.UPDATE_MIRROR) },
            )
        }

        CardView(title = "集成点说明") {
            Text(
                text = "每个可编辑项通过 onEditSystemSetting 回调暴露。" +
                    "集成阶段将在此接入弹窗表单与 PanelConfigRepository 持久化。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** 系统设置可编辑项的枚举键，用于 [SystemSettingsScreen] 的编辑回调区分目标。 */
enum class SystemSettingKey {
    TIMEZONE,
    LANGUAGE,
    PROXY,
    UPDATE_MIRROR,
}

/** 单行「标签 —— 值 ... 编辑」展示行。 */
@Composable
private fun SettingsRow(
    label: String,
    value: String,
    onClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = label,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = value,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        TextButton(onClick = onClick) {
            Text("编辑", color = AppColors.primary)
        }
    }
}
