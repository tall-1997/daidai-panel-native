package com.daidai.daidai_app.data.prefs

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONObject

/**
 * 服务端推送渠道配置偏好（自包含）。
 *
 * 本类是【服务端推送渠道配置】的持久化层，与 [LocalNotificationPrefs]（本机
 * 系统通知的总开关/渠道开关）**职责不同、互不冲突**：
 *
 *  - [LocalNotificationPrefs]：控制「面板是否在设备上弹出系统通知」，
 *    键前缀 `local_notify_*`，SharedPreferences 文件 `local_notification_prefs`；
 *  - [PushChannelPrefs]：保存「服务端推送渠道」的全局偏好 —— 任务执行成功/失败
 *    是否推送、系统事件告警是否推送、自定义推送模板，以及按渠道类型保存的
 *    配置草稿（webhook URL / bot token / bark key 等），
 *    键前缀 `push_*`，SharedPreferences 文件 `push_channel_prefs`。
 *
 * 两者可以同时开启：本机通知管「显示」，服务端推送管「投递到外部渠道」。
 * 单例：通过 [getInstance] 懒加载，内部持有 applicationContext，避免 Activity 泄漏。
 * 所有写入均即时 `apply()` 持久化，不依赖外部装配。
 */
class PushChannelPrefs private constructor(context: Context) {

    private val prefs: SharedPreferences = context.applicationContext
        .getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)

    // ---- 任务执行结果通知开关 ----

    /** 任务执行成功时是否推送。默认：开启。 */
    var notifyOnSuccess: Boolean
        get() = prefs.getBoolean(KEY_NOTIFY_ON_SUCCESS, true)
        set(value) {
            prefs.edit().putBoolean(KEY_NOTIFY_ON_SUCCESS, value).apply()
        }

    /** 任务执行失败时是否推送。默认：开启。 */
    var notifyOnFailure: Boolean
        get() = prefs.getBoolean(KEY_NOTIFY_ON_FAILURE, true)
        set(value) {
            prefs.edit().putBoolean(KEY_NOTIFY_ON_FAILURE, value).apply()
        }

    // ---- 系统事件告警开关 ----

    /** 系统事件（资源告警 / 登录通知 / 更新结果等）是否推送。默认：开启。 */
    var notifySystemEvents: Boolean
        get() = prefs.getBoolean(KEY_SYSTEM_EVENTS, true)
        set(value) {
            prefs.edit().putBoolean(KEY_SYSTEM_EVENTS, value).apply()
        }

    // ---- 自定义推送模板 ----

    /**
     * 自定义推送模板文本，支持 {{title}} 与 {{content}} 占位符。
     * 用于 custom 等渠道的正文渲染；留空使用渠道默认模板。
     */
    var customTemplate: String
        get() = prefs.getString(KEY_CUSTOM_TEMPLATE, "") ?: ""
        set(value) {
            prefs.edit().putString(KEY_CUSTOM_TEMPLATE, value).apply()
        }

    // ---- 按渠道类型保存的配置草稿 ----

    /**
     * 读取某渠道类型的配置草稿（JSON 对象文本）。从未保存过返回 "{}"。
     * 草稿仅在本地保存，服务端渠道的真实配置仍由服务端 / 本地 fallback 数据库
     * 的 notification_channels 表管理；此处草稿用于「填了一半的配置」在页面
     * 切换时不丢失，以及本地无法直连服务端时让测试/展示有据可依。
     */
    fun getChannelDraft(type: String): JSONObject = runCatching {
        JSONObject(prefs.getString(draftKey(type), "{}") ?: "{}")
    }.getOrElse { JSONObject() }

    /** 保存某渠道类型的配置草稿（合并到已有草稿上，不整体覆盖）。 */
    fun updateChannelDraft(type: String, values: Map<String, String>) {
        val merged = getChannelDraft(type)
        values.forEach { (k, v) -> merged.put(k, v) }
        prefs.edit().putString(draftKey(type), merged.toString()).apply()
    }

    /** 清空某渠道类型的配置草稿。 */
    fun clearChannelDraft(type: String) {
        prefs.edit().remove(draftKey(type)).apply()
    }

    private fun draftKey(type: String): String = KEY_DRAFT_PREFIX + type

    companion object {
        private const val FILE_NAME = "push_channel_prefs"
        private const val KEY_NOTIFY_ON_SUCCESS = "push_notify_on_success"
        private const val KEY_NOTIFY_ON_FAILURE = "push_notify_on_failure"
        private const val KEY_SYSTEM_EVENTS = "push_system_events"
        private const val KEY_CUSTOM_TEMPLATE = "push_custom_template"
        private const val KEY_DRAFT_PREFIX = "push_draft_"

        @Volatile
        private var instance: PushChannelPrefs? = null

        /** 自包含单例：多次调用返回同一实例。 */
        fun getInstance(context: Context): PushChannelPrefs =
            instance ?: synchronized(this) {
                instance ?: PushChannelPrefs(context).also { instance = it }
            }
    }
}
