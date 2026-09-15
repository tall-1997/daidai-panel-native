package com.daidai.daidai_app.ui.screens.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.shape.RoundedCornerShape
import com.daidai.daidai_app.data.model.MachineCode
import com.daidai.daidai_app.data.model.PanelSettingsSnapshot
import com.daidai.daidai_app.data.model.PanelUpdateStatus
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors
import kotlinx.coroutines.flow.StateFlow

/**
 * C9 系统信息页：机器码 + 面板版本 + 平台设置 + 更新状态。
 *
 * 诚实展示语义：
 *   - 更新走 phase/message 原文；Android 平台安装器语义下明确告知
 *     「不可自更新，由平台安装器接管」，不伪造支持。
 *   - 面板设置中 Go 端可能返回 data.{key:{value,default_value}} 或 data.{key:string}，
 *     解析器已做归一化。
 */
@Composable
fun AboutScreen(
    repository: BackupRepository? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val clipboard = LocalClipboardManager.current
    val snackbarHostState = remember { SnackbarHostState() }

    var panelVersion by remember { mutableStateOf("") }
    var machineCode by remember { mutableStateOf<MachineCode?>(null) }
    var settings by remember { mutableStateOf<PanelSettingsSnapshot?>(null) }
    var updateStatus by remember { mutableStateOf<PanelUpdateStatus?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var showFullCode by remember { mutableStateOf(false) }

    LaunchedEffect(resolvedRepository) {
        loading = true
        error = null
        try {
            panelVersion = resolvedRepository.panelVersion()
            machineCode = resolvedRepository.machineCode()
            settings = resolvedRepository.panelSettings()
            updateStatus = resolvedRepository.updateStatus()
        } catch (failure: Exception) {
            if (failure is kotlinx.coroutines.CancellationException) throw failure
            error = failure.message ?: "读取系统信息失败"
        } finally {
            loading = false
        }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MaterialTheme.colorScheme.background,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Column {
                    Text(
                        text = "系统信息",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                        color = AppColors.slate900,
                    )
                    Text(
                        text = "面板版本、机器码、设置与更新",
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when {
                loading -> LoadingView()
                error != null -> ErrorView(message = error ?: "加载失败", onRetry = { /* reload effect */ })
                else -> Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    SectionCard(title = "面板版本") {
                        Text(
                            text = panelVersion.ifBlank { "-" },
                            style = MaterialTheme.typography.titleMedium,
                            color = AppColors.slate900,
                        )
                    }
                    SectionCard(title = "机器码") {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                text = machineCode?.code?.take(12)
                                    ?.plus(if (showFullCode) "" else "…")
                                    .orEmpty()
                                    .ifBlank { "-" },
                                style = MaterialTheme.typography.bodyMedium,
                                fontFamily = FontFamily.Monospace,
                                color = AppColors.slate800,
                            )
                            Spacer(modifier = Modifier.width(8.dp))
                            TextButton(
                                onClick = { showFullCode = !showFullCode },
                            ) { Text(if (showFullCode) "收起" else "展开") }
                            TextButton(
                                onClick = {
                                    machineCode?.code?.takeIf { it.isNotBlank() }?.let {
                                        clipboard.setText(AnnotatedString(it))
                                    }
                                },
                            ) { Text("复制") }
                        }
                    }
                    SectionCard(title = "更新") {
                        val status = updateStatus
                        val isAndroidImmutable = status?.isPlatformInstallerManaged == true
                        if (isAndroidImmutable) {
                            Text(
                                text = "Android 端 APK 不可自更新；更新由平台安装器接管",
                                color = AppColors.slate600,
                            )
                            Text(
                                text = "Phase: ${status?.phase.orEmpty()} / Manager: ${status?.updateManager.orEmpty()}",
                                style = MaterialTheme.typography.bodySmall,
                                color = AppColors.slate400,
                            )
                        } else {
                            Text("状态：${status?.status.orEmpty().ifBlank { "-" }}")
                            if (!status?.phase.isNullOrBlank()) {
                                Text("Phase: ${status?.phase.orEmpty()}")
                            }
                            if (!status?.message.isNullOrBlank()) {
                                Text(status?.message.orEmpty())
                            }
                        }
                    }
                    SectionCard(title = "面板设置") {
                        settings?.let {
                            SettingRow("面板标题", it.panelTitle.ifBlank { "-" })
                            SettingRow("面板图标", it.panelIcon.ifBlank { "-" })
                            SettingRow("编辑器背景色", it.editorBackgroundColor.ifBlank { "-" })
                            SettingRow("日志背景色", it.logBackgroundColor.ifBlank { "-" })
                            SettingRow("运行时模式", it.panelRuntimeMode.ifBlank { "-" })
                            SettingRow("服务管理器", it.panelServiceManager.ifBlank { "-" })
                        } ?: Text("读取面板设置失败", color = AppColors.slate500)
                    }
                }
            }
        }
    }
}

@Composable
private fun SectionCard(title: String, content: @Composable () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.titleSmall,
            fontWeight = FontWeight.SemiBold,
            color = AppColors.slate500,
        )
        content()
    }
}

@Composable
private fun SettingRow(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, color = AppColors.slate500, style = MaterialTheme.typography.bodyMedium)
        Text(value, color = AppColors.slate900, style = MaterialTheme.typography.bodyMedium)
    }
}
