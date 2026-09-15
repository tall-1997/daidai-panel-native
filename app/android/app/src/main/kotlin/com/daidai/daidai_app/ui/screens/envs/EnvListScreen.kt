package com.daidai.daidai_app.ui.screens.envs

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.model.parseQlEnvText
import com.daidai.daidai_app.data.repository.EnvsRepository
import com.daidai.daidai_app.data.repository.PanelEnvsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 环境变量列表页（Compose 原生，阶段 3-2 + F5 增强）。
 *
 * 展示环境变量卡片：名称 / 脱敏值 / 分组 / 备注 / 启用开关 / 编辑 / 删除 /
 * 排序（上移 / 下移 / 置顶），并覆盖 加载中、空态、错误（含重试）三个分支。
 * 顶部提供：分组筛选 chips（全部 / 默认 / 各分组）、导入、导出、新建。
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配读仓库。
 *
 * [onCreate] / [onEdit] 为「新建 / 编辑」的跳转回调：**未接导航**，调用方不接线时
 * 为空操作，本组件自包含可编译。
 */
@Composable
fun EnvListScreen(
    modifier: Modifier = Modifier,
    repository: EnvsRepository? = null,
    viewModel: EnvsViewModel? = null,
    onCreate: () -> Unit = {},
    onEdit: (EnvVar) -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelEnvsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        EnvsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 表单/详情在独立返回栈条目保存，回到本页时刷新一次保证数据最新。
    LaunchedEffect(Unit) { screenViewModel.refresh() }
    var pendingDeleteId by remember { mutableStateOf<Long?>(null) }
    var showImport by remember { mutableStateOf(false) }
    var showGroupManage by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current

    if (pendingDeleteId != null) {
        AlertDialog(
            onDismissRequest = { pendingDeleteId = null },
            title = { Text("删除环境变量") },
            text = { Text("确定删除该环境变量？此操作不可撤销。") },
            confirmButton = {
                TextButton(onClick = {
                    screenViewModel.delete(pendingDeleteId!!)
                    pendingDeleteId = null
                }) { Text("删除", color = AppColors.errorColor) }
            },
            dismissButton = {
                TextButton(onClick = { pendingDeleteId = null }) { Text("取消") }
            },
        )
    }

    if (showImport) {
        ImportEnvDialog(
            onDismiss = { showImport = false },
            onConfirm = { items ->
                showImport = false
                screenViewModel.importEnvItems(items)
            },
        )
    }

    if (showGroupManage) {
        GroupManageDialog(
            groups = state.groups,
            ungroupedCount = state.envs.count { it.groups.isEmpty() },
            onDismiss = { showGroupManage = false },
            onRename = { oldName, newName ->
                val trimmed = newName.trim()
                if (trimmed.isEmpty()) return@GroupManageDialog
                if (oldName.isEmpty()) {
                    // 新建分组：把第一个未分组 env 挂到新组（分组列表由 env 的 groups 推导）。
                    // 若无未分组 env，提示用户在「新建变量 / 编辑」表单中选择自定义分组创建。
                    val firstUngrouped = state.envs.firstOrNull { it.groups.isEmpty() }
                    if (firstUngrouped != null) {
                        screenViewModel.assignGroups(firstUngrouped.id, listOf(trimmed))
                    }
                } else {
                    state.envs.filter { it.primaryGroup() == oldName }.forEach { env ->
                        val updated = env.groups.map { if (it == oldName) trimmed else it }
                        screenViewModel.assignGroups(env.id, updated)
                    }
                }
            },
            onDelete = { groupName ->
                state.envs.filter { it.primaryGroup() == groupName }.forEach { env ->
                    screenViewModel.assignGroups(env.id, env.groups.filterNot { it == groupName })
                }
            },
        )
    }

    // 导入结果反馈：仅复制动作在 LaunchedEffect 中一次性执行；对话框由用户点击关闭，
    // 避免 LaunchedEffect 消费过快导致对话框闪现。
    state.importResult?.let { result ->
        val message = buildString {
            append("导入完成：成功 ${result.success} 项")
            if (result.skipped > 0) append("，跳过 ${result.skipped} 项")
            if (result.errors.isNotEmpty()) append("\n").append(result.errors.take(3).joinToString("\n"))
        }
        AlertDialog(
            onDismissRequest = { screenViewModel.consumeImportResult() },
            title = { Text("导入结果") },
            text = { Text(message) },
            confirmButton = {
                TextButton(onClick = { screenViewModel.consumeImportResult() }) { Text("确定") }
            },
        )
    }

    // 导出结果反馈：复制到剪贴板（一次性），对话框由用户点击关闭。
    LaunchedEffect(state.exportResult) {
        state.exportResult?.let { result ->
            clipboard.setText(AnnotatedString(result.text))
        }
    }
    state.exportResult?.let { result ->
        val message = buildString {
            append("已导出 ${result.count} 项到剪贴板")
            if (result.skippedNames.isNotEmpty()) {
                append("\n跳过非法名称：").append(result.skippedNames.take(5).joinToString(", "))
            }
        }
        AlertDialog(
            onDismissRequest = { screenViewModel.consumeExportResult() },
            title = { Text("导出结果") },
            text = { Text(message) },
            confirmButton = {
                TextButton(onClick = { screenViewModel.consumeExportResult() }) { Text("确定") }
            },
        )
    }

    val filteredEnvs = remember(state.envs, state.selectedGroup) {
        when (state.selectedGroup) {
            null -> state.envs
            "" -> state.envs.filter { it.primaryGroup().isEmpty() }
            else -> state.envs.filter { it.primaryGroup() == state.selectedGroup }
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        EnvHeader(
            count = state.envs.size,
            onCreate = onCreate,
            onImport = { showImport = true },
            onExport = { screenViewModel.exportEnvText() },
            onManageGroups = { showGroupManage = true },
        )
        GroupFilterBar(
            groups = state.groups,
            selectedGroup = state.selectedGroup,
            onSelect = { screenViewModel.selectGroup(it) },
        )
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background),
        ) {
            when (state.phase) {
                EnvListPhase.Loading -> LoadingContent()
                EnvListPhase.Error -> ErrorContent(
                    message = state.errorMessage ?: "环境变量加载失败",
                    onRetry = screenViewModel::refresh,
                )
                EnvListPhase.Loaded ->
                    if (filteredEnvs.isEmpty()) {
                        EmptyContent(onCreate = onCreate)
                    } else {
                        EnvList(
                            envs = filteredEnvs,
                            busyId = state.busyId,
                            busyType = state.busyType,
                            onToggle = { env -> screenViewModel.setEnabled(env.id, !env.enabled) },
                            onEdit = onEdit,
                            onDelete = { id -> pendingDeleteId = id },
                            onMoveTop = { env -> screenViewModel.move(env.id, MoveDirection.Top) },
                            onMoveUp = { env -> screenViewModel.move(env.id, MoveDirection.Up) },
                            onMoveDown = { env -> screenViewModel.move(env.id, MoveDirection.Down) },
                        )
                    }
            }
        }
    }
}

/** 分组筛选栏：全部 / 默认分组 / 各分组。 */
@Composable
private fun GroupFilterBar(
    groups: List<String>,
    selectedGroup: String?,
    onSelect: (String?) -> Unit,
    modifier: Modifier = Modifier,
) {
    val labels = buildList {
        add(null to "全部")
        add("" to "默认分组")
        groups.forEach { add(it to it) }
    }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        labels.forEach { (group, label) ->
            FilterChip(
                selected = selectedGroup == group,
                onClick = { onSelect(group) },
                label = { Text(label) },
            )
        }
    }
}

@Composable
private fun EnvHeader(
    count: Int,
    onCreate: () -> Unit,
    onImport: () -> Unit,
    onExport: () -> Unit,
    onManageGroups: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 20.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "环境变量",
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
            fontWeight = FontWeight.Bold,
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = if (count > 0) "共 $count 项" else "",
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.slate400,
        )
        Spacer(Modifier.weight(1f))
        TextButton(onClick = onImport) {
            Text("导入", color = AppColors.primary)
        }
        TextButton(onClick = onExport) {
            Text("导出", color = AppColors.primary)
        }
        TextButton(onClick = onManageGroups) {
            Text("分组", color = AppColors.primary)
        }
        Button(onClick = onCreate) {
            Text("新建变量")
        }
    }
}

@Composable
private fun EnvList(
    envs: List<EnvVar>,
    busyId: Long?,
    busyType: EnvsUiState.BusyType,
    onToggle: (EnvVar) -> Unit,
    onEdit: (EnvVar) -> Unit,
    onDelete: (Long) -> Unit,
    onMoveTop: (EnvVar) -> Unit,
    onMoveUp: (EnvVar) -> Unit,
    onMoveDown: (EnvVar) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(envs, key = { it.id }) { env ->
            EnvVarCard(
                env = env,
                toggling = busyId == env.id && busyType == EnvsUiState.BusyType.Toggle,
                deleting = busyId == env.id && busyType == EnvsUiState.BusyType.Delete,
                onToggle = { onToggle(env) },
                onEdit = { onEdit(env) },
                onDelete = { onDelete(env.id) },
                onMoveTop = { onMoveTop(env) },
                onMoveUp = { onMoveUp(env) },
                onMoveDown = { onMoveDown(env) },
            )
        }
    }
}

@Composable
private fun EnvVarCard(
    env: EnvVar,
    toggling: Boolean,
    deleting: Boolean,
    onToggle: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onMoveTop: () -> Unit,
    onMoveUp: () -> Unit,
    onMoveDown: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = env.name,
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
                fontWeight = FontWeight.Bold,
            )
            Spacer(Modifier.width(8.dp))
            EnvEnabledBadge(env.enabled)
            Spacer(Modifier.weight(1f))
            if (toggling) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                Spacer(Modifier.width(6.dp))
            }
            Switch(checked = env.enabled, onCheckedChange = { onToggle() })
        }
        if (env.value.isNotEmpty()) {
            Spacer(Modifier.height(4.dp))
            Text(
                text = "值：${maskEnvValue(env.value)}",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
                maxLines = 1,
            )
        }
        if (env.primaryGroup().isNotEmpty() || env.remark.isNotBlank()) {
            Spacer(Modifier.height(6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                if (env.primaryGroup().isNotEmpty()) {
                    EnvGroupBadge(env.primaryGroup())
                }
                if (env.remark.isNotBlank()) {
                    Text(
                        text = env.remark,
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                        maxLines = 1,
                    )
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onEdit) {
                Text("编辑", color = AppColors.primary)
            }
            TextButton(onClick = onMoveTop) {
                Text("置顶", color = AppColors.slate500)
            }
            TextButton(onClick = onMoveUp) {
                Text("上移", color = AppColors.slate500)
            }
            TextButton(onClick = onMoveDown) {
                Text("下移", color = AppColors.slate500)
            }
            TextButton(
                onClick = onDelete,
                enabled = !deleting,
            ) {
                if (deleting) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.width(6.dp))
                }
                Text("删除", color = AppColors.errorColor)
            }
        }
    }
}

@Composable
private fun EnvEnabledBadge(enabled: Boolean, modifier: Modifier = Modifier) {
    val background = if (enabled) AppColors.primaryLight else AppColors.slate300
    val content = if (enabled) AppColors.primaryDark else AppColors.slate700
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

@Composable
private fun EnvGroupBadge(group: String, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(4.dp))
            .background(AppColors.blue100)
            .padding(horizontal = 8.dp, vertical = 2.dp),
    ) {
        Text(
            text = group,
            style = MaterialTheme.typography.labelSmall,
            color = AppColors.blue600,
        )
    }
}

/** 脱敏显示：保留前 3 位 + **** + 后 3 位；长度过短时整体掩码。 */
internal fun maskEnvValue(value: String): String {
    if (value.isEmpty()) return ""
    if (value.length <= 6) return "****"
    return value.take(3) + "****" + value.takeLast(3)
}

/** 批量导入对话框：多行文本粘贴解析青龙格式，预览可导入条目。 */
@Composable
private fun ImportEnvDialog(
    onDismiss: () -> Unit,
    onConfirm: (List<com.daidai.daidai_app.data.model.QlEnvItem>) -> Unit,
) {
    var text by remember { mutableStateOf("") }
    val parsed = remember(text) { parseQlEnvText(text) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("批量导入环境变量") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    label = { Text("青龙 ql 格式，每行一条") },
                    placeholder = {
                        Text("示例：\nJD_COOKIE=\"123abc\" 我的京东\nAPI_KEY=abc123 备注\nTOKEN 456 空格格式")
                    },
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(180.dp),
                )
                Text(
                    text = "识别 ${parsed.items.size} 条" +
                        if (parsed.errors.isNotEmpty()) "，${parsed.errors.size} 行无法解析" else "",
                    style = MaterialTheme.typography.bodySmall,
                    color = if (parsed.errors.isNotEmpty()) AppColors.errorColor else AppColors.slate500,
                )
                if (parsed.errors.isNotEmpty()) {
                    Text(
                        text = parsed.errors.take(3).joinToString("\n"),
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.errorColor,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(parsed.items) },
                enabled = parsed.items.isNotEmpty(),
            ) {
                Text("导入")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        },
    )
}

/** 分组管理对话框：新建 / 重命名 / 删除分组。 */
@Composable
private fun GroupManageDialog(
    groups: List<String>,
    ungroupedCount: Int,
    onDismiss: () -> Unit,
    onRename: (String, String) -> Unit,
    onDelete: (String) -> Unit,
) {
    var creating by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }
    var renaming by remember { mutableStateOf<String?>(null) }
    var renameValue by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("分组管理") },
        text = {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(300.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (creating) {
                    OutlinedTextField(
                        value = newName,
                        onValueChange = { newName = it },
                        label = { Text("新分组名称") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    if (ungroupedCount == 0) {
                        Text(
                            text = "所有变量都已分组，新建分组请到「新建变量 / 编辑」下拉选择自定义分组",
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.warningColor,
                        )
                    }
                    Row {
                        TextButton(onClick = {
                            val name = newName.trim()
                            if (name.isNotEmpty()) {
                                onRename("", name)
                                creating = false
                                newName = ""
                            }
                        }) { Text("保存") }
                        TextButton(onClick = {
                            creating = false
                            newName = ""
                        }) { Text("取消") }
                    }
                } else {
                    TextButton(onClick = { creating = true }) {
                        Text("+ 新建分组", color = AppColors.primary)
                    }
                }
                if (renaming != null) {
                    OutlinedTextField(
                        value = renameValue,
                        onValueChange = { renameValue = it },
                        label = { Text("重命名分组") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row {
                        TextButton(onClick = {
                            val target = renaming
                            val name = renameValue.trim()
                            if (target != null && name.isNotEmpty()) {
                                onRename(target, name)
                            }
                            renaming = null
                        }) { Text("保存") }
                        TextButton(onClick = { renaming = null }) { Text("取消") }
                    }
                }
                if (groups.isEmpty() && !creating) {
                    Text(
                        text = "暂无分组，环境变量默认在「默认分组」",
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
                groups.forEach { group ->
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = group,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurface,
                            modifier = Modifier.weight(1f),
                        )
                        TextButton(onClick = {
                            renaming = group
                            renameValue = group
                        }) { Text("重命名", color = AppColors.primary) }
                        TextButton(onClick = { onDelete(group) }) {
                            Text("删除", color = AppColors.errorColor)
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("完成") }
        },
    )
}

@Composable
private fun LoadingContent(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            CircularProgressIndicator(color = AppColors.primary)
            Spacer(Modifier.height(12.dp))
            Text(
                text = "正在加载环境变量…",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate500,
            )
        }
    }
}

@Composable
private fun ErrorContent(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = message,
            style = MaterialTheme.typography.bodyMedium,
            color = AppColors.errorColor,
        )
        Spacer(Modifier.height(12.dp))
        Button(onClick = onRetry) {
            Text("重试")
        }
    }
}

@Composable
private fun EmptyContent(onCreate: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = "暂无环境变量",
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
            fontWeight = FontWeight.Bold,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = "点击「新建变量」添加第一个环境变量",
            style = MaterialTheme.typography.bodyMedium,
            color = AppColors.slate500,
        )
        Spacer(Modifier.height(16.dp))
        Button(onClick = onCreate) {
            Text("新建变量")
        }
    }
}
