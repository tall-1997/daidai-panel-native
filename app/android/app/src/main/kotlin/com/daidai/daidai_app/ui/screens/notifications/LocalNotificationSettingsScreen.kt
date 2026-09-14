package com.daidai.daidai_app.ui.screens.notifications

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.prefs.LocalNotificationPrefs
import com.daidai.daidai_app.ui.components.CardView

/**
 * 本机通知设置页（Compose 原生）。
 *
 * 与 Flutter `local_notification_settings_page.dart` 对齐：顶部【本机通知】总开关 +
 * 【通知渠道】渠道开关列表（任务通知 / 系统通知）。所有开关即时持久化。
 *
 * 依赖通过参数注入（prefs / viewModel）；不传时默认通过 [LocalNotificationPrefs] 单例装配。
 * 不接线 AppNavHost，页面自包含、可独立编译。
 */
@Composable
fun LocalNotificationSettingsScreen(
    modifier: Modifier = Modifier,
    prefs: LocalNotificationPrefs? = null,
    viewModel: LocalNotificationViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
LocalNotificationViewModel(prefs ?: LocalNotificationPrefs.getInstance(context))
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "master") {
            CardView(title = "本机通知") {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = "允许接收本机通知",
                            style = MaterialTheme.typography.bodyLarge,
                        )
                        Text(
                            text = "总开关关闭后，本机将不再推送任何通知",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Switch(
                        checked = state.enabled,
                        onCheckedChange = { screenViewModel.setEnabled(it) },
                    )
                }
            }
        }

        item(key = "channels_header") {
            Text(
                text = "通知渠道",
                style = MaterialTheme.typography.labelLarge,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
        }

        items(state.channels, key = { it.key }) { channel ->
            val checked = state.perChannel[channel.key] ?: true
            CardView {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(channel.title, style = MaterialTheme.typography.bodyLarge)
                        Text(
                            text = channel.subtitle,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Switch(
                        checked = checked,
                        enabled = state.enabled,
                        onCheckedChange = { screenViewModel.setChannelEnabled(channel.key, it) },
                    )
                }
            }
        }
    }
}
