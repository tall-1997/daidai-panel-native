package com.daidai.daidai_app.ui.screens.scripts

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.ScriptFile
import com.daidai.daidai_app.data.repository.PanelScriptsRepository
import com.daidai.daidai_app.data.repository.ScriptsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 脚本文件树列表页（Compose 原生，阶段 3-3）。
 *
 * 展示脚本目录树：目录可逐级展开 / 收起，脚本条目支持查看内容、运行、删除；
 * 覆盖加载中 / 空态 / 错误（含重试）分支。点击脚本文件条目会在页面内切换到
 * [ScriptViewScreen] 查看只读内容并运行。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配自包含仓库。**未接线导航**，
 * 本组件自包含可编译。
 */
@Composable
fun ScriptListScreen(
    modifier: Modifier = Modifier,
    repository: ScriptsRepository? = null,
    viewModel: ScriptsViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelScriptsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        ScriptsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()
    var pendingDelete by remember { mutableStateOf<ScriptFile?>(null) }
    var pendingRename by remember { mutableStateOf<ScriptFile?>(null) }
    var pendingCopy by remember { mutableStateOf<ScriptFile?>(null) }
    var pendingDownload by remember { mutableStateOf<String?>(null) }
    var pendingRunParams by remember { mutableStateOf<String?>(null) }
    var runParams by remember { mutableStateOf("") }
    pendingDelete?.let { file ->
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除脚本") },
            text = { Text(
                if (file.isDirectory) "“${file.name}”是文件夹，删除将移除其中的全部内容，且不可撤销。"
                else "确定删除脚本“${file.name}”？此操作不可撤销。"
            ) },
            confirmButton = {
                TextButton(onClick = {
                    screenViewModel.deleteScript(file.path, file.isDirectory)
                    pendingDelete = null
                }) { Text("删除", color = AppColors.errorColor) }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("取消") }
            },
        )
    }

    // 内部内容页切换：未接导航，用状态位在列表页与内容页间切换。
    var viewingPath by remember { mutableStateOf<String?>(null) }
    val activePath = viewingPath ?: state.selectedPath

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        when {
            // 正在查看某个脚本内容
            activePath != null -> {
                ScriptViewScreen(
                    path = activePath,
                    viewModel = screenViewModel,
                    onBack = { viewingPath = null },
                )
            }
            // 列表页三态
            state.phase == ScriptsUiState.Phase.Loading -> LoadingView()
            state.phase == ScriptsUiState.Phase.Error -> ErrorView(
                message = state.errorMessage ?: "脚本列表加载失败",
                onRetry = screenViewModel::refreshTree,
            )
            state.tree.isEmpty() -> EmptyView(
                title = "暂无脚本",
                description = "还没有任何脚本文件",
            )
            else -> {
                val rows = buildRows(state.tree, state.expandedPaths)
                Column(modifier = Modifier.fillMaxSize()) {
                    TreeHeader(onRefresh = screenViewModel::refreshTree)
                    LazyColumn(
                        modifier = Modifier.weight(1f),
                        contentPadding = PaddingValues(vertical = 8.dp),
                    ) {
                        items(rows, key = { it.first }) { (path, file, depth) ->
                            ScriptNodeRow(
                                file = file,
                                depth = depth,
                                running = state.runningPath == file.path,
                                onOpenFile = { p ->
                                    viewingPath = p
                                    screenViewModel.loadContent(p)
                                },
                                onToggleExpand = screenViewModel::toggleExpanded,
                                onRun = screenViewModel::runScript,
                                onDelete = { f -> pendingDelete = f },
                                onDownload = screenViewModel::downloadScript,
                            )
                        }
                    }
                }
            }
        }
    }
}

/**
 * 依据展开集合把嵌套树拍平成 (key, ScriptFile, depth) 三元组列表，
 * 供单个 LazyColumn 直接渲染，天然支持任意深度缩进。
 */
private fun buildRows(
    items: List<ScriptFile>,
    expanded: Set<String>,
    startDepth: Int = 0,
): List<Triple<String, ScriptFile, Int>> {
    val result = ArrayList<Triple<String, ScriptFile, Int>>()
    val stack = ArrayDeque<Pair<ScriptFile, Int>>()
    // 深度优先：反序入栈以保持显示顺序。
    for (i in items.indices.reversed()) {
        stack.addLast(items[i] to startDepth)
    }
    while (stack.isNotEmpty()) {
        val (file, depth) = stack.removeLast()
        result.add(Triple(file.path.ifBlank { "node-$depth-${stack.size}" }, file, depth))
        if (file.isDirectory && expanded.contains(file.path)) {
            for (i in file.children.indices.reversed()) {
                stack.addLast(file.children[i] to (depth + 1))
            }
        }
    }
    return result
}


@Composable
private fun TreeHeader(onRefresh: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(
            text = "脚本文件",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
        )
        Row {
            TextButton(onClick = { /* todo: 新建文件 */ }) {
                Text("新建文件")
            }
            TextButton(onClick = { /* todo: 上传文件 */ }) {
                Text("上传")
            }
            IconButton(onClick = onRefresh) {
                Icon(
                    imageVector = Icons.Filled.Refresh,
                    contentDescription = "刷新",
                    tint = AppColors.primary,
                )
            }
        }
    }
    HorizontalDivider(color = AppColors.glassDivider)
}


/**
 * 单个脚本节点行：按 [depth] 缩进，目录展示折叠箭头，脚本展示打开 / 运行 / 删除入口。
 */
@Composable
private fun ScriptNodeRow(
    file: ScriptFile,
    depth: Int,
    running: Boolean,
    onOpenFile: (String) -> Unit,
    onToggleExpand: (String) -> Unit,
    onRun: (String) -> Unit,
    onDelete: (ScriptFile) -> Unit,
    onRename: (String) -> Unit = {},
    onCopy: (String) -> Unit = {},
    onDownload: (String) -> Unit = {},
) {
    val isDir = file.isDirectory
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 2.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .clickable {
                if (isDir) onToggleExpand(file.path) else onOpenFile(file.path)
            }
            .padding(
                start = (6 + depth * 18).dp,
                top = 10.dp,
                bottom = 10.dp,
                end = 8.dp,
            ),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (isDir) {
            Icon(
                imageVector = Icons.Filled.KeyboardArrowRight,
                contentDescription = "目录",
                tint = AppColors.slate500,
                modifier = Modifier.size(18.dp),
            )
            Spacer(Modifier.width(4.dp))
        } else {
            Spacer(Modifier.width(18.dp))
            Icon(
                imageVector = Icons.Filled.PlayArrow,
                contentDescription = "脚本",
                tint = AppColors.slate400,
                modifier = Modifier.size(20.dp),
            )
        }
        Spacer(Modifier.width(8.dp))
        Text(
            text = file.name.ifBlank { file.path },
            style = MaterialTheme.typography.bodyMedium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (!isDir) {
            if (running) {
                Text(
                    text = "运行中",
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.runningColor,
                )
                Spacer(Modifier.width(8.dp))
            } else {
                TextButton(onClick = { onRunFile(file.path) }) {
                    Text("运行", color = AppColors.primary)
                }
            }
            IconButton(onClick = { onDelete(file) }) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = "删除",
                    tint = AppColors.errorColor,
                )
            }
        }
    }
}
