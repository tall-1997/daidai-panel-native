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
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.daidai.daidai_app.data.model.LogChannel
import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.model.LogStatus
import com.daidai.daidai_app.data.repository.LogsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.theme.AppColors

@Composable
fun LogListScreen(
    modifier: Modifier = Modifier,
    repository: LogsRepository? = null,
    viewModel: LogsViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: viewModel(initializer = {
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
    val listState = rememberLazyListState()
    
    // 自动滚动到最新
    LaunchedEffect(state.logs) {
        if (state.followMode) {
            listState.animateScrollToItem(state.logs.size - 1)
        }
    }
    
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        // 搜索和控制区
        SearchAndControls(
            keyword = state.keyword,
            followMode = state.followMode,
            onKeywordChange = { screenViewModel.setSearchKeyword(it) },
            onFollowModeChange = screenViewModel::setFollowMode,
        )
        
        // Tab 行
        TabRow(
            channels = LogChannel.values().toList(),
            selectedChannel = state.selectedChannel,
            onChannelSelected = screenViewModel::selectChannel,
        )
        
        // 日志列表
        when {
            state.loading && state.logs.isEmpty() -> {
                LoadingView(modifier = Modifier.weight(1f))
            }
            state.errorMessage != null && state.logs.isEmpty() -> {
                ErrorView(
                    message = state.errorMessage,
                    onRetry = screenViewModel::refresh,
                    modifier = Modifier.weight(1f),
                )
            }
            state.logs.isEmpty() -> {
                EmptyView(
                    message = "暂无日志",
                    hint = "该分类下暂无日志",
                    modifier = Modifier.weight(1f),
                )
            }
            else -> {
                LogList(
                    state = state,
                    listState = listState,
                    onLoadMore = screenViewModel::loadMore,
                    onDelete = screenViewModel::deleteLog,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        
        // 清理配置区
        CleanupConfigSection(
            config = state.cleanupConfig,
            onConfigChange = screenViewModel::updateCleanupConfig,
            onCleanup = { screenViewModel.cleanupLogs(state.selectedChannel, config = state.cleanupConfig) },
        )
    }
}

@Composable
private fun SearchAndControls(
    keyword: String,
    followMode: Boolean,
    onKeywordChange: (String) -> Unit,
    onFollowModeChange: (Boolean) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(16.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            BasicTextField(
                value = keyword,
                onValueChange = onKeywordChange,
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(horizontal = 16.dp, vertical = 12.dp),
                decorationBox = { innerTextField ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = "搜索",
                            style = MaterialTheme.typography.bodyMedium,
                            color = AppColors.slate400,
                            modifier = Modifier.padding(end = 8.dp),
                        )
                        innerTextField()
                    }
                },
            )
            Spacer(Modifier.width(12.dp))
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(horizontal = 12.dp, vertical = 8.dp)
                    .clickable { onFollowModeChange(!followMode) },
            ) {
                Text(
                    text = if (followMode) "跟随" else "暂停",
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (followMode) AppColors.success else AppColors.slate400,
                )
            }
        }
    }
}

@Composable
private fun TabRow(
    channels: List<LogChannel>,
    selectedChannel: LogChannel,
    onChannelSelected: (LogChannel) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        channels.forEach { channel ->
            val isSelected = channel == selectedChannel
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(20.dp))
                    .background(if (isSelected) AppColors.skyBlue else AppColors.slate100)
                    .clickable { onChannelSelected(channel) }
                    .padding(horizontal = 16.dp, vertical = 8.dp),
            ) {
                Text(
                    text = channel.displayName,
                    style = MaterialTheme.typography.bodySmall,
                    fontWeight = if (isSelected) FontWeight.W700 else FontWeight.W400,
                    color = if (isSelected) AppColors.white else AppColors.slate700,
                )
            }
        }
    }
    Spacer(Modifier.height(8.dp))
}

@Composable
private fun LogList(
    state: LogsUiState,
    listState: androidx.compose.foundation.lazy.LazyListState,
    onLoadMore: () -> Unit,
    onDelete: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    val filteredLogs = remember(state.logs, state.keyword) {
        if (state.keyword.isBlank()) {
            state.logs
        } else {
            state.logs.filter { log ->
                log.content.contains(state.keyword, ignoreCase = true) ||
                log.taskName?.contains(state.keyword, ignoreCase = true) == true
            }
        }
    }
    
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        state = listState,
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(filteredLogs, key = { it.id }) { log ->
                LogEntryCard(
                    log = log,
                    keyword = state.keyword,
                    deleting = state.deletingId == log.id,
                    onDelete = { onDelete(log.id) },
                )
        }
        if (state.loadingMore || state.hasMore) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    if (state.loadingMore) {
                        CircularProgressIndicator(modifier = Modifier.size(22.dp))
                    } else if (state.hasMore) {
                        Text(
                            text = "加载更多",
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate400,
                            modifier = Modifier.clickable { onLoadMore() },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun LogEntryCard(
    log: LogEntry,
    keyword: String,
    deleting: Boolean,
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val statusColor = statusColor(log.status)
    val logText = log.content
    
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(CircleShape)
                .background(statusColor)
                .align(Alignment.CenterVertically),
        )
        Spacer(Modifier.width(12.dp))
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = log.taskName ?: "任务 #${log.taskId}",
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.W700,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = log.status?.label ?: "未知",
                    style = MaterialTheme.typography.labelSmall,
                    color = statusColor,
                )
            }
            
            // 高亮搜索关键词
            if (logText.isNotBlank()) {
                val highlightedText = buildAnnotatedString {
                    val keyword = searchKeyword.trim()
                    if (keyword.isEmpty()) {
                        append(logText)
                    } else {
                        var startIndex = 0
                        val lowerText = logText.lowercase()
                        val lowerKeyword = keyword.lowercase()
                        var index = lowerText.indexOf(lowerKeyword, startIndex)
                        while (index >= 0) {
                            append(logText.substring(startIndex, index))
                            withStyle(
                                SpanStyle(
                                    backgroundColor = AppColors.amber200,
                                    fontWeight = FontWeight.W700,
                                ),
                            ) {
                                append(logText.substring(index, index + keyword.length))
                            }
                            startIndex = index + keyword.length
                            index = lowerText.indexOf(lowerKeyword, startIndex)
                        }
                        append(logText.substring(startIndex))
                    }
                }
                
                Text(
                    text = highlightedText,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            
            Row {
                Text(
                    text = formatCreatedAt(log.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.slate400,
                )
                if (log.duration != null) {
                    Spacer(Modifier.width(8.dp))
                    Text(
                        text = "耗时 ${formatDuration(log.duration)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = AppColors.slate400,
                    )
                }
            }
        }
        
        if (deleting) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp))
        } else {
            Text(
                text = "删除",
                style = MaterialTheme.typography.labelSmall,
                color = AppColors.red500,
                modifier = Modifier.clickable(onClick = onDelete),
            )
        }
    }
}

@Composable
private fun CleanupConfigSection(
    config: LogCleanupConfig?,
    onConfigChange: (LogCleanupConfig) -> Unit,
    onCleanup: () -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "日志清理配置",
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.W700,
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = if (expanded) "收起" else "展开",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
        }
        
        if (expanded && config != null) {
            Spacer(Modifier.height(12.dp))
            Row(
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "自动清理",
                    style = MaterialTheme.typography.bodyMedium,
                )
                Spacer(Modifier.width(12.dp))
                Switch(
                    checked = config.autoClean,
                    onCheckedChange = {
                        onConfigChange(config.copy(autoClean = it))
                    },
                )
            }
            Spacer(Modifier.height(8.dp))
            RetentionDaysSelector(
                days = config.retentionDays,
                onDaysChange = { days ->
                    onConfigChange(config.copy(retentionDays = days))
                },
            )
            Spacer(Modifier.height(12.dp))
            Text(
                text = "立即清理",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.primary,
                modifier = Modifier.clickable { onCleanup() },
            )
        }
    }
}

@Composable
private fun RetentionDaysSelector(
    days: Int,
    onDaysChange: (Int) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val options = listOf(7, 15, 30, 60, 90, 180, 365)
    
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = !expanded },
    ) {
        BasicTextField(
            value = "$days 天",
            onValueChange = {},
            modifier = Modifier
                .menuAnchor()
                .clip(RoundedCornerShape(8.dp))
                .background(MaterialTheme.colorScheme.background)
                .padding(12.dp),
            readOnly = true,
            decorationBox = { innerTextField ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(text = "保留", style = MaterialTheme.typography.bodyMedium)
                    Spacer(Modifier.width(8.dp))
                    innerTextField()
                }
            },
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            options.forEach { option ->
                Text(
                    text = "$option 天",
                    modifier = Modifier
                        .clickable {
                            onDaysChange(option)
                            expanded = false
                        }
                        .padding(12.dp),
                )
            }
        }
    }
}

private fun statusColor(status: LogStatus?): Color = when (status) {
    LogStatus.Success -> AppColors.green600
    LogStatus.Failed -> AppColors.red500
    LogStatus.Failed,
    LogStatus.Running -> AppColors.skyBlue
    LogStatus.Aborted -> AppColors.slate400
    null -> AppColors.slate400
}

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

private fun formatDuration(seconds: Double): String {
    if (seconds < 0) return "-"
    if (seconds < 1) return "${(seconds * 1000).toInt()}ms"
    if (seconds < 60) return String.format("%.1fs", seconds)
    val total = seconds.toInt()
    val minutes = total / 60
    val secs = total % 60
    return "${minutes}m${secs}s"
}
