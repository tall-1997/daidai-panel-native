package com.daidai.daidai_app.ui.screens.notifications

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.NotificationChannelType
import com.daidai.daidai_app.data.model.NotificationChannelSchemas
import com.daidai.daidai_app.data.prefs.PushChannelPrefs
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.json.JSONObject

/**
 * 推送配置页 UI 状态。
 *
 * @param channelTypes 22 个服务端渠道类型定义（schema）
 * @param notifyOnSuccess 任务执行成功是否推送
 * @param notifyOnFailure 任务执行失败是否推送
 * @param notifySystemEvents 系统事件告警是否推送
 * @param customTemplate 自定义推送模板（支持 {{title}} / {{content}}）
 * @param drafts 各渠道类型的配置草稿（JSON 对象，类型 -> 键值）
 * @param testMessage 发送测试按钮的反馈消息（成功 / 失败）
 * @param sendingTest 是否正在发送测试
 * @param errorMessage 全局错误消息
 */
data class PushConfigUiState(
    val channelTypes: List<NotificationChannelType> = NotificationChannelSchemas.serverChannels,
    val notifyOnSuccess: Boolean = true,
    val notifyOnFailure: Boolean = true,
    val notifySystemEvents: Boolean = true,
    val customTemplate: String = "",
    val drafts: Map<String, Map<String, String>> = emptyMap(),
    val testMessage: String? = null,
    val sendingTest: Boolean = false,
    val errorMessage: String? = null,
)

/**
 * 推送配置页 ViewModel。
 *
 * 读取/写入 [PushChannelPrefs] 中的全局开关与渠道配置草稿，并负责「发送测试」：
 * 优先走服务端推送服务（POST /api/notifications/send，或本地 fallback 的
 * /api/notifications/send），请求失败时给出标注说明「本地 fallback 无法发送」。
 *
 * 与 [LocalNotificationViewModel]（本机系统通知设置）无交集：本 VM 管理的是
 * 「服务端推送渠道配置」，LocalNotification 管理「本机通知总开关/渠道开关」。
 */
class PushConfigViewModel(
    private val prefs: PushChannelPrefs,
    private val sendTestRequest: suspend (type: String, config: Map<String, String>) -> String = defaultSendTestRequest,
) : ViewModel() {

    private val _uiState = MutableStateFlow(PushConfigUiState())
    val uiState: StateFlow<PushConfigUiState> = _uiState.asStateFlow()

    init {
        load()
    }

    /** 从偏好加载全部状态。 */
    fun load() {
        val channelTypes = NotificationChannelSchemas.serverChannels
        _uiState.update {
            it.copy(
                channelTypes = channelTypes,
                notifyOnSuccess = prefs.notifyOnSuccess,
                notifyOnFailure = prefs.notifyOnFailure,
                notifySystemEvents = prefs.notifySystemEvents,
                customTemplate = prefs.customTemplate,
                drafts = channelTypes.associate { type ->
                    type.type to draftToMap(prefs.getChannelDraft(type.type))
                },
                errorMessage = null,
            )
        }
    }

    /** 任务执行成功通知开关。 */
    fun setNotifyOnSuccess(enabled: Boolean) {
        prefs.notifyOnSuccess = enabled
        _uiState.update { it.copy(notifyOnSuccess = enabled) }
    }

    /** 任务执行失败通知开关。 */
    fun setNotifyOnFailure(enabled: Boolean) {
        prefs.notifyOnFailure = enabled
        _uiState.update { it.copy(notifyOnFailure = enabled) }
    }

    /** 系统事件告警开关。 */
    fun setNotifySystemEvents(enabled: Boolean) {
        prefs.notifySystemEvents = enabled
        _uiState.update { it.copy(notifySystemEvents = enabled) }
    }

    /** 自定义推送模板。 */
    fun setCustomTemplate(template: String) {
        prefs.customTemplate = template
        _uiState.update { it.copy(customTemplate = template) }
    }

    /** 更新某渠道类型的配置草稿并持久化。 */
    fun updateDraft(type: String, values: Map<String, String>) {
        prefs.updateChannelDraft(type, values)
        _uiState.update { state ->
            val next = state.drafts.toMutableMap()
            next[type] = (next[type] ?: emptyMap()) + values
            state.copy(drafts = next)
        }
    }

    /** 清空某渠道类型的配置草稿。 */
    fun clearDraft(type: String) {
        prefs.clearChannelDraft(type)
        _uiState.update { state ->
            val next = state.drafts.toMutableMap()
            next[type] = emptyMap()
            state.copy(drafts = next)
        }
    }

    /**
     * 发送测试通知。
     *
     * 用当前草稿配置向服务端推送服务发一条测试；服务端不可达或本地 fallback
     * 不支持该渠道时返回失败消息（UI 标注「本地 fallback 发送能力有限」）。
     */
    fun sendTest(type: String, config: Map<String, String>) {
        if (_uiState.value.sendingTest) return
        _uiState.update { it.copy(sendingTest = true, testMessage = null, errorMessage = null) }
        viewModelScope.launch {
            try {
                val message = sendTestRequest(type, config)
                _uiState.update { it.copy(sendingTest = false, testMessage = message) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                val hint = if (error.message?.contains("本地") == true) {
                    "（本地 fallback 发送能力有限，发送依赖后端推送服务）"
                } else {
                    "（发送依赖后端推送服务）"
                }
                _uiState.update {
                    it.copy(
                        sendingTest = false,
                        testMessage = "测试发送失败：${error.message ?: "未知错误"}$hint",
                    )
                }
            }
        }
    }

    private fun draftToMap(json: JSONObject): Map<String, String> {
        val result = LinkedHashMap<String, String>()
        val keys = json.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            result[key] = json.optString(key)
        }
        return result
    }

    companion object {
        /**
         * 默认测试发送实现：走服务端推送服务的 send 接口。
         *
         * 注意：本函数依赖 baseUrl/accessToken，实际实现由
         * [PushConfigScreen] 经 repository 注入，这里仅作兜底。
         */
        val defaultSendTestRequest: suspend (type: String, config: Map<String, String>) -> String = { _, _ ->
            throw IllegalStateException("发送依赖后端推送服务（本地 fallback 无法发送）")
        }
    }
}
