package com.daidai.daidai_app.ui.screens

import org.json.JSONObject

data class GeetestCaptchaPayload(
    val lotNumber: String = "",
    val captchaOutput: String = "",
    val passToken: String = "",
    val genTime: String = "",
) {
    fun toJson(): JSONObject = JSONObject()
        .put("lot_number", lotNumber)
        .put("captcha_output", captchaOutput)
        .put("pass_token", passToken)
        .put("gen_time", genTime)

    val isCompleted: Boolean
        get() = lotNumber.isNotBlank()
            && captchaOutput.isNotBlank()
            && passToken.isNotBlank()
            && genTime.isNotBlank()
}

data class LoginCaptchaConfig(
    val enabled: Boolean = false,
    val captchaId: String = "",
    val configured: Boolean = false,
    val implemented: Boolean = true,
    val required: Boolean = false,
    val failMode: String = "",
    val requireAfterFailures: Int = 0,
    val message: String = "",
) {
    val canProceed: Boolean
        get() = !enabled || !configured || !required

    companion object {
        fun fromJson(json: JSONObject): LoginCaptchaConfig = LoginCaptchaConfig(
            enabled = json.optBoolean("enabled"),
            captchaId = json.optString("captcha_id"),
            configured = json.optBoolean("configured"),
            implemented = json.optBoolean("implemented"),
            required = json.optBoolean("required"),
            failMode = json.optString("captcha_fail_mode"),
            requireAfterFailures = json.optInt("require_after_failures"),
            message = json.optString("message"),
        )
    }
}

data class CaptchaChallenge(
    val captchaId: String,
    val reason: String = "",
    val invalid: Boolean = false,
    val serviceUnavailable: Boolean = false,
    val failMode: String = "",
    val requireAfterFailures: Int = 0,
)

fun parseCaptchaChallengeFromError(statusCode: Int, body: String): CaptchaChallenge? {
    if (statusCode != 401 && statusCode != 503) return null
    return runCatching {
        val json = JSONObject(body)
        val required = json.optBoolean("captcha_required")
        if (!required) return null
        CaptchaChallenge(
            captchaId = json.optString("captcha_id"),
            reason = json.optString("captcha_reason"),
            invalid = json.optBoolean("captcha_invalid"),
            serviceUnavailable = json.optBoolean("captcha_service_unavailable"),
            failMode = json.optString("captcha_fail_mode"),
            requireAfterFailures = json.optInt("require_after_failures"),
        )
    }.getOrNull()?.takeIf { it.captchaId.isNotBlank() }
}
