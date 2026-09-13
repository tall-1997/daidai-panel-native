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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
 * 任务新建 / 编辑表单（Compose 原生，阶段 3-1）。
 *
 * 包含 名称 / 脚本路径 / 调度表达式（支持多行 cron） 三个核心输入，以及
 * 可选的任务类型。提交后经 [TasksRepository] 写入后端：
 *  - [editingTask] 为 null 时走新建（POST /api/tasks）
 *  - [editingTask] 非 null 时走更新（PUT /api/tasks/:id）
 *
 * 不接线导航：保存成功后通过 [onSaved] 回调交给调用方。
 */
@Composable
fun TaskFormScreen(
    modifier: Modifier = Modifier,
    editingTask: Task? = null,
    repository: TasksRepository? = null,
    viewModel: TasksViewModel? = null,
    onSaved: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(context) {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelTasksRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        TasksViewModel(resolved)
    }
    // 表单进入时拉取一次（保证已装配校验；失败不阻塞编辑）。首次编辑态直接可用。
    LaunchedEffect(Unit) { screenViewModel.refresh() }

    var name by remember { mutableStateOf(editingTask?.name ?: "") }
    var scriptPath by remember { mutableStateOf(editingTask?.scriptPath ?: "") }
    var schedule by remember { mutableStateOf(editingTask?.schedule ?: "") }
    var isCron by remember { mutableStateOf(editingTask?.type == "cron" || editingTask == null) }
    var saving by remember { mutableStateOf(false) }
    var errorMessage by remember { mutableStateOf<String?>(null) }

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
            .background(AppColors.lightPage)
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
            androidx.compose.material3.FilterChip(
                selected = isCron,
                onClick = { isCron = true },
                label = { Text("常规定时 (cron)") },
            )
            Spacer(Modifier.width(8.dp))
            androidx.compose.material3.FilterChip(
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
                errorMessage = null
                saving = true
                val type = if (isCron) "cron" else "manual"
                val payload = TaskWritePayload(
                    name = trimmedName,
                    scriptPath = trimmedScript,
                    schedule = schedule.trim(),
                    type = type,
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