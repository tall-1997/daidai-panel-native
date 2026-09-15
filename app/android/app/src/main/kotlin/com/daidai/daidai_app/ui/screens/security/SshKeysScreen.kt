package com.daidai.daidai_app.ui.screens.security

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.repository.SshKey
import com.daidai.daidai_app.data.repository.SshKeysRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class SshKeysUiState(
    val keys: List<SshKey> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
) {
    enum class Phase { Loading, Loaded, Error }
}

class SshKeysViewModel(
    private val repository: SshKeysRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(SshKeysUiState())
    val uiState: StateFlow<SshKeysUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update { it.copy(phase = SshKeysUiState.Phase.Error, errorMessage = "SSH 密钥后端未装配") }
            return
        }
        _uiState.update { it.copy(phase = SshKeysUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val keys = repo.list()
                _uiState.update { it.copy(keys = keys, phase = SshKeysUiState.Phase.Loaded) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = SshKeysUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "SSH 密钥加载失败，请重试",
                    )
                }
            }
        }
    }
}

/**
 * SSH 密钥管理页：列表 + 新建/编辑（名称 + 私钥）+ 删除确认。
 * 对齐服务端 `GET/POST /api/ssh-keys`、`GET/PUT/DELETE /api/ssh-keys/:id`。
 */
@Composable
fun SshKeysScreen(
    modifier: Modifier = Modifier,
    repository: SshKeysRepository? = null,
    viewModel: SshKeysViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val config = AppServices.configRepository(context).config.value
        SshKeysViewModel(
            SshKeysRepository(baseUrl = config.serverUrl, accessToken = config.accessToken),
        )
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val repo = remember(context) {
        repository ?: run {
            val config = AppServices.configRepository(context).config.value
            SshKeysRepository(baseUrl = config.serverUrl, accessToken = config.accessToken)
        }
    }

    var editing by remember { mutableStateOf<SshKey?>(null) }
    var showCreate by remember { mutableStateOf(false) }
    var pendingDelete by remember { mutableStateOf<SshKey?>(null) }
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }

    fun reload() = scope.launch { screenViewModel.refresh() }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        floatingActionButton = {
            ExtendedFloatingActionButton(onClick = { showCreate = true }) { Text("添加密钥") }
        },
    ) { padding ->
        Column(modifier = Modifier.fillMaxSize().padding(padding)) {
            notice?.let {
                Text(
                    it,
                    color = MaterialTheme.colorScheme.primary,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp),
                )
            }
            when (state.phase) {
                SshKeysUiState.Phase.Loading -> LoadingView()
                SshKeysUiState.Phase.Error -> ErrorView(
                    message = state.errorMessage ?: "SSH 密钥加载失败",
                    onRetry = screenViewModel::refresh,
                )
                SshKeysUiState.Phase.Loaded ->
                    if (state.keys.isEmpty()) {
                        EmptyView(title = "暂无 SSH 密钥", description = "点击右下角添加用于订阅拉取与部署的私钥")
                    } else {
                        LazyColumn(
                            modifier = Modifier.fillMaxSize(),
                            contentPadding = androidx.compose.foundation.layout.PaddingValues(20.dp),
                            verticalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            items(state.keys, key = { it.id }) { key ->
                                SshKeyCard(
                                    key = key,
                                    onEdit = { editing = it },
                                    onDelete = { pendingDelete = it },
                                )
                            }
                        }
                    }
            }
        }
    }

    if (showCreate || editing != null) {
        SshKeyEditDialog(
            initial = editing,
            busy = busy,
            onDismiss = { showCreate = false; editing = null },
            onSave = { name, privateKey ->
                scope.launch {
                    busy = true
                    runCatching {
                        val target = editing
                        if (target == null) repo.create(name, privateKey) else repo.update(target.id, name, privateKey)
                    }.onSuccess {
                        notice = if (editing == null) "密钥已添加" else "密钥已更新"
                        showCreate = false
                        editing = null
                        reload()
                    }.onFailure { failure ->
                        notice = failure.message?.takeIf(String::isNotBlank) ?: "保存失败，请重试"
                    }
                    busy = false
                }
            },
        )
    }

    pendingDelete?.let { target ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除 SSH 密钥") },
            text = { Text("确定删除密钥“${target.name}”？使用它的订阅与部署将无法再认证。") },
            confirmButton = {
                TextButton(onClick = {
                    scope.launch {
                        busy = true
                        runCatching { repo.delete(target.id) }
                            .onSuccess { notice = "密钥已删除"; reload() }
                            .onFailure { notice = it.message ?: "删除失败" }
                        busy = false
                        pendingDelete = null
                    }
                }) { Text("删除", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { pendingDelete = null }) { Text("取消") } },
        )
    }
}

@Composable
private fun SshKeyCard(
    key: SshKey,
    onEdit: (SshKey) -> Unit,
    onDelete: (SshKey) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp)
            .background(MaterialTheme.colorScheme.surface, RoundedCornerShape(16.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(key.name.ifBlank { "密钥 #${key.id}" }, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Text(
                    "添加于 ${key.createdAt.ifBlank { "-" }}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            TextButton(onClick = { onEdit(key) }) { Text("编辑") }
            TextButton(onClick = { onDelete(key) }) { Text("删除", color = MaterialTheme.colorScheme.error) }
        }
        Text(
            key.privateKeyMasked.ifBlank { "********" },
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
        )
    }
}

@Composable
private fun SshKeyEditDialog(
    initial: SshKey?,
    busy: Boolean,
    onDismiss: () -> Unit,
    onSave: (name: String, privateKey: String) -> Unit,
) {
    var name by remember { mutableStateOf(initial?.name ?: "") }
    var privateKey by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (initial == null) "添加 SSH 密钥" else "编辑 SSH 密钥") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("名称") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = privateKey,
                    onValueChange = { privateKey = it },
                    label = { Text(if (initial == null) "私钥（PEM）" else "私钥（留空保持不变）") },
                    modifier = Modifier.fillMaxWidth(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    visualTransformation = PasswordVisualTransformation(),
                    minLines = 3,
                )
            }
        },
        confirmButton = {
            Button(
                onClick = { onSave(name.trim(), privateKey) },
                enabled = !busy && name.isNotBlank() && (initial != null || privateKey.isNotBlank()),
            ) { Text("保存") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}
