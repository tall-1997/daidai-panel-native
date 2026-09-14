package com.daidai.daidai_app.ui.screens.openapi

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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.foundation.clickable
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.data.repository.OpenApiRepository
import com.daidai.daidai_app.data.repository.PanelOpenApiRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * Open API 应用只读列表页（Compose 原生，阶段 2-4）。
 *
 * 展示已创建应用的卡片：名称 / 密钥前缀 / 权限范围 / 启用状态 / 限流上限；
 * 并覆盖 加载中 / 空态 / 错误（含重试）三个分支。本页只读，不提供增删改。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配只读仓库。**未接线导航**，
 * 本组件自包含可编译。
 */
@Composable
fun OpenApiListScreen(
    modifier: Modifier = Modifier,
    repository: OpenApiRepository? = null,
    viewModel: OpenApiViewModel? = null,
    onCreate: () -> Unit = {},
    onOpenDetail: (OpenApiApp) -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelOpenApiRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        OpenApiViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 创建/编辑在独立返回栈条目完成，回到本页时刷新一次。
    LaunchedEffect(Unit) { screenViewModel.refresh() }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        when (state.phase) {
            OpenApiUiState.Phase.Loading -> LoadingContent()
            OpenApiUiState.Phase.Error -> ErrorContent(
                message = state.errorMessage ?: "Open API 应用加载失败",
                onRetry = screenViewModel::refresh,
            )
            OpenApiUiState.Phase.Loaded ->
                if (state.apps.isEmpty()) {
                    EmptyContent()
                } else {
                    AppList(apps = state.apps, onOpenDetail = onOpenDetail)
                }
        }
        androidx.compose.material3.ExtendedFloatingActionButton(
            onClick = onCreate,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(20.dp),
        ) {
            Text("新建应用")
        }
    }
}

@Composable
private fun AppList(apps: List<OpenApiApp>, onOpenDetail: (OpenApiApp) -> Unit, modifier: Modifier = Modifier) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(apps, key = { it.id }) { app ->
            OpenApiAppCard(app, onClick = { onOpenDetail(app) })
        }
    }
}

@Composable
private fun OpenApiAppCard(app: OpenApiApp, onClick: () -> Unit = {}, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .background(MaterialTheme.colorScheme.surface)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = app.name.ifBlank { "未命名应用" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f, fill = false),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.width(8.dp))
            EnabledBadge(enabled = app.enabled)
        }

        Text(
            text = app.appKey.ifBlank { "无密钥" }.let { "App Key: $it" },
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )

        if (app.scopes.isNotEmpty()) {
            Text(
                text = "权限范围：${app.scopes.joinToString(" / ")}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = "限流上限：${app.rateLimit}/分钟",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (app.createdAt.isNotBlank()) {
                Text(
                    text = "创建于 ${app.createdAt}",
                    style = MaterialTheme.typography.labelSmall,
                    color = AppColors.slate400,
                )
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
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(
            text = if (enabled) "启用" else "禁用",
            style = MaterialTheme.typography.labelSmall,
            color = content,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
private fun LoadingContent(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            CircularProgressIndicator(color = AppColors.primary)
            Spacer(Modifier.height(12.dp))
            Text(
                text = "正在加载 Open API 应用…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun ErrorContent(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize().padding(32.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = "加载失败",
                style = MaterialTheme.typography.titleMedium,
                color = AppColors.errorColor,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                text = message,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            Spacer(Modifier.height(16.dp))
            Button(onClick = onRetry) {
                Text("重试")
            }
        }
    }
}

@Composable
private fun EmptyContent(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize().padding(32.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = "还没有 Open API 应用",
                style = MaterialTheme.typography.titleMedium,
                color = AppColors.slate600,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                text = "创建应用后可在此查看其密钥、作用域与启用状态",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
        }
    }
}