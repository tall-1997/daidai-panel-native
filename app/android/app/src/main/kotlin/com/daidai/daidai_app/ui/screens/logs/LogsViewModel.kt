package com.daidai.daidai_app.ui.screens.logs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.LogChannel
import com.daidai.daidai_app.data.model.LogCleanupConfig
import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.repository.LogsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 运行日志页逻辑模型（ViewModel）。
 * 复用 Flutter `app/lib/features/logs/log_list_page.dart` 的交互逻辑：分页、筛选、实时流、关注模式、关键字搜索。
 *
 * 主要实现要点：
 * - 尊重会话隔离：通过 LogsRepository 与 PanelRequests 完成 API 调用
 * - 未知状态映射：LogStatus 枚举覆盖后端 0/1/2/3/4（成功/失败/运行中/终止/超时）
 * - 分页与搜索：支持关键词搜索
 */
class LogsViewModel(
    private val repository: LogsRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(LogsUiState())
    val uiState: StateFlow<LogsUiState> = _uiState.asStateFlow()

    private var currentPage = 1
    private var currentChannel: String? = null

init {
        // 默认加载 Web 日志
        setChannel(LogChannel.Web.value, refresh = true)
    }

    fun setChannel(channel: String, refresh: Boolean = false) {
        viewModelScope.launch {
            val trimmedChannel = channel.trim()
            if (currentChannel != trimmedChannel || refresh) {
                currentChannel = trimmedChannel
                currentPage = 1
                _uiState.update {
                    it.copy(
                        channels = it.channels,
                        selectedChannel = trimmedChannel,
                        logs = emptyList(),
                        total = 0,
                        currentPage = 1,
                        hasMore = true,
                    )
                }
                loadLogs(refresh = true)
            }
        }
    }

    fun loadLogs(refresh: Boolean = false) {
        viewModelScope.launch {
            try {
                if (refresh) {
                    currentPage = 1
                    _uiState.update { it.copy(loading = true, error = null) }
                } else {
                    if (_uiState.value.loading || !_uiState.value.hasMore) return@launch
                    _uiState.update { it.copy(loadingMore = true, error = null) }
                }

                val channel = currentChannel ?: return@launch
                val result = repository.getLogs(
                    page = currentPage,
                    pageSize = _uiState.value.pageSize,
                    channel = channel,
                    keyword = _uiState.value.keyword,
                )

                if (result.success) {
                    val logs = result.data?.logs ?: emptyList()
                    val isRefresh = currentPage == 1
                    val merged = if (isRefresh) logs else _uiState.value.logs + logs
                    val total = result.data?.total ?: 0

                    _uiState.update {
                        it.copy(
                            logs = merged,
                            total = total,
                            currentPage = currentPage,
                            hasMore = logs.size == it.pageSize,
                            loading = false,
                            loadingMore = false,
                            error = null,
                        )
                    }
                    if (logs.isNotEmpty()) currentPage++
                } else {
                    _uiState.update {
                        it.copy(
                            loading = false,
                            loadingMore = false,
                            error = result.error ?: "获取日志失败"
                        )
                    }
                }
            } catch (e: Exception) {
                _uiState.update {
                    it.copy(
                        loading = false,
                        loadingMore = false,
                        error = e.message ?: "未知错误"
                    )
                }
            }
        }
    }

    fun loadMore() {
        if (!_uiState.value.loading && !_uiState.value.loadingMore && _uiState.value.hasMore) {
            loadLogs()
        }
    }

    fun deleteLog(logId: Long) {
        viewModelScope.launch {
            _uiState.update { it.copy(deletingId = logId) }
            try {
                repository.deleteLog(logId)
                _uiState.update { state ->
                    state.copy(
                        logs = state.logs.filter { it.id != logId },
                        deletingId = null,
                    )
                }
            } catch (e: Exception) {
                _uiState.update {
                    it.copy(
                        deletingId = null,
                        error = e.message ?: "删除日志失败"
                    )
                }
            }
        }
    }

    fun selectChannel(channel: String) {
        setChannel(channel, refresh = true)
    }

    fun clearError() {
        _uiState.update { it.copy(error = null) }
    }

    fun search(keyword: String) {
        viewModelScope.launch {
            _uiState.update { it.copy(keyword = keyword) }
            // 清空现有日志并重新加载
            currentPage = 1
            _uiState.update { it.copy(logs = emptyList(), total = 0) }
            loadLogs(refresh = true)
        }
    }

    fun toggleFollowMode() {
        _uiState.update { it.copy(followMode = !it.followMode) }
    }

    fun setFollowMode(enabled: Boolean) {
        _uiState.update { it.copy(followMode = enabled) }
    }

    fun setSearchKeyword(keyword: String) {
        search(keyword)
    }

    fun startStreamLogs() {
        val channel = currentChannel ?: return
        viewModelScope.launch {
            try {
                repository.streamLogs(channel).collect { entry ->
                    _uiState.update { state ->
                        val logs = state.logs.toMutableList()
                        // 在顶部插入新日志
                        logs.add(0, entry)
                        // 限制内存占用
                        val limited = if (logs.size > 1000) logs.takeLast(1000) else logs
                        state.copy(logs = limited)
                    }
                }
            } catch (e: Exception) {
                _uiState.update { it.copy(error = e.message ?: "流式日志错误") }
            }
        }
    }

    fun cleanupLogs(channel: String, config: LogCleanupConfig? = null) {
        viewModelScope.launch {
            try {
                _uiState.update { it.copy(loading = true, error = null) }
                val retentionDays = config?.retentionDays ?: 30
                val olderThan = java.time.Instant.now().minusSeconds(retentionDays.toLong() * 86400).toString()
                val count = repository.cleanLogs(channel, olderThan)
                _uiState.update {
                    it.copy(
                        loading = false,
                        showCleanupSuccess = true,
                    )
                }
            } catch (e: Exception) {
                _uiState.update {
                    it.copy(
                        loading = false,
                        error = e.message ?: "清理失败"
                    )
                }
            }
        }
    }

    fun updateCleanupConfig(channel: String, retentionDays: Int, autoClean: Boolean) {
        viewModelScope.launch {
            try {
                _uiState.update { it.copy(loading = true, error = null) }
                repository.updateCleanupConfig(
                    LogCleanupConfig(
                        retentionDays = retentionDays,
                        autoClean = autoClean,
                        channel = channel,
                    )
                )
                _uiState.update { it.copy(cleanupConfig = it.cleanupConfig?.copy(retentionDays = retentionDays, autoClean = autoClean)) }
                _uiState.update { it.copy(loading = false) }
            } catch (e: Exception) {
                _uiState.update {
                    it.copy(
                        loading = false,
                        error = e.message ?: "保存配置失败"
                    )
                }
            }
        }
    }
}

/** Logs 页面 UI 状态。 */
data class LogsUiState(
    val channels: List<String> = listOf(
        LogChannel.Web.value,
        LogChannel.System.value,
        LogChannel.Script.value,
        LogChannel.Cron.value,
        LogChannel.SSH.value,
        LogChannel.Subscription.value,
    ),
    val selectedChannel: String = LogChannel.Web.value,
    val logs: List<LogEntry> = emptyList(),
    val total: Int = 0,
    val currentPage: Int = 1,
    val pageSize: Int = 20,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val error: String? = null,
    val hasMore: Boolean = true,
    val followMode: Boolean = false,
    val keyword: String = "",
    val deletingId: Long? = null,
    val cleanupConfig: LogCleanupConfig? = null,
    val showCleanupSuccess: Boolean = false,
)

/** 空的 AbortSignal 实现，用于 ViewModel 中。 */
private object emptyAbortSignal {
    fun cancel() {}
}
