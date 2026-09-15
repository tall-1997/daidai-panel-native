package com.daidai.daidai_app.ui.screens.subscriptions

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

@Composable
internal fun SubscriptionLogDialog(state: SubscriptionsUiState, vm: SubscriptionsViewModel) {
    DisposableEffect(vm) { onDispose { vm.closeLogs() } }
    val id = state.logId ?: return
    var history by remember(id) { mutableStateOf(false) }
    var confirmStop by remember(id) { mutableStateOf(false) }
    AlertDialog(onDismissRequest = vm::closeLogs, title = { Text("订阅 #$id 日志") }, text = {
        Column {
            Text("${state.logStatus}。实时缓存上限 128 KiB。", style = MaterialTheme.typography.labelSmall)
            Text("本地端返回历史快照；中止接口仅记录请求，实际终止能力待后端补齐。", style = MaterialTheme.typography.labelSmall)
            Row {
                TextButton(onClick = { history = false }) { Text("实时") }
                TextButton(onClick = { history = true; vm.history(1) }) { Text("历史") }
                TextButton(onClick = { vm.openLogs(id, resume = true) }) { Text("重连") }
            }
            if (state.streaming || state.historyLoading) LinearProgressIndicator(Modifier.fillMaxWidth())
            Column(Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState())) {
                if (history) {
                    state.history?.entries?.forEach { entry ->
                        Text("${entry.createdAt} · ${entry.status} · ${entry.operationId}", style = MaterialTheme.typography.labelSmall)
                        Text(entry.content)
                        HorizontalDivider()
                    }
                    if (state.history?.entries?.isEmpty() == true) Text("暂无历史日志")
                } else Text(state.logText.ifEmpty { "等待日志" })
            }
            if (history) Row {
                val page = state.history?.page ?: 1
                TextButton(enabled = page > 1 && !state.historyLoading, onClick = { vm.history(page - 1) }) { Text("上一页") }
                TextButton(enabled = state.history?.hasNext == true && !state.historyLoading, onClick = { vm.history(page + 1) }) { Text("下一页") }
            }
            state.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        }
    }, confirmButton = { TextButton(enabled = !state.stopping, onClick = { confirmStop = true }) { Text("中止拉取") } },
        dismissButton = { TextButton(onClick = vm::closeLogs) { Text("关闭") } })
    if (confirmStop) AlertDialog(onDismissRequest = { confirmStop = false }, title = { Text("中止当前拉取？") },
        text = { Text("部分文件可能已经更新。") },
        confirmButton = { TextButton(onClick = { confirmStop = false; vm.stopPull(id) }) { Text("确认中止") } },
        dismissButton = { TextButton(onClick = { confirmStop = false }) { Text("返回") } })
}
