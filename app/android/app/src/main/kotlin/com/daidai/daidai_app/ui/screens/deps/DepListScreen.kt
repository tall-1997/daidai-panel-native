package com.daidai.daidai_app.ui.screens.deps

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.model.SystemPackage
import com.daidai.daidai_app.data.repository.DepsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors
import com.daidai.daidai_app.ui.theme.AppTypography

@Composable
fun DepListScreen(
    modifier: Modifier = Modifier,
    repository: DepsRepository? = null,
    viewModel: DepsViewModel? = null,
    onNavigateToInstall: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: DepsRepository(context)
        DepsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    var selectedTab by remember { mutableStateOf(0) }
    var searchQuery by remember { mutableStateOf("") }
    var pendingUninstallId by remember { mutableStateOf<Long?>(null) }

    // Restore cache dialog
    var showRestoreCacheDialog by remember { mutableStateOf(false) }
    if (showRestoreCacheDialog) {
        AlertDialog(
            onDismissRequest = { showRestoreCacheDialog = false },
            title = { Text("恢复缓存") },
            text = { Text("将恢复系统包缓存，确认继续？") },
            confirmButton = {
                TextButton(onClick = {
                    screenViewModel.restoreCache()
                    showRestoreCacheDialog = false
                }) { Text("恢复") }
            },
            dismissButton = { TextButton(onClick = { showRestoreCacheDialog = false }) { Text("取消") } }
        )
    }

    // System command line terminal dialog
    var terminalOpen by remember { mutableStateOf(false) }
    if (terminalOpen) {
        SystemTerminalDialog(onDismiss = { terminalOpen = false })
    }

    if (pendingUninstallId != null) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { pendingUninstallId = null },
            title = { Text("卸载依赖") },
            text = { Text("确定卸载该依赖？相关运行环境可能受影响。") },
            confirmButton = {
                TextButton(onClick = {
                    screenViewModel.uninstall(pendingUninstallId!!)
                    pendingUninstallId = null
                }) { Text("卸载", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { pendingUninstallId = null }) { Text("取消") }
            },
        )
    }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        Column(Modifier.fillMaxSize().padding(bottom = 80.dp)) {
            DepsCapabilityControls(state, screenViewModel)

            // Tabs
            TabRow(selectedTabIndex = selectedTab) {
                Tab(
                    selected = selectedTab == 0,
                    onClick = { selectedTab = 0 },
                    text = { Text("pip") }
                )
                Tab(
                    selected = selectedTab == 1,
                    onClick = { selectedTab = 1 },
                    text = { Text("npm") }
                )
                Tab(
                    selected = selectedTab == 2,
                    onClick = { selectedTab = 2 },
                    text = { Text("系统包") }
                )
            }

            // Search bar
            OutlinedTextField(
                value = searchQuery,
                onValueChange = { searchQuery = it },
                placeholder = { Text("搜索 / 过滤包") },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 20.dp, vertical = 8.dp),
                singleLine = true,
            )

            // Restore cache + terminal buttons
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 20.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.End
            ) {
                TextButton(onClick = { showRestoreCacheDialog = true }) { Text("恢复缓存") }
                TextButton(onClick = { terminalOpen = true }) { Text("系统命令行") }
            }

            when {
                selectedTab != 2 && state.phase == DepsUiState.Phase.Loading -> LoadingView()
                selectedTab == 2 && state.systemPhase == DepsUiState.Phase.Loading -> LoadingView()
                selectedTab != 2 && state.phase == DepsUiState.Phase.Error -> ErrorView(
                    message = state.errorMessage ?: "加载依赖失败，请重试",
                    onRetry = screenViewModel::refresh,
                )
                selectedTab == 2 && state.systemPhase == DepsUiState.Phase.Error -> ErrorView(
                    message = state.errorMessage ?: "加载系统包失败",
                    onRetry = screenViewModel::listSystemPackages,
                )
                selectedTab != 2 && state.deps.isEmpty() -> EmptyView(
                    title = "暂无依赖",
                    description = "点击底部按钮安装 pip / npm 依赖",
                )
                selectedTab == 2 && state.systemPackages.isEmpty() -> EmptyView(
                    title = "暂无系统包",
                    description = "恢复缓存以列出已安装的系统包",
                )
                else -> {
                    when (selectedTab) {
                        0 -> DepList(
                            state = state,
                            deps = state.deps.filter { it.isPython && matchesFilter(it, searchQuery) },
                            onUninstall = { id -> pendingUninstallId = id },
                            onReinstall = screenViewModel::reinstall,
                            onLog = screenViewModel::watch,
                            onSelect = screenViewModel::select,
                        )
                        1 -> DepList(
                            state = state,
                            deps = state.deps.filter { it.type == "node" && matchesFilter(it, searchQuery) },
                            onUninstall = { id -> pendingUninstallId = id },
                            onReinstall = screenViewModel::reinstall,
                            onLog = screenViewModel::watch,
                            onSelect = screenViewModel::select,
                        )
                        2 -> SystemPackageList(
                            packages = state.systemPackages.filter { matchesFilter(it, searchQuery) },
                            viewModel = screenViewModel,
                        )
                    }
                }
            }
        }

        InstallEntryBar(
            onClick = onNavigateToInstall,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .padding(horizontal = 20.dp, vertical = 16.dp),
        )
    }
}

private fun matchesFilter(item: Any, query: String): Boolean {
    if (query.isBlank()) return true
    val name = when (item) {
        is DepItem -> item.name
        is SystemPackage -> item.name
        else -> item.toString()
    }
    return name.contains(query, ignoreCase = true)
}

@Composable
private fun DepList(
    state: DepsUiState,
    deps: List<DepItem>,
    onUninstall: (Long) -> Unit,
    onReinstall: (Long) -> Unit,
    onLog: (Long) -> Unit,
    onSelect: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 20.dp, top = 8.dp, end = 20.dp, bottom = 96.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(deps, key = { it.id }) { dep ->
            Column {
                Row {
                    androidx.compose.material3.Checkbox(
                        checked = dep.id in state.selected,
                        onCheckedChange = { onSelect(dep.id) },
                        enabled = !dep.active && !state.actionBusy
                    )
                    TextButton(onClick = { onLog(dep.id) }) {
                        Text("状态 / 日志 · ${DepItem.statusLabel(dep.status)}")
                    }
                }
                DepCard(
                    dep = dep,
                    operating = state.operatingId == dep.id,
                    onUninstall = { onUninstall(dep.id) },
                    onReinstall = { onReinstall(dep.id) },
                )
            }
        }
    }
}

@Composable
private fun SystemPackageList(
    packages: List<SystemPackage>,
    viewModel: DepsViewModel,
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 20.dp, top = 8.dp, end = 20.dp, bottom = 96.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(packages, key = { it.name }) { pkg ->
            SystemPackageCard(
                pkg = pkg,
                onUninstall = { viewModel.uninstallSystemPackage(pkg.name) }
            )
        }
    }
}

@Composable
private fun SystemPackageCard(
    pkg: SystemPackage,
    onUninstall: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(12.dp)
                .clip(CircleShape)
                .background(MaterialTheme.colorScheme.secondary)
        )
        Spacer(Modifier.width(12.dp))
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = pkg.name,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.W700,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = "系统包",
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.miuixBlue,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(AppColors.blue100)
                        .padding(horizontal = 8.dp, vertical = 3.dp),
                )
            }
            Text(
                text = pkg.version.ifEmpty { "版本未知" },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = pkg.description.takeIf { it.isNotBlank() } ?: "系统包",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
                maxLines = 2,
            )
        }
        Spacer(Modifier.width(12.dp))
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
            TextButton(onClick = onUninstall) {
                Text("卸载", color = MaterialTheme.colorScheme.error)
            }
        }
    }
}

@Composable
private fun DepCard(
    dep: DepItem,
    operating: Boolean,
    onUninstall: () -> Unit,
    onReinstall: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(12.dp)
                .clip(CircleShape)
                .background(if (dep.isPython) AppColors.successColor else AppColors.miuixBlue),
        )
        Spacer(Modifier.width(12.dp))
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = dep.name,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.W700,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = dep.typeLabel,
                    style = MaterialTheme.typography.labelSmall,
                    color = if (dep.isPython) AppColors.successColor else AppColors.miuixBlue,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (dep.isPython) AppColors.red50 else AppColors.blue100)
                        .padding(horizontal = 8.dp, vertical = 3.dp),
                )
            }
            Text(
                text = dep.version.ifEmpty { "版本未知" },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (dep.runtime.isNotBlank() && dep.isPython) {
                Text(
                    text = "运行时 Python ${dep.runtime}",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                )
            }
        }
        Spacer(Modifier.width(12.dp))
        if (operating) {
            CircularProgressIndicator(
                modifier = Modifier.size(18.dp),
                strokeWidth = 2.dp,
                color = AppColors.primary,
            )
        } else if (!dep.active) {
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                TextButton(onClick = onReinstall) {
                    Text("重装", color = AppColors.successColor)
                }
                TextButton(onClick = onUninstall) {
                    Text("卸载", color = AppColors.errorColor)
                }
            }
        }
    }
}

@Composable
private fun InstallEntryBar(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Button(
        onClick = onClick,
        modifier = modifier,
        shape = RoundedCornerShape(14.dp),
    ) {
        Text(
            text = "安装依赖",
            modifier = Modifier.padding(vertical = 2.dp),
            fontWeight = FontWeight.W600,
        )
    }
}
