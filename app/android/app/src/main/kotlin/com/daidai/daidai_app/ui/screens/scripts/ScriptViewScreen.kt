package com.daidai.daidai_app.ui.screens.scripts

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.ScriptContent
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 脚本内容查看页（Compose 原生，阶段 3-3）。
 *
 * 给定脚本 [path]，展示后端返回的脚本内容（只读），并提供「运行」按钮发起运行。
 * 通过 [ScriptsViewModel] 共享的 StateFlow 读取内容与运行状态；本组件依赖由上层
 * 注入的同一 ViewModel，自包含可编译，未接导航，用 [onBack] 返回上一级。
 *
 * @param path      目标脚本路径。
 * @param viewModel 已装载仓库的 ScriptsViewModel（由 ScriptListScreen 传入）。
 * @param onBack    返回回调。
 */
@Composable
fun ScriptViewScreen(
    path: String,
    viewModel: ScriptsViewModel? = null,
    onBack: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val state: ScriptsUiState
    if (viewModel != null) {
        state = viewModel.uiState.collectAsStateWithLifecycle().value
    } else {
        state = ScriptsUiState()
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(AppColors.lightPage),
    ) {
        ViewHeader(
            path = path,
            running = state.runningPath == path,
            runId = state.runId,
            onBack = onBack,
            onRun = { viewModel?.runScript(path) },
            onStop = { state.runId?.let { id -> viewModel?.stopScript(id) } },
        )
        HorizontalDivider(color = AppColors.glassDivider)
        ContentBody(
            modifier = Modifier.weight(1f),
            loading = state.loadingContent,
            content = state.content,
            error = state.actionError,
            onLoad = { viewModel?.loadContent(path) },
        )
        // 轻提示区（保存/运行/停止结果）
        val message = state.actionMessage
        val error = state.actionError
        if (message != null || error != null) {
            Text(
                text = error ?: message!!,
                style = MaterialTheme.typography.bodySmall,
                color = if (error != null) AppColors.errorColor else AppColors.successColor,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun ViewHeader(
    path: String,
    running: Boolean,
    runId: String?,
    onBack: (() -> Unit)?,
    onRun: () -> Unit,
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
                Icon(
                    imageVector = Icons.Filled.ArrowBack,
                    contentDescription = "返回",
                    tint = AppColors.slate700,
                )
            }
        }
        Text(
            text = path.substringAfterLast('/').ifBlank { path },
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.weight(1f),
        )
        if (running) {
            Text(
                text = "运行中",
                style = MaterialTheme.typography.labelMedium,
                color = AppColors.runningColor,
                modifier = Modifier.padding(end = 8.dp),
            )
            TextButton(onClick = onStop) {
                Text("停止", color = AppColors.errorColor)
            }
        } else {
            TextButton(onClick = onRun) {
                Text("运行", color = AppColors.primary)
            }
        }
    }
}

@Composable
private fun ContentBody(
    modifier: Modifier = Modifier,
    loading: Boolean,
    content: ScriptContent?,
    error: String?,
    onLoad: () -> Unit,
) {
    Box(modifier = modifier) {
        when {
            loading -> Text(
                text = "加载中…",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate500,
                modifier = Modifier.padding(16.dp),
            )
            content != null && content.isBinary -> Text(
                text = "该文件为二进制文件，无法预览",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.warningColor,
                modifier = Modifier.padding(16.dp),
            )
            content != null -> CodeBlock(content.content)
            error != null -> Column(modifier = Modifier.padding(16.dp)) {
                Text(
                    text = error,
                    style = MaterialTheme.typography.bodyMedium,
                    color = AppColors.errorColor,
                )
                TextButton(onClick = onLoad) {
                    Text("重新加载", color = AppColors.primary)
                }
            }
            else -> Text(
                text = "脚本内容尚未加载。",
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.slate500,
                modifier = Modifier.padding(16.dp),
            )
        }
    }
}

/** 只读代码块：等宽字体 + 横向纵向滚动（不提供编辑）。 */
@Composable
private fun CodeBlock(text: String) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .horizontalScroll(rememberScrollState())
            .verticalScroll(rememberScrollState())
            .background(AppColors.slate950)
            .padding(16.dp),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.slate100,
            fontFamily = FontFamily.Monospace,
        )
        Spacer(Modifier.height(8.dp))
    }
}
