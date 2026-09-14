package com.daidai.daidai_app.ui.screens

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 登录页的网络契约（最小写入接口）。
 * 具体实现由应用装配层提供，避免登录页依赖尚未完成的网络/存储模块。
 * 约定与 LocalPanelHttpServer 的公共认证路由一致：
 *   GET  /api/auth/check-init   -> { "need_init": bool }
 *   POST /api/auth/init         -> body { username, password }（need_init 为 true 时第一次初始化）
 *   POST /api/auth/login        -> body { username, password }，成功返回 { access_token, user: {...} }
 */
interface LoginRepository {
    /** 查询面板是否已完成初始化。返回 true 表示需要先初始化。 */
    suspend fun checkInit(): Boolean

    /** 首次初始化面板（用户名/密码创建管理员）。仅在 need_init 为 true 时调用。 */
    suspend fun init(username: String, password: String)

    /** 登录并取得 access_token；[totpCode] 为两步验证码（开启 2FA 的面板必填）。校验失败抛异常。 */
    suspend fun login(username: String, password: String, totpCode: String? = null): LoginResult
}

/** 登录成功回调所需的最小凭据结果。 */
data class LoginResult(
    val accessToken: String,
    val username: String,
)

/**
 * LoginScreen 的 ViewModel：负责 check-init 后的初始化/登录流程与 loading/error/success 状态。
 *
 * 默认的 repository 未装配时会走 TODO 桩 `DefaultLoginRepository`
 * （有意抛异常“登录后端未装配”，绝不伪造成功），以便布局可预览、行为不谎报。
 */
class LoginViewModel(
    private val repository: LoginRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(LoginUiState())
    val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    init {
        refreshInitState()
    }

    fun setUsername(value: String) {
        _uiState.update { it.copy(username = value, errorMessage = null) }
    }

    fun setPassword(value: String) {
        _uiState.update { it.copy(password = value, errorMessage = null) }
    }

    fun setTotpCode(value: String) {
        _uiState.update { it.copy(totpCode = value, errorMessage = null) }
    }

    fun refreshInitState() {
        val repository = repository ?: run {
            _uiState.update { it.copy(phase = LoginUiState.Phase.Error, errorMessage = "登录后端未装配") }
            return
        }
        _uiState.update { it.copy(phase = LoginUiState.Phase.CheckingInitialization, errorMessage = null) }
        viewModelScope.launch {
            try {
                val needInit = repository.checkInit()
                _uiState.update {
                    it.copy(
                        phase = if (needInit) LoginUiState.Phase.RequiresInitialization else LoginUiState.Phase.Ready,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = LoginUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "无法检查面板状态，请重试",
                    )
                }
            }
        }
    }

    /** 首次初始化并随后登录；或直接登录。成功后回调 onSuccess。 */
    fun submit(onSuccess: () -> Unit) {
        val repository = repository ?: run {
            _uiState.update { it.copy(errorMessage = "登录后端未装配") }
            return
        }
        val state = _uiState.value
        val username = state.username.trim()
        val password = state.password
        if (username.isBlank() || password.isBlank()) {
            _uiState.update { it.copy(errorMessage = "请输入用户名和密码") }
            return
        }
        // 在进入 Loading 之前捕获“是否需要首次初始化”的依据：
        // 若 check-init 的异步刷新尚未完成（CheckingInitialization），
        // 必须等待其真实结果，否则会在 loading 覆盖 phase 后丢失该信息，
        // 导致未初始化面板跳过 init 而直接登录（401）。
        val phaseAtSubmit = state.phase

        viewModelScope.launch {
            _uiState.update { it.copy(phase = LoginUiState.Phase.Loading, errorMessage = null) }
            try {
                var needInit = phaseAtSubmit == LoginUiState.Phase.RequiresInitialization
                if (phaseAtSubmit == LoginUiState.Phase.CheckingInitialization) {
                    needInit = repository.checkInit().also { needInitResult ->
                        _uiState.update {
                            it.copy(phase = if (needInitResult) LoginUiState.Phase.RequiresInitialization else LoginUiState.Phase.Ready)
                        }
                    }
                }
                if (needInit) {
                    repository.init(username, password)
                }
                val result = repository.login(username, password, state.totpCode.trim().takeIf(String::isNotEmpty))
                _uiState.update {
                    it.copy(
                        phase = LoginUiState.Phase.Success,
                        username = result.username,
                    )
                }
                onSuccess()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = LoginUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "登录失败，请重试",
                    )
                }
            }
        }
    }

}
