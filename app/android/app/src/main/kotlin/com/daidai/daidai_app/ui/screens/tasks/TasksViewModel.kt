package com.daidai.daidai_app.ui.screens.tasks

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.model.TaskWritePayload
import com.daidai.daidai_app.data.repository.TasksRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 任务列表页的可观察状态。 */
data class TasksUiState(
    val tasks: List<Task> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    /** 正在执行的操作任务 id（用于禁用重复点击的按钮级 loading 提示）。 */
    val busyTaskId: Long? = null,
    /** 最近一次 创建/更新 是否成功（表单据此在成功后回调导航）。 */
    val mutationSucceeded: Boolean = false,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 任务模块 ViewModel（阶段 3-1）。
 *
 * 通过 [TasksRepository] 拉取任务列表并暴露为 [StateFlow]，同时提供
 * 新建 / 更新 / 删除 / 运行 / 启停切换 的 CRUD 入口。CRUD 完成后会自动刷新列表。
 *
 * repository 缺省为 null 时有意报「任务后端未装配」，绝不伪造空数据，以便
 * 布局可预览、行为不谎报（与 OpenApiViewModel / LoginViewModel 的约定一致）。
 */
class TasksViewModel(
    private val repository: TasksRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(TasksUiState())
    val uiState: StateFlow<TasksUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = TasksUiState.Phase.Error, errorMessage = "任务后端未装配")
            }
            return
        }
        _uiState.update { it.copy(phase = TasksUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val tasks = repo.getTasks()
                _uiState.update {
                    it.copy(tasks = tasks, phase = TasksUiState.Phase.Loaded, errorMessage = null)
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        phase = TasksUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "任务加载失败，请重试",
                    )
                }
            }
        }
    }

    /** 新建任务。 */
    fun createTask(payload: TaskWritePayload) {
        val repo = repository ?: return
        _uiState.update { it.copy(phase = TasksUiState.Phase.Loading, mutationSucceeded = false) }
        viewModelScope.launch {
            try {
                repo.createTask(payload)
                reloadAfterMutation(success = true)
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(errorMessage = friendly(error, "创建失败"))
                }
            }
        }
    }

    /** 更新指定任务。 */
    fun updateTask(id: Long, payload: TaskWritePayload) {
        val repo = repository ?: return
        _uiState.update { it.copy(errorMessage = null, mutationSucceeded = false) }
        viewModelScope.launch {
            try {
                repo.updateTask(id, payload)
                reloadAfterMutation(success = true)
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(errorMessage = friendly(error, "保存失败"))
                }
            }
        }
    }

    /** 删除指定任务。 */
    fun deleteTask(id: Long) {
        val repo = repository ?: return
        _uiState.update { it.copy(busyTaskId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.deleteTask(id)
                reloadAfterMutation()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(busyTaskId = null, errorMessage = friendly(error, "删除失败"))
                }
            }
        }
    }

    /** 立即运行指定任务。 */
    fun runTask(id: Long) {
        val repo = repository ?: return
        _uiState.update { it.copy(busyTaskId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.runTask(id)
                reloadAfterMutation()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(busyTaskId = null, errorMessage = friendly(error, "运行失败"))
                }
            }
        }
    }

    /** 启停切换：enabled 为 true 表示要启用任务，false 表示要禁用。 */
    fun toggleTask(id: Long, enabled: Boolean) {
        val repo = repository ?: return
        _uiState.update { it.copy(busyTaskId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.toggleTask(id, enabled)
                reloadAfterMutation()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(busyTaskId = null, errorMessage = friendly(error, "切换失败"))
                }
            }
        }
    }

    /** 清除一次性错误提示与暂态成功标记。 */
    fun clearError() {
        _uiState.update { it.copy(errorMessage = null, busyTaskId = null, mutationSucceeded = false) }
    }

    /** CRUD 成功后刷新列表；仍保持 Loaded 态避免闪回 Loading。 */
    private fun reloadAfterMutation(success: Boolean = false) {
        viewModelScope.launch {
            try {
                val tasks = repository?.getTasks().orEmpty()
                _uiState.update {
                    it.copy(
                        tasks = tasks,
                        busyTaskId = null,
                        errorMessage = null,
                        mutationSucceeded = success,
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        busyTaskId = null,
                        mutationSucceeded = false,
                        phase = TasksUiState.Phase.Error,
                        errorMessage = friendly(error, "列表刷新失败"),
                    )
                }
            }
        }
    }

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf(String::isNotBlank) ?: fallback
}