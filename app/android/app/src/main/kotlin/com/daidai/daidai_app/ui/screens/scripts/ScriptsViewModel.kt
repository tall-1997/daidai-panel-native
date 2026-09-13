package com.daidai.daidai_app.ui.screens.scripts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.ScriptContent
import com.daidai.daidai_app.data.model.ScriptFile
import com.daidai.daidai_app.data.repository.ScriptsRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 脚本页 UI 状态：目录树 / 内容视图 / 交互操作。 */
data class ScriptsUiState(
    // 树
    val tree: List<ScriptFile> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    // 展开的目录路径（UI 局部状态也可由 Screen 持有，此处集中便于持久化）
    val expandedPaths: Set<String> = emptySet(),
    // 内容
    val selectedPath: String? = null,
    val content: ScriptContent? = null,
    val loadingContent: Boolean = false,
    // 操作
    val runningPath: String? = null,     // 正在运行的脚本路径
    val runId: String? = null,
    val deletingPath: String? = null,
    val actionError: String? = null,
    val actionMessage: String? = null,   // 轻提示（已运行 / 已停止 / 已保存）
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 脚本模块 ViewModel：加载文件树、查看/保存内容、运行/停止、删除。
 *
 * 复用阶段 0 的 Loading/Loaded/Error 三态思路，并为目录展开、内容加载、
 * 操作进行细分状态管理。repository 缺省为 null 时有意报「脚本模块未装配」，
 * 绝不伪造数据，与 LoginViewModel / OpenApiViewModel 的约定一致。
 */
class ScriptsViewModel(
    private val repository: ScriptsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(ScriptsUiState())
    val uiState: StateFlow<ScriptsUiState> = _uiState.asStateFlow()

    init {
        refreshTree()
    }

    // ------------------------------------------------------------------
    // 目录树
    // ------------------------------------------------------------------

    fun refreshTree() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(
                    phase = ScriptsUiState.Phase.Error,
                    errorMessage = "脚本模块后端未装配，请先配置服务器并登录",
                )
            }
            return
        }
        _uiState.update { it.copy(phase = ScriptsUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val tree = repo.listTree()
                _uiState.update {
                    it.copy(
                        tree = tree,
                        phase = ScriptsUiState.Phase.Loaded,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        phase = ScriptsUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "脚本列表加载失败，请重试",
                    )
                }
            }
        }
    }

    /** 展开 / 收起指定目录路径。 */
    fun toggleExpanded(path: String) {
        _uiState.update {
            val next = if (it.expandedPaths.contains(path)) {
                it.expandedPaths - path
            } else {
                it.expandedPaths + path
            }
            it.copy(expandedPaths = next)
        }
    }

    // ------------------------------------------------------------------
    // 内容查看 / 保存
    // ------------------------------------------------------------------

    fun loadContent(path: String) {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(
                    errorMessage = "脚本模块后端未装配",
                    actionError = null,
                    actionMessage = null,
                )
            }
            return
        }
        _uiState.update {
            it.copy(
                selectedPath = path,
                loadingContent = true,
                errorMessage = null,
                actionError = null,
                actionMessage = null,
            )
        }
        viewModelScope.launch {
            try {
                val content = repo.getContent(path)
                _uiState.update {
                    it.copy(
                        selectedPath = path,
                        content = content,
                        loadingContent = false,
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        loadingContent = false,
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "脚本内容加载失败",
                    )
                }
            }
        }
    }

    fun saveContent(path: String, content: String, message: String = "保存") {
        val repo = repository ?: return
        viewModelScope.launch {
            try {
                repo.saveContent(path, content, message)
                _uiState.update {
                    it.copy(
                        content = ScriptContent(path = path, content = content),
                        actionMessage = "已保存：$path",
                        actionError = null,
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "保存脚本失败",
                        actionMessage = null,
                    )
                }
            }
        }
    }

    // ------------------------------------------------------------------
    // 运行 / 停止 / 删除
    // ------------------------------------------------------------------

    fun runScript(path: String) {
        val repo = repository ?: return
        _uiState.update { it.copy(runningPath = path, actionError = null, actionMessage = null) }
        viewModelScope.launch {
            try {
                val runId = repo.runScript(path)
                _uiState.update {
                    it.copy(
                        runningPath = null,
                        runId = runId,
                        actionMessage = "已启动：$path",
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        runningPath = null,
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "运行脚本失败",
                    )
                }
            }
        }
    }

    fun stopScript(runId: String) {
        val repo = repository ?: return
        if (runId.isBlank()) {
            _uiState.update { it.copy(actionError = "缺少运行 ID，无法停止") }
            return
        }
        viewModelScope.launch {
            try {
                repo.stopScript(runId)
                _uiState.update {
                    it.copy(
                        runId = null,
                        actionMessage = "已停止运行",
                        actionError = null,
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "停止脚本失败",
                    )
                }
            }
        }
    }

    fun deleteScript(path: String, isDirectory: Boolean) {
        val repo = repository ?: return
        _uiState.update {
            it.copy(
                deletingPath = path,
                actionError = null,
                actionMessage = null,
            )
        }
        viewModelScope.launch {
            try {
                repo.deleteScript(path, isDirectory)
                // 删除后重载树，保持列表一致。
                refreshTree()
                _uiState.update {
                    it.copy(
                        deletingPath = null,
                        actionMessage = "已删除：$path",
                    )
                }
            } catch (error: Exception) {
                _uiState.update {
                    it.copy(
                        deletingPath = null,
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "删除脚本失败",
                    )
                }
            }
        }
    }

    /** 清除瞬态的提示 / 错误信息。 */
    fun clearMessages() {
        _uiState.update {
            it.copy(actionError = null, actionMessage = null)
        }
    }
}
