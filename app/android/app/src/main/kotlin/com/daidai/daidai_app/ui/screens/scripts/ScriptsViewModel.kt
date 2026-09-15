package com.daidai.daidai_app.ui.screens.scripts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.ScriptContent
import com.daidai.daidai_app.data.model.ScriptFile
import com.daidai.daidai_app.data.model.ScriptRunLogPage
import com.daidai.daidai_app.data.model.ScriptRunRecord
import com.daidai.daidai_app.data.model.ScriptVersion
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.repository.ScriptsRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.time.Instant

/** 脚本页 UI 状态：目录树 / 内容视图 / 交互操作 / C2 运行历史与版本。 */
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
    // C2：运行历史（本地条目 + 服务端实时状态合并）
    val runHistory: List<ScriptRunListItem> = emptyList(),
    val loadingRunHistory: Boolean = false,
    // C2：某次运行的日志查看
    val viewingRunId: String? = null,
    val viewingRunLogs: List<String> = emptyList(),
    val viewingRunPage: ScriptRunLogPage? = null,
    // C2：版本历史 / 版本详情
    val versionsPath: String? = null,
    val versions: List<ScriptVersion> = emptyList(),
    val loadingVersions: Boolean = false,
    val viewingVersion: ScriptVersion? = null,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/** 运行历史列表项：本地记录 + 最近一次拉取到的服务端状态（null=服务端已无此记录）。 */
data class ScriptRunListItem(
    val record: ScriptRunRecord = ScriptRunRecord(),
    val page: ScriptRunLogPage? = null,
)

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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
                if (!runId.isNullOrBlank()) {
                    addCurrentRunToHistory(path, runId)
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
                if (error is kotlinx.coroutines.CancellationException) throw error
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
            it.copy(actionError = null, actionMessage = null, viewingVersion = null, viewingRunId = null)
        }
    }

    // ------------------------------------------------------------------
    // C2：运行历史 / 删除记录 / 日志查看 / 版本历史
    // ------------------------------------------------------------------

    fun setHistoryStore(store: ScriptRunHistoryStore?) {
        this.historyStore = store
        if (store == null) {
            _uiState.update { it.copy(runHistory = emptyList()) }
        } else {
            refreshRunHistory()
        }
    }

    fun refreshRunHistory() {
        val repo = repository ?: return
        val store = historyStore ?: run {
            _uiState.update { it.copy(runHistory = emptyList()) }
            return
        }
        _uiState.update { it.copy(loadingRunHistory = true) }
        viewModelScope.launch {
            try {
                val records = store.list()
                val refreshed = records.map { r ->
                    ScriptRunListItem(record = r, page = runCatching { repo.getRunLogs(r.runId, Long.MAX_VALUE) }.getOrNull())
                }
                _uiState.update {
                    it.copy(runHistory = refreshed, loadingRunHistory = false)
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update {
                    it.copy(loadingRunHistory = false, actionError = error.message?.takeIf(String::isNotBlank) ?: "运行历史刷新失败")
                }
            }
        }
    }

    fun openRunLogs(runId: String) {
        val repo = repository ?: return
        runLogsPollJob?.cancel()
        _uiState.update {
            it.copy(viewingRunId = runId, viewingRunLogs = emptyList(), viewingRunPage = null)
        }
        viewModelScope.launch {
            try {
                val page = repo.getRunLogs(runId, 0) ?: return@launch
                _uiState.update {
                    it.copy(viewingRunLogs = page.logs, viewingRunPage = page)
                }
                if (!page.isFinished) {
                    runLogsPollJob = launch { pollRunLogs(repo, runId, page.cursor, page.logs) }
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(actionError = error.message ?: "日志加载失败") }
            }
        }
    }

    /** 关闭日志查看页（清理轮询）。 */
    fun closeRunLogs() {
        runLogsPollJob?.cancel()
        runLogsPollJob = null
        _uiState.update { it.copy(viewingRunId = null, viewingRunLogs = emptyList(), viewingRunPage = null) }
    }

    fun deleteRunRecord(runId: String) {
        val repo = repository ?: return
        _uiState.update { it.copy(actionError = null) }
        viewModelScope.launch {
            try {
                repo.clearRun(runId)
                runLogsPollJob?.cancel()
                runLogsPollJob = null
                if (_uiState.value.viewingRunId == runId) {
                    _uiState.update { it.copy(viewingRunId = null, viewingRunLogs = emptyList()) }
                }
                historyStore?.remove(runId)
                refreshRunHistory()
                _uiState.update {
                    it.copy(actionMessage = "已删除运行记录：$runId")
                }
            } catch (error: PanelApiException) {
                if (error.statusCode == 409) {
                    val stop = runCatching { repo.stopScript(runId) }.isSuccess
                    if (stop) {
                        runCatching { repo.clearRun(runId) }
                    }
                    runLogsPollJob?.cancel()
                    runLogsPollJob = null
                    if (_uiState.value.viewingRunId == runId) {
                        _uiState.update { it.copy(viewingRunId = null, viewingRunLogs = emptyList()) }
                    }
                    historyStore?.remove(runId)
                    refreshRunHistory()
                    _uiState.update { it.copy(actionMessage = "已停止并删除运行记录") }
                } else {
                    _uiState.update { it.copy(actionError = error.serverMessage ?: "删除运行记录失败") }
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(actionError = error.message ?: "删除运行记录失败") }
            }
        }
    }

    fun loadVersions(path: String) {
        val repo = repository ?: return
        _uiState.update {
            it.copy(versionsPath = path, versions = emptyList(), loadingVersions = true, actionError = null)
        }
        viewModelScope.launch {
            try {
                val versions = repo.listVersions(path)
                _uiState.update { it.copy(versions = versions, loadingVersions = false) }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(loadingVersions = false, actionError = error.message?.takeIf(String::isNotBlank) ?: "版本历史加载失败") }
            }
        }
    }

    fun openVersion(versionId: Long) {
        val repo = repository ?: return
        viewModelScope.launch {
            try {
                val v = repo.getVersion(versionId)
                _uiState.update { it.copy(viewingVersion = v) }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(actionError = error.message?.takeIf(String::isNotBlank) ?: "版本详情加载失败") }
            }
        }
    }

    fun rollbackVersion(versionId: Long) {
        val repo = repository ?: return
        val path = _uiState.value.versionsPath ?: _uiState.value.selectedPath ?: return
        _uiState.update { it.copy(actionError = null) }
        viewModelScope.launch {
            try {
                val newVersion = repo.rollbackVersion(versionId)
                _uiState.update {
                    it.copy(
                        actionMessage = "已回滚到 v$newVersion",
                        viewingVersion = null,
                        versions = emptyList(),
                    )
                }
                if (_uiState.value.selectedPath == path) {
                    loadContent(path)
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(actionError = error.message?.takeIf(String::isNotBlank) ?: "回滚失败") }
            }
        }
    }

    fun clearVersions(path: String) {
        val repo = repository ?: return
        _uiState.update { it.copy(actionError = null) }
        viewModelScope.launch {
            try {
                val count = repo.clearVersions(path)
                _uiState.update {
                    it.copy(
                        versions = emptyList(),
                        actionMessage = "版本历史已清空（$count 条）",
                        viewingVersion = if (it.viewingVersion?.scriptPath == path) null else it.viewingVersion,
                    )
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(actionError = error.message?.takeIf(String::isNotBlank) ?: "清空版本历史失败") }
            }
        }
    }

    fun addCurrentRunToHistory(path: String, runId: String) {
        val repo = repository ?: return
        val store = historyStore ?: return
        val existing = _uiState.value.runHistory.firstOrNull { it.record.runId == runId }
        if (existing != null) return
        val record = ScriptRunRecord(runId = runId, path = path, startedAt = Instant.now().toString())
        store.record(record)
        refreshRunHistory()
    }

    // ------------------------------------------------------------------

    private suspend fun pollRunLogs(
        repo: ScriptsRepository,
        runId: String,
        initialCursor: Long,
        initialLogs: List<String>,
    ) {
        var page: ScriptRunLogPage? = null
        var cursor = initialCursor
        val logs = ArrayList(initialLogs)
        try {
            while (true) {
                delay(POLL_INTERVAL_MS)
                page = repo.getRunLogs(runId, cursor) ?: break
                if (page.logs.isNotEmpty()) {
                    logs += page.logs
                    _uiState.update { state ->
                        if (state.viewingRunId == runId) {
                            state.copy(viewingRunLogs = logs.toList(), viewingRunPage = page)
                        } else {
                            state
                        }
                    }
                }
                cursor = page.cursor
                if (page.isFinished) break
            }
            val done = page
            if (done != null) {
                _uiState.update { state ->
                    state.copy(runHistory = state.runHistory.map {
                        if (it.record.runId == runId) it.copy(page = done) else it
                    })
                }
            }
        } catch (error: CancellationException) {
            throw error
        } catch (_: Exception) {
            // 轮询失败静默；历史列表刷新时可再拉。
        }
    }

    // ------------------------------------------------------------------

    private var historyStore: ScriptRunHistoryStore? = null
    private var runLogsPollJob: Job? = null

    override fun onCleared() {
        super.onCleared()
        runLogsPollJob?.cancel()
    }

    private companion object {
        const val POLL_INTERVAL_MS = 1000L
    }
}

/**
 * 客户端脚本运行历史持久化（SharedPreferences，上限 100 条）。
 * 用于：服务端无 run 列表端点，通过本端初始化记录 + 服务端日志拉取聚合状态。
 */
class ScriptRunHistoryStore(context: android.content.Context) {

    private val prefs = context.applicationContext.getSharedPreferences(PREF_NAME, android.content.Context.MODE_PRIVATE)

    fun list(): List<ScriptRunRecord> {
        val raw = prefs.getString(KEY_ENTRIES, "[]") ?: "[]"
        val arr = org.json.JSONArray(raw)
        val result = ArrayList<ScriptRunRecord>(arr.length())
        for (i in 0 until arr.length()) {
            val obj = arr.optJSONObject(i)
            if (obj != null) result += ScriptRunRecord.fromJson(obj)
        }
        return result.take(MAX_ENTRIES)
    }

    fun record(entry: ScriptRunRecord) {
        val existing = list().toMutableList()
        existing.removeAll { it.runId == entry.runId }
        existing.add(0, entry)
        persist(existing.take(MAX_ENTRIES))
    }

    fun remove(runId: String) {
        val updated = list().filterNot { it.runId == runId }
        persist(updated)
    }

    fun clear() {
        prefs.edit().putString(KEY_ENTRIES, "[]").apply()
    }

    private fun persist(items: List<ScriptRunRecord>) {
        val arr = org.json.JSONArray()
        items.take(MAX_ENTRIES).forEach { arr.put(it.toJson()) }
        prefs.edit().putString(KEY_ENTRIES, arr.toString()).apply()
    }

    private companion object {
        const val PREF_NAME = "daidai_script_run_history"
        const val KEY_ENTRIES = "entries"
        const val MAX_ENTRIES = 100
    }


    /** 重命名脚本或目录 */
    fun renameScript(oldPath: String, newPath: String) {
        viewModelScope.launch {
            safeRun {
                repository.renameScript(oldPath, newPath)
                refreshTree()
            }
        }
    }

    /** 复制脚本或目录 */
    fun copyScript(sourcePath: String, targetPath: String) {
        viewModelScope.launch {
            safeRun {
                repository.copyScript(sourcePath, targetPath)
                refreshTree()
            }
        }
    }

    /** 上传脚本文件 */
    fun uploadScript(dirPath: String, fileName: String, content: String) {
        viewModelScope.launch {
            safeRun {
                repository.uploadScript(dirPath, fileName, content)
                refreshTree()
            }
        }
    }

    /** 下载脚本内容 */
    fun downloadScript(path: String) {
        viewModelScope.launch {
            safeRun {
                val content = repository.downloadScript(path)
                _downloadResult.emit(content)
            }
        }
    }

    /** 带参数运行脚本 */
    fun runScriptWithParams(path: String, params: Map<String, String>) {
        viewModelScope.launch {
            safeRun {
                val runId = repository.runScriptWithParams(path, params)
                if (runId != null) {
                    _runId.emit(runId)
                }
            }
        }
    }

}