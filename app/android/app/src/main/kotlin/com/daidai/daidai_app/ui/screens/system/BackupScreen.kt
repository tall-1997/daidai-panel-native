package com.daidai.daidai_app.ui.screens.system

import android.app.Activity
import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import com.daidai.daidai_app.data.model.RestoreProgress
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 备份管理页（Compose 原生，阶段 4-5；C8 扩展上传/下载/恢复）。
 *
 * 备份列表（名称 / 大小 / 创建时间）+ 创建 / 删除 / 上传 / 下载 / 恢复写操作，
 * 覆盖 加载中 / 空态 / 错误（含重试）三个分支。**未接导航**，自包含可编译。
 *
 * 危险操作（删除 / 恢复）均有二次确认对话框；恢复冲突（活跃任务 409）等
 * 服务端错误通过 snackbar 诚实展示，不静默吞掉。
 * SAF 导入导出：上传走 OpenDocument，下载走 CreateDocument。
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
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = { SystemViewModel(resolvedRepository) })

    val state by screenViewModel.uiState.collectAsState()
    var pendingDelete by remember { mutableStateOf<BackupRecord?>(null) }
    var pendingRestore by remember { mutableStateOf<BackupRecord?>(null) }
    var restorePassword by remember { mutableStateOf("") }
    val snackbarHostState = remember { SnackbarHostState() }

    // SAF：导入备份（任意类型，服务端按扩展名白名单校验）
    val uploadLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument(),
    ) { uri: Uri? ->
        if (uri != null) {
            screenViewModel.uploadBackupFromUri(uri, queryDisplayName(context, uri))
        }
    }

    // SAF：导出备份到用户选择的位置
    val saveLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("application/octet-stream"),
    ) { uri: Uri? ->
        if (uri != null) {
            screenViewModel.finishDownload(uri)
        } else {
            screenViewModel.cancelPendingDownload()
        }
    }

    // 下载完成（缓存文件就绪）后拉起 Save 对话框
    LaunchedEffect(state.pendingDownloadFile) {
        state.pendingDownloadFile?.let {
            saveLauncher.launch(it.name.substringAfter('-', it.name))
        }
    }

    // 操作成功/失败文案经 snackbar 一次性展示
    LaunchedEffect(state.actionMessage) {
        state.actionMessage?.let { snackbarHostState.showSnackbar(it) }
    }
    LaunchedEffect(state.errorMessage) {
        state.errorMessage?.let { snackbarHostState.showSnackbar(it) }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MaterialTheme.colorScheme.background,
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
                onUpload = { uploadLauncher.launch(arrayOf("*/*")) },
                uploading = state.uploading,
                uploadProgress = state.uploadProgress,
                uploadFileName = state.uploadFileName,
                restoring = state.restoring,
                restore = state.restore,
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
                            downloadingName = state.downloadingName,
                            onDelete = { pendingDelete = it },
                            onDownload = { screenViewModel.downloadBackup(it) },
                            onRestore = {
                                restorePassword = ""
                                pendingRestore = it
                            },
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

    // 恢复确认对话框（危险操作：覆盖当前配置；带可选密码输入）
    pendingRestore?.let { record ->
        AlertDialog(
            onDismissRequest = { pendingRestore = null },
            title = { Text("恢复备份") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        "恢复「${record.name}」将覆盖当前面板数据，操作期间新任务会被暂停。确定继续吗？",
                        color = AppColors.errorColor,
                    )
                    OutlinedTextField(
                        value = restorePassword,
                        onValueChange = { restorePassword = it },
                        label = { Text("备份密码（如备份加密）") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            },
            confirmButton = {
                Button(onClick = {
                    screenViewModel.restoreBackup(record, restorePassword)
                    pendingRestore = null
                }) { Text("确认恢复") }
            },
            dismissButton = {
                Button(onClick = { pendingRestore = null }) { Text("取消") }
            },
        )
    }
}

/** 从 content:// Uri 查询显示名（OpenDocument 结果展示用）。 */
private fun queryDisplayName(context: Context, uri: Uri): String? = runCatching {
    context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
        if (cursor.moveToFirst() && !cursor.isNull(0)) cursor.getString(0) else null
    }
}.getOrNull()

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
private fun BackupCreateBar(
    creating: Boolean,
    onCreate: () -> Unit,
    onUpload: () -> Unit,
    uploading: Boolean,
    uploadProgress: Float?,
    uploadFileName: String,
    restoring: Boolean,
    restore: RestoreProgress?,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        // 上传 / 恢复 进度条（含阶段文案），空闲时隐藏
        if (uploading || restoring) {
            val percent = when {
                restoring && restore != null -> restore.percent.toFloat().coerceIn(0f, 100f) / 100f
                else -> uploadProgress ?: 0f
            }
            val label = when {
                restoring -> "正在恢复：${restore?.stage.orEmpty().ifBlank { "处理中" }}（${restore?.percent ?: 0}%）"
                else -> "正在上传：${uploadFileName.ifBlank { "备份文件" }}" +
                    (uploadProgress?.let { "（${(it * 100).toInt()}%）" } ?: "")
            }
            Text(label, style = MaterialTheme.typography.bodySmall, color = AppColors.slate600)
            if (uploading && uploadProgress == null) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth().height(4.dp))
            } else {
                LinearProgressIndicator(
                    modifier = Modifier.fillMaxWidth().height(4.dp),
                    progress = { percent },
                )
            }
            Spacer(modifier = Modifier.height(10.dp))
        }
        HorizontalDivider(color = AppColors.slate200)
        Spacer(modifier = Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(
                onClick = onUpload,
                enabled = !uploading && !restoring,
                modifier = Modifier.weight(1f),
            ) { Text("上传备份") }
            Button(
                onClick = onCreate,
                enabled = !creating && !uploading && !restoring,
                modifier = Modifier.weight(2f).height(48.dp),
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
}

@Composable
private fun BackupList(
    backups: List<BackupRecord>,
    deletingName: String?,
    downloadingName: String?,
    onDelete: (BackupRecord) -> Unit,
    onDownload: (BackupRecord) -> Unit,
    onRestore: (BackupRecord) -> Unit,
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
                downloading = downloadingName == record.name,
                busy = downloadingName != null || deletingName != null,
                onDelete = { onDelete(record) },
                onDownload = { onDownload(record) },
                onRestore = { onRestore(record) },
            )
        }
    }
}

@Composable
private fun BackupRow(
    record: BackupRecord,
    deleting: Boolean,
    downloading: Boolean,
    busy: Boolean,
    onDelete: () -> Unit,
    onDownload: () -> Unit,
    onRestore: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
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
        if (downloading) {
            CircularProgressIndicator(modifier = Modifier.size(18.dp))
            Spacer(modifier = Modifier.width(12.dp))
        } else {
            TextButton(onClick = onDownload, enabled = !busy) { Text("下载") }
        }
        TextButton(onClick = onRestore, enabled = !busy) { Text("恢复") }
        IconButton(onClick = onDelete, enabled = !deleting && !busy) {
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
