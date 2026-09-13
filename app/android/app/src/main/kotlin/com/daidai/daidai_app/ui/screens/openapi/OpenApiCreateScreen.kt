package com.daidai.daidai_app.ui.screens.openapi

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.CreateAppPayload
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/** 权限范围候选项（与 Flutter open_api_page.dart 的 scopes 选项对齐）。 */
private val SCOPE_OPTIONS = listOf("read", "write", "admin")

/**
 * 创建成功后由父级（列表 / 详情）展示的凭据载体。secret 只在创建时返回一次。
 */
data class OpenApiAppWithSecret(
    val appKey: String = "",
    val secret: String = "",
)

/**
 * Open API 应用创建表单（Compose 原生，补齐-2）。
 *
 * 填入 名称 / 权限范围（多选）/ 限流上限 后调 [OpenApiWriteViewModel.create] 创建。
 * 成功后经 [onCreated] 把一次性密钥交回父级展示——本组件不接导航，自包含可编译。
 * 未提供 viewModel 时用连接配置装配自含写客户端。
 */
@Composable
fun OpenApiCreateScreen(
    modifier: Modifier = Modifier,
    viewModel: OpenApiWriteViewModel? = null,
    onBack: () -> Unit = {},
    onCreated: (OpenApiAppWithSecret) -> Unit = {},
) {
    val context = LocalContext.current
    val resolvedVm = viewModel ?: remember(context) {
        val config = AppServices.configRepository(context).config.value
        OpenApiWriteViewModel(
            baseUrl = config.serverUrl,
            accessToken = config.accessToken,
            localToken = config.localToken,
        )
    }
    val state by resolvedVm.uiState.collectAsStateWithLifecycle()

    var name by remember { mutableStateOf("") }
    var rateLimit by remember { mutableStateOf("") }
    var selectedScopes by remember { mutableStateOf(setOf<String>()) }
    var scopesError by remember { mutableStateOf(false) }

    // 创建成功副作用：一次性交回密钥并复位写状态。
    val created = state.createdApp
    val secret = state.resetSecret
    LaunchedEffect(created) {
        if (created != null && secret != null && state.mutationSucceeded) {
            onCreated(OpenApiAppWithSecret(appKey = created.appKey, secret = secret.secret))
            resolvedVm.consumeSecret()
            resolvedVm.consumeMutation()
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(AppColors.lightPage),
    ) {
        CreateScreenTopBar(onBack = onBack)
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            OutlinedTextField(
                value = name,
                onValueChange = {
                    name = it
                    scopesError = false
                },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("应用名称") },
                placeholder = { Text("例如：我的 Open API 应用") },
                supportingText = { Text("用于在列表中识别的应用名称") },
            )

            Text(
                text = "权限范围",
                style = MaterialTheme.typography.labelLarge,
                color = AppColors.slate700,
            )
            if (scopesError) {
                Text(
                    text = "请至少选择一项权限范围",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.errorColor,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                SCOPE_OPTIONS.forEach { scope ->
                    ScopeChip(
                        label = scope,
                        selected = scope in selectedScopes,
                        onToggle = {
                            selectedScopes = if (scope in selectedScopes) {
                                selectedScopes - scope
                            } else {
                                selectedScopes + scope
                            }
                            scopesError = false
                        },
                    )
                }
            }

            OutlinedTextField(
                value = rateLimit,
                onValueChange = { rateLimit = it.filter { ch -> ch.isDigit() } },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("限流上限（次/分钟）") },
                placeholder = { Text("默认 100") },
                supportingText = { Text("留空则按 100 处理") },
            )

            state.errorMessage?.let { msg ->
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(8.dp))
                        .background(AppColors.errorColor.copy(alpha = 0.1f))
                        .padding(12.dp),
                ) {
                    Text(
                        text = msg,
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.errorColor,
                    )
                }
            }

            Button(
                onClick = {
                    val trimmedName = name.trim()
                    if (trimmedName.isEmpty()) return@Button
                    if (selectedScopes.isEmpty()) {
                        scopesError = true
                        return@Button
                    }
                    resolvedVm.create(
                        CreateAppPayload(
                            name = trimmedName,
                            scopes = selectedScopes.toList(),
                            rateLimit = rateLimit.toIntOrNull()?.takeIf { it > 0 } ?: 100,
                        ),
                    )
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp),
                enabled = !state.busy,
            ) {
                if (state.busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.width(20.dp).height(20.dp),
                        color = Color.White,
                        strokeWidth = 2.dp,
                    )
                    Spacer(Modifier.width(8.dp))
                    Text("创建中…")
                } else {
                    Text("创建应用")
                }
            }
        }
    }
}

@Composable
private fun CreateScreenTopBar(onBack: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "‹",
            style = MaterialTheme.typography.headlineMedium,
            color = AppColors.primary,
            modifier = Modifier.clickable(onClick = onBack),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = "创建 Open API 应用",
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

@Composable
private fun ScopeChip(label: String, selected: Boolean, onToggle: () -> Unit) {
    val background = if (selected) AppColors.primary else AppColors.glassCard
    val content = if (selected) Color.White else AppColors.slate700
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(20.dp))
            .background(background)
            .clickable(onClick = onToggle)
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelLarge,
            fontWeight = FontWeight.SemiBold,
            color = content,
        )
    }
}
