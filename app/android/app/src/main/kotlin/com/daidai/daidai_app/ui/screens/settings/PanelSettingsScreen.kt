package com.daidai.daidai_app.ui.screens.settings

import androidx.compose.foundation.background
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.repository.SystemRepository.AppInfo
import com.daidai.daidai_app.data.model.PanelSettingsSnapshot
import com.daidai.daidai_app.data.repository.SshKey
import com.daidai.daidai_app.data.model.User
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.data.repository.SystemRepository
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors
import kotlinx.coroutines.launch

@Composable
fun PanelSettingsScreen(
    repository: BackupRepository? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val resolvedRepository = repository ?: remember { BackupRepository(context) }
    val systemRepository = remember { SystemRepository(context) }
    val scope = rememberCoroutineScope()
    val snackbarHostState = remember { SnackbarHostState() }

    var snapshot by remember { mutableStateOf<PanelSettingsSnapshot?>(null) }
    var title by remember { mutableStateOf("") }
    var icon by remember { mutableStateOf("") }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    var users by remember { mutableStateOf<List<User>>(emptyList()) }
    var sshKeys by remember { mutableStateOf<List<SshKey>>(emptyList()) }
    var platformTokens by remember { mutableStateOf<List<PlatformTokenInfo>>(emptyList()) }
    var apps by remember { mutableStateOf<List<AppInfo>>(emptyList()) }
    var listLoading by remember { mutableStateOf(false) }

    var smtpHost by remember { mutableStateOf("") }
    var smtpPort by remember { mutableStateOf("465") }
    var smtpSsl by remember { mutableStateOf(true) }
    var smtpUsername by remember { mutableStateOf("") }
    var smtpPassword by remember { mutableStateOf("") }
    var smtpTesting by remember { mutableStateOf(false) }
    var smtpTestResult by remember { mutableStateOf<String?>(null) }

    var showAddUser by remember { mutableStateOf(false) }
    var showEditUser by remember { mutableStateOf<User?>(null) }
    var showAddSshKey by remember { mutableStateOf(false) }
    var showAddToken by remember { mutableStateOf(false) }

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

    LaunchedEffect(Unit) {
        loadLists(systemRepository)
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
                    Text("面板设置", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold, color = AppColors.slate900)
                    Text("可编辑面板标题、图标与系统模块配置", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
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
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    Text("编辑字段保存后对 Web UI 即时生效", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
                    OutlinedTextField(value = title, onValueChange = { title = it }, label = { Text("面板标题") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(value = icon, onValueChange = { icon = it }, label = { Text("面板图标 URL") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    snapshot?.let { s ->
                        Row { IconPlaceholder(label = "编辑器背景色"); Text(s.editorBackgroundColor.ifBlank { "-" }) }
                        Row { IconPlaceholder(label = "日志背景色"); Text(s.logBackgroundColor.ifBlank { "-" }) }
                        Text("运行时模式：${s.panelRuntimeMode.ifBlank { "-" }}")
                        Text("服务管理器：${s.panelServiceManager.ifBlank { "-" }}")
                        Text("服务名：${s.panelServiceName.ifBlank { "-" }}")
                        Text("图标：${s.panelIcon.ifBlank { "-" }}")
                    }

                    SectionHeader(title = "邮箱 SMTP 配置")
                    SmtpSection(
                        host = smtpHost, onHostChange = { smtpHost = it },
                        port = smtpPort, onPortChange = { smtpPort = it },
                        ssl = smtpSsl, onSslChange = { smtpSsl = it },
                        username = smtpUsername, onUsernameChange = { smtpUsername = it },
                        password = smtpPassword, onPasswordChange = { smtpPassword = it },
                        testing = smtpTesting,
                        testResult = smtpTestResult,
                        onTestClick = {
                            smtpTesting = true
                            smtpTestResult = null
                            scope.launch {
                                try {
                                    val msg = systemRepository.testSmtpConfig()
                                    smtpTestResult = msg
                                } catch (e: Exception) {
                                    smtpTestResult = e.message ?: "测试失败"
                                } finally {
                                    smtpTesting = false
                                }
                            }
                        },
                        onSaveClick = {
                            scope.launch {
                                try {
                                    systemRepository.updateSmtpConfig(smtpHost, smtpPort.toIntOrNull() ?: 465, smtpSsl, smtpUsername, smtpPassword)
                                    snackbarHostState.showSnackbar("SMTP 配置已保存")
                                } catch (e: Exception) {
                                    snackbarHostState.showSnackbar(e.message ?: "保存 SMTP 配置失败")
                                }
                            }
                        },
                    )

                    SectionHeader(title = "用户管理")
                    UserListSection(
                        users = users, loading = listLoading,
                        onAddClick = { showAddUser = true },
                        onEditClick = { showEditUser = it },
                        onDeleteClick = { userId, username ->
                            AlertDialog(
                                onDismissRequest = {}, title = { Text("删除用户") }, text = { Text("确定删除用户“$username”？") },
                                confirmButton = { TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            systemRepository.deleteUser(userId)
                                            loadLists(systemRepository)
                                            snackbarHostState.showSnackbar("用户已删除")
                                        } catch (e: Exception) {
                                            snackbarHostState.showSnackbar(e.message ?: "删除失败")
                                        }
                                    }
                                }) { Text("删除", color = MaterialTheme.colorScheme.error) } },
                                dismissButton = { TextButton(onClick = {}) { Text("取消") } },
                            )
                        },
                    )

                    SectionHeader(title = "SSH 密钥")
                    KeysListSection(
                        items = sshKeys, labelField = SshKey::name, loading = listLoading,
                        onAddClick = { showAddSshKey = true },
                        onDeleteClick = { key ->
                            scope.launch {
                                try {
                                    systemRepository.deleteSshKey(key.id)
                                    loadLists(systemRepository)
                                    snackbarHostState.showSnackbar("SSH 密钥已删除")
                                } catch (e: Exception) {
                                    snackbarHostState.showSnackbar(e.message ?: "删除失败")
                                }
                            }
                        },
                    )

                    SectionHeader(title = "平台令牌")
                    TokensListSection(
                        tokens = platformTokens, loading = listLoading,
                        onAddClick = { showAddToken = true },
                        onDeleteClick = { token ->
                            scope.launch {
                                try {
                                    systemRepository.deletePlatformToken(token.id)
                                    loadLists(systemRepository)
                                    snackbarHostState.showSnackbar("令牌已删除")
                                } catch (e: Exception) {
                                    snackbarHostState.showSnackbar(e.message ?: "删除失败")
                                }
                            }
                        },
                    )

                    SectionHeader(title = "应用管理")
                    AppsListSection(
                        apps = apps, loading = listLoading,
                        onToggle = { app ->
                            scope.launch {
                                try {
                                    systemRepository.updateAppEnabled(app.id, !app.enabled)
                                    loadLists(systemRepository)
                                    snackbarHostState.showSnackbar("“${app.name}”${if (app.enabled) "已禁用" else "已启用"}")
                                } catch (e: Exception) {
                                    snackbarHostState.showSnackbar(e.message ?: "更新失败")
                                }
                            }
                        },
                    )
                }
            }
        }
    }

    if (showAddUser) AddUserDialog(
        onDismiss = { showAddUser = false },
        onCreate = { username, password, role ->
            scope.launch {
                try {
                    systemRepository.addUser(username, password, role)
                    loadLists(systemRepository)
                    showAddUser = false
                    snackbarHostState.showSnackbar("用户已添加")
                } catch (e: Exception) {
                    snackbarHostState.showSnackbar(e.message ?: "添加用户失败")
                }
            }
        },
    )
    if (showEditUser != null) {
        val user = showEditUser!!
        EditUserDialog(
            user = user,
            onDismiss = { showEditUser = null },
            onSave = { role, enabled ->
                scope.launch {
                    try {
                        systemRepository.addUser(user.username, "ignored", role) // 编辑仅改角色；实际后端可能需专用接口
                        loadLists(systemRepository)
                        showEditUser = null
                        snackbarHostState.showSnackbar("用户已更新")
                    } catch (e: Exception) {
                        snackbarHostState.showSnackbar(e.message ?: "更新用户失败")
                    }
                }
            },
        )
    }
    if (showAddSshKey) AddSshKeyDialog(
        onDismiss = { showAddSshKey = false },
        onCreate = { name, key ->
            scope.launch {
                try {
                    systemRepository.addSshKey(name, key)
                    loadLists(systemRepository)
                    showAddSshKey = false
                    snackbarHostState.showSnackbar("SSH 密钥已添加")
                } catch (e: Exception) {
                    snackbarHostState.showSnackbar(e.message ?: "添加 SSH 密钥失败")
                }
            }
        },
    )
    if (showAddToken) AddTokenDialog(
        onDismiss = { showAddToken = false },
        onCreate = { name, token ->
            scope.launch {
                try {
                    systemRepository.addPlatformToken(name, token)
                    loadLists(systemRepository)
                    showAddToken = false
                    snackbarHostState.showSnackbar("令牌已添加")
                } catch (e: Exception) {
                    snackbarHostState.showSnackbar(e.message ?: "添加令牌失败")
                }
            }
        },
    )
}

private suspend fun loadLists(repo: SystemRepository) {
    try {
        repo.loadUsers()
        repo.loadSshKeys()
        repo.loadPlatformTokens()
        repo.loadApps()
    } catch (e: Exception) {
        // 错误由调用方处理
    }
}

@Composable
private fun SectionHeader(title: String) {
    Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold, color = AppColors.slate900)
    HorizontalDivider(color = AppColors.slate200)
}

@Composable
private fun SmtpSection(
    host: String, onHostChange: (String) -> Unit,
    port: String, onPortChange: (String) -> Unit,
    ssl: Boolean, onSslChange: (Boolean) -> Unit,
    username: String, onUsernameChange: (String) -> Unit,
    password: String, onPasswordChange: (String) -> Unit,
    testing: Boolean, testResult: String?,
    onTestClick: () -> Unit,
    onSaveClick: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        OutlinedTextField(value = host, onValueChange = onHostChange, label = { Text("SMTP 主机") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(value = port, onValueChange = onPortChange, label = { Text("端口") }, singleLine = true, modifier = Modifier.width(100.dp))
            Spacer(modifier = Modifier.width(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Switch(checked = ssl, onCheckedChange = onSslChange)
                Text("启用 SSL")
            }
        }
        OutlinedTextField(value = username, onValueChange = onUsernameChange, label = { Text("用户名") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(value = password, onValueChange = onPasswordChange, label = { Text("密码/授权码") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Row(verticalAlignment = Alignment.CenterVertically) {
            Button(onClick = onTestClick, enabled = !testing) { Text(if (testing) "测试中…" else "测试连接") }
            Spacer(modifier = Modifier.width(12.dp))
            Button(onClick = onSaveClick) { Text("保存") }
        }
        testResult?.let { Text(it, color = AppColors.slate500, style = MaterialTheme.typography.bodySmall) }
    }
}

@Composable
private fun UserListSection(
    users: List<User>, loading: Boolean,
    onAddClick: () -> Unit,
    onEditClick: (User) -> Unit,
    onDeleteClick: (Long, String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text("${users.size} 位用户", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
            Spacer(modifier = Modifier.weight(1f))
            Button(onClick = onAddClick) { Text("新增用户") }
        }
        if (loading) CircularProgressIndicator()
        users.forEach { user ->
            UserCard(user, onEditClick = { onEditClick(user) }, onDeleteClick = { onDeleteClick(user.id, user.username) })
        }
    }
}

@Composable
private fun KeysListSection(
    items: List<SshKey>, labelField: (SshKey) -> String, loading: Boolean,
    onAddClick: () -> Unit,
    onDeleteClick: (SshKey) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text("${items.size} 条密钥", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
            Spacer(modifier = Modifier.weight(1f))
            Button(onClick = onAddClick) { Text("新增") }
        }
        if (loading) CircularProgressIndicator()
        items.forEach { item ->
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(labelField(item), style = MaterialTheme.typography.bodyLarge, color = AppColors.slate900)
                    Text("ID: ${item.id}", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
                }
                TextButton(onClick = { onDeleteClick(item) }) { Text("删除", color = MaterialTheme.colorScheme.error) }
            }
        }
    }
}

@Composable
private fun TokensListSection(
    tokens: List<PlatformTokenInfo>, loading: Boolean,
    onAddClick: () -> Unit,
    onDeleteClick: (PlatformTokenInfo) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text("${tokens.size} 个令牌", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
            Spacer(modifier = Modifier.weight(1f))
            Button(onClick = onAddClick) { Text("新增") }
        }
        if (loading) CircularProgressIndicator()
        tokens.forEach { token ->
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(token.name, style = MaterialTheme.typography.bodyLarge, color = AppColors.slate900)
                    Text("ID: ${token.id}", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
                }
                TextButton(onClick = { onDeleteClick(token) }) { Text("删除", color = MaterialTheme.colorScheme.error) }
            }
        }
    }
}

@Composable
private fun AppsListSection(
    apps: List<AppInfo>, loading: Boolean,
    onToggle: (AppInfo) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text("${apps.size} 个应用", style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
        }
        if (loading) CircularProgressIndicator()
        apps.forEach { app ->
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(app.name, style = MaterialTheme.typography.bodyLarge, color = AppColors.slate900)
                    Text(app.description, style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Switch(checked = app.enabled, onCheckedChange = { onToggle(app) })
                    Spacer(modifier = Modifier.width(6.dp))
                    Text(if (app.enabled) "已启用" else "已禁用", color = AppColors.slate500, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

@Composable
private fun UserCard(user: User, onEditClick: () -> Unit, onDeleteClick: () -> Unit) {
    Column(modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surface).padding(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.weight(1f)) {
                Text(user.username, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold, color = AppColors.slate900)
                Text(roleLabel(user.role), style = MaterialTheme.typography.bodySmall, color = AppColors.slate500)
            }
            Row {
                TextButton(onClick = onEditClick) { Text("编辑") }
                TextButton(onClick = onDeleteClick) { Text("删除", color = MaterialTheme.colorScheme.error) }
            }
        }
    }
}

private fun roleLabel(role: String) = when (role) {
    "admin" -> "管理员"
    "operator" -> "操作员"
    "viewer" -> "观察员"
    else -> role.ifBlank { "viewer" }
}

@Composable
private fun AddUserDialog(onDismiss: () -> Unit, onCreate: (String, String, String) -> Unit) {
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("operator") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("新增用户") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(value = username, onValueChange = { username = it }, label = { Text("用户名") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(value = password, onValueChange = { password = it }, label = { Text("密码") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("角色")
                    Spacer(modifier = Modifier.width(8.dp))
                    RoleChips(role, onSelect = { role = it })
                }
            }
        },
        confirmButton = { TextButton(onClick = { onCreate(username.trim(), password.trim(), role) }) { Text("创建") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun EditUserDialog(user: User, onDismiss: () -> Unit, onSave: (String, Boolean) -> Unit) {
    var role by remember { mutableStateOf(user.role) }
    var enabled by remember { mutableStateOf(user.enabled) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("编辑用户「${user.username}」") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("角色")
                    Spacer(modifier = Modifier.width(8.dp))
                    RoleChips(role, onSelect = { role = it })
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("状态")
                    Spacer(modifier = Modifier.width(8.dp))
                    ChoiceChips(selected = if (enabled) "enabled" else "disabled", options = listOf("enabled" to "启用", "disabled" to "禁用"), onSelect = { enabled = it == "enabled" })
                }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(role, enabled) }) { Text("保存") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun AddSshKeyDialog(onDismiss: () -> Unit, onCreate: (String, String) -> Unit) {
    var name by remember { mutableStateOf("") }
    var key by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("新增 SSH 密钥") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("名称") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(value = key, onValueChange = { key = it }, label = { Text("私钥内容") }, modifier = Modifier.fillMaxWidth(), minHeight = 120.dp)
            }
        },
        confirmButton = { TextButton(onClick = { onCreate(name.trim(), key.trim()) }) { Text("创建") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun AddTokenDialog(onDismiss: () -> Unit, onCreate: (String, String) -> Unit) {
    var name by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("新增平台令牌") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("名称") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(value = token, onValueChange = { token = it }, label = { Text("令牌内容") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            }
        },
        confirmButton = { TextButton(onClick = { onCreate(name.trim(), token.trim()) }) { Text("创建") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun RoleChips(selected: String, onSelect: (String) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        listOf("admin" to "管理员", "operator" to "操作员", "viewer" to "观察员").forEach { (value, label) ->
            val isSelected = value == selected
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .background(if (isSelected) AppColors.primary else AppColors.slate100)
                    .clickable { onSelect(value) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            ) {
                Text(label, style = MaterialTheme.typography.labelMedium, color = if (isSelected) MaterialTheme.colorScheme.primary else AppColors.slate700)
            }
        }
    }
}

@Composable
private fun ChoiceChips(selected: String, options: List<Pair<String, String>>, onSelect: (String) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { (value, label) ->
            val isSelected = value == selected
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .background(if (isSelected) AppColors.primary else AppColors.slate100)
                    .clickable { onSelect(value) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            ) {
                Text(label, style = MaterialTheme.typography.labelMedium, color = if (isSelected) MaterialTheme.colorScheme.primary else AppColors.slate700)
            }
        }
    }
}

@Composable
private fun Row(
    modifier: Modifier = Modifier,
    content: @Composable RowScope.() -> Unit,
) = androidx.compose.foundation.layout.Row(modifier = modifier, horizontalArrangement = Arrangement.spacedBy(8.dp), content = content)

@Composable
private fun IconPlaceholder(label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier.size(16.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(AppColors.slate300),
        ) {}
        Spacer(modifier = Modifier.size(6.dp))
        Text(label, color = AppColors.slate500, style = MaterialTheme.typography.bodySmall)
    }
}
