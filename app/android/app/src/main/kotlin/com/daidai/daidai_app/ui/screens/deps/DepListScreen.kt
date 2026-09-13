package com.daidai.daidai_app.ui.screens.deps

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
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
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
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.repository.DepsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 依赖列表页（Compose 原生，阶段 3-4）。
 *
 * 展示依赖条目卡片（类型 / 名称 / 版本 / 状态），覆盖加载中 / 空态 / 错误重试，
 * 每行提供卸载与重装操作，并提供“安装依赖”入口。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配 [DepsRepository]。
 * **未接线导航**，本组件自包含可编译，集成阶段再接入 AppNavHost。
 *
 * @param onNavigateToInstall 安装入口点击回调；未接线导航时默认为空实现。
 */
@Composable
fun DepListScreen(
    modifier: Modifier = Modifier,
    repository: DepsRepository? = null,
    viewModel: DepsViewModel? = null,
    onNavigateToInstall: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(repository, context) {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            DepsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        DepsViewModel(resolved)
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(AppColors.lightPage),
    ) {
        when {
            state.phase == DepsUiState.Phase.Loading -> LoadingView()
            state.phase == DepsUiState.Phase.Error -> ErrorView(
                message = state.errorMessage ?: "加载依赖失败，请重试",
                onRetry = screenViewModel::refresh,
            )
            state.deps.isEmpty() -> EmptyView(
                title = "暂无依赖",
                description = "点击底部按钮安装 pip / npm 依赖",
            )
            else -> DepList(
                state = state,
                onUninstall = screenViewModel::uninstall,
                onReinstall = screenViewModel::reinstall,
            )
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

@Composable
private fun DepList(
    state: DepsUiState,
    onUninstall: (Long) -> Unit,
    onReinstall: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 20.dp, top = 20.dp, end = 20.dp, bottom = 96.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(state.deps, key = { it.id }) { dep ->
            DepCard(
                dep = dep,
                operating = state.operatingId == dep.id,
                onUninstall = { onUninstall(dep.id) },
                onReinstall = { onReinstall(dep.id) },
            )
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
            .background(AppColors.glassCard)
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
        } else {
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                ActionPill(
                    text = "重装",
                    container = AppColors.red50,
                    content = AppColors.successColor,
                    onClick = onReinstall,
                )
                ActionPill(
                    text = "卸载",
                    container = AppColors.red50,
                    content = AppColors.errorColor,
                    onClick = onUninstall,
                )
            }
        }
    }
}

@Composable
private fun ActionPill(
    text: String,
    container: Color,
    content: Color,
    onClick: () -> Unit,
) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelMedium,
        color = content,
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(container)
            .padding(horizontal = 10.dp, vertical = 5.dp)
            .clickable(onClick = onClick),
    )
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
