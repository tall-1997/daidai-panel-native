package com.daidai.daidai_app.ui.screens.applock

import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 应用锁设置页（阶段 4-2，Compose 原生，参照 Flutter `app_lock_settings_page.dart`）。
 *
 * 提供：应用锁总开关、密码配置/重置、图案配置/重置、生物识别开关。
 * 生物识别当前为占位开关（未引入 androidx.biometric），开启被拒绝并提示集成点。
 * 状态由 [AppLockViewModel] 承载，本页仅负责 UI 编排与交互。
 */
@Composable
fun AppLockSettingsScreen(
    viewModel: AppLockViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
AppLockViewModel(context) })
    val state by screenViewModel.uiState.collectAsState()

    var showMessage by remember { mutableStateOf<String?>(null) }
    var showPasswordDialog by remember { mutableStateOf(false) }
    var showPatternDialog by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            "应用锁",
            style = androidx.compose.material3.MaterialTheme.typography.titleLarge,
            color = AppColors.primary,
        )
        Text(
            "解锁呆呆面板前需验证图案或密码。",
            style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
            color = AppColors.slate500,
        )

        // 总开关
        CardView(title = "开启应用锁") {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("启用后启动时需要验证", style = androidx.compose.material3.MaterialTheme.typography.bodyMedium)
                Switch(
                    checked = state.enabled,
                    onCheckedChange = { value ->
                        if (value && !state.hasAnyMethod) {
                            showMessage = "请先到下方至少配置一种验证方式"
                        } else {
                            screenViewModel.setEnabled(value) { msg -> showMessage = msg }
                        }
                    },
                )
            }
        }

        // 图案
        CardView(title = "图案锁") {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    if (state.hasPattern) "已配置：请绘制 ≥4 个点位的图案" else "未配置：点击下方按钮绘制图案",
                    style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
                    color = if (state.hasPattern) AppColors.successColor else AppColors.slate500,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedButton(onClick = { showPatternDialog = true }) {
                        Text(if (state.hasPattern) "重设图案" else "设置图案")
                    }
                    if (state.hasPattern) {
                        TextButton(onClick = { screenViewModel.disablePattern() }) {
                            Text("关闭图案", color = AppColors.red500)
                        }
                    }
                }
            }
        }

        // 密码
        CardView(title = "密码锁") {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    if (state.hasPassword) "已配置：请输入 ≥4 位密码" else "未配置：点击下方按钮设置密码",
                    style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
                    color = if (state.hasPassword) AppColors.successColor else AppColors.slate500,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedButton(onClick = { showPasswordDialog = true }) {
                        Text(if (state.hasPassword) "修改密码" else "设置密码")
                    }
                    if (state.hasPassword) {
                        TextButton(onClick = { screenViewModel.disablePassword() }) {
                            Text("关闭密码", color = AppColors.red500)
                        }
                    }
                }
            }
        }

        // 生物识别（占位）
        CardView(title = "生物识别") {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        "使用系统指纹 / 面容解锁",
                        style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
                        color = AppColors.slate400,
                    )
                    Switch(
                        checked = state.biometricEnabled,
                        onCheckedChange = {
                            screenViewModel.setBiometricEnabled(!state.biometricEnabled)
                        },
                        enabled = false,
                    )
                }
                Text(
                    "集成点：在 build.gradle 引入 androidx.biometric 并接入 BiometricPrompt 后启用（见 AppLockViewModel.BIOMETRIC_NOTICE）。",
                    style = androidx.compose.material3.MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }

        Spacer(Modifier.height(8.dp))
    }

    if (showMessage != null) {
        AlertDialog(
            onDismissRequest = { showMessage = null },
            title = { Text("提示") },
            text = { Text(showMessage.orEmpty()) },
            confirmButton = { TextButton(onClick = { showMessage = null }) { Text("知道了") } },
        )
    }

    if (showPasswordDialog) {
        PasswordDialog(
            hasPassword = state.hasPassword,
            onSave = { password ->
                val hadPassword = state.hasPassword
                val ok = screenViewModel.enablePassword(password)
                if (ok) showMessage = if (hadPassword) "密码已更新" else "密码已设置"
                showPasswordDialog = false
            },
            onDismiss = { showPasswordDialog = false },
        )
    }

    if (showPatternDialog) {
        PatternDialog(
            viewModel = screenViewModel,
            onDismiss = { showPatternDialog = false },
        )
    }
}

@Composable
private fun PasswordDialog(
    hasPassword: Boolean,
    onSave: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var password by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf("") }
    val confirmed = password.isNotBlank() && password == confirm
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (hasPassword) "修改应用锁密码" else "设置应用锁密码") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text("新密码（至少 4 位）") },
                    modifier = Modifier.fillMaxWidth(),
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    singleLine = true,
                )
                OutlinedTextField(
                    value = confirm,
                    onValueChange = { confirm = it },
                    label = { Text("确认密码") },
                    modifier = Modifier.fillMaxWidth(),
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    singleLine = true,
                    isError = confirm.isNotEmpty() && confirm != password,
                )
                if (confirm.isNotEmpty() && confirm != password) {
                    Text("两次输入不一致", color = AppColors.red500, style = androidx.compose.material3.MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            Button(onClick = { onSave(password) }, enabled = confirmed) { Text("保存") }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        },
    )
}

@Composable
private fun PatternDialog(
    viewModel: AppLockViewModel,
    onDismiss: () -> Unit,
) {
    var step by remember { mutableStateOf(0) } // 0=绘制, 1=确认
    var firstPattern by remember { mutableStateOf(listOf<Int>()) }
    var error by remember { mutableStateOf<String?>(null) }

    // 一步：确认时与首次一致才保存；不一致则回到绘制。
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (step == 0) "绘制解锁图案" else "再次绘制确认") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    "建议使用至少 4 个点位，避免过于简单。",
                    style = androidx.compose.material3.MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                )
                error?.let {
                    Text(it, color = AppColors.red500, style = androidx.compose.material3.MaterialTheme.typography.bodySmall)
                }
                PatternPad(
                    onPatternComplete = { points ->
                        if (step == 0) {
                            firstPattern = points
                            step = 1
                            error = null
                        } else {
                            if (points == firstPattern) {
                                viewModel.enablePattern(points)
                                onDismiss()
                            } else {
                                error = "两次图案不一致，请重新绘制"
                                step = 0
                                firstPattern = emptyList()
                            }
                        }
                    },
                    title = if (step == 0) "首次绘制" else "确认",
                    enabled = true,
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        },
    )
}
