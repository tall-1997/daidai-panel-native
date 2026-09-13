package com.daidai.daidai_app.data.prefs

import android.content.Context
import android.content.SharedPreferences

/**
 * 本机通知偏好（自包含）。
 *
 * 用 SharedPreferences 持久化本机通知的【总开关】与【各渠道开关】。
 * 渠道键统一使用 `local_notify_channel_` 前缀 + 渠道名，与 Flutter 端
 * `local_notification_settings_page.dart` / `local_notification_service.dart`
 * 的 task / system 渠道语义对齐，渠道默认开启。
 *
 * 单例：通过 [getInstance] 懒加载，内部持有 applicationContext，避免 Activity 泄漏。
 * 所有写入均即时 `apply()` 持久化，不依赖外部装配。
 */
class LocalNotificationPrefs private constructor(context: Context) {

    private val prefs: SharedPreferences = context.applicationContext
        .getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)

    /** 本机通知总开关。默认：开启。 */
    var enabled: Boolean
        get() = prefs.getBoolean(KEY_ENABLED, true)
        set(value) {
            prefs.edit().putBoolean(KEY_ENABLED, value).apply()
        }

    /** 读取单个渠道开关（从未配置过时默认开启）。 */
    fun getChannelEnabled(channel: String): Boolean =
        prefs.getBoolean(channelKey(channel), true)

    /** 写入单个渠道开关并即时持久化。 */
    fun setChannelEnabled(channel: String, value: Boolean) {
        prefs.edit().putBoolean(channelKey(channel), value).apply()
    }

    private fun channelKey(channel: String): String = KEY_CHANNEL_PREFIX + channel

    companion object {
        private const val FILE_NAME = "local_notification_prefs"
        private const val KEY_ENABLED = "local_notify_master_enabled"
        private const val KEY_CHANNEL_PREFIX = "local_notify_channel_"

        @Volatile
        private var instance: LocalNotificationPrefs? = null

        /** 自包含单例：多次调用返回同一实例。 */
        fun getInstance(context: Context): LocalNotificationPrefs =
            instance ?: synchronized(this) {
                instance ?: LocalNotificationPrefs(context).also { instance = it }
            }
    }
}
