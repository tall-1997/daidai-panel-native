package com.daidai.daidai_app.ui.screens.subscriptions

import androidx.compose.foundation.background
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.Subscription
import com.daidai.daidai_app.data.repository.SubscriptionsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 订阅列表页（Compose 原生，阶段 3-5）。
 *
 * 展示订阅卡片：名称 / 地址 / 目标路径 / 启用状态 / 最近拉取时间，并提供
 * 拉取（触发更新）、编辑（进入详情）与删除（含确认）操作；底部/头部提供新建。
 * 覆盖 加载中 / 空态 / 错误（含重试） 三个分支。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时用
 * [AppServices.configRepository] 装配的 [SubscriptionsRepository]。**未接线导航**，
 * 编辑/新建时在页内切换到 [SubscriptionDetailScreen]，本组件自包含可编译。
 */
@Composable
fun SubscriptionListScreen(
    modifier: Modifier = Modifier,
    repository: SubscriptionsRepository? = null,
    viewModel: SubscriptionsViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
SubscriptionsViewModel(repository ?: SubscriptionsRepository(context))
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 本地导航状态：编辑/新建目标；null 表示在列表模式。
    var editing by remember { mutableStateOf<Subscription?>(null) }
    var showingCreate by remember { mutableStateOf(false) }
    // 删除确认框的候选订阅。
    var pendingDelete by remember { mutableStateOf<Subscription?>(null) }

    Box(modifier = modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        when {
            editing != null -> SubscriptionDetailScreen(
                modifier = Modifier.fillMaxSize(),
                subscription = editing,
                repository = repository,
                viewModel = screenViewModel,
                onBack = { editing = null },
                onSaved = { editing = null },
            )
            showingCreate -> SubscriptionDetailScreen(
                modifier = Modifier.fillMaxSize(),
                subscription = null,
                repository = repository,
                viewModel = screenViewModel,
                onBack = { showingCreate = false },
                onSaved = { showingCreate = false },
            )
            else -> SubscriptionListContent(
                state = state,
                onCreate = { showingCreate = true },
                onEdit = { editing = it },
                onDelete = { pendingDelete = it },
                onPull = screenViewModel::pull,
                onRetry = screenViewModel::refresh,
                onClearError = screenViewModel::clearError,
            )
        }
    }

    // 删除确认对话框（置于 Box 之上）。
    pendingDelete?.let { sub ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除订阅") },
            text = { Text("确定要删除订阅“${sub.name.ifBlank { "未命名" }}”吗？此操作不可恢复。") },
            confirmButton = {
                TextButton(
                    onClick = {
                        screenViewModel.delete(sub.id)
                        pendingDelete = null
                    },
                ) {
                    Text("删除", color = AppColors.errorColor)
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) {
                    Text("取消")
                }
            },
        )
    }
}

@Composable
private fun SubscriptionListContent(
    state: SubscriptionsUiState,
    onCreate: () -> Unit,
    onEdit: (Subscription) -> Unit,
    onDelete: (Subscription) -> Unit,
    onPull: (Long) -> Unit,
    onRetry: () -> Unit,
    onClearError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize()) {
        // 顶部区：标题 + 新建按钮
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "订阅",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    text = "管理 Git 订阅的更新来源",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Button(onClick = onCreate) {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("新建订阅")
            }
        }

        if (state.errorMessage != null) {
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
                    text = state.errorMessage,
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.errorColor,
                    modifier = Modifier.weight(1f),
                )
                IconButton(onClick = onClearError, modifier = Modifier.size(20.dp)) {
                    Icon(Icons.Filled.Refresh, contentDescription = "清除错误", tint = AppColors.slate500)
                }
            }
        }

        when (state.phase) {
            SubscriptionsUiState.Phase.Loading -> LoadingView()
            SubscriptionsUiState.Phase.Error -> ErrorView(
                message = state.errorMessage ?: "订阅加载失败",
                onRetry = onRetry,
            )
            SubscriptionsUiState.Phase.Loaded ->
                if (state.subscriptions.isEmpty()) {
                    EmptyView(
                        title = "暂无订阅",
                        description = "还没有任何订阅，点击右上角“新建订阅”添加",
                    )
                } else {
                    SubscriptionList(
                        subscriptions = state.subscriptions,
                        busyId = state.busyId,
                        pullingId = state.pullingId,
                        onEdit = onEdit,
                        onDelete = onDelete,
                        onPull = onPull,
                    )
                }
        }
    }
}

@Composable
private fun SubscriptionList(
    subscriptions: List<Subscription>,
    busyId: Long?,
    pullingId: Long?,
    onEdit: (Subscription) -> Unit,
    onDelete: (Subscription) -> Unit,
    onPull: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(subscriptions, key = { it.id }) { sub ->
            SubscriptionCard(
                subscription = sub,
                busy = busyId == sub.id,
                pulling = pullingId == sub.id,
                onEdit = { onEdit(sub) },
                onDelete = { onDelete(sub) },
                onPull = { onPull(sub.id) },
            )
        }
    }
}

@Composable
private fun SubscriptionCard(
    subscription: Subscription,
    busy: Boolean,
    pulling: Boolean,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onPull: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = subscription.name.ifBlank { "未命名订阅" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f, fill = false),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.width(8.dp))
            EnabledBadge(enabled = subscription.enabled)
        }

        Text(
            text = subscription.url.ifBlank { "（未填写地址）" },
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.slate600,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )

        if (subscription.targetPath.isNotBlank()) {
            Text(
                text = "目标路径：${subscription.targetPath}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        Text(
            text = if (subscription.lastPullAt.isNotBlank()) {
                "最近拉取：${subscription.lastPullAt}"
            } else {
                "尚未拉取"
            },
            style = MaterialTheme.typography.labelSmall,
            color = AppColors.slate400,
        )

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TextButton(
                onClick = onPull,
                enabled = !busy && !pulling,
                modifier = Modifier.height(36.dp),
            ) {
                if (pulling) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(14.dp),
                        color = AppColors.primary,
                        strokeWidth = 2.dp,
                    )
                    Spacer(Modifier.width(6.dp))
                } else {
                    Icon(
                        Icons.Filled.Refresh,
                        contentDescription = null,
                        modifier = Modifier.size(16.dp),
                        tint = AppColors.primary,
                    )
                    Spacer(Modifier.width(4.dp))
                }
                Text(if (pulling) "拉取中…" else "拉取")
            }

            IconButton(onClick = onEdit, enabled = !busy && !pulling, modifier = Modifier.size(36.dp)) {
                Icon(Icons.Filled.Edit, contentDescription = "编辑", tint = AppColors.slate600)
            }
            IconButton(
                onClick = onDelete,
                enabled = !busy && !pulling,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(Icons.Filled.Delete, contentDescription = "删除", tint = AppColors.errorColor)
            }
        }
    }
}

@Composable
private fun EnabledBadge(enabled: Boolean, modifier: Modifier = Modifier) {
    val background = if (enabled) AppColors.primary else AppColors.slate300
    val content = if (enabled) Color.White else AppColors.slate700
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(4.dp))
            .background(background)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(
            text = if (enabled) "启用" else "禁用",
            style = MaterialTheme.typography.labelSmall,
            color = content,
            fontWeight = FontWeight.Bold,
        )
    }
}
