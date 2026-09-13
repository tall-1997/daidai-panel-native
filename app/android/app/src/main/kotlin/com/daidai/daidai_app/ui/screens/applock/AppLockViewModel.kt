package com.daidai.daidai_app.ui.screens.applock

import android.content.Context
import androidx.lifecycle.ViewModel
import com.daidai.daidai_app.data.prefs.AppLockStore
import java.security.MessageDigest
import java.security.SecureRandom
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * 应用锁的 UI 状态（阶段 4-2）。
 *
 * @param enabled          应用锁总开关是否开启。
 * @param hasPassword      是否已配置密码（存在非空密码哈希）。
 * @param hasPattern       是否已配置图案（存在非空图案哈希）。
 * @param biometricEnabled 生物识别开关（班级占位，见 [AppLockViewModel.BIOMETRIC_NOTICE]）。
 * @param locked           当前门禁是否处于锁定态（true 时需验证通过才能放行）。
 * @param failedAttempts   本轮连续失败次数（用于计算退避）。
 * @param lockedUntilEpochMs 退避到期时间（epoch 毫秒）；0 表示未在退避中。
 * @param notice           面向用户的最新提示（错误 / 退避 / 成功等），UI 消费后可清空。
 */
data class AppLockUiState(
    val enabled: Boolean = false,
    val hasPassword: Boolean = false,
    val hasPattern: Boolean = false,
    val biometricEnabled: Boolean = false,
    val locked: Boolean = false,
    val failedAttempts: Int = 0,
    val lockedUntilEpochMs: Long = 0L,
    val notice: String? = null,
) {
    val hasAnyMethod: Boolean get() = hasPassword || hasPattern
}

/**
 * 应用锁 ViewModel：持有 [AppLockUiState]，实现图案/密码的加盐迭代
 * SHA-256 校验、失败退避、锁存活期状态等逻辑。
 *
 * 与 Flutter `app_lock_provider.dart` 对齐：
 * - 哈希格式 `sha256-iter-v1:40000:salt:digest`，对 `alt :: value` 迭代 SHA-256。
 * - 图案点位以 `-` 连接、点位 1~9，密码至少 4 位、图案至少 4 个点位。
 * - 退避：连续 3 次 → 10s，5 次 → 30s，8 次及以上 → 60s。
 */
class AppLockViewModel(
    context: Context,
    private val store: AppLockStore = AppLockStore.get(context),
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        AppLockUiState(
            enabled = store.isEnabled(),
            hasPassword = store.getPasswordHash().isNotEmpty(),
            hasPattern = store.getPatternHash().isNotEmpty(),
            biometricEnabled = store.isBiometricEnabled(),
            failedAttempts = store.getFailedAttempts(),
        ),
    )
    val uiState: StateFlow<AppLockUiState> = _uiState.asStateFlow()

    /**
     * 生物识别集成点（当前未引入 androidx.biometric）：
     * Compose 端在门禁/设置页仅提供开关占位。接入时可在此处：
     * 1) 在 build.gradle 增加 `androidx.biometric:biometric:1.1.0`；
     * 2) 用 [androidx.biometric.BiometricPrompt] 发起验证，成功后调用 [unlockSession]。
     * 开关的本地持久化已由 [AppLockStore] 完成，仅需替换验证路径。
     */
    private val biometricAvailable: Boolean = false

    /** 刷新本地存储到状态（设置页 init 时调用）。 */
    fun refresh() {
        _uiState.update {
            it.copy(
                enabled = store.isEnabled(),
                hasPassword = store.getPasswordHash().isNotEmpty(),
                hasPattern = store.getPatternHash().isNotEmpty(),
                biometricEnabled = store.isBiometricEnabled(),
            )
        }
    }

    /** 总开关：开启要求至少已配置一种验证方式；关闭则解除锁定。 */
    fun setEnabled(enabled: Boolean, onError: (String) -> Unit = {}) {
        if (enabled && !_uiState.value.hasAnyMethod) {
            onError("请先至少配置一种验证方式")
            return
        }
        store.setEnabled(enabled)
        _uiState.update {
            it.copy(enabled = enabled, notice = null).let { s ->
                if (!enabled) s.copy(locked = false) else s
            }
        }
    }

    /** 设置/重设图案（图案为 1~9 的点位列表）。成功后自动开启应用锁。 */
    fun enablePattern(points: List<Int>): Boolean {
        if (points.size < MIN_POINTS) {
            _uiState.update { it.copy(notice = "图案至少需要 $MIN_POINTS 个点位") }
            return false
        }
        val raw = points.joinToString("-")
        store.setPatternHash(hashSecret(raw))
        store.setEnabled(true)
        store.setBiometricEnabled(false)
        clearBackoff()
        _uiState.update {
            it.copy(
                hasPattern = true,
                enabled = true,
                biometricEnabled = false,
                failedAttempts = 0,
                lockedUntilEpochMs = 0L,
                notice = null,
            )
        }
        return true
    }

    /** 关闭图案验证。 */
    fun disablePattern() {
        store.removePatternHash()
        _uiState.update { it.copy(hasPattern = false, notice = null) }
    }

    /** 设置/重设密码。成功后自动开启应用锁。 */
    fun enablePassword(password: String): Boolean {
        if (password.length < MIN_PASSWORD_LENGTH) {
            _uiState.update { it.copy(notice = "密码至少需要 $MIN_PASSWORD_LENGTH 位") }
            return false
        }
        store.setPasswordHash(hashSecret(password))
        store.setEnabled(true)
        store.setBiometricEnabled(false)
        clearBackoff()
        _uiState.update {
            it.copy(
                hasPassword = true,
                enabled = true,
                biometricEnabled = false,
                failedAttempts = 0,
                lockedUntilEpochMs = 0L,
                notice = null,
            )
        }
        return true
    }

    /** 关闭密码验证。 */
    fun disablePassword() {
        store.removePasswordHash()
        _uiState.update { it.copy(hasPassword = false, notice = null) }
    }

    /** 生物识别开关（占位）：当前设备未引入 BiometricPrompt 时强制拒绝并提示集成点。 */
    fun setBiometricEnabled(enabled: Boolean) {
        if (enabled && !biometricAvailable) {
            _uiState.update {
                it.copy(notice = "当前版本暂未接入系统生物识别，集成点见 AppLockViewModel.BIOMETRIC_NOTICE")
            }
            return
        }
        store.setBiometricEnabled(enabled)
        _uiState.update {
            it.copy(
                biometricEnabled = enabled,
                enabled = enabled || it.enabled,
                failedAttempts = 0,
                lockedUntilEpochMs = 0L,
                notice = null,
            )
        }
    }

    /** 应用锁开启时进入锁定态（启动门禁由集成阶段调用）。 */
    fun lockIfEnabled() {
        val current = _uiState.value
        if (current.enabled) {
            _uiState.update { it.copy(locked = true, notice = null) }
        }
    }

    /** 解锁成功：结束锁定并清零失败计数与退避。 */
    fun unlockSession() {
        clearBackoff()
        _uiState.update {
            it.copy(
                locked = false,
                failedAttempts = 0,
                lockedUntilEpochMs = 0L,
                notice = null,
            )
        }
    }

    /** 重置会话态（不清除配置，仅解除锁定与退避）。 */
    fun resetSession() {
        _uiState.update {
            it.copy(locked = false, failedAttempts = 0, lockedUntilEpochMs = 0L, notice = null)
        }
    }

    /** 消费一条提示（UI 展示后调用）。 */
    fun consumeNotice() {
        _uiState.update { it.copy(notice = null) }
    }

    /** 验证图案：校验通过返回 true 并解锁；否则记录失败。 */
    fun verifyPattern(points: List<Int>): Boolean {
        if (!_uiState.value.hasPattern) return false
        if (isInBackoff()) return false
        val raw = points.joinToString("-")
        val ok = matchesSecret(raw, store.getPatternHash())
        return if (ok) {
            unlockSession()
            true
        } else {
            recordFailedAttempt("图案不正确")
            false
        }
    }

    /** 验证密码：校验通过返回 true 并解锁；否则记录失败。 */
    fun verifyPassword(password: String): Boolean {
        if (!_uiState.value.hasPassword) return false
        if (isInBackoff()) return false
        val ok = matchesSecret(password, store.getPasswordHash())
        return if (ok) {
            unlockSession()
            true
        } else {
            recordFailedAttempt("密码不正确")
            false
        }
    }

    /** 是否处于退避中（含跨进程：本地态与存储态任一命中即认为退避）。 */
    fun isInBackoff(): Boolean {
        val current = _uiState.value
        if (current.lockedUntilEpochMs > 0L && System.currentTimeMillis() < current.lockedUntilEpochMs) {
            return true
        }
        // 本地退避到期后清理本地字段；持久化的失败计数在下次验证时重置。
        if (current.lockedUntilEpochMs > 0L && System.currentTimeMillis() >= current.lockedUntilEpochMs) {
            _uiState.update { it.copy(lockedUntilEpochMs = 0L, failedAttempts = 0) }
            store.setFailedAttempts(0)
        }
        return false
    }

    private fun recordFailedAttempt(message: String) {
        val nextAttempts = _uiState.value.failedAttempts + 1
        val cooldownMs = cooldownForAttempts(nextAttempts)
        store.setFailedAttempts(nextAttempts)
        if (cooldownMs == null) {
            _uiState.update {
                it.copy(failedAttempts = nextAttempts, lockedUntilEpochMs = 0L, notice = message)
            }
            return
        }
        val until = System.currentTimeMillis() + cooldownMs
        _uiState.update {
            it.copy(
                failedAttempts = nextAttempts,
                lockedUntilEpochMs = until,
                notice = "连续输错过多，请在 ${cooldownMs / 1000}s 后重试",
            )
        }
    }

    /**
     * 按连续失败次数计算退避时长（毫秒）；返回 null 表示无需退避。
     * 对齐 Flutter：3 → 10s，5 → 30s，8 → 60s。
     */
    fun cooldownForAttempts(attempts: Int): Long? {
        return when {
            attempts >= 8 -> 60_000L
            attempts >= 5 -> 30_000L
            attempts >= 3 -> 10_000L
            else -> null
        }
    }

    /** 清零失败计数与退避（成功验证 / 重新配置时调用）。 */
    private fun clearBackoff() {
        _uiState.update { it.copy(failedAttempts = 0, lockedUntilEpochMs = 0L, notice = null) }
        store.setFailedAttempts(0)
    }

    // ---- 密码学 ----

    /** 生成 `version:rounds:salt:digest` 格式的哈希。 */
    private fun hashSecret(value: String): String {
        val salt = generateSalt()
        val digest = deriveSecret(value, salt, HASH_ROUNDS)
        return "$HASH_VERSION:$HASH_ROUNDS:$salt:$digest"
    }

    /** 校验明文是否匹配已存哈希。 */
    private fun matchesSecret(value: String, storedHash: String): Boolean {
        if (storedHash.isEmpty()) return false
        val parts = storedHash.split(":")
        if (parts.size != 4 || parts[0] != HASH_VERSION) return false
        val rounds = parts[1].toIntOrNull() ?: return false
        if (rounds <= 0 || parts[2].isEmpty() || parts[3].isEmpty()) return false
        return deriveSecret(value, parts[2], rounds) == parts[3]
    }

    private fun deriveSecret(value: String, salt: String, rounds: Int): String {
        var current = "$salt::$value".toByteArray(Charsets.UTF_8)
        repeat(rounds) {
            current = sha256(current)
        }
        return current.joinToString("") { "%02x".format(it) }
    }

    private fun sha256(input: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(input)

    private fun generateSalt(): String {
        val bytes = ByteArray(16)
        SecureRandom().nextBytes(bytes)
        return android.util.Base64.encodeToString(bytes, android.util.Base64.NO_WRAP or android.util.Base64.URL_SAFE)
    }

    companion object {
        /** 集成点说明文案，供设置页灰显区展示。 */
        const val BIOMETRIC_NOTICE = "系统生物识别为集成点：需在 build.gradle 引入 androidx.biometric 并用 BiometricPrompt 加载验证路径。"
        private const val HASH_VERSION = "sha256-iter-v1"
        private const val HASH_ROUNDS = 40000
        private const val MIN_PASSWORD_LENGTH = 4
        private const val MIN_POINTS = 4
    }
}
