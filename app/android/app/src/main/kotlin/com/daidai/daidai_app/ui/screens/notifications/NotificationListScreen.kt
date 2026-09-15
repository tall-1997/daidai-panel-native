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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
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
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val repo = repository ?: defaultNotificationsRepository(context)
        NotificationsViewModel(repo)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()
    var showPushConfig by remember { mutableStateOf(false) }
    var pendingDelete by remember { mutableStateOf<com.daidai.daidai_app.data.model.NotificationChannel?>(null) }

    if (showPushConfig) {
        PushConfigScreen(
            modifier = modifier,
            repository = repository,
            onBack = { showPushConfig = false },
        )
        return
    }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "通知渠道",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = "服务端推送渠道配置见「推送配置」；本机系统通知开关在本页之外。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Button(onClick = { showPushConfig = true }) {
                Text("推送配置")
            }
        }
        when {
            state.isLoading -> LoadingContent(Modifier.fillMaxSize())
            state.errorMessage != null && state.channels.isEmpty() -> ErrorContent(
                message = state.errorMessage.orEmpty(),
                onRetry = { screenViewModel.load() },
                modifier = Modifier.fillMaxSize(),
            )
            state.channels.isEmpty() -> EmptyContent(Modifier.fillMaxSize())
            else -> ChannelList(
                channels = state.channels,
                onToggle = { screenViewModel.setChannelEnabled(it.id, !it.enabled) },
                onTest = { screenViewModel.testChannel(it) },
                onDelete = { pendingDelete = it },
                modifier = Modifier.fillMaxSize(),
            )
        }
    }

    pendingDelete?.let { channel ->
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除通知渠道") },
            text = { Text("确定删除渠道“${channel.name.ifBlank { "渠道 #" + channel.id }}”？") },
            confirmButton = {
                TextButton(onClick = { screenViewModel.deleteChannel(channel.id); pendingDelete = null }) {
                    Text("删除", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = { TextButton(onClick = { pendingDelete = null }) { Text("取消") } },
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
    onToggle: (NotificationChannel) -> Unit = {},
    onTest: (NotificationChannel) -> Unit = {},
    onDelete: (NotificationChannel) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(channels, key = { it.id }) { channel ->
            ChannelCard(
                channel = channel,
                onToggle = { onToggle(channel) },
                onTest = { onTest(channel) },
                onDelete = { onDelete(channel) },
            )
        }
    }
}

@Composable
private fun ChannelCard(
    channel: NotificationChannel,
    onToggle: () -> Unit = {},
    onTest: () -> Unit = {},
    onDelete: () -> Unit = {},
) {
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
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        if (channel.enabled) "已启用" else "已停用",
                        style = MaterialTheme.typography.labelMedium,
                        color = if (channel.enabled) AppColors.successColor else AppColors.slate500,
                    )
                    Switch(checked = channel.enabled, onCheckedChange = { onToggle() })
                }
                Row {
                    TextButton(onClick = onTest) { Text("测试") }
                    TextButton(onClick = onDelete) { Text("删除", color = MaterialTheme.colorScheme.error) }
                }
            }
        }
    }
}

private fun typeName(type: String): String {
    if (type.isBlank()) return "未知类型"
    com.daidai.daidai_app.data.model.NotificationChannelSchemas.byType(type)?.let { return it.name }
    return when (type.lowercase()) {
        "wechat", "qywx" -> "企业微信"
        "ding" -> "钉钉"
        else -> "类型：$type"
    }
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
