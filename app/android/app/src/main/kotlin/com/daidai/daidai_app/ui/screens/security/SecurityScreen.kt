package com.daidai.daidai_app.ui.screens.security

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.IpWhitelistEntry
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.model.SessionPolicy
import com.daidai.daidai_app.data.model.TwoFactorStatus
import com.daidai.daidai_app.data.repository.SecurityRepository
import com.daidai.daidai_app.ui.components.EmptyView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 阶段 4-1 安全模块（Compose 原生）。
 *
 * 分区展示：概览卡片 + 登录日志 + 在线会话（可撤销）+ 审计日志 + 双重验证
 * （2FA 状态与配置引导）+ IP 白名单（增删）+ 会话策略（网页端/APP 端最大
 * 会话数），顶部用分段标签切换分区。依赖经参数注入（repository / viewModel）；
 * 不传时用 [SecurityRepository]（经 AppServices 读取连接信息）装配。
 *
 * 崩溃修复：所有分区数据经 [SecurityViewModel] 容错后到达本组件；HTTP 400/
 * 非 2xx 只显示分区错误横幅（[SectionErrorBanner]），不再导致主线程 FATAL。
 * **未接线导航**，本组件自包含可编译。
 */
@Composable
fun SecurityScreen(
    modifier: Modifier = Modifier,
    repository: SecurityRepository? = null,
    viewModel: SecurityViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
SecurityViewModel(repository ?: SecurityRepository(context))
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
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
                onRetry = screenViewModel::refresh,
                onClearAction = screenViewModel::clearActionMessage,
                onRevokeSession = screenViewModel::revokeSession,
                onRevokeOthers = screenViewModel::revokeOtherSessions,
                onAddIpWhitelist = screenViewModel::addIpWhitelist,
                onRemoveIpWhitelist = screenViewModel::removeIpWhitelist,
                onSavePolicy = screenViewModel::saveSessionPolicy,
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
    onRetry: () -> Unit,
    onClearAction: () -> Unit,
    onRevokeSession: (Long) -> Unit,
    onRevokeOthers: () -> Unit,
    onAddIpWhitelist: (String, String) -> Unit,
    onRemoveIpWhitelist: (Long) -> Unit,
    onSavePolicy: (Int, Int) -> Unit,
    onEnable2fa: () -> Unit,
    onDisable2fa: (String) -> Unit,
    onVerify2fa: (String) -> Unit,
    onForceLogout: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SecurityHeader(state, onSelectTab)
        if (state.actionMessage != null) {
            ActionBanner(message = state.actionMessage, onDismiss = onClearAction)
        }
        when (state.tab) {
            SecurityUiState.SecurityTab.Overview -> OverviewContent(state.overview, state.overviewError, onRetry)
            SecurityUiState.SecurityTab.LoginLogs -> LoginLogsContent(state.loginLogs, state.loginLogsError, onRetry)
            SecurityUiState.SecurityTab.Sessions -> SessionsContent(
                sessions = state.sessions,
                error = state.sessionsError,
                busy = state.actionBusy || state.forceLogoutLoading,
                onRetry = onRetry,
                onRevokeSession = onRevokeSession,
                onRevokeOthers = onRevokeOthers,
                onForceLogout = onForceLogout,
                forceLogoutLoading = state.forceLogoutLoading,
            )
            SecurityUiState.SecurityTab.AuditLogs -> AuditLogsContent(state.auditLogs, state.auditLogsError, onRetry)
            SecurityUiState.SecurityTab.TwoFactor -> TwoFactorContent(
                status = state.twoFactor,
                setup = state.twoFactorSetup,
                setupError = state.twoFactorSetupError,
                error = state.twoFactorError,
                onRetry = onRetry,
                onEnable2fa = onEnable2fa,
                onDisable2fa = onDisable2fa,
                onVerify2fa = onVerify2fa,
            )
            SecurityUiState.SecurityTab.IpWhitelist -> IpWhitelistContent(
                entries = state.ipWhitelist,
                error = state.ipWhitelistError,
                busy = state.actionBusy,
                onRetry = onRetry,
                onAdd = onAddIpWhitelist,
                onRemove = onRemoveIpWhitelist,
            )
            SecurityUiState.SecurityTab.SessionPolicy -> SessionPolicyContent(
                policy = state.sessionPolicy,
                error = state.sessionPolicyError,
                busy = state.actionBusy,
                onRetry = onRetry,
                onSave = onSavePolicy,
            )
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
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            SecurityUiState.SecurityTab.entries.forEach { tab ->
                TabChip(
                    label = tab.label,
                    selected = state.tab == tab,
                    onClick = { onSelectTab(tab) },
                )
            }
        }
    }
}

@Composable
private fun TabChip(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) AppColors.primary else MaterialTheme.colorScheme.surface)
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 9.dp),
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

/** 分区级错误横幅：某分区请求失败（如 HTTP 400）时局部展示，不崩溃。 */
@Composable
private fun SectionErrorBanner(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(AppColors.red50)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        androidx.compose.material3.Icon(
            imageVector = Icons.Filled.Warning,
            contentDescription = null,
            tint = AppColors.red500,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = message,
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.red600,
        )
        TextButton(
            onClick = onRetry,
            contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 8.dp),
        ) {
            Text("重试", style = MaterialTheme.typography.labelSmall, color = AppColors.red600)
        }
    }
}

/** 写操作成功提示横幅。 */
@Composable
private fun ActionBanner(
    message: String,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(AppColors.primaryLight)
            .clickable(onClick = onDismiss)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        androidx.compose.material3.Icon(
            imageVector = Icons.Filled.CheckCircle,
            contentDescription = null,
            tint = AppColors.primaryDark,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = message,
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.bodySmall,
            color = AppColors.primaryDark,
        )
        androidx.compose.material3.Icon(
            imageVector = Icons.Filled.Close,
            contentDescription = "关闭",
            tint = AppColors.slate400,
            modifier = Modifier.size(16.dp),
        )
    }
}

@Composable
private fun OverviewContent(
    overview: SecurityOverview,
    error: String?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error != null) {
            item { SectionErrorBanner(message = error, onRetry = onRetry) }
        }
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
    GlassCard(modifier = modifier.fillMaxWidth()) {
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                StatCell(
                    modifier = Modifier.weight(1f),
                    label = "总登录",
                    value = overview.totalLogins.toString(),
                )
                StatCell(
                    modifier = Modifier.weight(1f),
                    label = "成功",
                    value = overview.successLogins.toString(),
                    tint = AppColors.primaryDark,
                )
                StatCell(
                    modifier = Modifier.weight(1f),
                    label = "失败",
                    value = overview.failedLogins.toString(),
                    tint = if (overview.failedLogins > 0) AppColors.red500 else AppColors.slate900,
                )
            }
            HorizontalDivider(color = AppColors.glassDivider)
            SecurityRow(icon = Icons.Filled.Lock, tint = AppColors.primary) {
                Text(
                    text = "活跃会话：${overview.activeSessions} 个",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate600,
                )
            }
            SecurityRow(icon = Icons.Filled.Lock, tint = AppColors.primary) {
                Text(
                    text = "锁定账号：${overview.lockedAccounts} 个",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate600,
                )
            }
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
private fun LoginLogsContent(
    logs: List<LoginLog>,
    error: String?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (logs.isEmpty() && error == null) {
        EmptyView(title = "暂无登录日志", description = "没有可显示的登录历史记录")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (error != null) {
            item { SectionErrorBanner(message = error, onRetry = onRetry) }
        }
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
                    if (log.ip.isNotBlank() || log.clientName.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = listOf(log.ip, log.clientName, log.method)
                                .filter { it.isNotBlank() }
                                .joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall,
                            color = AppColors.slate500,
                        )
                    }
                    if (log.message.isNotBlank()) {
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = log.message,
                            style = MaterialTheme.typography.labelSmall,
                            color = AppColors.slate400,
                        )
                    }
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
private fun SessionsContent(
    sessions: List<Session>,
    error: String?,
    busy: Boolean,
    onRetry: () -> Unit,
    onRevokeSession: (Long) -> Unit,
    onRevokeOthers: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (sessions.isEmpty() && error == null) {
        EmptyView(title = "暂无在线会话", description = "当前没有活跃的登录会话")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (error != null) {
            item { SectionErrorBanner(message = error, onRetry = onRetry) }
        }
        item {
            OutlinedButton(
                onClick = onRevokeOthers,
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("撤销其他会话")
            }
        }
        items(sessions, key = { it.id }) { session ->
            GlassCard {
                SecurityRow(icon = Icons.Filled.Person, tint = AppColors.primary) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(
                                    text = "${session.username.ifBlank { "-" }} · ${session.ip.ifBlank { "-" }}",
                                    style = MaterialTheme.typography.bodyMedium,
                                    fontWeight = FontWeight.SemiBold,
                                    color = Color(0xFF0F172A),
                                )
                                if (session.current) {
                                    Spacer(Modifier.width(6.dp))
                                    Text(
                                        text = "当前",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = AppColors.primaryDark,
                                    )
                                }
                            }
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
                        if (!session.current) {
                            Spacer(Modifier.width(8.dp))
                            OutlinedButton(
                                onClick = { onRevokeSession(session.id) },
                                enabled = !busy,
                                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                            ) {
                                Text("撤销", style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                }
            }
        }
    }
}

// ── 审计日志 ──

@Composable
private fun AuditLogsContent(
    logs: List<AuditLog>,
    error: String?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (logs.isEmpty() && error == null) {
        EmptyView(title = "暂无审计日志", description = "没有可显示的安全审计记录")
        return
    }
    LazyColumn(
        modifier = modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (error != null) {
            item { SectionErrorBanner(message = error, onRetry = onRetry) }
        }
        items(logs, key = { it.id }) { audit ->
            GlassCard {
                SecurityRow(icon = Icons.Filled.Lock, tint = AppColors.primary) {
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

// ── 双重验证（2FA / TOTP） ──

@Composable
private fun TwoFactorContent(
    status: TwoFactorStatus,
    error: String?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error != null) {
            SectionErrorBanner(message = error, onRetry = onRetry)
        }
        GlassCard {
            SecurityRow(
                icon = Icons.Filled.CheckCircle,
                tint = if (status.enabled) AppColors.primary else AppColors.slate400,
            ) {
                Text(
                    text = if (status.enabled) "双重验证已启用" else "双重验证待启用",
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = if (status.enabled) AppColors.primaryDark else AppColors.slate600,
                )
            }
        }
        GlassCard {
            SectionTitle("配置引导")
            Spacer(Modifier.height(8.dp))
            Text(
                text = when {
                    !status.supported -> status.reason.ifBlank {
                        "当前后端（Kotlin 本地 fallback）尚未实现真实 TOTP，不会报告为已启用。连接 Go 后端 v3.2.7 后即可在此启用两步验证。"
                    }
                    status.enabled -> "已启用：每次登录需额外输入验证器（如 Google Authenticator）中的 6 位动态验证码。"
                    else -> "未启用：可依次调用 POST /api/security/2fa/setup（获取 secret/URI）→ POST /api/security/2fa/verify（提交验证码）启用；禁用走 DELETE /api/security/2fa。"
                },
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
            )
        }
        GlassCard {
            SectionTitle("端点对齐（Go 后端 v3.2.7）")
            Spacer(Modifier.height(8.dp))
            Text(
                text = "GET /api/security/2fa/status · POST /api/security/2fa/setup\nPOST /api/security/2fa/verify · DELETE /api/security/2fa",
                style = MaterialTheme.typography.labelSmall,
                color = AppColors.slate400,
            )
        }
    }
}

// ── IP 白名单 ──

@Composable
private fun IpWhitelistContent(
    entries: List<IpWhitelistEntry>,
    error: String?,
    busy: Boolean,
    onRetry: () -> Unit,
    onAdd: (String, String) -> Unit,
    onRemove: (Long) -> Unit,
    modifier: Modifier = Modifier,
) {
    var ip by remember { mutableStateOf("") }
    var remarks by remember { mutableStateOf("") }
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error != null) {
            SectionErrorBanner(message = error, onRetry = onRetry)
        }
        GlassCard {
            SectionTitle("添加白名单")
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = ip,
                onValueChange = { ip = it },
                label = { Text("IP 或网段") },
                placeholder = { Text("例如 192.168.1.0/24") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = remarks,
                onValueChange = { remarks = it },
                label = { Text("备注（可选）") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            Button(
                onClick = {
                    if (ip.isNotBlank()) {
                        onAdd(ip, remarks)
                        ip = ""
                        remarks = ""
                    }
                },
                enabled = !busy && ip.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            ) {
                androidx.compose.material3.Icon(
                    imageVector = Icons.Filled.Add,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                )
                Spacer(Modifier.width(6.dp))
                Text("添加")
            }
        }
        if (entries.isEmpty()) {
            EmptyView(title = "暂无白名单", description = "添加 IP 后只有白名单内的地址可访问管理端")
        } else {
            entries.forEach { entry ->
                GlassCard {
                    SecurityRow(icon = Icons.Filled.Lock, tint = AppColors.primary) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text(
                                        text = entry.ip.ifBlank { "-" },
                                        style = MaterialTheme.typography.bodyMedium,
                                        fontWeight = FontWeight.SemiBold,
                                        color = Color(0xFF0F172A),
                                    )
                                    if (!entry.enabled) {
                                        Spacer(Modifier.width(6.dp))
                                        Text(
                                            text = "停用",
                                            style = MaterialTheme.typography.labelSmall,
                                            color = AppColors.slate400,
                                        )
                                    }
                                }
                                if (entry.remarks.isNotBlank()) {
                                    Spacer(Modifier.height(2.dp))
                                    Text(
                                        text = entry.remarks,
                                        style = MaterialTheme.typography.bodySmall,
                                        color = AppColors.slate500,
                                    )
                                }
                                if (entry.createdAt.isNotBlank()) {
                                    Spacer(Modifier.height(2.dp))
                                    Text(
                                        text = entry.createdAt,
                                        style = MaterialTheme.typography.labelSmall,
                                        color = AppColors.slate400,
                                    )
                                }
                            }
                            Spacer(Modifier.width(8.dp))
                            OutlinedButton(
                                onClick = { onRemove(entry.id) },
                                enabled = !busy,
                                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                            ) {
                                androidx.compose.material3.Icon(
                                    imageVector = Icons.Filled.Delete,
                                    contentDescription = null,
                                    modifier = Modifier.size(14.dp),
                                )
                                Spacer(Modifier.width(4.dp))
                                Text("删除", style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                }
            }
        }
    }
}

// ── 会话策略 ──

@Composable
private fun SessionPolicyContent(
    policy: SessionPolicy,
    error: String?,
    busy: Boolean,
    onRetry: () -> Unit,
    onSave: (Int, Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    var webText by remember { mutableStateOf(policy.maxWebSessions?.toString() ?: "") }
    var appText by remember { mutableStateOf(policy.maxAppSessions?.toString() ?: "") }
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error != null) {
            SectionErrorBanner(message = error, onRetry = onRetry)
        }
        GlassCard {
            SectionTitle("会话数上限")
            Spacer(Modifier.height(8.dp))
            Text(
                text = "同一用户同时在线的最大会话数；超出后最旧的会话会被撤销。范围为 1–20。",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = webText,
                onValueChange = { webText = it.filter(Char::isDigit).take(2) },
                label = { Text("网页端会话上限") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = appText,
                onValueChange = { appText = it.filter(Char::isDigit).take(2) },
                label = { Text("APP 端会话上限") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            Button(
                onClick = {
                    val web = webText.toIntOrNull() ?: return@Button
                    val app = appText.toIntOrNull() ?: return@Button
                    onSave(web, app)
                },
                enabled = !busy && webText.toIntOrNull() != null && appText.toIntOrNull() != null,
                modifier = Modifier.fillMaxWidth(),
            ) {
                androidx.compose.material3.Icon(
                    imageVector = Icons.Filled.Settings,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                )
                Spacer(Modifier.width(6.dp))
                Text("保存")
            }
        }
        GlassCard {
            SectionTitle("说明")
            Spacer(Modifier.height(8.dp))
            Text(
                text = "配置键：max_web_sessions / max_app_sessions（Go 后端 v3.2.7 在登录时强制；本地 Kotlin fallback 记录配置，联网 Go 后端后生效）。",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate600,
            )
        }
    }
}

// ── 通用小程序件 ──

/** 玻璃卡片容器。 */
@Composable
private fun GlassCard(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .background(AppColors.glassCard)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        content()
    }
}

@Composable
private fun SecurityRow(
    icon: ImageVector,
    tint: Color,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.Top,
    ) {
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
