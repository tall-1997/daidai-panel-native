package com.daidai.daidai_app.ui.screens.system

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
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.model.BackupRecord
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 备份管理页（Compose 原生，阶段 4-5）。
 *
 * 展示备份列表（名称 / 大小 / 创建时间 / 删除），提供「新建备份」与「删除」写操作，
 * 覆盖 加载中 / 空态 / 错误（含重试） 三个分支。**未接导航**，自包含可编译。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时按项目惯例用
 * [BackupRepository] 装配（context 驱动连接解析）。
 */
@Composable
fun BackupScreen(
    repository: BackupRepository? = null,
    viewModel: SystemViewModel? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val screenViewModel = viewModel ?: remember { SystemViewModel(resolvedRepository) }

    val state by screenViewModel.uiState.collectAsState()
    var pendingDelete by remember { mutableStateOf<BackupRecord?>(null) }
    val snackbarHostState = remember { SnackbarHostState() }

    // 操作成功/失败文案经 snackbar 一次性展示
    LaunchedEffect(state.actionMessage) {
        state.actionMessage?.let { snackbarHostState.showSnackbar(it) }
    }
    LaunchedEffect(state.errorMessage) {
        state.errorMessage?.let { snackbarHostState.showSnackbar(it) }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = AppColors.lightPage,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            BackupTopBar(
                onRefresh = screenViewModel::refreshBackups,
                refreshing = state.backupPhase == SystemUiState.BackupPhase.Loading,
            )
        },
        bottomBar = {
            BackupCreateBar(
                creating = state.creatingBackup,
                onCreate = { if (!state.creatingBackup) screenViewModel.createBackup() },
            )
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when (state.backupPhase) {
                SystemUiState.BackupPhase.Loading -> LoadingView()
                SystemUiState.BackupPhase.Error -> ErrorView(
                    message = state.errorMessage ?: "加载备份列表失败",
                    onRetry = screenViewModel::refreshBackups,
                )
                SystemUiState.BackupPhase.Loaded -> {
                    if (state.backups.isEmpty()) {
                        EmptyView(
                            title = "暂无备份",
                            description = "点击下方按钮立即创建一份备份",
                        )
                    } else {
                        BackupList(
                            backups = state.backups,
                            deletingName = state.deletingBackupName,
                            onDelete = { pendingDelete = it },
                        )
                    }
                }
            }
        }
    }

    // 删除确认对话框
    pendingDelete?.let { record ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除备份") },
            text = { Text("确定删除备份「${record.name}」吗？该操作不可恢复。") },
            confirmButton = {
                Button(onClick = {
                    screenViewModel.deleteBackup(record)
                    pendingDelete = null
                }) { Text("删除") }
            },
            dismissButton = {
                Button(onClick = { pendingDelete = null }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun BackupTopBar(onRefresh: () -> Unit, refreshing: Boolean) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Column(verticalArrangement = Arrangement.Center) {
            Text(
                text = "系统备份",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
                color = AppColors.slate900,
            )
            Text(
                text = "备份面板配置与数据，可随时恢复",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
            )
        }
        IconButton(onClick = onRefresh, enabled = !refreshing) {
            if (refreshing) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp))
            } else {
                Icon(Icons.Filled.Refresh, contentDescription = "刷新", tint = AppColors.primaryDark)
            }
        }
    }
}

@Composable
private fun BackupCreateBar(creating: Boolean, onCreate: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(AppColors.lightPage)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        HorizontalDivider(color = AppColors.slate200)
        Spacer(modifier = Modifier.height(12.dp))
        Button(
            onClick = onCreate,
            enabled = !creating,
            modifier = Modifier.fillMaxWidth().height(48.dp),
        ) {
            if (creating) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp))
                Spacer(modifier = Modifier.width(8.dp))
                Text("正在创建…")
            } else {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(modifier = Modifier.width(8.dp))
                Text("新建备份")
            }
        }
    }
}

@Composable
private fun BackupList(
    backups: List<BackupRecord>,
    deletingName: String?,
    onDelete: (BackupRecord) -> Unit,
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(backups, key = { it.id }) { record ->
            BackupRow(
                record = record,
                deleting = deletingName == record.name,
                onDelete = { onDelete(record) },
            )
        }
    }
}

@Composable
private fun BackupRow(record: BackupRecord, deleting: Boolean, onDelete: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(AppColors.glassCard)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = record.name,
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
                color = AppColors.slate900,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = "${formatSize(record.size)} · ${record.createdAt.ifBlank { "-" }}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
            )
        }
        IconButton(onClick = onDelete, enabled = !deleting) {
            if (deleting) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp))
            } else {
                Icon(
                    Icons.Filled.Delete,
                    contentDescription = "删除",
                    tint = AppColors.errorColor,
                )
            }
        }
    }
}

/** 把字节数格式化为可读单位（B / KB / MB / GB）。 */
private fun formatSize(bytes: Long): String {
    if (bytes <= 0) return "0 B"
    val units = arrayOf("B", "KB", "MB", "GB", "TB")
    var value = bytes.toDouble()
    var unit = 0
    while (value >= 1024 && unit < units.lastIndex) {
        value /= 1024
        unit++
    }
    return if (value >= 100 || unit == 0) {
        "${value.toLong()} ${units[unit]}"
    } else {
        String.format(java.util.Locale.CHINA, "%.1f %s", value, units[unit])
    }
}
