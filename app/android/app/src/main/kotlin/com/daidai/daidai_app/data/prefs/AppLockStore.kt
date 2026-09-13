package com.daidai.daidai_app.data.prefs

import android.content.Context
import android.content.SharedPreferences

/**
 * 应用锁本地存储（自包含，阶段 4-2）。
 *
 * 与 SecurePreferences 不同，本模块把加盐迭代的 SHA-256 密码哈希当作可明文存放的
 * 凭据校验值（参考 Flutter `app_lock_provider.dart` 的 `sha256-iter-v1` 方案）：
 * 哈希本身即一次性、不可逆，不依赖 Android Keystore 也能跨进程/跨重启保持一致。
 * 因此直接基于 SharedPreferences 存储，保持自包含、可独立编译与测试。
 *
 * 存储字段：
 * - `enabled`        应用锁总开关（boolean）
 * - `password_hash`  密码哈希（`version:rounds:salt:digest`）
 * - `pattern_hash`   图案哈希（同上，图案点位以 `-` 连接后哈希）
 * - `biometric_enabled` 生物识别开关（boolean，当前离线占位，见 AppLockBiometric 集成点）
 * - `failed_attempts`   连续失败次数（解锁退避计算基数，跨进程持久化）
 *
 * 全部通过单例 [get] 获取；多个界面共享同一实例即天然跨页面一致。
 */
class AppLockStore private constructor(context: Context) {

    private val preferences: SharedPreferences =
        context.applicationContext.getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)

    fun isEnabled(): Boolean = preferences.getBoolean(KEY_ENABLED, false)

    fun setEnabled(enabled: Boolean) {
        preferences.edit().putBoolean(KEY_ENABLED, enabled).apply()
    }

    fun getPasswordHash(): String = preferences.getString(KEY_PASSWORD_HASH, "") ?: ""

    fun setPasswordHash(hash: String) {
        preferences.edit().putString(KEY_PASSWORD_HASH, hash).apply()
    }

    fun removePasswordHash() {
        preferences.edit().remove(KEY_PASSWORD_HASH).apply()
    }

    fun getPatternHash(): String = preferences.getString(KEY_PATTERN_HASH, "") ?: ""

    fun setPatternHash(hash: String) {
        preferences.edit().putString(KEY_PATTERN_HASH, hash).apply()
    }

    fun removePatternHash() {
        preferences.edit().remove(KEY_PATTERN_HASH).apply()
    }

    fun isBiometricEnabled(): Boolean = preferences.getBoolean(KEY_BIOMETRIC, false)

    fun setBiometricEnabled(enabled: Boolean) {
        preferences.edit().putBoolean(KEY_BIOMETRIC, enabled).apply()
    }

    fun getFailedAttempts(): Int = preferences.getInt(KEY_FAILED, 0)

    fun setFailedAttempts(attempts: Int) {
        preferences.edit().putInt(KEY_FAILED, attempts).apply()
    }

    companion object {
        private const val FILE_NAME = "app_lock_config"
        private const val KEY_ENABLED = "enabled"
        private const val KEY_PASSWORD_HASH = "password_hash"
        private const val KEY_PATTERN_HASH = "pattern_hash"
        private const val KEY_BIOMETRIC = "biometric_enabled"
        private const val KEY_FAILED = "failed_attempts"

        @Volatile
        private var instance: AppLockStore? = null

        /** 获取进程级单例；幂等，多次调用不重复创建。 */
        fun get(context: Context): AppLockStore {
            val current = instance
            if (current != null) return current
            return synchronized(this) {
                val existing = instance
                if (existing != null) existing else AppLockStore(context).also { instance = it }
            }
        }
    }
}
