package com.daidai.daidai_app.ui.screens.envs

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
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.repository.EnvsRepository
import com.daidai.daidai_app.data.repository.PanelEnvsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 环境变量列表页（Compose 原生，阶段 3-2）。
 *
 * 展示环境变量卡片：名称 / 备注 / 启用开关 / 编辑 / 删除，并覆盖 加载中、
 * 空态、错误（含重试）三个分支。依赖通过参数注入（repository / viewModel）；
 * 不传时默认用 [AppServices.configRepository] 读到的连接信息装配读仓库。
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
    if (pendingDeleteId != null) {
        androidx.compose.material3.AlertDialog(
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

    Column(modifier = modifier.fillMaxSize()) {
        EnvHeader(
            count = state.envs.size,
            onCreate = onCreate,
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
                    if (state.envs.isEmpty()) {
                        EmptyContent(onCreate = onCreate)
                    } else {
                        EnvList(
                            envs = state.envs,
                            busyId = state.busyId,
                            busyType = state.busyType,
                            onToggle = { env -> screenViewModel.setEnabled(env.id, !env.enabled) },
                            onEdit = onEdit,
                            onDelete = { id -> pendingDeleteId = id },
                        )
                    }
            }
        }
    }
}

@Composable
private fun EnvHeader(count: Int, onCreate: () -> Unit, modifier: Modifier = Modifier) {
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
            } else {
                Switch(checked = env.enabled, onCheckedChange = { onToggle() })
            }
        }
        if (env.remark.isNotBlank()) {
            Spacer(Modifier.height(6.dp))
            Text(
                text = env.remark,
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
            )
        }
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = onEdit) {
                Text("编辑", color = AppColors.primary)
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