package com.daidai.daidai_app.ui.screens.system

import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
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
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors
import kotlinx.coroutines.launch

/**
 * 配置脚本页（C8/C9）。
 *
 * GET/PUT /api/system/config-script 读写 config.sh；保存前二次确认（覆盖文件、
 * 重启生效）。导入/导出走 SAF：导出 CreateDocument(text/plain)，导入 OpenDocument
 * 读入编辑器并标记脏，保存仍走确认对话框（不直接 PUT）。
 */
@Composable
fun ConfigScriptScreen(
    repository: BackupRepository? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val scope = rememberCoroutineScope()
    val snackbarHostState = remember { SnackbarHostState() }

    var content by remember { mutableStateOf("") }
    var path by remember { mutableStateOf("config.sh") }
    var dirty by remember { mutableStateOf(false) }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var showSaveConfirm by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var reloadKey by remember { mutableIntStateOf(0) }

    LaunchedEffect(reloadKey) {
        loading = true
        error = null
        try {
            val result = resolvedRepository.configScript()
            content = result.content
            path = result.path.ifBlank { "config.sh" }
            dirty = false
        } catch (failure: Exception) {
            if (failure is kotlinx.coroutines.CancellationException) throw failure
            error = failure.message ?: "读取配置脚本失败"
        } finally {
            loading = false
        }
    }

    val exportLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("text/plain"),
    ) { uri: Uri? ->
        if (uri != null) {
            scope.launch {
                val ok = writeTextToUri(context, uri, content)
                snackbarHostState.showSnackbar(if (ok) "配置脚本已导出" else "导出失败")
            }
        }
    }

    val importLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument(),
    ) { uri: Uri? ->
        if (uri != null) {
            val text = readTextFromUri(context, uri)
            if (text != null) {
                content = text
                dirty = true
                scope.launch { snackbarHostState.showSnackbar("已导入内容，保存后生效") }
            } else {
                scope.launch { snackbarHostState.showSnackbar("导入失败：无法读取所选文件") }
            }
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
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "配置脚本",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                        color = AppColors.slate900,
                    )
                    Text(
                        text = path,
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
                TextButton(
                    onClick = { importLauncher.launch(arrayOf("text/plain", "*/*")) },
                    enabled = !loading && !saving,
                ) { Text("导入") }
                Spacer(modifier = Modifier.width(4.dp))
                TextButton(
                    onClick = { exportLauncher.launch(path) },
                    enabled = !loading && content.isNotBlank(),
                ) { Text("导出") }
            }
        },
        bottomBar = {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.background)
                    .padding(horizontal = 16.dp, vertical = 12.dp),
            ) {
                if (saving) {
                    LinearProgressIndicator(modifier = Modifier.fillMaxWidth().height(4.dp))
                    Spacer(modifier = Modifier.height(8.dp))
                }
                HorizontalDivider(color = AppColors.slate200)
                Spacer(modifier = Modifier.height(12.dp))
                Button(
                    onClick = { showSaveConfirm = true },
                    enabled = !loading && !saving && dirty,
                    modifier = Modifier.fillMaxWidth().height(48.dp),
                ) { Text(if (dirty) "保存（有未保存修改）" else "保存") }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when {
                loading -> LoadingView()
                error != null -> ErrorView(
                    message = error ?: "加载失败",
                    onRetry = { reloadKey++ },
                )
                else -> SelectionContainer {
                    OutlinedTextField(
                        value = content,
                        onValueChange = { content = it; dirty = true },
                        modifier = Modifier
                            .fillMaxSize()
                            .padding(16.dp),
                        textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    )
                }
            }
        }
    }

    if (showSaveConfirm) {
        AlertDialog(
            onDismissRequest = { showSaveConfirm = false },
            title = { Text("覆盖配置脚本") },
            text = { Text("保存将直接覆盖 $path，面板重启后生效。确定保存吗？") },
            confirmButton = {
                Button(
                    onClick = {
                        showSaveConfirm = false
                        saving = true
                        scope.launch {
                            try {
                                val msg = resolvedRepository.saveConfigScript(content)
                                dirty = false
                                snackbarHostState.showSnackbar(msg.ifBlank { "配置脚本已保存" })
                            } catch (failure: Exception) {
                                if (failure is kotlinx.coroutines.CancellationException) throw failure
                                snackbarHostState.showSnackbar(failure.message ?: "保存失败")
                            } finally {
                                saving = false
                            }
                        }
                    },
                ) { Text("确认保存") }
            },
            dismissButton = {
                Button(onClick = { showSaveConfirm = false }) { Text("取消") }
            },
        )
    }
}

/** 读取 content:// 文本（SAF 导入）。 */
private fun readTextFromUri(context: Context, uri: Uri): String? = runCatching {
    context.contentResolver.openInputStream(uri)?.use { input ->
        input.bufferedReader().readText()
    }
}.getOrNull()

/** 写文本到 content://（SAF 导出）。 */
private fun writeTextToUri(context: Context, uri: Uri, text: String): Boolean = runCatching {
    context.contentResolver.openOutputStream(uri, "w")?.use { output ->
        output.write(text.toByteArray())
    } != null
}.getOrDefault(false)
