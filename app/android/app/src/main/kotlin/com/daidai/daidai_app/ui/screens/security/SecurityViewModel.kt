package com.daidai.daidai_app.ui.screens.security

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.IpWhitelistEntry
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.model.SessionPolicy
import com.daidai.daidai_app.data.model.TwoFactorStatus
import com.daidai.daidai_app.data.model.TwoFactorSetupResult
import com.daidai.daidai_app.data.model.TwoFactorVerifyResult
import com.daidai.daidai_app.data.model.LoginLogFilter
import com.daidai.daidai_app.data.model.SecurityLoadResult
import com.daidai.daidai_app.data.repository.SecurityDataSource
import kotlinx.coroutines.async
import kotlinx.coroutines.supervisorScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 安全模块 UI 状态。
 *
 * [SecurityTab] 用于分区展示（概览 / 登录日志 / 会话 / 审计日志 / 双重验证 /
 * IP 白名单 / 会话策略）；概览与列表在一次刷新中并行拉取。
 *
 * 容错约定（崩溃修复）：repository 承诺 HTTP 失败不抛异常，而是返回
 * [com.daidai.daidai_app.data.model.SecurityLoadResult]；本状态为每个分区
 * 保留独立 error 字段，单分区失败不拖垮整页（历史缺陷：HTTP 400 冒泡导致
 * 主线程 FATAL）。
 */

data class SecurityUiState(
    val tab: SecurityTab = SecurityTab.Overview,
    val overview: SecurityOverview = SecurityOverview(),
    val overviewError: String? = null,
    val loginLogs: List<LoginLog> = emptyList(),
    val loginLogsError: String? = null,
    val sessions: List<Session> = emptyList(),
    val sessionsError: String? = null,
    val auditLogs: List<AuditLog> = emptyList(),
    val auditLogsError: String? = null,
    val twoFactor: TwoFactorStatus = TwoFactorStatus(),
    val twoFactorSetup: TwoFactorSetupResult = TwoFactorSetupResult(),
    val twoFactorSetupError: String? = null,
    val twoFactorCode: String = "",
    val twoFactorVerificationResult: TwoFactorVerifyResult? = null,
    val twoFactorError: String? = null,
    val ipWhitelist: List<IpWhitelistEntry> = emptyList(),
    val ipWhitelistError: String? = null,
    val sessionPolicy: SessionPolicy = SessionPolicy(),
    val sessionPolicyError: String? = null,
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    /** 上一次写操作（撤销 / 白名单 / 保存策略）的结果提示；一次性展示。 */
    val actionMessage: String? = null,
    /** 写操作进行中，用于禁用按钮避免重复提交。 */
    val actionBusy: Boolean = false,
    val forceLogoutLoading: Boolean = false,
) {
    enum class Phase { Loading, Loaded, Error }

    enum class SecurityTab(val label: String) {
        Overview("概览"),
        LoginLogs("登录日志"),
        Sessions("会话"),
        AuditLogs("审计日志"),
        TwoFactor("双重验证"),
        IpWhitelist("IP白名单"),
        SessionPolicy("会话策略"),
    }
}

/**
 * 安全模块 ViewModel：通过 [SecurityDataSource] 拉取概览与各列表并执行
 * 写操作（撤销会话 / 白名单 / 会话策略）。repository 缺省为 null 时有意报
 * 「安全后端未装配」，绝不伪造空数据（与 LoginViewModel / LogsViewModel /
 * OpenApiViewModel 约定一致）。
 */
class SecurityViewModel(
    private val repository: SecurityDataSource? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(SecurityUiState())
    val uiState: StateFlow<SecurityUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    /** 切换展示分区（本地状态，不触发网络）。 */
    fun selectTab(tab: SecurityUiState.SecurityTab) {
        _uiState.update { it.copy(tab = tab) }
    }

    /** 清空一次性操作提示。 */
    fun clearActionMessage() {
        _uiState.update { it.copy(actionMessage = null) }
    }

    /** 重新拉取概览与全部列表。 */
    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = SecurityUiState.Phase.Error, errorMessage = "安全后端未装配")
            }
            return
        }
        _uiState.update { it.copy(phase = SecurityUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            // supervisorScope：单个 async 子协程失败不会取消父协程；
            // 异常保留在 Deferred 中，由下方 await() 重新抛出并被 catch 兜底。
            supervisorScope {
            try {
                // 各分区并行拉取；repository 已容错，await 不会因 HTTP 错误崩溃。
                // 防御性兜底：若 repository 实现漏容错（历史缺陷：HTTP 400 冒泡），
                // 在此捕获并转为 Error 态，保证任何异常都不逃逸到主线程。
                val overview = async { repo.getSecurityOverview() }
                val logs = async { repo.getLoginLogs() }
                val sessions = async { repo.getSessions() }
                val audits = async { repo.getAuditLogs() }
                val twoFactor = async { repo.getTwoFactorStatus() }
                val whitelist = async { repo.getIpWhitelist() }
                val policy = async { repo.getSessionPolicy() }
                val rOverview = overview.await()
                val rLogs = logs.await()
                val rSessions = sessions.await()
                val rAudits = audits.await()
                val rTwoFactor = twoFactor.await()
                val rWhitelist = whitelist.await()
                val rPolicy = policy.await()
                _uiState.update {
                    it.copy(
                        overview = rOverview.data,
                        overviewError = rOverview.error,
                        loginLogs = rLogs.data,
                        loginLogsError = rLogs.error,
                        sessions = rSessions.data,
                        sessionsError = rSessions.error,
                        auditLogs = rAudits.data,
                        auditLogsError = rAudits.error,
                        twoFactor = rTwoFactor.data,
                        twoFactorError = rTwoFactor.error,
                        ipWhitelist = rWhitelist.data,
                        ipWhitelistError = rWhitelist.error,
                        sessionPolicy = rPolicy.data,
                        sessionPolicyError = rPolicy.error,
                        phase = SecurityUiState.Phase.Loaded,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = SecurityUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "安全数据加载失败，请重试",
                    )
                }
            }
            }
        }
    }

    /** 撤销指定会话；成功后从列表移除。 */
    fun revokeSession(id: Long) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.revokeSession(id)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message?.takeIf(String::isNotBlank) ?: "撤销会话失败")
            }
            _uiState.update {
                val sessions = if (result.isSuccess) it.sessions.filterNot { s -> s.id == id } else it.sessions
                it.copy(
                    sessions = sessions,
                    sessionsError = if (result.isSuccess) it.sessionsError else result.error,
                    actionMessage = if (result.isSuccess) "会话已撤销" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 撤销当前用户的其他会话。 */
    fun revokeOtherSessions() {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.revokeOtherSessions()
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message?.takeIf(String::isNotBlank) ?: "撤销其他会话失败")
            }
            val fresh = if (result.isSuccess) {
                try {
                    repo.getSessions()
                } catch (error: kotlinx.coroutines.CancellationException) {
                    throw error
                } catch (error: Exception) {
                    SecurityLoadResult(emptyList(), error.message?.takeIf(String::isNotBlank) ?: "刷新会话列表失败")
                }
            } else null
            _uiState.update {
                it.copy(
                    sessions = fresh?.data ?: it.sessions,
                    sessionsError = if (result.isSuccess) fresh?.error ?: it.sessionsError else result.error,
                    actionMessage = if (result.isSuccess) "已撤销其他会话" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 新增 IP 白名单条目；成功后刷新白名单列表。 */
    fun addIpWhitelist(ip: String, remarks: String) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.addIpWhitelist(ip, remarks)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult<IpWhitelistEntry?>(null, error.message?.takeIf(String::isNotBlank) ?: "添加白名单失败")
            }
            val fresh = if (result.isSuccess) {
                try {
                    repo.getIpWhitelist()
                } catch (error: kotlinx.coroutines.CancellationException) {
                    throw error
                } catch (error: Exception) {
                    SecurityLoadResult(emptyList(), error.message?.takeIf(String::isNotBlank) ?: "刷新白名单失败")
                }
            } else null
            _uiState.update {
                it.copy(
                    ipWhitelist = fresh?.data ?: it.ipWhitelist,
                    ipWhitelistError = if (result.isSuccess) fresh?.error ?: it.ipWhitelistError else result.error,
                    actionMessage = if (result.isSuccess) "白名单已添加" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 删除 IP 白名单条目；成功后刷新白名单列表。 */
    fun removeIpWhitelist(id: Long) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.removeIpWhitelist(id)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message?.takeIf(String::isNotBlank) ?: "删除白名单失败")
            }
            val fresh = if (result.isSuccess) {
                try {
                    repo.getIpWhitelist()
                } catch (error: kotlinx.coroutines.CancellationException) {
                    throw error
                } catch (error: Exception) {
                    SecurityLoadResult(emptyList(), error.message?.takeIf(String::isNotBlank) ?: "刷新白名单失败")
                }
            } else null
            _uiState.update {
                it.copy(
                    ipWhitelist = fresh?.data ?: it.ipWhitelist,
                    ipWhitelistError = if (result.isSuccess) fresh?.error ?: it.ipWhitelistError else result.error,
                    actionMessage = if (result.isSuccess) "白名单已删除" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 保存网页端 / APP 端最大会话数；成功后刷新策略。 */
    fun saveSessionPolicy(maxWebSessions: Int, maxAppSessions: Int) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.setSessionPolicy(maxWebSessions, maxAppSessions)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message?.takeIf(String::isNotBlank) ?: "保存会话策略失败")
            }
            val fresh = if (result.isSuccess) {
                try {
                    repo.getSessionPolicy()
                } catch (error: kotlinx.coroutines.CancellationException) {
                    throw error
                } catch (error: Exception) {
                    SecurityLoadResult(SessionPolicy(), error.message?.takeIf(String::isNotBlank) ?: "刷新会话策略失败")
                }
            } else null
            _uiState.update {
                it.copy(
                    sessionPolicy = fresh?.data ?: it.sessionPolicy,
                    sessionPolicyError = if (result.isSuccess) fresh?.error ?: it.sessionPolicyError else result.error,
                    actionMessage = if (result.isSuccess) "会话策略已保存" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 启用 2FA（POST /api/security/2fa/setup）。 */
    fun enable2fa() {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.enable2fa()
            } catch (error: Exception) {
                SecurityLoadResult<TwoFactorSetupResult>(TwoFactorSetupResult(), error.message ?: "启用 2FA 失败")
            }
            _uiState.update {
                it.copy(
                    twoFactorSetup = result.data,
                    twoFactorSetupError = if (result.isSuccess) null else result.error,
                    actionMessage = if (result.isSuccess) "2FA 启用请求已发送" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 禁用 2FA（POST /api/security/2fa/disable）。 */
    fun disable2fa(code: String) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.disable2fa(code)
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message ?: "禁用 2FA 失败")
            }
            _uiState.update {
                it.copy(
                    twoFactor = TwoFactorStatus(enabled = false, supported = it.twoFactor.supported),
                    twoFactorError = if (result.isSuccess) null else result.error,
                    actionMessage = if (result.isSuccess) "2FA 已禁用" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 验证 2FA 验证码。 */
    fun verify2fa(code: String) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true, actionMessage = null) }
            val result = try {
                repo.verify2fa(code)
            } catch (error: Exception) {
                SecurityLoadResult<Boolean>(false, error.message ?: "验证 2FA 验证码失败")
            }
            _uiState.update {
                val verified = result.data
                it.copy(
                    twoFactor = if (verified) TwoFactorStatus(enabled = true, supported = it.twoFactor.supported) else it.twoFactor,
                    twoFactorVerificationResult = TwoFactorVerifyResult(success = verified, message = result.error ?: ""),
                    actionMessage = if (result.isSuccess) "2FA 验证码验证成功" else result.error,
                    actionBusy = false,
                )
            }
        }
    }

    /** 强制下线指定会话。 */
    fun forceLogout(sessionId: Long) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(forceLogoutLoading = true, actionBusy = true, actionMessage = null) }
            val result = try {
                repo.forceLogout(sessionId)
            } catch (error: Exception) {
                SecurityLoadResult<Unit>(Unit, error.message ?: "强制下线失败")
            }
            val fresh = if (result.isSuccess) {
                try {
                    repo.getSessions()
                } catch (error: Exception) {
                    SecurityLoadResult(emptyList(), error.message ?: "刷新会话列表失败")
                }
            } else null
            _uiState.update {
                it.copy(
                    sessions = fresh?.data ?: it.sessions,
                    forceLogoutLoading = false,
                    actionBusy = false,
                    actionMessage = if (result.isSuccess) "已强制下线" else result.error ?: "强制下线失败",
                )
            }
            }
        }
    }

    /** 获取筛选后的登录日志。 */
    fun fetchFilteredLoginLogs(filter: LoginLogFilter) {
        val repo = repository ?: return
        viewModelScope.launch {
            _uiState.update { it.copy(actionBusy = true) }
            val result = try {
                repo.getLoginLogs(filter)
            } catch (error: Exception) {
                SecurityLoadResult<List<LoginLog>>(emptyList(), error.message ?: "获取登录日志失败")
            }
            _uiState.update {
                it.copy(
                    loginLogs = result.data,
                    loginLogsError = result.error,
                    actionBusy = false,
                )
            }
        }
    }
}
