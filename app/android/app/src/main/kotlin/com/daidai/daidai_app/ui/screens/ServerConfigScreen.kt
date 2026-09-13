package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/** 服务器配置页：选择服务来源，配置远程地址并保存。 */
@Composable
fun ServerConfigScreen(
    onContinue: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    repository: ServerConfigRepository? = null,
    viewModel: ServerConfigViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(repository, context) {
        ServerConfigViewModel(repository ?: AppServices.serverConfigRepository(context))
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("配置服务器", style = MaterialTheme.typography.headlineMedium, color = AppColors.primary)
        Text(
            "选择 Daidai 面板的运行方式。你可以连接远程服务，或使用由应用管理的本地服务。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Spacer(Modifier.height(8.dp))
        Text("服务模式", style = MaterialTheme.typography.titleMedium)
        Row(modifier = Modifier.fillMaxWidth()) {
            FilterChip(
                selected = state.mode == ServerMode.Remote,
                onClick = { screenViewModel.selectMode(ServerMode.Remote) },
                label = { Text("Remote") },
            )
            Spacer(Modifier.width(12.dp))
            FilterChip(
                selected = state.mode == ServerMode.ManagedLocal,
                onClick = { screenViewModel.selectMode(ServerMode.ManagedLocal) },
                label = { Text("Managed Local") },
            )
        }

        if (state.mode == ServerMode.Remote) {
            OutlinedTextField(
                value = state.remoteUrl,
                onValueChange = screenViewModel::setRemoteUrl,
                modifier = Modifier.fillMaxWidth(),
                label = { Text("远程服务 URL") },
                placeholder = { Text("https://example.com") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                supportingText = { Text("请输入包含 http:// 或 https:// 的地址") },
            )
        } else {
            Surface(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.surfaceVariant,
                shape = MaterialTheme.shapes.medium,
            ) {
                Text(
                    "应用将管理本地服务，无需填写远程地址。",
                    modifier = Modifier.padding(16.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        state.errorMessage?.let {
            Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        }

        Spacer(Modifier.height(8.dp))
        Button(
            onClick = { screenViewModel.save(onContinue ?: {}) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !state.isSaving,
        ) {
            if (state.isSaving) {
                CircularProgressIndicator(strokeWidth = 2.dp, modifier = Modifier.width(20.dp).height(20.dp))
                Spacer(Modifier.width(8.dp))
            }
            Text(if (state.isSaving) "保存中…" else "保存并继续")
        }
    }
}
