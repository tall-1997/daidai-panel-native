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
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.collect
import com.daidai.daidai_app.data.model.SubscriptionLogPage
import com.daidai.daidai_app.data.repository.boundedCapabilityLog
import org.json.JSONObject

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
    val logId: Long? = null,
    val logText: String = "",
    val logStatus: String = "",
    val streaming: Boolean = false,
    val history: SubscriptionLogPage? = null,
    val historyLoading: Boolean = false,
    val stopping: Boolean = false,
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
    private var streamJob: Job? = null
    private var historyJob: Job? = null
    private var cursor = ""
    private var operation = ""
    private var streamGeneration = 0

    fun closeLogs() {
        streamGeneration++
        streamJob?.cancel()
        historyJob?.cancel()
        _uiState.update { it.copy(logId = null, streaming = false, historyLoading = false) }
    }

    fun openLogs(id: Long, resume: Boolean = false) {
        val repo = repository ?: return
        val same = _uiState.value.logId == id
        closeLogs()
        val generation = streamGeneration
        if (!resume || !same) { cursor = ""; operation = "" }
        _uiState.update { it.copy(logId = id, logText = if (resume && same) it.logText else "", history = null,
            streaming = true, logStatus = "连接中", errorMessage = null) }
        streamJob = viewModelScope.launch {
            try {
                repo.pullStream(id, cursor, operation).collect { event ->
                    when (event.type) {
                        "operation" -> {
                            if (operation.isNotEmpty() && operation != event.data) {
                                cursor = ""
                                _uiState.update { it.copy(logText = "") }
                            }
                            operation = event.data
                        }
                        "done" -> _uiState.update { it.copy(logStatus = "日志流结束：${event.data}") }
                        else -> {
                            val next = event.id.toLongOrNull()
                            if (next == null || next > (cursor.toLongOrNull() ?: 0L)) {
                                val text = if (event.data.startsWith("{")) runCatching {
                                    JSONObject(event.data).optString("message", event.data)
                                }.getOrDefault(event.data) else event.data
                                _uiState.update { it.copy(logText = boundedCapabilityLog(it.logText, text + "\n"), logStatus = "接收日志") }
                                if (next != null) cursor = event.id
                            }
                        }
                    }
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = friendly(error, "日志连接失败")) }
            } finally {
                if (generation == streamGeneration) _uiState.update { it.copy(streaming = false) }
            }
        }
        history(1)
    }

    fun history(page: Int) {
        val repo = repository ?: return
        val id = _uiState.value.logId ?: return
        historyJob?.cancel()
        _uiState.update { it.copy(historyLoading = true) }
        historyJob = viewModelScope.launch {
            try {
                val result = repo.logs(id, page)
                _uiState.update { it.copy(history = result) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = friendly(error, "历史日志加载失败")) }
            } finally { _uiState.update { it.copy(historyLoading = false) } }
        }
    }

    fun stopPull(id: Long) {
        val repo = repository ?: return
        if (_uiState.value.stopping) return
        _uiState.update { it.copy(stopping = true) }
        viewModelScope.launch {
            try {
                repo.stopPull(id)
                _uiState.update { it.copy(logStatus = "已提交中止请求，请核对后续日志") }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = friendly(error, "中止失败")) }
            } finally { _uiState.update { it.copy(stopping = false) } }
        }
    }

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
        if (_uiState.value.pullingId != null) return
        _uiState.update { it.copy(pullingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.pull(id)
                openLogs(id)
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
