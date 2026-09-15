package com.daidai.daidai_app.ui.screens.tasks

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.model.TaskWritePayload
import com.daidai.daidai_app.data.repository.PanelTasksRepository
import com.daidai.daidai_app.data.repository.TasksRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 任务新建 / 编辑表单（Compose 原生，阶段 3-1，F4 补齐高级配置）。
 *
 * 基础输入：名称 / 脚本路径 / 任务类型 / 调度表达式（支持多行 cron）。
 * 高级输入（F4 新增，JSON key 与 Go 后端 task_mutate.go 及本地 LocalPanelStore
 * 对齐）：
 *  - 超时秒数（timeout）
 *  - 重试次数（max_retries）与重试间隔秒（retry_interval）
 *  - 定时停止 cron（stop_schedule）
 *  - 依赖任务（depends_on，从任务列表单选）
 *  - 前置/后置钩子（task_before / task_after，脚本路径或命令）
 *  - 并发控制：允许多实例（allow_multiple_instances）+ 调度策略（schedule_policy）
 *  - Python 版本（python_version）
 *  - 运行通知开关（notify_on_failure / notify_on_success / notify_on_abort）
 *
 * cron 快捷选择：预设按钮（每 5 分钟 / 每小时 / 每天 / 每周一 / 每月 1 日）点击后
 * 填入调度表达式输入框。
 *
 * 提交后经 [TasksRepository] 写入后端：
 *  - [editingTask] 为 null 时走新建（POST /api/tasks）
 *  - [editingTask] 非 null 时走更新（PUT /api/tasks/:id）
 *
 * 不接线导航：保存成功后通过 [onSaved] 回调交给调用方。
 */

/** cron 快捷预设：显示名 → 标准 5 段 cron 表达式。 */
private val cronPresets: List<Pair<String, String>> = listOf(
    "每 5 分钟" to "*/5 * * * *",
    "每小时" to "0 * * * *",
    "每天" to "0 0 * * *",
    "每周一" to "0 9 * * 1",
    "每月 1 日" to "0 0 1 * *",
)

@Composable
fun TaskFormScreen(
    modifier: Modifier = Modifier,
    editingTask: Task? = null,
    repository: TasksRepository? = null,
    viewModel: TasksViewModel? = null,
    onSaved: () -> Unit = {},
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

    var name by remember { mutableStateOf(editingTask?.name ?: "") }
    var scriptPath by remember { mutableStateOf(editingTask?.scriptPath ?: "") }
    var schedule by remember { mutableStateOf(editingTask?.schedule ?: "") }
    val originalType = editingTask?.type
    var isCron by remember { mutableStateOf(editingTask?.type == "cron" || editingTask == null) }
    var saving by remember { mutableStateOf(false) }
    var errorMessage by remember { mutableStateOf<String?>(null) }

    // ---- F4 高级字段状态 ----
    var timeoutText by remember { mutableStateOf(editingTask?.timeout?.takeIf { it > 0 }?.toString() ?: "") }
    var maxRetriesText by remember { mutableStateOf(editingTask?.maxRetries?.takeIf { it > 0 }?.toString() ?: "") }
    var retryIntervalText by remember { mutableStateOf(editingTask?.retryInterval?.takeIf { it > 0 }?.toString() ?: "60") }
    var stopSchedule by remember { mutableStateOf(editingTask?.stopSchedule ?: "") }
    var dependsOnId by remember { mutableStateOf(editingTask?.dependsOn) }
    var taskBefore by remember { mutableStateOf(editingTask?.taskBefore ?: "") }
    var taskAfter by remember { mutableStateOf(editingTask?.taskAfter ?: "") }
    var allowMultipleInstances by remember { mutableStateOf(editingTask?.allowMultipleInstances ?: false) }
    var schedulePolicy by remember { mutableStateOf(editingTask?.schedulePolicy ?: "skip") }
    var pythonVersion by remember { mutableStateOf(editingTask?.pythonVersion ?: "") }
    var notifyOnFailure by remember { mutableStateOf(editingTask?.notifyOnFailure ?: false) }
    var notifyOnSuccess by remember { mutableStateOf(editingTask?.notifyOnSuccess ?: false) }
    var notifyOnAbort by remember { mutableStateOf(editingTask?.notifyOnAbort ?: false) }

    // 依赖任务下拉候选：拉取全量任务（排除自身）。
    var dependencyCandidates by remember { mutableStateOf<List<Task>>(emptyList()) }
    var dependencyLoading by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        dependencyLoading = true
        try {
            val repo = repository ?: run {
                val config = AppServices.configRepository(context).config.value
                PanelTasksRepository(
                    baseUrl = config.serverUrl,
                    accessToken = config.accessToken,
                    localToken = config.localToken,
                )
            }
            dependencyCandidates = repo.getTasks().filter { it.id != editingTask?.id }
        } catch (_: Exception) {
            dependencyCandidates = emptyList()
        } finally {
            dependencyLoading = false
        }
    }

    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 提交后监听结果：失败显示内联错误；成功回调 onSaved（供调用方接手导航/关闭）。
    LaunchedEffect(state.errorMessage) {
        if (saving && state.errorMessage != null) {
            errorMessage = state.errorMessage
            saving = false
        }
    }
    LaunchedEffect(state.mutationSucceeded) {
        if (saving && state.mutationSucceeded) {
            saving = false
            screenViewModel.clearError()
            onSaved()
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = if (editingTask == null) "新建任务" else "编辑任务",
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onSurface,
        )

        OutlinedTextField(
            value = name,
            onValueChange = { name = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("任务名称") },
            placeholder = { Text("例如：每日数据同步") },
            singleLine = true,
        )

        OutlinedTextField(
            value = scriptPath,
            onValueChange = { scriptPath = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("脚本路径") },
            placeholder = { Text("例如：/scripts/sync.sh 或 python /app/job.py") },
            supportingText = { Text("任务实际执行的脚本 / 命令") },
        )

        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "任务类型：",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
            )
            Spacer(Modifier.width(8.dp))
            FilterChip(
                selected = isCron,
                onClick = { isCron = true },
                label = { Text("常规定时 (cron)") },
            )
            Spacer(Modifier.width(8.dp))
            FilterChip(
                selected = !isCron,
                onClick = { isCron = false },
                label = { Text("手动运行 (manual)") },
            )
        }

        OutlinedTextField(
            value = schedule,
            onValueChange = { schedule = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = isCron,
            label = { Text("调度表达式") },
            placeholder = { Text("cron 表达式，每行一条，例如：0 0 * * *") },
            supportingText = {
                Text(if (isCron) "留空表示仅手动/开机触发；多行可写多个表达式" else "手动任务无需调度")
            },
            minLines = 1,
            maxLines = 4,
        )

        // ---- F4：cron 快捷选择（仅 cron 任务显示） ----
        if (isCron) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(
                    text = "快捷选择：",
                    style = MaterialTheme.typography.labelMedium,
                    color = AppColors.slate600,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    cronPresets.forEach { (label, expr) ->
                        FilterChip(
                            selected = false,
                            onClick = { schedule = expr },
                            label = { Text(label) },
                        )
                    }
                }
            }
        }

        // ---- F4：高级执行配置 ----
        Text(
            text = "高级配置",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onSurface,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            OutlinedTextField(
                value = timeoutText,
                onValueChange = { timeoutText = it.filter(Char::isDigit).take(6) },
                modifier = Modifier.weight(1f),
                label = { Text("超时（秒）") },
                supportingText = { Text("0=不限，最大 604800") },
                singleLine = true,
            )
            OutlinedTextField(
                value = maxRetriesText,
                onValueChange = { maxRetriesText = it.filter(Char::isDigit).take(2) },
                modifier = Modifier.weight(1f),
                label = { Text("重试次数") },
                supportingText = { Text("0-20") },
                singleLine = true,
            )
        }

        OutlinedTextField(
            value = retryIntervalText,
            onValueChange = { retryIntervalText = it.filter(Char::isDigit).take(5) },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("重试间隔（秒）") },
            supportingText = { Text("最大 86400，默认 60") },
            singleLine = true,
        )

        OutlinedTextField(
            value = stopSchedule,
            onValueChange = { stopSchedule = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = isCron,
            label = { Text("定时停止 cron（可选）") },
            placeholder = { Text("例如：0 23 * * *（到点停止）") },
            supportingText = { Text("到指定时间停止任务；留空不启用") },
            singleLine = true,
        )

        // 依赖任务（depends_on 单值，两端一致）
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                text = "依赖任务（先完成再运行本任务）：",
                style = MaterialTheme.typography.labelMedium,
                color = AppColors.slate600,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                FilterChip(
                    selected = dependsOnId == null,
                    onClick = { dependsOnId = null },
                    label = { Text("无") },
                )
                dependencyCandidates.take(4).forEach { candidate ->
                    FilterChip(
                        selected = dependsOnId == candidate.id,
                        onClick = { dependsOnId = candidate.id },
                        label = { Text(candidate.name.ifBlank { "任务#${candidate.id}" }) },
                    )
                }
                if (dependencyLoading) {
                    CircularProgressIndicator(modifier = Modifier.width(16.dp).height(16.dp), strokeWidth = 2.dp)
                }
            }
            if (dependencyCandidates.isEmpty() && !dependencyLoading) {
                Text(
                    text = "暂无可依赖的任务（依赖任务需先创建）",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate400,
                )
            }
        }

        OutlinedTextField(
            value = taskBefore,
            onValueChange = { taskBefore = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("前置钩子 task_before（可选）") },
            placeholder = { Text("脚本路径或命令，例如 /scripts/pre.sh") },
            supportingText = { Text("运行任务前执行") },
            singleLine = true,
        )

        OutlinedTextField(
            value = taskAfter,
            onValueChange = { taskAfter = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("后置钩子 task_after（可选）") },
            placeholder = { Text("脚本路径或命令，例如 /scripts/post.sh") },
            supportingText = { Text("运行任务后执行") },
            singleLine = true,
        )

        // 并发控制
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "允许多实例并发：",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = allowMultipleInstances,
                onCheckedChange = { allowMultipleInstances = it },
                colors = SwitchDefaults.colors(
                    checkedThumbColor = AppColors.primary,
                    checkedTrackColor = AppColors.primary.copy(alpha = 0.3f),
                ),
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "调度并发策略：",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
            )
            Spacer(Modifier.width(8.dp))
            FilterChip(
                selected = schedulePolicy == "skip",
                onClick = { schedulePolicy = "skip" },
                label = { Text("跳过 skip") },
            )
            Spacer(Modifier.width(6.dp))
            FilterChip(
                selected = schedulePolicy == "queue",
                onClick = { schedulePolicy = "queue" },
                label = { Text("排队 queue") },
            )
            Spacer(Modifier.width(6.dp))
            FilterChip(
                selected = schedulePolicy == "parallel",
                onClick = { schedulePolicy = "parallel" },
                label = { Text("并行 parallel") },
            )
        }

        OutlinedTextField(
            value = pythonVersion,
            onValueChange = { pythonVersion = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("Python 版本（可选）") },
            placeholder = { Text("例如：3.11；留空使用默认") },
            supportingText = { Text("运行脚本所需的 Python 版本") },
            singleLine = true,
        )

        // 通知开关
        Text(
            text = "运行通知",
            style = MaterialTheme.typography.titleSmall,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "失败通知",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = notifyOnFailure,
                onCheckedChange = { notifyOnFailure = it },
                colors = SwitchDefaults.colors(
                    checkedThumbColor = AppColors.primary,
                    checkedTrackColor = AppColors.primary.copy(alpha = 0.3f),
                ),
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "成功通知",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = notifyOnSuccess,
                onCheckedChange = { notifyOnSuccess = it },
                colors = SwitchDefaults.colors(
                    checkedThumbColor = AppColors.primary,
                    checkedTrackColor = AppColors.primary.copy(alpha = 0.3f),
                ),
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "中止通知",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate600,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = notifyOnAbort,
                onCheckedChange = { notifyOnAbort = it },
                colors = SwitchDefaults.colors(
                    checkedThumbColor = AppColors.primary,
                    checkedTrackColor = AppColors.primary.copy(alpha = 0.3f),
                ),
            )
        }

        if (errorMessage != null) {
            Text(
                text = errorMessage.orEmpty(),
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.errorColor,
            )
        }

        Button(
            onClick = {
                val trimmedName = name.trim()
                val trimmedScript = scriptPath.trim()
                if (trimmedName.isEmpty()) {
                    errorMessage = "请输入任务名称"
                    return@Button
                }
                if (trimmedScript.isEmpty()) {
                    errorMessage = "请输入脚本路径"
                    return@Button
                }
                if (isCron && schedule.isBlank()) {
                    errorMessage = "Cron 任务需要至少一个调度表达式"
                    return@Button
                }
                val timeout = timeoutText.toIntOrNull()?.coerceIn(0, 604800) ?: 0
                val maxRetries = maxRetriesText.toIntOrNull()?.coerceIn(0, 20) ?: 0
                val retryInterval = retryIntervalText.toIntOrNull()?.coerceIn(0, 86400) ?: 60
                errorMessage = null
                saving = true
                // 编辑 startup 任务时保持原类型，避免被表单静默改写为 manual。
                val type = when {
                    originalType == "startup" -> "startup"
                    isCron -> "cron"
                    else -> "manual"
                }
                val payload = TaskWritePayload(
                    name = trimmedName,
                    scriptPath = trimmedScript,
                    schedule = schedule.trim(),
                    type = type,
                    timeout = timeout,
                    maxRetries = maxRetries,
                    retryInterval = retryInterval,
                    stopSchedule = stopSchedule.trim(),
                    dependsOn = dependsOnId,
                    taskBefore = taskBefore,
                    taskAfter = taskAfter,
                    allowMultipleInstances = allowMultipleInstances,
                    schedulePolicy = schedulePolicy,
                    pythonVersion = pythonVersion.trim(),
                    notifyOnFailure = notifyOnFailure,
                    notifyOnSuccess = notifyOnSuccess,
                    notifyOnAbort = notifyOnAbort,
                )
                if (editingTask == null) {
                    screenViewModel.createTask(payload)
                } else {
                    screenViewModel.updateTask(editingTask.id, payload)
                }
            },
            modifier = Modifier.fillMaxWidth(),
            enabled = !saving,
        ) {
            if (saving) {
                CircularProgressIndicator(
                    modifier = Modifier.width(16.dp).height(16.dp),
                    color = AppColors.primary,
                )
                Spacer(Modifier.width(8.dp))
                Text("保存中…")
            } else {
                Text(if (editingTask == null) "创建任务" else "保存修改")
            }
        }
    }
}
