package com.daidai.daidai_app.ui.screens.platformtokens

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.PlatformInfo
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.data.repository.PlatformTokensRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 平台令牌页可观察状态。 */
data class PlatformTokensUiState(
    val platforms: List<PlatformInfo> = emptyList(),
    val tokens: List<PlatformTokenInfo> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    /** 一次性操作反馈（创建/更新/删除/开关成功文案）。 */
    val actionMessage: String? = null,
    /** 正在执行的写操作标记（按钮 loading / 禁用重复点击）。 */
    val busyAction: String? = null,
    /** 令牌列表的平台过滤（null = 全部）。 */
    val filterPlatformId: Long? = null,
) {
    enum class Phase { Loading, Loaded, Error }
}

/**
 * A1 平台/令牌管理 ViewModel。
 *
 * 令牌明文只在 [createToken] 参数中短暂存在；列表回显永远使用服务端掩码。
 * 删除平台为危险操作（Go 端级联删除其下令牌），确认逻辑在 UI 层。
 */
class PlatformTokensViewModel(
    private val repository: PlatformTokensRepository,
) : ViewModel() {

    constructor(context: Context) : this(PlatformTokensRepository(context))

    private val _uiState = MutableStateFlow(PlatformTokensUiState())
    val uiState: StateFlow<PlatformTokensUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    /** 并行刷新平台列表与令牌列表。 */
    fun refresh() {
        viewModelScope.launch {
            _uiState.update { it.copy(phase = PlatformTokensUiState.Phase.Loading, errorMessage = null) }
            try {
                val platforms = repository.listPlatforms()
                val tokens = repository.listTokens(_uiState.value.filterPlatformId)
                _uiState.update {
                    it.copy(platforms = platforms, tokens = tokens, phase = PlatformTokensUiState.Phase.Loaded)
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = PlatformTokensUiState.Phase.Error,
                        errorMessage = friendly(error, "加载平台令牌失败，请重试"),
                    )
                }
            }
        }
    }

    /** 切换平台过滤并重新拉取令牌。 */
    fun setFilter(platformId: Long?) {
        _uiState.update { it.copy(filterPlatformId = platformId) }
        viewModelScope.launch {
            try {
                val tokens = repository.listTokens(platformId)
                _uiState.update { it.copy(tokens = tokens) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = friendly(error, "过滤令牌失败")) }
            }
        }
    }

    // ---- 平台操作 ----

    fun createPlatform(name: String, label: String, icon: String) {
        runBusy("platform-create") {
            repository.createPlatform(name, label, icon)
            "平台已创建"
        }
    }

    /** 危险操作：UI 必须先二次确认（级联删除其下令牌）。 */
    fun deletePlatform(platform: PlatformInfo) {
        runBusy("platform-delete-${platform.id}") {
            repository.deletePlatform(platform.id)
            "平台「${platform.displayName}」及其令牌已删除"
        }
    }

    // ---- 令牌操作 ----

    fun createToken(
        platformId: Long,
        name: String,
        token: String,
        service: String,
        serviceUser: String,
        remarks: String,
    ) {
        runBusy("token-create") {
            repository.createToken(platformId, name, token, service, serviceUser, remarks)
            "令牌已创建"
        }
    }

    /** [token] 传 null/掩码表示不修改令牌内容。 */
    fun updateToken(
        id: Long,
        name: String?,
        token: String?,
        service: String?,
        serviceUser: String?,
        remarks: String?,
    ) {
        runBusy("token-update-$id") {
            repository.updateToken(id, name, token, service, serviceUser, remarks)
            "令牌已更新"
        }
    }

    /** 危险操作：UI 二次确认。 */
    fun deleteToken(token: PlatformTokenInfo) {
        runBusy("token-delete-${token.id}") {
            repository.deleteToken(token.id)
            "令牌「${token.name}」已删除"
        }
    }

    fun setTokenEnabled(token: PlatformTokenInfo, enabled: Boolean) {
        runBusy("token-toggle-${token.id}") {
            repository.setTokenEnabled(token.id, enabled)
            "令牌「${token.name}」已${if (enabled) "启用" else "禁用"}"
        }
    }

    /** 清除一次性反馈。 */
    fun consumeActionMessage() {
        _uiState.update { it.copy(actionMessage = null) }
    }

    private fun runBusy(key: String, block: suspend () -> String) {
        if (_uiState.value.busyAction != null) return
        viewModelScope.launch {
            _uiState.update { it.copy(busyAction = key, errorMessage = null) }
            try {
                val message = block()
                _uiState.update { it.copy(busyAction = null, actionMessage = message) }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(busyAction = null, errorMessage = friendly(error, "操作失败，请重试"))
                }
                refresh()
            }
        }
    }

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf(String::isNotBlank) ?: fallback
}
