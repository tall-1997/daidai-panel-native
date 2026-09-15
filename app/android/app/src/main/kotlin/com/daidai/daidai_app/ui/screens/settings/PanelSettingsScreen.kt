package com.daidai.daidai_app.ui.screens.settings

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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors
import kotlinx.coroutines.launch

/**
 * 面板设置页（C8/C9）。
 *
 * 显示 Go 端 data.{key:{value,default_value}} 或本地端 data.{key:value}
 * 两种归一化形态后的可编辑字段；保存走 POST /api/configs（key=value 逐条写入），
 * 并提示"修改对 Web UI 即时生效"。标题/图标写回后回读刷新。
 * 编辑器背景色与日志背景色以只读色块展示。
 */
@Composable
fun PanelSettingsScreen(
    repository: BackupRepository? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val scope = rememberCoroutineScope()
    val snackbarHostState = remember { SnackbarHostState() }

    var snapshot by remember { mutableStateOf<com.daidai.daidai_app.data.model.PanelSettingsSnapshot?>(null) }
    var title by remember { mutableStateOf("") }
    var icon by remember { mutableStateOf("") }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(resolvedRepository) {
        loading = true
        error = null
        try {
            val settings = resolvedRepository.panelSettings()
            snapshot = settings
            title = settings.panelTitle
            icon = settings.panelIcon
        } catch (failure: Exception) {
            if (failure is kotlinx.coroutines.CancellationException) throw failure
            error = failure.message ?: "读取面板设置失败"
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
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "面板设置",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                        color = AppColors.slate900,
                    )
                    Text(
                        text = "可编辑面板标题与图标",
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
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
                    onClick = {
                        saving = true
                        scope.launch {
                            try {
                                resolvedRepository.savePanelSettings(title, icon)
                                val refreshed = resolvedRepository.panelSettings()
                                snapshot = refreshed
                                title = refreshed.panelTitle
                                icon = refreshed.panelIcon
                                snackbarHostState.showSnackbar("面板设置已保存")
                            } catch (failure: Exception) {
                                if (failure is kotlinx.coroutines.CancellationException) throw failure
                                snackbarHostState.showSnackbar(failure.message ?: "保存失败")
                            } finally {
                                saving = false
                            }
                        }
                    },
                    enabled = !loading && !saving,
                    modifier = Modifier.fillMaxWidth().height(48.dp),
                ) { Text("保存") }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when {
                loading -> LoadingView()
                error != null -> ErrorView(message = error ?: "加载失败")
                else -> Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Text(
                        text = "编辑字段保存后对 Web UI 即时生效",
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                    OutlinedTextField(
                        value = title,
                        onValueChange = { title = it },
                        label = { Text("面板标题") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = icon,
                        onValueChange = { icon = it },
                        label = { Text("面板图标 URL") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    snapshot?.let { s ->
                        Row {
                            IconPlaceholder(label = "编辑器背景色")
                            Text(s.editorBackgroundColor.ifBlank { "-" })
                        }
                        Row {
                            IconPlaceholder(label = "日志背景色")
                            Text(s.logBackgroundColor.ifBlank { "-" })
                        }
                        Text("运行时模式：${s.panelRuntimeMode.ifBlank { "-" }}")
                        Text("服务管理器：${s.panelServiceManager.ifBlank { "-" }}")
                        Text("服务名：${s.panelServiceName.ifBlank { "-" }}")
                        Text("图标：${s.panelIcon.ifBlank { "-" }}")
                    }
                }
            }
        }
    }
}

@Composable
private fun Row(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) = androidx.compose.foundation.layout.Row(modifier = modifier, horizontalArrangement = Arrangement.spacedBy(8.dp), content = content)

@Composable
private fun IconPlaceholder(label: String) {
    androidx.compose.foundation.layout.Row(verticalAlignment = Alignment.CenterVertically) {
        androidx.compose.foundation.layout.Box(
            modifier = androidx.compose.foundation.layout.size(16.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(AppColors.slate300),
        ) {}
        androidx.compose.foundation.layout.Spacer(modifier = androidx.compose.foundation.layout.size(6.dp))
        Text(label, color = AppColors.slate500, style = MaterialTheme.typography.bodySmall)
    }
}
