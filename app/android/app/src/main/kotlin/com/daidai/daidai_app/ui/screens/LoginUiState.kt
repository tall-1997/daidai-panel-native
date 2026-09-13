package com.daidai.daidai_app.ui.screens

/** 登录页面的可观察状态。 */
data class LoginUiState(
    val username: String = "",
    val password: String = "",
    val phase: Phase = Phase.CheckingInitialization,
    val errorMessage: String? = null,
) {
    enum class Phase {
        CheckingInitialization,
        RequiresInitialization,
        Ready,
        Loading,
        Success,
        Error,
    }
}
