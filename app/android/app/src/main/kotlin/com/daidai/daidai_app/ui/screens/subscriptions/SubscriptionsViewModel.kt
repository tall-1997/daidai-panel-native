package com.daidai.daidai_app.ui.screens.subscriptions

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.Subscription
import com.daidai.daidai_app.data.model.SubscriptionWritePayload
import com.daidai.daidai_app.data.repository.SubscriptionsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 订阅列表 / 详情共享的可观察状态（阶段 3-5）。
 *
 * reputation 沿用既有模块的 Phase 约定：Loading → Loaded / Error；
 * [mutationSucceeded] 用于表单在 新建/更新 成功后回调关闭详情页；
 * [busyId] 记录删除中的订阅（按钮级 loading），[pullingId] 记录拉取中的订阅。
 */
data class SubscriptionsUiState(
    val subscriptions: List<Subscription> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    /** 正在删除的订阅 id（用于禁用重复点击）。 */
    val busyId: Long? = null,
    /** 正在拉取更新的订阅 id。 */
    val pullingId: Long? = null,
    /** 最近一次 新建/更新 是否成功（表单据此在成功后回关闭详情）。 */
    val mutationSucceeded: Boolean = false,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 订阅模块 ViewModel（阶段 3-5）。
 *
 * 通过 [SubscriptionsRepository] 拉取订阅列表并暴露为 [StateFlow]，同时提供
 * 新建 / 更新 / 删除 / 拉取 的 CRUD 入口。CRUD 与拉取完成后自动刷新列表，
 * 保持 Loaded 态避免闪回加载。
 *
 * repository 缺省为 null 时有意报「订阅后端未装配」，绝不伪造空数据，以便布局
 * 可预览、行为不谎报（与 LoginViewModel / TasksViewModel 约定一致）。
 */
class SubscriptionsViewModel(
    private val repository: SubscriptionsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(SubscriptionsUiState())
    val uiState: StateFlow<SubscriptionsUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = SubscriptionsUiState.Phase.Error, errorMessage = "订阅后端未装配")
            }
            return
        }
        _uiState.update { it.copy(phase = SubscriptionsUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val list = repo.list()
                _uiState.update {
                    it.copy(
                        subscriptions = list,
                        phase = SubscriptionsUiState.Phase.Loaded,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = SubscriptionsUiState.Phase.Error,
                        errorMessage = friendly(error, "订阅加载失败，请重试"),
                    )
                }
            }
        }
    }

    /** 新建订阅。 */
    fun create(payload: SubscriptionWritePayload) {
        val repo = repository ?: return
        _uiState.update { it.copy(errorMessage = null, mutationSucceeded = false) }
        viewModelScope.launch {
            try {
                repo.create(payload)
                reloadAfterMutation(success = true)
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(errorMessage = friendly(error, "创建失败"))
                }
            }
        }
    }

    /** 更新指定订阅。 */
    fun update(id: Long, payload: SubscriptionWritePayload) {
        val repo = repository ?: return
        _uiState.update { it.copy(errorMessage = null, mutationSucceeded = false) }
        viewModelScope.launch {
            try {
                repo.update(id, payload)
                reloadAfterMutation(success = true)
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(errorMessage = friendly(error, "保存失败"))
                }
            }
        }
    }

    /** 删除指定订阅。 */
    fun delete(id: Long) {
        val repo = repository ?: return
        _uiState.update { it.copy(busyId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.delete(id)
                reloadAfterMutation()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(busyId = null, errorMessage = friendly(error, "删除失败"))
                }
            }
        }
    }

    /** 触发指定订阅的拉取更新。 */
    fun pull(id: Long) {
        val repo = repository ?: return
        _uiState.update { it.copy(pullingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.pull(id)
                reloadAfterMutation()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        pullingId = null,
                        errorMessage = friendly(error, "拉取失败：无法触发更新"),
                    )
                }
            }
        }
    }

    /** 清除一次性错误提示与暂态成功标记（详情页返回时调用）。 */
    fun clearError() {
        _uiState.update {
            it.copy(errorMessage = null, busyId = null, pullingId = null, mutationSucceeded = false)
        }
    }

    /**
     * 成功变更后刷新订阅列表；仍保持 Loaded 态避免闪回 Loading。
     * success=true 时置 mutationSucceeded，供表单在新建/更新后关闭详情。
     */
    private fun reloadAfterMutation(success: Boolean = false) {
        viewModelScope.launch {
            try {
                val list = repository?.list().orEmpty()
                _uiState.update {
                    it.copy(
                        subscriptions = list,
                        busyId = null,
                        pullingId = null,
                        errorMessage = null,
                        phase = SubscriptionsUiState.Phase.Loaded,
                        mutationSucceeded = success,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        busyId = null,
                        pullingId = null,
                        mutationSucceeded = false,
                        phase = SubscriptionsUiState.Phase.Error,
                        errorMessage = friendly(error, "列表刷新失败"),
                    )
                }
            }
        }
    }

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf(String::isNotBlank) ?: fallback
}
