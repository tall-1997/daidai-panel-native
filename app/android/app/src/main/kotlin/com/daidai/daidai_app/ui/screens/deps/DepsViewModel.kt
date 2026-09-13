package com.daidai.daidai_app.ui.screens.deps

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.DepInstallRequest
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.repository.DepsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * 依赖列表页 + 安装表单的可观察状态（内存态）。
 *
 * - [phase] 列表加载三态（Loading / Loaded / Error）。
 * - [operatingId] 正在执行卸载/重装的条目 id（用于禁用该行按钮 / 展示进度）。
 * - [submittingInstall] 安装请求是否提交中。
 * - [notice] 一次性操作提示（卸载/重装/安装成功的即时反馈），展示后由 UI 清空。
 */
data class DepsUiState(
    val deps: List<DepItem> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    val operatingId: Long? = null,
    val submittingInstall: Boolean = false,
    val notice: String? = null,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 依赖模块的 ViewModel。
 *
 * 默认 repository 未装配时（未接入 di）走“未装配”错误桩，便于布局预览且不谎报成功，
 * 与 LoginViewModel / LogsViewModel 的缺省实现风格保持一致。集成阶段由调用方（如
 * di/AppServices）注入真实实现。
 */
class DepsViewModel(
    private val repository: DepsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(DepsUiState())
    val uiState: StateFlow<DepsUiState> = _uiState.asStateFlow()

    private var refreshGeneration = 0

    init {
        refresh()
    }

    /** 重新拉取依赖列表。 */
    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(phase = DepsUiState.Phase.Error, errorMessage = "Deps 后端未装配")
            }
            return
        }
        val generation = ++refreshGeneration
        _uiState.update {
            it.copy(phase = DepsUiState.Phase.Loading, errorMessage = null)
        }
        viewModelScope.launch {
            try {
                val items = repo.list()
                if (generation != refreshGeneration) return@launch
                _uiState.update {
                    it.copy(deps = items, phase = DepsUiState.Phase.Loaded, errorMessage = null)
                }
            } catch (error: Exception) {
                if (generation != refreshGeneration) return@launch
                _uiState.update {
                    it.copy(
                        phase = DepsUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "加载依赖失败，请重试",
                    )
                }
            }
        }
    }

    /** 卸载单个依赖；成功后从列表移除并提示。 */
    fun uninstall(id: Long) {
        if (_uiState.value.operatingId != null) return
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }
            return
        }
        _uiState.update { it.copy(operatingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.uninstall(id)
                _uiState.update { state ->
                    state.copy(
                        deps = state.deps.filterNot { it.id == id },
                        operatingId = null,
                        notice = "卸载请求已提交",
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        operatingId = null,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "卸载失败，请重试",
                    )
                }
            }
        }
    }

    /** 重装单个依赖；成功后刷新列表并提示。 */
    fun reinstall(id: Long) {
        if (_uiState.value.operatingId != null) return
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }
            return
        }
        _uiState.update { it.copy(operatingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.reinstall(id)
                _uiState.update { it.copy(operatingId = null, notice = "重装请求已提交") }
                refresh()
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        operatingId = null,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "重装失败，请重试",
                    )
                }
            }
        }
    }

    /** 即时下发安装请求；返回是否提交成功（供表单决定是否提示/退出）。 */
    fun install(request: DepInstallRequest): Boolean {
        if (_uiState.value.submittingInstall) return false
        if (!request.isValid) {
            _uiState.update { it.copy(errorMessage = "包名不能为空") }
            return false
        }
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }
            return false
        }
        _uiState.update { it.copy(submittingInstall = true, errorMessage = null, notice = null) }
        viewModelScope.launch {
            try {
                repo.install(request)
                _uiState.update {
                    it.copy(submittingInstall = false, notice = "安装请求已提交")
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        submittingInstall = false,
                        errorMessage = error.message?.takeIf(String::isNotBlank) ?: "安装失败，请重试",
                    )
                }
            }
        }
        // 立即通知调用方“已提交”，UI 最终状态由 notice/errorMessage 驱动。
        return true
    }

    /** 清空一次性提示（notice）。 */
    fun consumeNotice() {
        _uiState.update { it.copy(notice = null) }
    }
}
