package com.daidai.daidai_app.ui.screens.openapi

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
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
import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * Open API 应用详情页（Compose 原生，补齐-2）。
 *
 * 展示应用信息卡片，并提供 重置密钥 / 启用停用 / 删除 三个写操作，全部经
 * [OpenApiWriteViewModel]：POST reset-secret、PUT apps/:id（enabled）、DELETE apps/:id。
 * 重置密钥成功后，一次性 secret 弹窗展示（[consumeSecret] 随后清空）。
 * 本组件不接导航，自包含可编译；未提供 viewModel 时用连接配置装配。
 */
@Composable
fun OpenApiDetailScreen(
    app: OpenApiApp,
    modifier: Modifier = Modifier,
    viewModel: OpenApiWriteViewModel? = null,
    onBack: () -> Unit = {},
    onChanged: (() -> Unit)? = null,
    onDeleted: (() -> Unit)? = null,
) {
    val context = LocalContext.current
    val resolvedVm = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val config = AppServices.configRepository(context).config.value
        OpenApiWriteViewModel(
            baseUrl = config.serverUrl,
            accessToken = config.accessToken,
            localToken = config.localToken,
        )
    })
    val state by resolvedVm.uiState.collectAsStateWithLifecycle()

    // 删除确认弹窗
    var showDeleteDialog by remember { mutableStateOf(false) }
    // 重置密钥结果弹窗：仅展示一次
    val resetSecret = state.resetSecret

    if (showDeleteDialog) {
        ConfirmDeleteDialog(
            appName = app.name,
            onCancel = { showDeleteDialog = false },
            onConfirm = {
                showDeleteDialog = false
                resolvedVm.delete(app.id)
            },
        )
    }

    // 重置成功副作用：展示一次性密钥。
    if (resetSecret?.hasSecret == true && state.mutationSucceeded) {
        OneTimeSecretDialog(
            title = "应用密钥",
            secret = resetSecret.secret,
            onDismiss = { resolvedVm.consumeSecret() },
        )
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        DetailTopBar(onBack = onBack)

        // 全局写错误提示
        state.errorMessage?.let { msg ->
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(AppColors.errorColor.copy(alpha = 0.1f))
                    .padding(12.dp)
                    .padding(horizontal = 20.dp),
            ) {
                Text(
                    text = msg,
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.errorColor,
                )
            }
        }

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            DetailCard(app = app)

            if (state.busy) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    CircularProgressIndicator(color = AppColors.primary, strokeWidth = 2.dp)
                    Spacer(Modifier.width(8.dp))
                    Text("操作进行中…", style = MaterialTheme.typography.bodySmall)
                }
            }

            ResetSecretButton(
                enabled = !state.busy,
                onClick = { resolvedVm.resetSecret(app.id) },
            )

            ToggleEnabledRow(
                app = app,
                enabled = !state.busy,
                onToggle = { resolvedVm.toggleEnabled(app) },
            )

            DeleteButton(
                enabled = !state.busy,
                onClick = { showDeleteDialog = true },
            )
        }
    }
}

@Composable
private fun DetailTopBar(onBack: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "‹",
            style = MaterialTheme.typography.headlineMedium,
            color = AppColors.primary,
            modifier = Modifier.clickable(onClick = onBack),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = "应用详情",
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

@Composable
private fun DetailCard(app: OpenApiApp, modifier: Modifier = Modifier) {
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
                text = app.name.ifBlank { "未命名应用" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f, fill = false),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.width(8.dp))
            EnabledBadge(enabled = app.enabled)
        }

        DetailEntry("App Key", app.appKey.ifBlank { "无密钥" })
        DetailEntry(
            "权限范围",
            if (app.scopes.isEmpty()) "无" else app.scopes.joinToString(" / "),
        )
        DetailEntry("限流上限", "${app.rateLimit} 次/分钟")
        if (app.createdAt.isNotBlank()) {
            DetailEntry("创建时间", app.createdAt)
        }
    }
}

@Composable
private fun DetailEntry(label: String, value: String) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = AppColors.slate400,
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            color = AppColors.slate700,
        )
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

@Composable
private fun ResetSecretButton(enabled: Boolean, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        enabled = enabled,
        colors = ButtonDefaults.buttonColors(containerColor = AppColors.primary),
    ) {
        Text("重置密钥")
    }
}

@Composable
private fun ToggleEnabledRow(app: OpenApiApp, enabled: Boolean, onToggle: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                text = if (app.enabled) "当前已启用" else "当前已禁用",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate700,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = if (app.enabled) "点击开关可停用该应用" else "点击开关可启用该应用",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
        }
        Switch(
            checked = app.enabled,
            onCheckedChange = { onToggle() },
            enabled = enabled,
            colors = SwitchDefaults.colors(
                checkedTrackColor = AppColors.primary,
            ),
        )
    }
}

@Composable
private fun DeleteButton(enabled: Boolean, onClick: () -> Unit) {
    OutlinedButton(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        enabled = enabled,
        colors = ButtonDefaults.outlinedButtonColors(contentColor = AppColors.errorColor),
    ) {
        Text("删除应用")
    }
}

@Composable
private fun ConfirmDeleteDialog(appName: String, onCancel: () -> Unit, onConfirm: () -> Unit) {
    AlertDialog(
        onDismissRequest = onCancel,
        title = { Text("删除应用") },
        text = { Text("确定删除「${appName.ifBlank { "未命名应用" }}」吗？此操作不可恢复，其密钥将全部失效。") },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text("删除", color = AppColors.errorColor)
            }
        },
        dismissButton = {
            TextButton(onClick = onCancel) { Text("取消") }
        },
    )
}

/** 一次性密钥展示弹窗：展示后调用方应调 [OpenApiWriteViewModel.consumeSecret] 复位。 */
@Composable
private fun OneTimeSecretDialog(title: String, secret: String, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = "该密钥仅展示这一次，请立即妥善保存，关闭后无法再次查看。",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                )
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(8.dp))
                        .background(AppColors.primaryLight)
                        .padding(12.dp),
                ) {
                    Text(
                        text = secret,
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = AppColors.slate700,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("我已保存") }
        },
    )
}
