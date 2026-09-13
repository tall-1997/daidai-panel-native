package com.daidai.daidai_app.ui.screens.users

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.User
import com.daidai.daidai_app.data.repository.PanelUsersRepository
import com.daidai.daidai_app.data.repository.UsersRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 用户管理列表页（Compose 原生，阶段 4-4）。
 *
 * 展示现有用户的卡片（用户名 / 角色 / 启停状态 / 创建时间），提供
 * 新建、编辑（改角色 / 启停）、删除、重置密码四项操作，并覆盖
 * 加载中 / 空态 / 错误（含重试）三个分支。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配读写仓库。**未接线导航**，
 * 本组件自包含可编译。OpenApi 模块的同类写操作（POST/PUT/DELETE/reset-secret）
 * 不在本页范围内，留待后续阶段集成。
 */
@Composable
fun UserListScreen(
    modifier: Modifier = Modifier,
    repository: UsersRepository? = null,
    viewModel: UsersViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(context) {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelUsersRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        UsersViewModel(resolved)
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }

    // 操作成功 / 失败提示
    LaunchedEffect(state.actionMessage, state.actionError) {
        state.actionMessage?.let { msg ->
            snackbarHostState.showSnackbar(msg)
            screenViewModel.clearMessages()
        }
        state.actionError?.let { msg ->
            snackbarHostState.showSnackbar(msg)
            screenViewModel.clearMessages()
        }
    }

    var showCreateDialog by remember { mutableStateOf(false) }
    var editingUser by remember { mutableStateOf<User?>(null) }
    var resettingUser by remember { mutableStateOf<User?>(null) }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(AppColors.lightPage),
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            // 顶部标题 + 新建入口
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 20.dp, vertical = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "用户管理",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier.weight(1f),
                )
                Button(onClick = { showCreateDialog = true }) {
                    Text("＋ 新建用户")
                }
            }

            when (state.phase) {
                UsersUiState.Phase.Loading -> LoadingContent()
                UsersUiState.Phase.Error -> ErrorContent(
                    message = state.errorMessage ?: "用户列表加载失败",
                    onRetry = screenViewModel::refresh,
                )
                UsersUiState.Phase.Loaded ->
                    if (state.users.isEmpty()) {
                        EmptyContent()
                    } else {
                        UserList(
                            users = state.users,
                            inProgressId = state.userIdInProgress,
                            onEdit = { editingUser = it },
                            onDelete = screenViewModel::deleteUser,
                            onResetPassword = { resettingUser = it },
                        )
                    }
            }
        }

        SnackbarHost(
            hostState = snackbarHostState,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
    }

    if (showCreateDialog) {
        CreateUserDialog(
            onDismiss = { showCreateDialog = false },
            onCreate = { username, password, role ->
                screenViewModel.createUser(username, password, role)
                showCreateDialog = false
            },
        )
    }

    editingUser?.let { user ->
        EditUserDialog(
            user = user,
            onDismiss = { editingUser = null },
            onSave = { role, enabled ->
                screenViewModel.updateUser(user.id, role, enabled, user.username)
                editingUser = null
            },
        )
    }

    resettingUser?.let { user ->
        ResetPasswordDialog(
            user = user,
            onDismiss = { resettingUser = null },
            onConfirm = { newPassword ->
                screenViewModel.resetPassword(user.id, user.username, newPassword)
                resettingUser = null
            },
        )
    }
}

@Composable
private fun LoadingContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        CircularProgressIndicator(color = AppColors.primary)
        Spacer(Modifier.height(12.dp))
        Text(
            text = "正在加载用户列表…",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun ErrorContent(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = "😕",
            style = MaterialTheme.typography.headlineLarge,
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(16.dp))
        OutlinedButton(onClick = onRetry) {
            Text("重试")
        }
    }
}

@Composable
private fun EmptyContent(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = "👤",
            style = MaterialTheme.typography.headlineLarge,
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = "暂无用户，点右上角「新建用户」开始添加",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}

@Composable
private fun UserList(
    users: List<User>,
    inProgressId: Long?,
    onEdit: (User) -> Unit,
    onDelete: (Long, String) -> Unit,
    onResetPassword: (User) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 20.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(users, key = { it.id }) { user ->
            UserCard(
                user = user,
                inProgress = user.id == inProgressId,
                onEdit = { onEdit(user) },
                onDelete = { onDelete(user.id, user.username) },
                onResetPassword = { onResetPassword(user) },
            )
        }
    }
}

@Composable
private fun UserCard(
    user: User,
    inProgress: Boolean,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onResetPassword: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(AppColors.glassCard)
            .border(1.dp, AppColors.glassCardBorder, RoundedCornerShape(16.dp))
            .padding(16.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // 头像占位
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(AppColors.primaryLight),
                contentAlignment = Alignment.Center,
            ) {
                Text(text = "👤", style = MaterialTheme.typography.titleMedium)
            }
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = user.username.ifBlank { "（未命名用户）" },
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Spacer(Modifier.height(2.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = roleLabel(user.role),
                        style = MaterialTheme.typography.labelMedium,
                        color = AppColors.slate500,
                    )
                    Spacer(Modifier.width(8.dp))
                    EnabledBadge(enabled = user.enabled)
                }
            }
        }

        if (user.createdAt.isNotBlank()) {
            Spacer(Modifier.height(8.dp))
            Text(
                text = "创建于 ${user.createdAt}",
                style = MaterialTheme.typography.labelSmall,
                color = AppColors.slate400,
            )
        }

        Spacer(Modifier.height(12.dp))
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedButton(
                onClick = onEdit,
                enabled = !inProgress,
                modifier = Modifier.weight(1f),
            ) {
                Text("编辑")
            }
            OutlinedButton(
                onClick = onResetPassword,
                enabled = !inProgress,
                modifier = Modifier.weight(1f),
            ) {
                Text("重置密码")
            }
            OutlinedButton(
                onClick = onDelete,
                enabled = !inProgress,
                modifier = Modifier.weight(1f),
            ) {
                if (inProgress) {
                    CircularProgressIndicator(
                        color = AppColors.errorColor,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(16.dp),
                    )
                } else {
                    Text("删除", color = AppColors.errorColor)
                }
            }
        }
    }
}

@Composable
private fun EnabledBadge(enabled: Boolean, modifier: Modifier = Modifier) {
    val background = if (enabled) AppColors.primary else AppColors.slate300
    val content = if (enabled) Color.White else AppColors.slate700
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(4.dp))
            .background(background)
            .padding(horizontal = 6.dp, vertical = 2.dp),
    ) {
        Text(
            text = if (enabled) "已启用" else "已禁用",
            style = MaterialTheme.typography.labelSmall,
            color = content,
        )
    }
}

private fun roleLabel(role: String): String = when (role) {
    "admin" -> "管理员"
    "operator" -> "操作员"
    "viewer" -> "观察员"
    else -> role.ifBlank { "viewer" }
}

@Composable
private fun CreateUserDialog(onDismiss: () -> Unit, onCreate: (String, String, String) -> Unit) {
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("operator") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("新建用户") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text("用户名") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text("密码（6-128 位）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("角色", style = MaterialTheme.typography.bodyMedium)
                    RoleChoiceChips(selected = role, onSelect = { role = it })
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = username.isNotBlank() && password.length in 6..128,
                onClick = { onCreate(username.trim(), password.trim(), role) },
            ) {
                Text("创建")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun EditUserDialog(
    user: User,
    onDismiss: () -> Unit,
    onSave: (String, Boolean) -> Unit,
) {
    var role by remember(user.id) { mutableStateOf(user.role) }
    var enabled by remember(user.id) { mutableStateOf(user.enabled) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("编辑用户「${user.username}」") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("角色", style = MaterialTheme.typography.bodyMedium)
                    RoleChoiceChips(selected = role, onSelect = { role = it })
                }
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("状态", style = MaterialTheme.typography.bodyMedium)
                    ChoiceChips(
                        selected = if (enabled) "enabled" else "disabled",
                        options = listOf("enabled" to "启用", "disabled" to "禁用"),
                        onSelect = { enabled = it == "enabled" },
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onSave(role, enabled) }) { Text("保存") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun ResetPasswordDialog(
    user: User,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var newPassword by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("重置密码") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = "为「${user.username}」设置新密码。重置后该用户的所有会话将失效。",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = newPassword,
                    onValueChange = { newPassword = it },
                    label = { Text("新密码（6-128 位）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(
                enabled = newPassword.length in 6..128,
                onClick = { onConfirm(newPassword.trim()) },
            ) {
                Text("确认重置")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
private fun RoleChoiceChips(
    selected: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    ChoiceChips(
        selected = selected,
        options = listOf(
            "admin" to "管理员",
            "operator" to "操作员",
            "viewer" to "观察员",
        ),
        onSelect = onSelect,
        modifier = modifier,
    )
}

@Composable
private fun ChoiceChips(
    selected: String,
    options: List<Pair<String, String>>,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        options.forEach { (value, label) ->
            val isSelected = value == selected
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .background(if (isSelected) AppColors.primary else AppColors.slate100)
                    .clickable { onSelect(value) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            ) {
                Text(
                    text = label,
                    style = MaterialTheme.typography.labelMedium,
                    color = if (isSelected) Color.White else AppColors.slate700,
                )
            }
        }
    }
}
