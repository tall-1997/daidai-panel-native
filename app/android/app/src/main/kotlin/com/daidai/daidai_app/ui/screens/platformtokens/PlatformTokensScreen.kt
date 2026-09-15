package com.daidai.daidai_app.ui.screens.platformtokens

import androidx.compose.foundation.background
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.model.PLATFORM_TOKEN_MASK
import com.daidai.daidai_app.data.model.PlatformInfo
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * A1 平台/令牌管理页（对齐 Web 端 PlatformToken 管理）。
 *
 * 令牌内容全程掩码（[PLATFORM_TOKEN_MASK]）显示，编辑时保持掩码提交即视为
 * 「不修改令牌」；明文只在创建表单出现一次。删除平台/删除令牌为危险操作，
 * 均走确认对话框；服务端错误（如本地端未实现平台删除返回 404）原样展示。
 *
 * **未接导航**，自包含可编译。
 */
@Composable
fun PlatformTokensScreen(
    viewModel: PlatformTokensViewModel? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel
        ?: androidx.lifecycle.viewmodel.compose.viewModel(
            initializer = { PlatformTokensViewModel(context) },
        )

    val state by screenViewModel.uiState.collectAsState()
    var selectedTab by remember { mutableStateOf(0) }
    var tokenForm by remember { mutableStateOf<PlatformTokenFormState?>(null) }
    var platformFormVisible by remember { mutableStateOf(false) }
    var pendingPlatformDelete by remember { mutableStateOf<PlatformInfo?>(null) }
    var pendingTokenDelete by remember { mutableStateOf<PlatformTokenInfo?>(null) }
    val snackbarHostState = remember { SnackbarHostState() }

    LaunchedEffect(state.actionMessage) {
        state.actionMessage?.let {
            snackbarHostState.showSnackbar(it)
            screenViewModel.consumeActionMessage()
        }
    }
    LaunchedEffect(state.errorMessage) {
        state.errorMessage?.let { snackbarHostState.showSnackbar(it) }
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MaterialTheme.colorScheme.background,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            Column {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 20.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Column(verticalArrangement = Arrangement.Center) {
                        Text(
                            text = "平台令牌",
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.Bold,
                            color = AppColors.slate900,
                        )
                        Text(
                            text = "第三方平台访问令牌，内容密封存储仅回显掩码",
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate500,
                        )
                    }
                    IconButton(
                        onClick = screenViewModel::refresh,
                        enabled = state.phase != PlatformTokensUiState.Phase.Loading,
                    ) {
                        if (state.phase == PlatformTokensUiState.Phase.Loading) {
                            CircularProgressIndicator(modifier = Modifier.size(20.dp))
                        } else {
                            Icon(Icons.Filled.Refresh, contentDescription = "刷新", tint = AppColors.primaryDark)
                        }
                    }
                }
                TabRow(selectedTabIndex = selectedTab) {
                    Tab(selected = selectedTab == 0, onClick = { selectedTab = 0 }, text = { Text("令牌") })
                    Tab(selected = selectedTab == 1, onClick = { selectedTab = 1 }, text = { Text("平台管理") })
                }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
            when {
                state.phase == PlatformTokensUiState.Phase.Loading -> LoadingView()
                state.phase == PlatformTokensUiState.Phase.Error -> ErrorView(
                    message = state.errorMessage ?: "加载平台令牌失败",
                    onRetry = screenViewModel::refresh,
                )
                selectedTab == 0 -> TokensTab(
                    state = state,
                    onAddToken = { tokenForm = PlatformTokenFormState.createNew() },
                    onEditToken = { tokenForm = PlatformTokenFormState.fromExisting(it) },
                    onDeleteToken = { pendingTokenDelete = it },
                    onToggle = screenViewModel::setTokenEnabled,
                    onFilter = screenViewModel::setFilter,
                )
                else -> PlatformsTab(
                    state = state,
                    onAddPlatform = { platformFormVisible = true },
                    onDeletePlatform = { pendingPlatformDelete = it },
                )
            }
        }
    }

    // 令牌创建 / 编辑表单
    tokenForm?.let { form ->
        TokenFormDialog(
            form = form,
            platforms = state.platforms,
            busy = state.busyAction != null,
            onDismiss = { tokenForm = null },
            onSubmit = { payload ->
                if (payload.isNew) {
                    screenViewModel.createToken(
                        platformId = payload.platformId,
                        name = payload.name,
                        token = payload.token,
                        service = payload.service,
                        serviceUser = payload.serviceUser,
                        remarks = payload.remarks,
                    )
                } else {
                    screenViewModel.updateToken(
                        id = payload.existingId ?: return@TokenFormDialog,
                        name = payload.name,
                        token = payload.token.takeIf { it.isNotBlank() && it != PLATFORM_TOKEN_MASK },
                        service = payload.service,
                        serviceUser = payload.serviceUser,
                        remarks = payload.remarks,
                    )
                }
                tokenForm = null
            },
        )
    }

    // 平台创建表单
    if (platformFormVisible) {
        PlatformFormDialog(
            busy = state.busyAction != null,
            onDismiss = { platformFormVisible = false },
            onSubmit = { name, label, icon ->
                screenViewModel.createPlatform(name, label, icon)
                platformFormVisible = false
            },
        )
    }

    // 危险操作确认：删除平台（级联删除令牌）
    pendingPlatformDelete?.let { platform ->
        AlertDialog(
            onDismissRequest = { pendingPlatformDelete = null },
            title = { Text("删除平台") },
            text = {
                Text("删除平台「${platform.displayName}」会同时删除其下全部令牌，该操作不可恢复。确定删除吗？")
            },
            confirmButton = {
                Button(onClick = {
                    screenViewModel.deletePlatform(platform)
                    pendingPlatformDelete = null
                }) { Text("删除平台") }
            },
            dismissButton = {
                Button(onClick = { pendingPlatformDelete = null }) { Text("取消") }
            },
        )
    }

    // 危险操作确认：删除令牌
    pendingTokenDelete?.let { token ->
        AlertDialog(
            onDismissRequest = { pendingTokenDelete = null },
            title = { Text("删除令牌") },
            text = { Text("确定删除令牌「${token.name}」吗？使用该令牌的凭据引用将失效。") },
            confirmButton = {
                Button(onClick = {
                    screenViewModel.deleteToken(token)
                    pendingTokenDelete = null
                }) { Text("删除") }
            },
            dismissButton = {
                Button(onClick = { pendingTokenDelete = null }) { Text("取消") }
            },
        )
    }
}

/** 令牌表单的暂存状态。 */
data class PlatformTokenFormState(
    val isNew: Boolean,
    val existingId: Long? = null,
    val platformId: Long = 0,
    val name: String = "",
    val token: String = "",
    val service: String = "",
    val serviceUser: String = "",
    val remarks: String = "",
) {
    companion object {
        fun createNew(): PlatformTokenFormState = PlatformTokenFormState(isNew = true)
        fun fromExisting(token: PlatformTokenInfo): PlatformTokenFormState = PlatformTokenFormState(
            isNew = false,
            existingId = token.id,
            platformId = token.platformId,
            name = token.name,
            token = PLATFORM_TOKEN_MASK,
            service = token.service,
            serviceUser = token.serviceUser,
            remarks = token.remarks,
        )
    }
}

@Composable
private fun TokensTab(
    state: PlatformTokensUiState,
    onAddToken: () -> Unit,
    onEditToken: (PlatformTokenInfo) -> Unit,
    onDeleteToken: (PlatformTokenInfo) -> Unit,
    onToggle: (PlatformTokenInfo, Boolean) -> Unit,
    onFilter: (Long?) -> Unit,
) {
    Column(modifier = Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScrollish()
                .padding(horizontal = 16.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            FilterChip(
                selected = state.filterPlatformId == null,
                onClick = { onFilter(null) },
                label = { Text("全部平台") },
            )
            state.platforms.forEach { platform ->
                FilterChip(
                    selected = state.filterPlatformId == platform.id,
                    onClick = { onFilter(platform.id) },
                    label = { Text(platform.displayName) },
                )
            }
        }
        if (state.tokens.isEmpty()) {
            EmptyView(
                title = "暂无令牌",
                description = "点击右下角按钮创建第一个平台令牌",
            )
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 88.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(state.tokens, key = { it.id }) { token ->
                    TokenRow(
                        token = token,
                        busy = state.busyAction == "token-toggle-${token.id}",
                        onEdit = { onEditToken(token) },
                        onDelete = { onDeleteToken(token) },
                        onToggle = { enabled -> onToggle(token, enabled) },
                    )
                }
            }
        }
        Button(
            onClick = onAddToken,
            enabled = state.platforms.isNotEmpty(),
            modifier = Modifier
                .align(Alignment.End)
                .padding(16.dp),
        ) {
            Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(modifier = Modifier.width(6.dp))
            Text(if (state.platforms.isEmpty()) "请先创建平台" else "新建令牌")
        }
    }
}

@Composable
private fun TokenRow(
    token: PlatformTokenInfo,
    busy: Boolean,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onToggle: (Boolean) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surface)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = token.name,
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
                color = AppColors.slate900,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = buildString {
                    append(token.platformDisplayName.ifBlank { "未知平台" })
                    append(" · ")
                    append(token.displayToken)
                    if (token.sealed) append(" · 已密封")
                },
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
            )
            if (token.service.isNotBlank() || token.serviceUser.isNotBlank() || token.remarks.isNotBlank()) {
                Text(
                    text = listOf(token.service, token.serviceUser, token.remarks)
                        .filter(String::isNotBlank)
                        .joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate400,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (token.expiresAt.isNotBlank()) {
                Text(
                    text = "过期：${token.expiresAt}",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate400,
                )
            }
        }
        if (busy) {
            CircularProgressIndicator(modifier = Modifier.size(18.dp))
        } else {
            Switch(checked = token.enabled, onCheckedChange = onToggle)
        }
        IconButton(onClick = onEdit) {
            Icon(Icons.Filled.Edit, contentDescription = "编辑", tint = AppColors.primaryDark)
        }
        IconButton(onClick = onDelete) {
            Icon(Icons.Filled.Delete, contentDescription = "删除", tint = AppColors.errorColor)
        }
    }
}

@Composable
private fun PlatformsTab(
    state: PlatformTokensUiState,
    onAddPlatform: () -> Unit,
    onDeletePlatform: (PlatformInfo) -> Unit,
) {
    Column(modifier = Modifier.fillMaxSize()) {
        if (state.platforms.isEmpty()) {
            EmptyView(
                title = "暂无平台",
                description = "先创建平台（如 github / gitee / nexus），再为其添加令牌",
            )
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(state.platforms, key = { it.id }) { platform ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(MaterialTheme.colorScheme.surface)
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = platform.displayName,
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.SemiBold,
                                color = AppColors.slate900,
                            )
                            if (platform.name != platform.displayName) {
                                Text(
                                    text = "标识：${platform.name}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = AppColors.slate500,
                                )
                            }
                        }
                        IconButton(
                            onClick = { onDeletePlatform(platform) },
                            enabled = state.busyAction != "platform-delete-${platform.id}",
                        ) {
                            if (state.busyAction == "platform-delete-${platform.id}") {
                                CircularProgressIndicator(modifier = Modifier.size(18.dp))
                            } else {
                                Icon(
                                    Icons.Filled.Delete,
                                    contentDescription = "删除",
                                    tint = AppColors.errorColor,
                                )
                            }
                        }
                    }
                }
            }
        }
        Button(
            onClick = onAddPlatform,
            modifier = Modifier
                .align(Alignment.End)
                .padding(16.dp),
        ) {
            Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(modifier = Modifier.width(6.dp))
            Text("新建平台")
        }
    }
}

@Composable
private fun TokenFormDialog(
    form: PlatformTokenFormState,
    platforms: List<PlatformInfo>,
    busy: Boolean,
    onDismiss: () -> Unit,
    onSubmit: (PlatformTokenFormState) -> Unit,
) {
    var platformId by remember(form) { mutableStateOf(form.platformId.takeIf { it > 0 } ?: platforms.firstOrNull()?.id ?: 0L) }
    var name by remember(form) { mutableStateOf(form.name) }
    var token by remember(form) { mutableStateOf(form.token) }
    var service by remember(form) { mutableStateOf(form.service) }
    var serviceUser by remember(form) { mutableStateOf(form.serviceUser) }
    var remarks by remember(form) { mutableStateOf(form.remarks) }
    var platformMenuVisible by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (form.isNew) "新建令牌" else "编辑令牌") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Box {
                    OutlinedTextField(
                        value = platforms.firstOrNull { it.id == platformId }?.displayName.orEmpty(),
                        onValueChange = { },
                        label = { Text("所属平台") },
                        readOnly = true,
                        trailingIcon = {
                            IconButton(onClick = { platformMenuVisible = true }) {
                                Icon(Icons.Filled.Add, contentDescription = "选择平台")
                            }
                        },
                        modifier = Modifier.fillMaxWidth(),
                    )
                    DropdownMenu(
                        expanded = platformMenuVisible,
                        onDismissRequest = { platformMenuVisible = false },
                    ) {
                        platforms.forEach { platform ->
                            DropdownMenuItem(
                                text = { Text(platform.displayName) },
                                onClick = {
                                    platformId = platform.id
                                    platformMenuVisible = false
                                },
                            )
                        }
                    }
                }
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("名称 *") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = token,
                    onValueChange = { token = it },
                    label = { Text(if (form.isNew) "令牌内容 *" else "令牌内容（保持掩码则不修改）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = service,
                    onValueChange = { service = it },
                    label = { Text("服务地址（可选）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = serviceUser,
                    onValueChange = { serviceUser = it },
                    label = { Text("服务用户名（可选）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = remarks,
                    onValueChange = { remarks = it },
                    label = { Text("备注（可选）") },
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            Button(
                onClick = {
                    onSubmit(
                        form.copy(
                            platformId = platformId,
                            name = name,
                            token = token,
                            service = service,
                            serviceUser = serviceUser,
                            remarks = remarks,
                        ),
                    )
                },
                enabled = !busy && name.isNotBlank() && platformId > 0 &&
                    (form.isNew == token.isNotBlank()),
            ) { Text("保存") }
        },
        dismissButton = {
            Button(onClick = onDismiss) { Text("取消") }
        },
    )
}

@Composable
private fun PlatformFormDialog(
    busy: Boolean,
    onDismiss: () -> Unit,
    onSubmit: (name: String, label: String, icon: String) -> Unit,
) {
    var name by remember { mutableStateOf("") }
    var label by remember { mutableStateOf("") }
    var icon by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("新建平台") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("平台标识 *（如 github）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = label,
                    onValueChange = { label = it },
                    label = { Text("显示名称（留空用标识）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = icon,
                    onValueChange = { icon = it },
                    label = { Text("图标（可选）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            Button(
                onClick = { onSubmit(name, label, icon) },
                enabled = !busy && name.isNotBlank(),
            ) { Text("创建") }
        },
        dismissButton = {
            Button(onClick = onDismiss) { Text("取消") }
        },
    )
}

private fun Modifier.horizontalScrollish(): Modifier = this
