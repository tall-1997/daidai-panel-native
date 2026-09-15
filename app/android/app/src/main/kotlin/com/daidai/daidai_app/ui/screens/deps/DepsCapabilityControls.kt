package com.daidai.daidai_app.ui.screens.deps

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp

@Composable
internal fun DepsCapabilityControls(state: DepsUiState, vm: DepsViewModel) {
    val context = LocalContext.current
    var confirmBatch by remember { mutableStateOf<Boolean?>(null) }
    var exportDialog by remember { mutableStateOf(false) }
    var exportType by remember { mutableStateOf("python") }
    var version by remember { mutableStateOf("") }
    var pendingExport by remember { mutableStateOf<Pair<String, String>?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("text/plain")) { uri ->
        val pending = pendingExport
        pendingExport = null
        if (uri != null && pending != null) vm.export(context.contentResolver, uri, pending.first, pending.second)
    }
    DisposableEffect(vm) { onDispose { vm.closeLog() } }
    Column {
        Row {
            TextButton(onClick = vm::refresh, enabled = !state.actionBusy) { Text("刷新") }
            TextButton(onClick = vm::loadMirrors, enabled = !state.actionBusy) { Text("镜像源") }
            TextButton(onClick = { exportDialog = true }, enabled = !state.actionBusy && pendingExport == null) { Text("导出") }
        }
        if (state.selected.isNotEmpty()) Row {
            TextButton(onClick = { confirmBatch = false }, enabled = !state.actionBusy) { Text("重装 ${state.selected.size} 项") }
            TextButton(onClick = { confirmBatch = true }, enabled = !state.actionBusy) { Text("删除 ${state.selected.size} 项") }
        }
        state.notice?.let { message -> TextButton(onClick = vm::consumeNotice) { Text(message) } }
        state.errorMessage?.let { message -> TextButton(onClick = vm::clearError) { Text(message, color = MaterialTheme.colorScheme.error) } }
        if (state.actionBusy) LinearProgressIndicator(Modifier.fillMaxWidth())
    }
    confirmBatch?.let { delete ->
        AlertDialog(onDismissRequest = { confirmBatch = null }, title = { Text(if (delete) "批量删除依赖" else "批量重装依赖") },
            text = { Text("操作 ${state.selected.size} 项依赖，可能影响任务运行。服务端会跳过正在执行操作的条目。") },
            confirmButton = { TextButton(onClick = { vm.batch(delete); confirmBatch = null }) { Text("确认") } },
            dismissButton = { TextButton(onClick = { confirmBatch = null }) { Text("取消") } })
    }
    if (exportDialog) AlertDialog(onDismissRequest = { exportDialog = false }, title = { Text("导出依赖清单") },
        text = { Column {
            Row { listOf("python", "nodejs", "linux").forEach { type ->
                TextButton(onClick = { exportType = type }) { Text(if (type == exportType) "[$type]" else type) }
            } }
            if (exportType == "python") OutlinedTextField(value = version, onValueChange = { version = it }, label = { Text("Python 版本（空为服务端默认）") })
        } }, confirmButton = { TextButton(onClick = {
            pendingExport = exportType to version
            exportDialog = false
            launcher.launch("dependencies-$exportType.txt")
        }) { Text("选择保存位置") } }, dismissButton = { TextButton(onClick = { exportDialog = false }) { Text("取消") } })
    state.mirrors?.let { mirrors ->
        var pip by remember(mirrors) { mutableStateOf(mirrors.pip) }
        var npm by remember(mirrors) { mutableStateOf(mirrors.npm) }
        var linux by remember(mirrors) { mutableStateOf(mirrors.linux) }
        AlertDialog(onDismissRequest = vm::closeMirrors, title = { Text("镜像源配置") }, text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                OutlinedTextField(value = pip, onValueChange = { pip = it }, label = { Text("pip") })
                OutlinedTextField(value = npm, onValueChange = { npm = it }, label = { Text("npm") })
                OutlinedTextField(value = linux, onValueChange = { linux = it }, enabled = mirrors.linuxSupported, label = { Text(mirrors.linuxLabel) })
                Text(mirrors.message)
                state.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        }, confirmButton = { TextButton(enabled = !state.actionBusy, onClick = { vm.saveMirrors(mirrors.copy(pip = pip, npm = npm, linux = linux)) }) { Text("保存") } },
            dismissButton = { TextButton(onClick = vm::closeMirrors) { Text("关闭") } })
    }
    state.detail?.let { detail ->
        var confirmCancel by remember(detail.id) { mutableStateOf(false) }
        AlertDialog(onDismissRequest = vm::closeLog, title = { Text("依赖 #${detail.id} · ${detail.status}") },
            text = { Column {
                Text("保留最近 128 KiB。本地取消接口仅更新状态，进程终止能力待后端补齐。", style = MaterialTheme.typography.labelSmall)
                Text(state.log.ifEmpty { "等待日志" }, Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState()))
                state.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (state.watching) LinearProgressIndicator(Modifier.fillMaxWidth())
            } }, confirmButton = { Row {
                TextButton(onClick = { vm.watch(detail.id) }) { Text("重连") }
                if (detail.active) TextButton(enabled = !state.actionBusy, onClick = { confirmCancel = true }) { Text("取消任务") }
            } }, dismissButton = { TextButton(onClick = vm::closeLog) { Text("关闭") } })
        if (confirmCancel) AlertDialog(onDismissRequest = { confirmCancel = false }, title = { Text("取消依赖操作？") },
            confirmButton = { TextButton(onClick = { confirmCancel = false; vm.cancel(detail.id) }) { Text("确认取消") } },
            dismissButton = { TextButton(onClick = { confirmCancel = false }) { Text("返回") } })
    }
}
