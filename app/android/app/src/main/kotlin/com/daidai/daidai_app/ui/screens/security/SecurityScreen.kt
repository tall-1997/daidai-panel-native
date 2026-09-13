package com.daidai.daidai_app.ui.screens.security

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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.repository.SecurityRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 阶段 4-1 安全模块（Compose 原生，只读）。
 *
 * 分区展示：概览卡片 + 登录日志 + 在线会话 + 审计日志，顶部用分段标签切换
 * 分区。依赖经参数注入（repository / viewModel）；不传时用
 * [SecurityRepository]（经 AppServices 读取连接信息）装配。**未接线导航**，
 * 本组件自包含可编译。
 */
@Composable
fun SecurityScreen(
    modifier: Modifier = Modifier,
    repository: SecurityRepository? = null,
    viewModel: SecurityViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(context) {
        SecurityViewModel(repository ?: SecurityRepository(context))
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(AppColors.lightPage),
    ) {
        when (state.phase) {
            SecurityUiState.Phase.Loading -> SecurityLoading()
            SecurityUiState.Phase.Error -> ErrorView(
                message = state.errorMessage ?: "安全数据加载失败",
                onRetry = screenViewModel::refresh,
            )
            SecurityUiState.Phase.Loaded -> SecurityLoaded(
                state = state,
                onSelectTab = screenViewModel::selectTab,
            )
        }
    }
}

@Composable
private fun SecurityLoading() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = AppColors.primary)
    }
}

@Composable
private fun SecurityLoaded(
    state: SecurityUiState,
    onSelectTab: (SecurityUiState.SecurityTab) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        SecurityHeader(state, onSelectTab)
        when (state.tab) {
            SecurityUiState.SecurityTab.Overview -> OverviewContent(state.overview)
            SecurityUiState.SecurityTab.LoginLogs -> LoginLogsContent(state.loginLogs)
            SecurityUiState.SecurityTab.Sessions -> SessionsContent(state.sessions)
            SecurityUiState.SecurityTab.AuditLogs -> AuditLogsContent(state.auditLogs)
        }
    }
}

@Composable
private fun SecurityHeader(
    state: SecurityUiState,
    onSelectTab: (SecurityUiState.SecurityTab) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "安全中心",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
                color = Color(0xFF0F172A),
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = "登录与会话安全",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            TabChip(
                modifier = Modifier.weight(1f),
                label = SecurityUiState.SecurityTab.Overview.label,
                selected = state.tab == SecurityUiState.SecurityTab.Overview,
                onClick = { onSelectTab(SecurityUiState.SecurityTab.Overview) },
            )
            TabChip(
                modifier = Modifier.weight(1f),
                label = SecurityUiState.SecurityTab.LoginLogs.label,
                selected = state.tab == SecurityUiState.SecurityTab.LoginLogs,
                onClick = { onSelectTab(SecurityUiState.SecurityTab.LoginLogs) },
            )
            TabChip(
                modifier = Modifier.weight(1f),
                label = SecurityUiState.SecurityTab.Sessions.label,
                selected = state.tab == SecurityUiState.SecurityTab.Sessions,
                onClick = { onSelectTab(SecurityUiState.SecurityTab.Sessions) },
            )
            TabChip(
                modifier = Modifier.weight(1f),
                label = SecurityUiState.SecurityTab.AuditLogs.label,
                selected = state.tab == SecurityUiState.SecurityTab.AuditLogs,
                onClick = { onSelectTab(SecurityUiState.SecurityTab.AuditLogs) },
            )
        }
    }
}

@Composable
private fun TabChip(
    modifier: Modifier = Modifier,
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) AppColors.primary else AppColors.glassCard)
            .clickable(onClick = onClick)
            .padding(vertical = 9.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal,
            color = if (selected) Color.White else AppColors.slate600,
        )
    }
}

@Composable
private fun OverviewContent(
    overview: SecurityOverview,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            OverviewCard(overview)
        }
        item {
            SectionTitle("风险提示")
        }
        if (overview.riskHints.isEmpty()) {
            item {
                GlassCard {
                    SecurityRow(icon = Icons.Filled.CheckCircle, tint = AppColors.primary) {
                        Text(
                            text = "近 ${overview.periodDays} 天未检测到明显风险",
                            style = MaterialTheme.typography.bodyMedium,
                            color = AppColors.slate600,
                        )
                    }
                }
            }
        } else {
            items(overview.riskHints) { hint ->
                GlassCard {
                    SecurityRow(icon = Icons.Filled.Warning, tint = AppColors.warningColor) {
                        Text(
                            text = hint,
                            style = MaterialTheme.typography.bodyMedium,
                            color = AppColors.slate600,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun OverviewCard(overview: SecurityOverview, modifier: Modifier = Modifier) {
    GlassCard(modifier = modifier) {
        Text(
            text = "登录统计（近 ${overview.periodDays} 天）",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = Color(0xFF0F172A),
        )
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            StatCell(modifier = Modifier.weight(1f), label = "总登录", value = overview.totalLogins.toString())
            StatCell(modifier = Modifier.weight(1f), label = "成功", value = overview.successLogins.toString(), tint = AppColors.primary)
            StatCell(modifier = Modifier.weight(1f), label = "失败", value = overview.failedLogins.toString(), tint = AppColors.red500)
            StatCell(modifier = Modifier.weight(1f), label = "在线会话", value = overview.activeSessions.toString())
        }
        Spacer(Modifier.height(12.dp))
        HorizontalDivider(color = AppColors.glassDivider)
        Spacer(Modifier.height(8.dp))
        SecurityRow(icon = Icons.Filled.Lock, tint = AppColors.primary) {
            Text(
                text = "锁定账号：${overview.lockedAccounts} 个",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
            )
        }
    }
}

@Composable
private fun StatCell(
    modifier: Modifier = Modifier,
    label: String,
    value: String,
    tint: Color = AppColors.slate900,
) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(10.dp))
            .background(AppColors.glassBg)
            .padding(vertical = 10.dp, horizontal = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = value,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
            color = tint,
        )
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = AppColors.slate500,
        )
    }
}

@Composable
private fun SectionTitle(title: String, modifier: Modifier = Modifier) {
    Text(
        text = title,
        modifier = modifier.padding(horizontal = 4.dp),
        style = MaterialTheme.typography.titleSmall,
        fontWeight = FontWeight.SemiBold,
        color = AppColors.slate600,
    )
}

// ── 登录日志 ──

@Composable
private fun LoginLogsContent(logs: List<LoginLog>, modifier: Modifier = Modifier) {
    if (logs.isEmpty()) {
        EmptyView(title = "暂无登录日志", description = "没有可显示的登录历史记录")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(logs, key = { it.id }) { log ->
            GlassCard {
                SecurityRow(
                    icon = if (log.success) Icons.Filled.CheckCircle else Icons.Filled.Close,
                    tint = if (log.success) AppColors.primary else AppColors.red500,
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Text(
                            text = log.username.ifBlank { "-" },
                            style = MaterialTheme.typography.bodyMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = Color(0xFF0F172A),
                        )
                        Text(
                            text = if (log.success) "成功" else "失败",
                            style = MaterialTheme.typography.labelSmall,
                            color = if (log.success) AppColors.primary else AppColors.red500,
                        )
                    }
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = listOf(log.ip, log.message)
                            .filter { it.isNotBlank() }
                            .joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                    if (log.createdAt.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = log.createdAt,
                            style = MaterialTheme.typography.labelSmall,
                            color = AppColors.slate400,
                        )
                    }
                }
            }
        }
    }
}

// ── 会话列表 ──

@Composable
private fun SessionsContent(sessions: List<Session>, modifier: Modifier = Modifier) {
    if (sessions.isEmpty()) {
        EmptyView(title = "暂无在线会话", description = "当前没有活跃的登录会话")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(sessions, key = { it.id }) { session ->
            GlassCard {
                SecurityRow(icon = Icons.Filled.Person, tint = AppColors.primary) {
                    Text(
                        text = "${session.username.ifBlank { "-" }} · ${session.ip.ifBlank { "-" }}",
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = Color(0xFF0F172A),
                    )
                    if (session.clientName.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = session.clientName,
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate500,
                        )
                    }
                    if (session.createdAt.isNotBlank() || session.expiresAt.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = "活跃: ${session.createdAt.ifBlank { "-" }} · 过期: ${session.expiresAt.ifBlank { "-" }}",
                            style = MaterialTheme.typography.labelSmall,
                            color = AppColors.slate400,
                        )
                    }
                }
            }
        }
    }
}

// ── 审计日志 ──

@Composable
private fun AuditLogsContent(logs: List<AuditLog>, modifier: Modifier = Modifier) {
    if (logs.isEmpty()) {
        EmptyView(title = "暂无审计日志", description = "没有可显示的安全审计记录")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(logs, key = { it.id }) { audit ->
            GlassCard {
                SecurityRow(icon = Icons.Filled.List, tint = AppColors.primary) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Text(
                            text = audit.action.ifBlank { "安全操作" },
                            style = MaterialTheme.typography.bodyMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = Color(0xFF0F172A),
                        )
                        if (audit.ip.isNotBlank()) {
                            Text(
                                text = audit.ip,
                                style = MaterialTheme.typography.labelSmall,
                                color = AppColors.slate400,
                            )
                        }
                    }
                    if (audit.detail.isNotBlank()) {
                        Spacer(Modifier.height(4.dp))
                        Text(
                            text = audit.detail,
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate500,
                        )
                    }
                    Spacer(Modifier.height(2.dp))
                    Text(
                        text = listOf(audit.username, audit.createdAt)
                            .filter { it.isNotBlank() }
                            .joinToString(" · ").ifBlank { audit.createdAt },
                        style = MaterialTheme.typography.labelSmall,
                        color = AppColors.slate400,
                    )
                }
            }
        }
    }
}

// ── 通用小程序件 ──

/** 玻璃卡片容器：圆角 + 白底 + 内边距，模拟 Flutter AppCard。 */
@Composable
private fun GlassCard(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(AppColors.glassCard)
            .padding(14.dp),
    ) {
        content()
    }
}

/** 图标 + 多行内容 的常规列表行。 */
@Composable
private fun SecurityRow(
    icon: ImageVector,
    tint: Color,
    content: @Composable () -> Unit,
) {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
        androidx.compose.material3.Icon(
            imageVector = icon,
            contentDescription = null,
            tint = tint,
            modifier = Modifier.size(16.dp),
        )
        Spacer(Modifier.width(8.dp))
        Column(modifier = Modifier.weight(1f)) {
            content()
        }
    }
}
