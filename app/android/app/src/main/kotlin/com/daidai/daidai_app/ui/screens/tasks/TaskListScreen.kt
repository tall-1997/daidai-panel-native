package com.daidai.daidai_app.ui.screens.tasks

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.TextButton
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
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
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.repository.PanelTasksRepository
import com.daidai.daidai_app.data.repository.TasksRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 任务列表页（Compose 原生，阶段 3-1）。
 *
 * 展示任务卡片：名称 / 类型 / 状态徽标 / 调度 / 启停开关 / 运行 / 编辑 / 删除，
 * 并覆盖 加载中 / 空态 / 错误（含重试）三个分支。不接线导航：本组件自包含可编译，
 * 需要进入表单页时通过 [onCreateTask] / [onEditTask] 回调交给调用方。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配读写仓库。
 */
@Composable
fun TaskListScreen(
    modifier: Modifier = Modifier,
    repository: TasksRepository? = null,
    viewModel: TasksViewModel? = null,
    onCreateTask: () -> Unit = {},
    onEditTask: (Task) -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelTasksRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        TasksViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 表单/详情在独立返回栈条目保存，回到本页时刷新一次保证数据最新。
    LaunchedEffect(Unit) { screenViewModel.refresh() }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        when (state.phase) {
            TasksUiState.Phase.Loading -> LoadingView()
            TasksUiState.Phase.Error -> ErrorView(
                message = state.errorMessage ?: "任务列表加载失败",
                onRetry = screenViewModel::refresh,
            )
            TasksUiState.Phase.Loaded ->
                if (state.tasks.isEmpty()) {
                    EmptyContent()
                } else {
                    TaskList(
                        tasks = state.tasks,
                        busyTaskId = state.busyTaskId,
                        errorMessage = state.errorMessage,
                        onToggle = screenViewModel::toggleTask,
                        onRun = screenViewModel::runTask,
                        onDelete = screenViewModel::deleteTask,
                        onEdit = onEditTask,
                        onPin = screenViewModel::setPinned,
                        onCopy = screenViewModel::copyTask,
                        onBatchRun = { ids -> screenViewModel.batchRun(ids) },
                        onClearError = screenViewModel::clearError,
                    )
                }
        }
        // 创建任务 FAB：固定在右下角、始终可见并绑定 onClick。
        // 旧实现把「新建任务」按钮嵌在列表/空态内部，空态下按钮被 EmptyView 顶到屏幕底部
        // 且与底部导航/手势区重叠，导致点击无响应；此处改为标准 ExtendedFAB（与
        // OpenApiListScreen 一致），onClick 直接透传 onCreateTask。
        androidx.compose.material3.ExtendedFloatingActionButton(
            onClick = onCreateTask,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(20.dp),
        ) {
            Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(6.dp))
            Text("新建任务")
        }
    }
}

@Composable
private fun EmptyContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize().padding(20.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        EmptyView(
            title = "暂无任务",
            description = "还没有创建任何任务，点击右下角新建任务",
        )
    }
}

@Composable
private fun TaskList(
    tasks: List<Task>,
    busyTaskId: Long?,
    errorMessage: String?,
    onToggle: (Long, Boolean) -> Unit,
    onRun: (Long) -> Unit,
    onDelete: (Long) -> Unit,
    onEdit: (Task) -> Unit,
    onPin: (Long, Boolean) -> Unit,
    onCopy: (Long) -> Unit,
    onBatchRun: (List<Long>) -> Unit,
    onClearError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var selecting by remember { mutableStateOf(false) }
    var selectedIds by remember { mutableStateOf(setOf<Long>()) }
    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.End,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (selecting) {
                Text(
                    "已选 ${selectedIds.size}/10",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = { selecting = false; selectedIds = emptySet() }) { Text("取消") }
                Button(
                    onClick = {
                        onBatchRun(selectedIds.toList())
                        selecting = false
                        selectedIds = emptySet()
                    },
                    enabled = selectedIds.isNotEmpty() && selectedIds.size <= 10,
                ) { Text("运行所选") }
            } else {
                TextButton(onClick = { selecting = true }) { Text("批量运行") }
            }
        }
        if (errorMessage != null) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 6.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(AppColors.errorColor.copy(alpha = 0.12f))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = errorMessage,
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.errorColor,
                    modifier = Modifier.weight(1f),
                )
                IconButton(onClick = onClearError, modifier = Modifier.size(20.dp)) {
                    Icon(Icons.Filled.Close, contentDescription = "关闭", tint = AppColors.slate500)
                }
            }
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(start = 20.dp, top = 20.dp, end = 20.dp, bottom = 96.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(tasks, key = { it.id }) { task ->
                TaskCard(
                    task = task,
                    busy = busyTaskId == task.id,
                    selected = selecting && task.id in selectedIds,
                    selecting = selecting,
                    onSelectToggle = {
                        selectedIds = if (task.id in selectedIds) selectedIds - task.id else selectedIds + task.id
                    },
                    onToggle = { onToggle(task.id, !task.enabled) },
                    onRun = { onRun(task.id) },
                    onDelete = { onDelete(task.id) },
                    onEdit = { onEdit(task) },
                    onPin = { onPin(task.id, !task.pinned) },
                    onCopy = { onCopy(task.id) },
                )
            }
        }
    }
}

@Composable
private fun TaskCard(
    task: Task,
    busy: Boolean,
    selected: Boolean,
    selecting: Boolean,
    onSelectToggle: () -> Unit,
    onToggle: () -> Unit,
    onRun: () -> Unit,
    onDelete: () -> Unit,
    onEdit: () -> Unit,
    onPin: () -> Unit,
    onCopy: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = task.name.ifBlank { "（未命名任务）" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
            StatusBadge(
                running = task.running,
                enabled = task.enabled,
                label = task.statusText,
            )
        }

        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                text = "类型：${task.typeText}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
            Text(
                text = "脚本：${task.scriptPath.ifBlank { "—" }}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = "调度：${task.schedule.ifBlank { "手动/未设置" }}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (task.createdAt.isNotBlank()) {
                Text(
                    text = "创建于 ${task.createdAt}",
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.slate400,
                )
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = if (task.enabled) "已启用" else "已禁用",
                    style = MaterialTheme.typography.labelMedium,
                    color = AppColors.slate600,
                )
                Switch(
                    checked = task.enabled,
                    onCheckedChange = { onToggle() },
                    enabled = !busy,
                    colors = SwitchDefaults.colors(
                        checkedThumbColor = AppColors.primary,
                        checkedTrackColor = AppColors.primary.copy(alpha = 0.3f),
                    ),
                )
            }

            Spacer(Modifier.weight(1f))

            IconButton(
                onClick = onRun,
                enabled = !busy,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    Icons.Filled.PlayArrow,
                    contentDescription = "运行",
                    tint = AppColors.primary,
                )
            }
            if (selecting) {
                androidx.compose.material3.Checkbox(checked = selected, onCheckedChange = { onSelectToggle() })
            }
            IconButton(
                onClick = onPin,
                enabled = !busy,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    Icons.Filled.Star,
                    contentDescription = if (task.pinned) "取消置顶" else "置顶",
                    tint = if (task.pinned) AppColors.primary else AppColors.slate500,
                )
            }
            TextButton(onClick = onCopy, enabled = !busy) { Text("复制") }
            IconButton(
                onClick = onEdit,
                enabled = !busy,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    Icons.Filled.Edit,
                    contentDescription = "编辑",
                    tint = AppColors.slate600,
                )
            }
            IconButton(
                onClick = onDelete,
                enabled = !busy,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    Icons.Filled.Delete,
                    contentDescription = "删除",
                    tint = AppColors.errorColor,
                )
            }
        }
    }
}

@Composable
private fun StatusBadge(
    running: Boolean,
    enabled: Boolean,
    label: String,
    modifier: Modifier = Modifier,
) {
    val background: Color
    val content: Color
    when {
        running -> { background = AppColors.runningColor; content = Color.White }
        enabled -> { background = AppColors.primaryLight; content = AppColors.primaryDark }
        else -> { background = AppColors.slate300; content = AppColors.slate700 }
    }
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(6.dp))
            .background(background)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = content,
            fontWeight = FontWeight.Bold,
            textAlign = TextAlign.Center,
        )
    }
}