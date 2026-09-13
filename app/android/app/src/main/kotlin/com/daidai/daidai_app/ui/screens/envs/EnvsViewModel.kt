package com.daidai.daidai_app.ui.screens.envs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.repository.EnvsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 环境变量列表页的 `UiState`。 */
enum class EnvListPhase { Loading, Loaded, Error }

/** 环境变量模块的可观察状态。 */
data class EnvsUiState(
    val envs: List<EnvVar> = emptyList(),
    val phase: EnvListPhase = EnvListPhase.Loading,
    val errorMessage: String? = null,
    /** 正在被启停/删除的 env id，用于展示行内忙碌态；null 表示无进行中的操作。 */
    val busyId: Long? = null,
    val busyType: BusyType = BusyType.None,
) {
    enum class BusyType { None, Toggle, Delete }
}

/**
 * 环境变量的 ViewModel：拉取列表 + 新建 / 编辑 / 删除 / 启停。
 *
 * [EnvsRepository] 缺省为 null 时有意报「环境变量后端未装配」，绝不伪造空列表，
 * 以便布局可预览、行为不谎报（与 LoginViewModel / LogsViewModel 约定一致）。
 */
class EnvsViewModel(
    private val repository: EnvsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(EnvsUiState())
    val uiState: StateFlow<EnvsUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    /** 重新拉取列表。 */
    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = EnvListPhase.Error, errorMessage = "环境变量后端未装配")
            }
            return
        }
        _uiState.update { it.copy(phase = EnvListPhase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val envs = repo.list()
                _uiState.update {
                    it.copy(envs = envs, phase = EnvListPhase.Loaded, errorMessage = null)
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        phase = EnvListPhase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "环境变量加载失败，请重试",
                    )
                }
            }
        }
    }

    /** 新建环境变量；成功后刷新列表并返回新建记录的 id。 */
    fun create(name: String, value: String, remark: String) {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(errorMessage = "环境变量后端未装配")
            }
            return
        }
        viewModelScope.launch {
            try {
                repo.create(name = name, value = value, remark = remark)
                refresh()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "新建环境变量失败，请重试",
                    )
                }
            }
        }
    }

    /** 更新环境变量；成功后刷新列表。 */
    fun update(id: Long, name: String, value: String, remark: String, enabled: Boolean) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        viewModelScope.launch {
            try {
                repo.update(id = id, name = name, value = value, remark = remark, enabled = enabled)
                refresh()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "更新环境变量失败，请重试",
                    )
                }
            }
        }
    }

    /** 删除环境变量；成功后刷新列表。 */
    fun delete(id: Long) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        if (_uiState.value.busyId != null) return
        _uiState.update { it.copy(busyId = id, busyType = EnvsUiState.BusyType.Delete) }
        viewModelScope.launch {
            try {
                repo.delete(id)
                _uiState.update { it.copy(envs = it.envs.filterNot { env -> env.id == id }) }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "删除环境变量失败，请重试",
                    )
                }
            } finally {
                _uiState.update { it.copy(busyId = null, busyType = EnvsUiState.BusyType.None) }
            }
        }
    }

    /** 切换环境变量启停；成功后本地更新该行的 enabled。 */
    fun setEnabled(id: Long, enabled: Boolean) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        if (_uiState.value.busyId != null) return
        _uiState.update { it.copy(busyId = id, busyType = EnvsUiState.BusyType.Toggle) }
        viewModelScope.launch {
            try {
                repo.setEnabled(id, enabled)
                _uiState.update { state ->
                    state.copy(
                        envs = state.envs.map { env ->
                            if (env.id == id) env.copy(enabled = enabled) else env
                        },
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "启停环境变量失败，请重试",
                    )
                }
            } finally {
                _uiState.update { it.copy(busyId = null, busyType = EnvsUiState.BusyType.None) }
            }
        }
    }
}