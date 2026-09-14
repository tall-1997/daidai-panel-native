package com.daidai.daidai_app.ui.screens.logs

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.model.LogStatus
import com.daidai.daidai_app.data.repository.LogsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 运行日志列表页（Compose 原生，阶段 2-2）。
 *
 * 展示任务日志条目卡片（任务名 / 状态 / 耗时 / 时间 / 内容预览），覆盖
 * 加载中 / 空态 / 错误重试 / 分页上拉加载 / 单条删除。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配 [LogsRepository]。
 * **未接线导航**，本组件自包含可编译，集成阶段再接入 AppNavHost。
 */
@Composable
fun LogListScreen(
    modifier: Modifier = Modifier,
    repository: LogsRepository? = null,
    viewModel: LogsViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            LogsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        LogsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        when {
            state.loading && state.logs.isEmpty() -> LoadingContent()
            state.errorMessage != null && state.logs.isEmpty() -> ErrorContent(
                message = state.errorMessage,
                onRetry = screenViewModel::refresh,
            )
            state.logs.isEmpty() -> EmptyContent()
            else -> LogList(
                state = state,
                onLoadMore = screenViewModel::loadMore,
                onDelete = screenViewModel::deleteLog,
            )
        }
    }
}

@Composable
private fun LogList(
    state: LogsUiState,
    onLoadMore: () -> Unit,
    onDelete: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 20.dp, top = 20.dp, end = 20.dp, bottom = 96.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(state.logs, key = { it.id }) { log ->
            LogEntryCard(
                log = log,
                deleting = state.deletingId == log.id,
                onDelete = { onDelete(log.id) },
            )
        }
        if (state.loadingMore || state.hasMore) {
            item(key = "footer") {
                LoadMoreFooter(loading = state.loadingMore, hasMore = state.hasMore, onLoadMore = onLoadMore)
            }
        }
    }
}

@Composable
private fun LogEntryCard(
    log: LogEntry,
    deleting: Boolean,
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val statusColor = statusColor(log.status)
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(12.dp)
                .clip(CircleShape)
                .background(statusColor),
        )
        Spacer(Modifier.width(12.dp))
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = log.taskName ?: "任务 #${log.taskId}",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.W700,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = log.status?.label ?: "未知状态",
                    style = MaterialTheme.typography.labelMedium,
                    color = statusColor,
                )
            }
            if (log.content.isNotBlank()) {
                Text(
                    text = log.content.replace('\n', ' '),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = formatCreatedAt(log.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.slate400,
                )
                if (log.duration != null) {
                    Spacer(Modifier.width(12.dp))
                    Text(
                        text = "耗时 ${formatDuration(log.duration)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = AppColors.slate400,
                    )
                }
            }
        }
        if (deleting) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
        } else {
            Text(
                text = "删除",
                style = MaterialTheme.typography.labelLarge,
                color = AppColors.red500,
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .background(AppColors.red50)
                    .padding(horizontal = 10.dp, vertical = 6.dp)
                    .clickable(onClick = onDelete),
            )
        }
    }
}

@Composable
private fun LoadMoreFooter(
    loading: Boolean,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.fillMaxWidth(),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(22.dp), strokeWidth = 2.dp)
        } else if (hasMore) {
            Button(onClick = onLoadMore) {
                Text("加载更多")
            }
        }
    }
}

@Composable
private fun LoadingContent(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator()
    }
}

@Composable
private fun ErrorContent(message: String?, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = "日志加载失败",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.W700,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = message ?: "未知错误，请重试",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(20.dp))
        Button(onClick = onRetry) {
            Text("重试")
        }
    }
}

@Composable
private fun EmptyContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = "暂无日志",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.W700,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = "运行中的任务日志会显示在这里",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}

private fun statusColor(status: LogStatus?): Color = when (status) {
    LogStatus.Success -> AppColors.primary
    LogStatus.Failed -> AppColors.red500
    LogStatus.Aborted -> AppColors.warningColor
    LogStatus.Running -> AppColors.miuixBlue
    null -> AppColors.slate400
}

/** 把 ISO 时间字符串（如 "2026-09-14T10:30:45Z"）格式化为 "yyyy-MM-dd HH:mm"。 */
private fun formatCreatedAt(raw: String?): String {
    if (raw.isNullOrBlank()) return "-"
    val t = raw.replace('T', ' ')
    val trimmed = t.dropLastWhile { it == 'Z' || it == 'z' }
    val parts = trimmed.split(' ')
    if (parts.size < 2) return raw
    val date = parts[0]
    val time = parts[1].take(5)
    return if (time.length == 5) "$date $time" else raw
}

/** 耗时展示：<1s 显示 ms，<60s 显示 s，否则显示 XmYs。 */
private fun formatDuration(seconds: Double): String {
    if (seconds < 0) return "-"
    if (seconds < 1) return "${(seconds * 1000).toInt()}ms"
    if (seconds < 60) return String.format("%.1fs", seconds)
    val total = seconds.toInt()
    val minutes = total / 60
    val secs = total % 60
    return "${minutes}m${secs}s"
}
