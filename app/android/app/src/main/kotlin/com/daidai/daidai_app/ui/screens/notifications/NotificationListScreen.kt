package com.daidai.daidai_app.ui.screens.notifications

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.NotificationChannel
import com.daidai.daidai_app.data.repository.NotificationsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 通知渠道列表页（Compose 原生，阶段 2-3）。
 *
 * 展示渠道卡片（渠道名 / 类型 / 开关状态），并处理加载中、空态与错误重试。
 * 依赖通过参数注入（repository / viewModel）；不传时默认从既有配置装配
 * NotificationsRepository（读取已持久化 serverUrl + accessToken）。
 *
 * 不接线 AppNavHost，页面自包含、可独立编译。
 */
@Composable
fun NotificationListScreen(
    modifier: Modifier = Modifier,
    repository: NotificationsRepository? = null,
    viewModel: NotificationsViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(repository, context) {
        val repo = repository ?: defaultNotificationsRepository(context)
        NotificationsViewModel(repo)
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    when {
        state.isLoading -> LoadingContent(modifier)
        state.errorMessage != null -> ErrorContent(
            message = state.errorMessage.orEmpty(),
            onRetry = { screenViewModel.load() },
            modifier = modifier,
        )
        state.channels.isEmpty() -> EmptyContent(modifier)
        else -> ChannelList(
            channels = state.channels,
            modifier = modifier,
        )
    }
}

@Composable
private fun LoadingContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        CircularProgressIndicator(color = AppColors.primary)
        Spacer(Modifier.width(8.dp))
        Text("正在加载通知渠道…", color = MaterialTheme.colorScheme.onSurface)
    }
}

@Composable
private fun ErrorContent(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("加载失败", style = MaterialTheme.typography.titleMedium, color = AppColors.errorColor)
        Spacer(Modifier.size(8.dp))
        Text(
            message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(Modifier.size(16.dp))
        Button(onClick = onRetry) {
            Text("重试")
        }
    }
}

@Composable
private fun EmptyContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("暂无通知渠道", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.size(8.dp))
        Text(
            "还没有配置任何通知渠道。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

@Composable
private fun ChannelList(
    channels: List<NotificationChannel>,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(channels) { channel ->
            ChannelCard(channel)
        }
    }
}

@Composable
private fun ChannelCard(channel: NotificationChannel) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    channel.name.ifBlank { "渠道 #${channel.id}" },
                    style = MaterialTheme.typography.titleMedium,
                )
                Spacer(Modifier.size(4.dp))
                val typeLabel = typeName(channel.type)
                Text(
                    typeLabel,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (channel.pushScope.isNotBlank() && channel.pushScope != "default") {
                    Spacer(Modifier.size(2.dp))
                    Text(
                        "推送范围：${channel.pushScope}",
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
            }
            Spacer(Modifier.width(12.dp))
            Column(horizontalAlignment = Alignment.End) {
                Text(
                    if (channel.enabled) "已启用" else "已停用",
                    style = MaterialTheme.typography.labelMedium,
                    color = if (channel.enabled) AppColors.successColor else AppColors.slate500,
                )
                Switch(checked = channel.enabled, onCheckedChange = null)
            }
        }
    }
}

private fun typeName(type: String): String = when (type.lowercase()) {
    "email" -> "邮件"
    "webhook" -> "Webhook"
    "discord" -> "Discord"
    "slack" -> "Slack"
    "telegram" -> "Telegram"
    "wecom", "wechat", "qywx" -> "企业微信"
    "dingtalk", "ding" -> "钉钉"
    "" -> "未知类型"
    else -> "类型：$type"
}

/**
 * 默认装配：从既有配置存储读取 serverUrl + accessToken 构造只读仓库。
 * 复用 AppServices.configRepository 读取（不修改装配层）。
 */
private fun defaultNotificationsRepository(context: android.content.Context): NotificationsRepository {
    val config = AppServices.configRepository(context).config.value
    return NotificationsRepository(
        baseUrl = config.serverUrl,
        accessToken = config.accessToken,
    )
}
