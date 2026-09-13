package com.daidai.daidai_app.ui.screens.system

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.model.HealthCheckItem
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 健康检查页（Compose 原生，阶段 4-5）。
 *
 * 执行健康检查（GET 读快照 / POST 立即重跑）并把各组件结果以 PASS / FAIL / 未知
 * 列表呈现。**未接导航**，自包含可编译。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时按项目惯例用 [BackupRepository] 装配。
 */
@Composable
fun HealthCheckScreen(
    repository: BackupRepository? = null,
    viewModel: SystemViewModel? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val screenViewModel = viewModel ?: remember { SystemViewModel(resolvedRepository) }

    val state by screenViewModel.uiState.collectAsState()
    val snackbarHostState = remember { SnackbarHostState() }

    // 页面首次进入时尝试读最近一次的检查快照
    LaunchedEffect(Unit) { screenViewModel.loadHealthSnapshot() }

    LaunchedEffect(state.errorMessage) {
        state.errorMessage?.let { snackbarHostState.showSnackbar(it) }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = AppColors.lightPage,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        bottomBar = {
            HealthCheckActionBar(
                running = state.healthRunning,
                onRun = { if (!state.healthRunning) screenViewModel.runHealthCheck() },
            )
        },
    ) { innerPadding ->
        Column(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            HealthHeader()
            val health = state.health
            when {
                state.healthRunning && health == null -> {
                    Column(modifier = Modifier.fillMaxSize(), verticalArrangement = Arrangement.Center) {
                        CircularProgressIndicator(
                            modifier = Modifier.align(Alignment.CenterHorizontally),
                            color = AppColors.primary,
                        )
                    }
                }
                health == null -> EmptyView(
                    title = "尚未进行健康检查",
                    description = "点击下方按钮立即执行一次系统健康检查",
                )
                health.items.isEmpty() -> EmptyView(
                    title = "健康检查通过（无告警项）",
                    description = health.lastCheckedAt.ifBlank { "最近未记录检查时间" },
                )
                else -> HealthList(items = health.items, lastCheckedAt = health.lastCheckedAt)
            }
        }
    }
}

@Composable
private fun HealthHeader() {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 12.dp),
    ) {
        Text(
            text = "系统健康检查",
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
            color = AppColors.slate900,
        )
        Text(
            text = "逐项检测系统关键组件运行状态",
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.slate500,
        )
    }
}

@Composable
private fun HealthCheckActionBar(running: Boolean, onRun: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(AppColors.lightPage)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Spacer(modifier = Modifier.height(0.dp))
        Button(
            onClick = onRun,
            enabled = !running,
            modifier = Modifier.fillMaxWidth().height(48.dp),
        ) {
            if (running) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp))
                Spacer(modifier = Modifier.width(8.dp))
                Text("检查中…")
            } else {
                Icon(
                    Icons.Filled.CheckCircle,
                    contentDescription = null,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(modifier = Modifier.width(8.dp))
                Text("立即检查")
            }
        }
    }
}

@Composable
private fun HealthList(items: List<HealthCheckItem>, lastCheckedAt: String) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item {
            Text(
                text = "最近检查：${lastCheckedAt.ifBlank { "-" }}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        items(items) { item -> HealthRow(item) }
    }
}

@Composable
private fun HealthRow(item: HealthCheckItem) {
    val ok = item.status.equals("pass", ignoreCase = true) ||
        item.status.equals("ok", ignoreCase = true) ||
        item.status.equals("up", ignoreCase = true)
    val warn = item.status.equals("warning", ignoreCase = true)
    val (icon, tint) = when {
        ok -> Icons.Filled.CheckCircle to AppColors.successColor
        warn -> Icons.Filled.Warning to AppColors.warningColor
        else -> Icons.Filled.Warning to AppColors.errorColor
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(AppColors.glassCard)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(imageVector = icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
        Spacer(modifier = Modifier.width(12.dp))
        Column(modifier = Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = item.name,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = AppColors.slate900,
                    modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(modifier = Modifier.width(8.dp))
                Text(
                    text = statusLabel(item.status),
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.Bold,
                    color = tint,
                )
            }
            if (item.message.isNotEmpty()) {
                Spacer(modifier = Modifier.height(4.dp))
                Text(
                    text = item.message,
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                )
            }
        }
    }
}

private fun statusLabel(status: String): String = when {
    status.equals("pass", true) || status.equals("ok", true) || status.equals("up", true) -> "PASS"
    status.equals("warning", true) -> "WARN"
    status.equals("fail", true) || status.equals("down", true) -> "FAIL"
    else -> "UNKNOWN"
}
