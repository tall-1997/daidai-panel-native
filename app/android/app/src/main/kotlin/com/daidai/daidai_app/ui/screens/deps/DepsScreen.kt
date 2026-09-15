package com.daidai.daidai_app.ui.screens.deps

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import com.daidai.daidai_app.data.model.DePackage
import com.daidai.daidai_app.ui.screens.deps.components.SystemTerminalDialog
import com.daidai.daidai_app.ui.theme.AppColors
import com.daidai.daidai_app.ui.screens.deps.DepsViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DepsScreen(onNavigateToInstall: () -> Unit) {
    val vm: DepsViewModel = hiltViewModel()
    val state by vm.uiState.collectAsState()
    var selectedTab by remember { mutableStateOf(0) }
    var searchQuery by remember { mutableStateOf("") }
    var terminalOpen by remember { mutableStateOf(false) }

    val tabs = listOf("pip", "npm", "系统包")

    LaunchedEffect(selectedTab) {
        when (selectedTab) {
            0, 1 -> vm.loadDeps()
            2 -> vm.listSystemPackages()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("依赖管理") },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.surface
                )
            )
        },
        floatingActionButton = {
            Column(
                verticalArrangement = Arrangement.spacedBy(8.dp),
                horizontalAlignment = Alignment.End
            ) {
                SmallFloatingActionButton(
                    onClick = { vm.restoreCache() },
                    containerColor = AppColors.primary
                ) {
                    Text("缓存")
                }
                SmallFloatingActionButton(
                    onClick = { terminalOpen = true }
                ) {
                    Text("终端")
                }
            }
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp, vertical = 8.dp)
        ) {
            TabRow(selectedTabIndex = selectedTab) {
                tabs.forEachIndexed { index, title ->
                    Tab(
                        selected = selectedTab == index,
                        onClick = { selectedTab = index },
                        text = { Text(title) }
                    )
                }
            }
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = searchQuery,
                onValueChange = { searchQuery = it },
                placeholder = { Text("搜索包") },
                leadingIcon = { Icon(Icons.Default.Search, null) },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search)
            )
            Spacer(Modifier.height(8.dp))

            when (selectedTab) {
                0, 1 -> {
                    when (state.phase) {
                        is DepsUiState.Phase.Loading -> {
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                CircularProgressIndicator()
                            }
                        }
                        is DepsUiState.Phase.Error -> {
                            Text("错误: ${(state.phase as DepsUiState.Phase.Error).message}", color = MaterialTheme.colorScheme.error)
                        }
                        is DepsUiState.Phase.Loaded -> {
                            val deps = (state.phase as DepsUiState.Phase.Loaded).deps
                            val filtered = when (selectedTab) {
                                0 -> deps.filter { it.isPython }
                                1 -> deps.filter { !it.isPython }
                                else -> emptyList()
                            }.filter { searchQuery.isBlank() || it.name.contains(searchQuery, ignoreCase = true) }
                            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                items(filtered, key = { it.name }) { dep ->
                                    DepItemRow(
                                        dep = dep,
                                        operating = state.operationalItems.contains(dep.name),
                                        onReinstall = { vm.reinstall(dep.name, dep.runtime) },
                                        onUninstall = { vm.uninstall(dep.name) }
                                    )
                                }
                                item { Spacer(Modifier.height(84.dp)) }
                            }
                        }
                    }
                }
                2 -> {
                    when (state.systemPhase) {
                        is DepsUiState.Phase.Loading -> {
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                CircularProgressIndicator()
                            }
                        }
                        is DepsUiState.Phase.Error -> {
                            Text("错误: ${(state.systemPhase as DepsUiState.Phase.Error).message}", color = MaterialTheme.colorScheme.error)
                        }
                        is DepsUiState.Phase.Loaded -> {
                            val systemDeps = (state.systemPhase as DepsUiState.Phase.Loaded).deps.map { sp ->
                                DePackage(
                                    name = sp.name,
                                    version = sp.version,
                                    type = "system",
                                    isSystem = true
                                )
                            }.filter { searchQuery.isBlank() || it.name.contains(searchQuery, ignoreCase = true) }
                            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                items(systemDeps, key = { it.name }) { dep ->
                                    DepItemRow(
                                        dep = dep,
                                        operating = state.operationalItems.contains(dep.name),
                                        onReinstall = { },
                                        onUninstall = { vm.uninstallSystemPackage(dep.name) }
                                    )
                                }
                                item { Spacer(Modifier.height(84.dp)) }
                            }
                        }
                    }
                }
            }
        }
    }

    if (terminalOpen) {
        SystemTerminalDialog(onDismiss = { terminalOpen = false })
    }
}

@Composable
fun DepItemRow(
    dep: DePackage,
    operating: Boolean,
    onReinstall: () -> Unit,
    onUninstall: () -> Unit
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
    ) {
        Row(
            modifier = Modifier
                .padding(12.dp)
                .fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(Modifier.weight(1f)) {
                Text(dep.name, style = MaterialTheme.typography.titleMedium)
                Text("v${dep.version}", style = MaterialTheme.typography.bodySmall)
            }
            Row {
                if (operating) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp))
                } else {
                    Button(onClick = onReinstall) { Text("重装") }
                    Spacer(Modifier.width(8.dp))
                    Button(onClick = onUninstall) { Text("卸载") }
                }
            }
        }
    }
}
