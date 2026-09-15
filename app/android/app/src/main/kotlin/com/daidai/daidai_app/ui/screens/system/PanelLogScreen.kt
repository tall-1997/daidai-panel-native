package com.daidai.daidai_app.ui.screens.system

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.model.PanelLogPage
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 面板日志页。
 *
 * Go: GET /api/system/panel-log?lines=&keyword=&level= 返回 data.logs 字符串数组；
 * 本地端目前通过 serveLogs 回退到任务日志列表（JSONArray），解析器已兼容两种结构。
 */
@Composable
fun PanelLogScreen(
    repository: BackupRepository? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val snackbarHostState = remember { SnackbarHostState() }

    var lines by remember { mutableStateOf(300) }
    var keyword by remember { mutableStateOf("") }
    var level by remember { mutableStateOf("") }
    var page by remember { mutableStateOf<PanelLogPage?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(resolvedRepository, lines, keyword, level) {
        loading = true
        error = null
        try {
            page = resolvedRepository.panelLog(lines, keyword, level)
        } catch (failure: Exception) {
            if (failure is kotlinx.coroutines.CancellationException) throw failure
            error = failure.message ?: "拉取面板日志失败"
        } finally {
            loading = false
        }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MaterialTheme.colorScheme.background,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            Column(modifier = Modifier.padding(horizontal = 20.dp, vertical = 12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = "面板日志",
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.Bold,
                            color = AppColors.slate900,
                        )
                        Text(
                            text = "查看面板自身运行日志",
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate500,
                        )
                    }
                    if (!loading) {
                        IconButton(onClick = { lines = lines }) {
                            Icon(Icons.Filled.Refresh, contentDescription = "刷新", tint = AppColors.primaryDark)
                        }
                    }
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    listOf("" to "全部", "debug" to "Debug", "info" to "Info", "warn" to "Warn", "error" to "Error")
                        .forEach { (value, label) ->
                            FilterChip(
                                selected = level == value,
                                onClick = { level = value },
                                label = { Text(label) },
                            )
                        }
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    listOf(100 to "100行", 300 to "300行", 1000 to "1000行", 2000 to "2000行")
                        .forEach { (value, label) ->
                            FilterChip(
                                selected = lines == value,
                                onClick = { lines = value },
                                label = { Text(label) },
                            )
                        }
                }
                OutlinedTextField(
                    value = keyword,
                    onValueChange = { keyword = it },
                    label = { Text("关键字（回车搜索）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                )
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when {
                loading -> LoadingView()
                error != null -> ErrorView(message = error ?: "加载失败", onRetry = { lines = lines })
                page == null || page!!.lines.isEmpty() -> EmptyView(title = "暂无日志", description = "调整筛选条件或稍后重试")
                else -> SelectionContainer {
                    LazyColumn(
                        modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
                    ) {
                        items(page!!.lines.size) { index ->
                            val text = page!!.lines[index]
                            Text(
                                text = text,
                                style = MaterialTheme.typography.bodySmall,
                                fontFamily = FontFamily.Monospace,
                                color = when {
                                    text.contains("error", true) -> AppColors.errorColor
                                    text.contains("warn", true) -> Color(0xFFB45309)
                                    else -> AppColors.slate700
                                },
                                maxLines = 3,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}
