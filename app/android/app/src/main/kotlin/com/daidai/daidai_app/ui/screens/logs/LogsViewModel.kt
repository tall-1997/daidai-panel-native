package com.daidai.daidai_app.ui.screens.logs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.LogEntry
import com.daidai.daidai_app.data.model.LogStatus
import com.daidai.daidai_app.data.repository.LogFilter
import com.daidai.daidai_app.data.repository.LogsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 日志列表页 UI 状态。 */
data class LogsUiState(
    val logs: List<LogEntry> = emptyList(),
    val total: Int = 0,
    val page: Int = 1,
    val pageSize: Int = 20,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val deletingId: Long? = null,
    val errorMessage: String? = null,
    val keyword: String = "",
    val taskId: String = "",
    val status: LogStatus? = null,
) {
    val hasMore: Boolean get() = logs.isNotEmpty() && logs.size < total
}

/**
 * 日志列表的 ViewModel：拉取 /api/logs 分页列表、筛选、上拉加载、删除单条。
 *
 * 默认 repository 未装配时（未接入 di）走“未装配”错误桩，便于布局预览且不谎报成功，
 * 与 LoginViewModel 的缺省实现风格保持一致。集成阶段由 AppServices 注入真实实现。
 */
class LogsViewModel(
    private val repository: LogsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(LogsUiState())
    val uiState: StateFlow<LogsUiState> = _uiState.asStateFlow()

    private var loadGeneration = 0

    init {
        loadFirstPage()
    }

    /** 更新筛选并回到第一页。 */
    fun setFilters(keyword: String = _uiState.value.keyword, taskId: String = _uiState.value.taskId, status: LogStatus? = _uiState.value.status) {
        if (keyword == _uiState.value.keyword &&
            taskId == _uiState.value.taskId &&
            status == _uiState.value.status
        ) return
        _uiState.update {
            it.copy(keyword = keyword, taskId = taskId, status = status)
        }
        loadFirstPage()
    }

    fun clearFilters() {
        if (_uiState.value.keyword.isEmpty() && _uiState.value.taskId.isEmpty() && _uiState.value.status == null) return
        _uiState.update { it.copy(keyword = "", taskId = "", status = null) }
        loadFirstPage()
    }

    fun refresh() = loadFirstPage()

    fun loadMore() {
        val state = _uiState.value
        if (state.loading || state.loadingMore || !state.hasMore) return
        loadPage(targetPage = state.page + 1, refresh = false)
    }

    fun deleteLog(id: Long) {
        if (_uiState.value.deletingId != null) return
        val repository = repository ?: run {
            _uiState.update { it.copy(errorMessage = "日志后端未装配") }
            return
        }
        _uiState.update { it.copy(deletingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repository.deleteLog(id)
                _uiState.update { state ->
                    val remaining = state.logs.filterNot { it.id == id }
                    state.copy(
                        logs = remaining,
                        total = maxOf(0, state.total - 1),
                        deletingId = null,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(deletingId = null, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "删除失败，请重试")
                }
            }
        }
    }

    private fun loadFirstPage() {
        loadPage(targetPage = 1, refresh = true)
    }

    private fun loadPage(targetPage: Int, refresh: Boolean) {
        val repository = repository ?: run {
            _uiState.update { it.copy(loading = false, errorMessage = "日志后端未装配") }
            return
        }
        val generation = ++loadGeneration
        val state = _uiState.value

        _uiState.update {
            it.copy(
                loading = if (refresh) true else it.loading,
                loadingMore = if (refresh) false else it.loadingMore || (targetPage > 1),
                errorMessage = null,
            )
        }
        viewModelScope.launch {
            try {
                val page = repository.getLogs(
                    filter = LogFilter(
                        keyword = state.keyword,
                        taskId = state.taskId,
                        status = state.status,
                    ),
                    page = targetPage,
                    pageSize = state.pageSize,
                )
                if (generation != loadGeneration) return@launch
                _uiState.update {
                    val existing = if (refresh) emptyList() else it.logs
                    it.copy(
                        logs = existing + page.items,
                        total = page.total,
                        page = page.page,
                        loading = false,
                        loadingMore = false,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                if (generation != loadGeneration) return@launch
                _uiState.update {
                    it.copy(
                        loading = false,
                        loadingMore = false,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "加载日志失败，请重试",
                    )
                }
            }
        }
    }
}
