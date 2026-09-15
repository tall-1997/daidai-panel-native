package com.daidai.daidai_app.ui.screens

/** 登录页面的可观察状态。 */
data class LoginUiState(
    val username: String = "",
    val password: String = "",
    val totpCode: String = "",
    val phase: Phase = Phase.CheckingInitialization,
    val errorMessage: String? = null,
    val captchaChallenge: CaptchaChallenge? = null,
    val captchaPayload: GeetestCaptchaPayload? = null,
    val captchaNotice: String? = null,
) {
    enum class Phase {
        CheckingInitialization,
        RequiresInitialization,
        Ready,
        Loading,
        Success,
        Error,
    }

    val requiresCaptcha: Boolean
        get() = captchaChallenge != null && captchaPayload == null

    val readyToSubmit: Boolean
        get() = phase != Phase.Loading
            && phase != Phase.Success
            && username.isNotBlank()
            && password.isNotBlank()
            && (!requiresCaptcha || captchaPayload?.isCompleted == true)
}
