package com.daidai.daidai_app.ui.screens

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.remote.PanelRequests
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

/**
 * 登录页的网络契约（最小写入接口）。
 * 具体实现由应用装配层提供（由主代理在 AppServices 中对接本地 fallback / 远程 Go），
 * 避免登录页依赖尚未完成的网络/存储模块。
 * 约定与 Go Core / LocalPanelHttpServer 的公共认证路由一致：
 *   GET  /api/auth/check-init   -> { "need_init": bool }
 *   GET  /api/auth/captcha-config -> { enabled, captcha_id, configured, required, ... }
 *   POST /api/auth/init         -> body { username, password }（need_init 为 true 时第一次初始化）
 *   POST /api/auth/login        -> body { username, password, totp_code, captcha? }
 *   POST /api/auth/captcha-config 的主代理接线由主代理完成，本页仅消费。
 *
 * 验证码协议（对齐 panel/server/handler/auth.go + service/captcha.go）：
 *  - 启用极验 v4 时每次登录都必须携带 captcha 四字段（lot_number /
 *    captcha_output / pass_token / gen_time），否则 401 code=captcha_required；
 *  - 校验失败 401 captcha_invalid；极验上游异常 503 captcha_service_unavailable
 *    （fail_mode=open 时服务端自行放行，客户端照常提交即可）。
 */
interface LoginRepository {
    /** 查询面板是否已完成初始化。返回 true 表示需要先初始化。 */
    suspend fun checkInit(): Boolean

    /** 首次初始化面板（用户名/密码创建管理员）。仅在 need_init 为 true 时调用。 */
    suspend fun init(username: String, password: String)

    /** 登录并取得 access_token；[totpCode] 为两步验证码（开启 2FA 的面板必填）。校验失败抛异常。 */
    suspend fun login(username: String, password: String, totpCode: String? = null): LoginResult

    /**
     * 携带极验验证码凭据登录。默认实现回退到 [login]（丢弃验证码，仅兼容旧装配，
     * 启用验证码的面板会再次返回 captcha_required）。装配层实现应把 captcha 字段
     * 放进 POST /api/auth/login 的 body.captcha。
     */
    suspend fun loginWithCaptcha(
        username: String,
        password: String,
        totpCode: String?,
        captcha: GeetestCaptchaPayload,
    ): LoginResult = login(username, password, totpCode)

    /**
     * 读取验证码运行时配置（GET /api/auth/captcha-config）。
     * 默认实现返回 null（不走网络，主代理在 AppServices 中按需接入）。
     */
    suspend fun captchaConfig(): LoginCaptchaConfig? = null
}

/** 登录成功回调所需的最小凭据结果。 */
data class LoginResult(
    val accessToken: String,
    val username: String,
)

/**
 * 通过公共端点 GET /api/auth/captcha-config 读取验证码配置（网络在 IO 线程）。
 * 由 [LoginRepository] 的默认实现或主代理按需调用。base 传空串时按本地回环地址处理；
 * 解析失败抛异常由调用方容错。
 */
internal suspend fun fetchLoginCaptchaConfig(base: String, username: String): LoginCaptchaConfig =
    withContext(Dispatchers.IO) {
        val trimmed = base.trim().trimEnd('/')
        val origin = trimmed.ifBlank { "http://127.0.0.1:8080" }
        val url = origin + "/api/auth/captcha-config" +
            if (username.isNotBlank()) "?username=" + JSONObject.quote(username).trim('"') else ""
        val body = PanelRequests.execute("GET", url)
        LoginCaptchaConfig.fromJson(JSONObject(body))
    }

/**
 * LoginScreen 的 ViewModel：负责 check-init 后的初始化/登录流程与 loading/error/success 状态，
 * 以及登录验证码（极验 v4）挑战流程。
 *
 * 默认的 repository 未装配时会保持“登录后端未装配”错误态（绝不伪造成功）。
 */
class LoginViewModel(
    private val repository: LoginRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(LoginUiState())
    val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    /** 单飞：同一时刻只允许一个登录/初始化流程，防止重复提交叠加请求。 */
    private var submitJob: Job? = null

    private var pendingTotp: String? = null

    init {
        refreshInitState()
    }

    fun setUsername(value: String) {
        _uiState.update {
            it.copy(username = value, errorMessage = null, captchaNotice = null)
        }
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
                if (error is CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = LoginUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "无法检查面板状态，请重试",
                    )
                }
            }
        }
    }

    /** WebView 验证成功回传：保存凭据并自动重新提交。 */
    fun onCaptchaCompleted(payload: GeetestCaptchaPayload) {
        if (!payload.isCompleted) {
            _uiState.update { it.copy(captchaNotice = "验证码凭据不完整，请重新完成人机验证") }
            return
        }
        _uiState.update { it.copy(captchaPayload = payload, captchaNotice = null, errorMessage = null) }
        submit()
    }

    /** 用户主动关闭验证：保留挑战态，允许稍后再次打开。 */
    fun onCaptchaDismissed() {
        _uiState.update { it.copy(captchaNotice = "尚未完成人机验证，登录后需要再次验证") }
    }

    /** 清除当前验证码挑战（用户名变化等场景）。 */
    fun resetCaptcha() {
        _uiState.update { it.copy(captchaChallenge = null, captchaPayload = null, captchaNotice = null) }
    }

    /** 首次初始化并随后登录；或直接登录。成功后回调 onSuccess。 */
    fun submit(onSuccess: () -> Unit = {}) {
        if (submitJob?.isActive == true) return
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
        val phaseAtSubmit = state.phase
        pendingTotp = state.totpCode.trim().takeIf(String::isNotEmpty)

        submitJob = viewModelScope.launch {
            performLogin(repository, username, password, onSuccess, phaseAtSubmit)
        }
    }

    /**
     * 单次登录尝试，含验证码门控与 503 宽松模式重试（最多 2 次）。
     */
    private suspend fun performLogin(
        repository: LoginRepository,
        username: String,
        password: String,
        onSuccess: () -> Unit,
        phaseAtSubmit: LoginUiState.Phase,
    ) {
        var attempts = 0
        while (attempts < 2) {
            attempts++
            _uiState.update { it.copy(phase = LoginUiState.Phase.Loading, errorMessage = null) }
            try {
                if (attempts == 2) {
                    _uiState.update { it.copy(captchaChallenge = null, captchaPayload = null) }
                }
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
                val captcha = _uiState.value.captchaPayload
                val challenge = _uiState.value.captchaChallenge
                if (captcha == null && challenge == null && attempts == 1) {
                    val config = repository.captchaConfig()
                    if (config != null && config.enabled && config.configured && config.required) {
                        _uiState.update {
                            it.copy(
                                phase = LoginUiState.Phase.Ready,
                                captchaChallenge = CaptchaChallenge(
                                    captchaId = config.captchaId,
                                    failMode = config.failMode,
                                    requireAfterFailures = config.requireAfterFailures,
                                ),
                                captchaNotice = config.message.ifBlank { "请先完成人机验证" },
                            )
                        }
                        return
                    }
                }
                val result = if (captcha != null) {
                    repository.loginWithCaptcha(username, password, pendingTotp, captcha)
                } else {
                    repository.login(username, password, pendingTotp)
                }
                pendingTotp = null
                _uiState.update {
                    it.copy(
                        phase = LoginUiState.Phase.Success,
                        username = result.username,
                        captchaChallenge = null,
                        captchaPayload = null,
                        captchaNotice = null,
                    )
                }
                onSuccess()
                return
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                if (handleLoginFailure(error) == RetryDecision.Retry) {
                    delay(300L)
                    continue
                }
                return
            }
        }
    }

    /**
     * 把登录异常映射到 UI 状态：验证码挑战优先于普通错误提示。
     * @return RetryDecision 是否应立即重试（503 宽松模式放行时）。
     */
    private fun handleLoginFailure(error: Exception): RetryDecision {
        val http = error as? PanelApiException
        val challenge = http?.let { parseCaptchaChallengeFromError(it.statusCode, it.responseBody) }
        if (challenge != null) {
            if (challenge.serviceUnavailable && challenge.failMode.equals("open", ignoreCase = true)) {
                _uiState.update {
                    it.copy(
                        captchaChallenge = null,
                        captchaPayload = null,
                        captchaNotice = "验证码服务暂不可用（宽松模式放行中），正在重试…",
                    )
                }
                return RetryDecision.Retry
            }
            _uiState.update {
                it.copy(
                    phase = LoginUiState.Phase.Error,
                    captchaChallenge = challenge,
                    captchaPayload = null,
                    errorMessage = http?.serverMessage ?: "请先完成人机验证",
                    captchaNotice = if (challenge.invalid) "验证码校验失败，请重新完成人机验证" else null,
                )
            }
            return RetryDecision.Stop
        }
        _uiState.update {
            it.copy(
                phase = LoginUiState.Phase.Error,
                errorMessage = error.message?.takeIf(String::isNotBlank) ?: "登录失败，请重试",
            )
        }
        return RetryDecision.Stop
    }

    override fun onCleared() {
        submitJob?.cancel()
        super.onCleared()
    }
}

private enum class RetryDecision { Retry, Stop }
