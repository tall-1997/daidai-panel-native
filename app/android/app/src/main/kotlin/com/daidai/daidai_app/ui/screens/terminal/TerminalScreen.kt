package com.daidai.daidai_app.ui.screens.terminal

import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.repository.PanelTerminalRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * A2 终端 PTY 客户端页面（Compose 原生）。
 *
 * 用户自身本地面板功能：登录后（operator/admin）在 rootfs bash 会话中执行命令。
 * 布局：标题栏（shell / 状态 / exit code + 溢出菜单）→ ANSI 转写区（自动滚底）→
 * 控制键行 → 输入行。退出页面（ViewModel onCleared）自动 stop + close 回收 PTY；
 * 服务端另有 idle 60s / TTL 5min 兜底清理。
 *
 * 渲染为线性转写（剥离 ANSI 序列），无全屏 VT100 模拟；vim/top 等程序呈现近似文本。
 *
 * 入口函数：[TerminalScreen]。主代理接线：AppNavHost 增加
 * `const val TERMINAL = "terminal"` 路由 + `composable(Routes.TERMINAL) { TerminalScreen(onBack = { navController.popBackStack() }) }`，
 * 「更多」页 entries 增加 `Triple(Routes.TERMINAL, "终端", "PTY 终端会话")`。
 */
@Composable
fun TerminalScreen(
    modifier: Modifier = Modifier,
    viewModel: TerminalViewModel? = null,
    onBack: (() -> Unit)? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val config = AppServices.configRepository(context).config.value
        TerminalViewModel(
            repository = PanelTerminalRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            ),
        )
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    var menuExpanded by remember { mutableStateOf(false) }
    var followOutput by remember { mutableStateOf(true) }
    val exportLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("text/plain"),
    ) { uri: Uri? ->
        uri?.let { writeTranscriptTo(context, it, state.transcript) }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        TerminalHeader(
            state = state,
            onBack = onBack,
            onRestart = screenViewModel::restart,
            menuExpanded = menuExpanded,
            onMenuToggle = { menuExpanded = it },
            followOutput = followOutput,
            onToggleFollow = {
                followOutput = !followOutput
                menuExpanded = false
            },
            onExport = {
                menuExpanded = false
                exportLauncher.launch("terminal-transcript.txt")
            },
            onStop = screenViewModel::stop,
        )
        HorizontalDivider(color = AppColors.glassDivider)

        val session = state.session
        when {
            session != null -> {
                TranscriptView(
                    transcript = state.transcript,
                    running = state.isRunning,
                    followOutput = followOutput,
                    error = state.errorMessage,
                    modifier = Modifier.weight(1f),
                )
                ControlKeyRow(enabled = state.isRunning, onKey = screenViewModel::sendControlKey)
                InputRow(
                    enabled = state.isRunning,
                    sending = state.sending,
                    stopping = state.stopping,
                    onSend = screenViewModel::sendInput,
                )
            }
            state.closed -> ErrorView(
                message = state.errorMessage ?: "终端会话不可用",
                onRetry = screenViewModel::restart,
            )
            else -> LoadingView()
        }
    }
}

@Composable
private fun TerminalHeader(
    state: TerminalUiState,
    onBack: (() -> Unit)?,
    onRestart: () -> Unit,
    menuExpanded: Boolean,
    onMenuToggle: (Boolean) -> Unit,
    followOutput: Boolean,
    onToggleFollow: () -> Unit,
    onExport: () -> Unit,
    onStop: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 8.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.ArrowBack, contentDescription = "返回", tint = AppColors.slate700)
            }
        }
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = "终端",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
            val session = state.session
            Text(
                text = when {
                    session == null -> "创建会话中…"
                    else -> buildString {
                        append(session.shell.ifBlank { "shell" })
                        append(" · ").append(session.status)
                        if (state.closed && session.status != "running") append(" · 已退出")
                        session.exitCode?.let { append(" · exit=").append(it) }
                        append(" · pid=").append(session.pid)
                    }
                },
                style = MaterialTheme.typography.bodySmall,
                color = when {
                    state.closed -> AppColors.slate500
                    else -> AppColors.runningColor
                },
            )
        }
        if (state.isRunning) {
            TextButton(onClick = onStop, enabled = !state.stopping) {
                Text(if (state.stopping) "停止中…" else "停止", color = AppColors.errorColor)
            }
        } else {
            IconButton(onClick = onRestart) {
                Icon(Icons.Filled.Refresh, contentDescription = "新建会话", tint = AppColors.primary)
            }
        }
        Box {
            IconButton(onClick = { onMenuToggle(true) }) {
                Icon(Icons.Filled.MoreVert, contentDescription = "更多", tint = AppColors.slate700)
            }
            DropdownMenu(expanded = menuExpanded, onDismissRequest = { onMenuToggle(false) }) {
                DropdownMenuItem(
                    text = { Text(if (followOutput) "关闭自动滚底" else "开启自动滚底") },
                    onClick = onToggleFollow,
                )
                DropdownMenuItem(
                    text = { Text("导出转写 (SAF)") },
                    onClick = onExport,
                )
                DropdownMenuItem(
                    text = { Text(if (state.session == null) "创建会话" else "重建会话") },
                    onClick = {
                        onMenuToggle(false)
                        onRestart()
                    },
                )
            }
        }
    }
}

@Composable
private fun TranscriptView(
    transcript: String,
    running: Boolean,
    followOutput: Boolean,
    error: String?,
    modifier: Modifier = Modifier,
) {
    val rendered = remember(transcript) { renderTranscriptForDisplay(transcript) }
    val scrollState = rememberScrollState()
    LaunchedEffect(rendered, followOutput) {
        if (followOutput) scrollState.scrollTo(scrollState.maxValue)
    }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(AppColors.slate950)
            .verticalScroll(scrollState)
            .horizontalScroll(rememberScrollState())
            .padding(12.dp),
    ) {
        Text(
            text = rendered.ifBlank { if (running) "（等待输出…）" else "（会话已结束）" },
            style = MaterialTheme.typography.bodySmall,
            fontSize = 13.sp,
            color = AppColors.slate100,
            fontFamily = FontFamily.Monospace,
        )
        if (error != null) {
            Spacer(Modifier.height(6.dp))
            Text(text = error, style = MaterialTheme.typography.bodySmall, color = AppColors.warningColor)
        }
    }
}

@Composable
private fun ControlKeyRow(enabled: Boolean, onKey: (ControlKey) -> Unit) {
    val keys = remember {
        listOf(
            ControlKey.Enter to "⏎",
            ControlKey.Backspace to "⌫",
            ControlKey.Tab to "Tab",
            ControlKey.Escape to "Esc",
            ControlKey.CtrlC to "^C",
            ControlKey.CtrlD to "^D",
            ControlKey.CtrlL to "^L",
            ControlKey.Up to "↑",
            ControlKey.Down to "↓",
            ControlKey.Left to "←",
            ControlKey.Right to "→",
        )
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 8.dp, vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        keys.forEach { (key, label) ->
            TextButton(
                onClick = { onKey(key) },
                enabled = enabled,
                modifier = Modifier.widthIn(min = 40.dp),
            ) {
                Text(label, fontSize = 13.sp, color = if (enabled) AppColors.primary else AppColors.slate400)
            }
        }
    }
}

@Composable
private fun InputRow(
    enabled: Boolean,
    sending: Boolean,
    stopping: Boolean,
    onSend: (String) -> Unit,
) {
    var text by remember { mutableStateOf("") }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 8.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            modifier = Modifier.weight(1f),
            placeholder = { Text("输入命令 / 文本", color = AppColors.slate400) },
            singleLine = true,
            enabled = enabled && !sending,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
            keyboardActions = KeyboardActions(
                onGo = {
                    if (text.isNotEmpty()) {
                        onSend(text)
                        text = ""
                    }
                },
            ),
        )
        TextButton(
            onClick = {
                onSend(text)
                text = ""
            },
            enabled = enabled && !sending && !stopping && text.isNotEmpty(),
        ) {
            Text(
                text = if (sending) "发送中…" else "发送",
                color = AppColors.primary,
            )
        }
    }
}

/** SAF 写回：把渲染后的转写文本落到用户选择的 document uri。 */
private fun writeTranscriptTo(context: Context, uri: Uri, transcript: String) {
    runCatching {
        context.contentResolver.openOutputStream(uri)?.use { stream ->
            stream.write(renderTranscriptForDisplay(transcript).toByteArray(Charsets.UTF_8))
        }
    }
}
