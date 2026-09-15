package com.daidai.daidai_app.ui.screens.envs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.model.QlEnvItem
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
    /** 新建/更新成功的一次性标记，表单页消费后经 [consumeMutation] 复位。 */
    val mutationSucceeded: Boolean = false,
    /** 正在被启停/删除的 env id，用于展示行内忙碌态；null 表示无进行中的操作。 */
    val busyId: Long? = null,
    val busyType: BusyType = BusyType.None,
    /** 全部分组（含默认组「默认分组」），用于分组筛选/管理/表单下拉。 */
    val groups: List<String> = emptyList(),
    /** 当前分组筛选；null 表示全部，空串表示默认分组（未分组）。 */
    val selectedGroup: String? = null,
    /** 批量导入结果提示；消费后经 [consumeImportResult] 复位。 */
    val importResult: EnvImportFeedback? = null,
    /** 导出文本提示（复制到剪贴板后由页面消费）。 */
    val exportResult: EnvExportFeedback? = null,
) {
    enum class BusyType { None, Toggle, Delete }
}

/** 排序操作方向：置顶 / 上移 / 下移。 */
enum class MoveDirection { Top, Up, Down }

/** 批量导入反馈。 */
data class EnvImportFeedback(
    val success: Int,
    val skipped: Int,
    val errors: List<String>,
) {
    val total: Int get() = success + skipped
}

/** 批量导出反馈。 */
data class EnvExportFeedback(
    val text: String,
    val count: Int,
    val skippedNames: List<String>,
)

/**
 * 环境变量的 ViewModel：拉取列表 + 新建 / 编辑 / 删除 / 启停 / 分组管理 /
 * 排序置顶 / 批量导入导出。
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

    /** 重新拉取列表与分组。 */
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
                val groups = runCatching { repo.listGroups() }.getOrDefault(emptyList())
                _uiState.update {
                    it.copy(
                        envs = envs,
                        groups = groups,
                        phase = EnvListPhase.Loaded,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
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
    fun create(name: String, value: String, remark: String, groups: List<String> = emptyList()) {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(errorMessage = "环境变量后端未装配")
            }
            return
        }
        viewModelScope.launch {
            try {
                repo.createWithGroups(name = name, value = value, remark = remark, groups = groups)
                refresh()
                _uiState.update { it.copy(mutationSucceeded = true) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
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
    fun update(
        id: Long,
        name: String,
        value: String,
        remark: String,
        enabled: Boolean,
        groups: List<String> = emptyList(),
    ) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        viewModelScope.launch {
            try {
                repo.update(id = id, name = name, value = value, remark = remark, enabled = enabled, groups = groups)
                refresh()
                _uiState.update { it.copy(mutationSucceeded = true) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "更新环境变量失败，请重试",
                    )
                }
            }
        }
    }

    /** 表单页消费成功标记后复位。 */
    fun consumeMutation() {
        _uiState.update { it.copy(mutationSucceeded = false) }
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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

    /** 选择分组筛选；null 表示全部，空串表示默认分组。 */
    fun selectGroup(group: String?) {
        _uiState.update { it.copy(selectedGroup = group) }
    }

    /** 更新指定环境变量的分组；成功后刷新列表。 */
    fun assignGroups(id: Long, groups: List<String>) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        viewModelScope.launch {
            try {
                repo.setGroups(id, groups)
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "更新分组失败，请重试",
                    )
                }
            }
        }
    }

    /** 上移 / 下移 / 置顶：重排列表后按新顺序逐项写回 `/envs/sort`（sort_order = 新下标）。
     *
     * 后端 `sortEnvs` 把 source 的 sort_order 原样设为 target_id，且列表排序为
     * `ORDER BY sort_order ASC, id DESC`；若只改单项在 sort_order 相同（默认全 0）时
     * 会被 id 决胜淹没。因此改为整表重排：n 很小（环境变量数量级），每次移动调用 n 次；
     * 结果确定且持久化。 */
    fun move(targetId: Long, direction: MoveDirection) {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        if (_uiState.value.busyId != null) return
        val current = _uiState.value.envs
        val index = current.indexOfFirst { it.id == targetId }
        if (index < 0) return
        val newIndex = when (direction) {
            MoveDirection.Top -> 0
            MoveDirection.Up -> if (index > 0) index - 1 else return
            MoveDirection.Down -> if (index < current.size - 1) index + 1 else return
        }
        if (newIndex == index) return
        val reordered = current.toMutableList().apply { add(newIndex, removeAt(index)) }
        val moves = reordered.mapIndexed { position, env -> env.id to position.toLong() }
        _uiState.update { it.copy(busyId = targetId, busyType = EnvsUiState.BusyType.Toggle) }
        viewModelScope.launch {
            try {
                for ((id, order) in moves) {
                    repo.sort(sourceId = id, targetId = order)
                }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "调整排序失败，请重试",
                    )
                }
            } finally {
                _uiState.update { it.copy(busyId = null, busyType = EnvsUiState.BusyType.None) }
            }
        }
    }

    /** 批量导入：解析后的条目调 /envs/import（merge 模式），成功后刷新。 */
    fun importEnvItems(items: List<QlEnvItem>, mode: String = "merge") {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        viewModelScope.launch {
            try {
                val result = repo.import(items, mode)
                refresh()
                _uiState.update {
                    it.copy(
                        importResult = EnvImportFeedback(
                            success = result.imported,
                            skipped = result.skipped,
                            errors = result.errors,
                        ),
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "批量导入失败，请重试",
                    )
                }
            }
        }
    }

    /** 消费批量导入结果提示。 */
    fun consumeImportResult() {
        _uiState.update { it.copy(importResult = null) }
    }

    /** 批量导出：把全部 env 序列化为青龙文本，交由页面复制/分享。 */
    fun exportEnvText() {
        val repo = repository ?: run {
            _uiState.update { it.copy(errorMessage = "环境变量后端未装配") }
            return
        }
        viewModelScope.launch {
            try {
                val envs = repo.list()
                val result = com.daidai.daidai_app.data.model.toQlExportText(envs)
                _uiState.update {
                    it.copy(
                        exportResult = EnvExportFeedback(
                            text = result.text,
                            count = envs.size,
                            skippedNames = result.skippedNames,
                        ),
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "导出失败，请重试",
                    )
                }
            }
        }
    }

    /** 消费导出结果提示。 */
    fun consumeExportResult() {
        _uiState.update { it.copy(exportResult = null) }
    }
}
