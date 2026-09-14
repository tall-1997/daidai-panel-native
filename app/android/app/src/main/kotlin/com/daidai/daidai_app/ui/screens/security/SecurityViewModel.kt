package com.daidai.daidai_app.ui.screens.security

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.repository.SecurityRepository
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 安全模块 UI 状态。
 *
 * [SecurityTab] 用于分区展示（概览 / 登录日志 / 会话 / 审计日志）；
 * overview 与三个列表均在一次刷新中并行拉取，任一失败整体进入 Error 分支。
 */
data class SecurityUiState(
    val tab: SecurityTab = SecurityTab.Overview,
    val overview: SecurityOverview = SecurityOverview(),
    val loginLogs: List<LoginLog> = emptyList(),
    val sessions: List<Session> = emptyList(),
    val auditLogs: List<AuditLog> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
) {
    enum class Phase { Loading, Loaded, Error }

    enum class SecurityTab(val label: String) {
        Overview("概览"),
        LoginLogs("登录日志"),
        Sessions("会话"),
        AuditLogs("审计日志"),
    }
}

/**
 * 安全模块只读 ViewModel：通过 [SecurityRepository] 拉取概览与三张列表，
 * 暴露为 [StateFlow]。repository 缺省为 null 时有意报「安全后端未装配」，
 * 绝不伪造空数据（与 LoginViewModel / LogsViewModel / OpenApiViewModel 约定一致）。
 */
class SecurityViewModel(
    private val repository: SecurityRepository? = null,
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
            try {
                // 四路并行拉取，降低总等待时长
                val overviewDeferred = this.async { repo.getSecurityOverview() }
                val logsDeferred = this.async { repo.getLoginLogs() }
                val sessionsDeferred = this.async { repo.getSessions() }
                val auditsDeferred = this.async { repo.getAuditLogs() }
                _uiState.update {
                    it.copy(
                        overview = overviewDeferred.await(),
                        loginLogs = logsDeferred.await(),
                        sessions = sessionsDeferred.await(),
                        auditLogs = auditsDeferred.await(),
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
