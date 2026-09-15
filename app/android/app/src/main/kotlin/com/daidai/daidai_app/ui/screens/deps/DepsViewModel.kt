package com.daidai.daidai_app.ui.screens.deps

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.DepInstallRequest
import com.daidai.daidai_app.data.model.DepItem
import com.daidai.daidai_app.data.model.DepStatus
import com.daidai.daidai_app.data.model.DepMirrors
import com.daidai.daidai_app.data.model.SystemPackage
import com.daidai.daidai_app.data.repository.DepsRepository
import com.daidai.daidai_app.data.repository.boundedCapabilityLog
import android.content.ContentResolver
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class DepsUiState(
    val deps: List<DepItem> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    val operatingId: Long? = null,
    val submittingInstall: Boolean = false,
    val notice: String? = null,
    val selected: Set<Long> = emptySet(),
    val detail: DepStatus? = null,
    val log: String = "",
    val watching: Boolean = false,
    val actionBusy: Boolean = false,
    val mirrors: DepMirrors? = null,
    val systemPackages: List<SystemPackage> = emptyList(),
    val systemPhase: Phase = Phase.Loading,
    val searchQuery: String = "",
    val restoreCacheRequested: Boolean = false,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

class DepsViewModel(
    private val repository: DepsRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(DepsUiState())
    val uiState: StateFlow<DepsUiState> = _uiState.asStateFlow()

    private var refreshGeneration = 0
    private var systemRefreshGeneration = 0
    private var watchJob: Job? = null

    fun select(id: Long) {
        _uiState.update { it.copy(selected = if (id in it.selected) it.selected - id else it.selected + id) }
    }

    fun setSearchQuery(query: String) {
        _uiState.update { it.copy(searchQuery = query) }
    }

    fun closeLog() {
        watchJob?.cancel()
        _uiState.update { it.copy(detail = null, watching = false, log = "") }
    }

    fun watch(id: Long) {
        val repo = repository ?: return
        closeLog()
        _uiState.update { it.copy(detail = DepStatus(id, "", ""), watching = true, errorMessage = null) }
        watchJob = viewModelScope.launch {
            try {
                var latest = repo.status(id)
                _uiState.update { it.copy(detail = latest) }
                repo.logStream(id).collect { event ->
                    if (event.type != "done") _uiState.update { it.copy(log = boundedCapabilityLog(it.log, event.data + "\n")) }
                }
                do {
                    latest = repo.status(id)
                    _uiState.update { state -> state.copy(detail = latest, log = latest.log,
                        deps = state.deps.map { if (it.id == id) it.copy(status = latest.status) else it }) }
                    if (latest.active) delay(1500)
                } while (latest.active)
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = error.message ?: "日志读取失败") }
            } finally { _uiState.update { it.copy(watching = false) } }
        }
    }

    private fun action(block: suspend (DepsRepository) -> Unit) {
        val repo = repository ?: return
        if (_uiState.value.actionBusy) return
        _uiState.update { it.copy(actionBusy = true, errorMessage = null) }
        viewModelScope.launch {
            try { block(repo) }
            catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(errorMessage = error.message ?: "操作失败") }
            } finally { _uiState.update { it.copy(actionBusy = false) } }
        }
    }

    fun cancel(id: Long) = action { repo ->
        repo.cancel(id)
        _uiState.update { it.copy(notice = "取消请求已提交") }
        watch(id)
    }

    fun batch(delete: Boolean) {
        val ids = _uiState.value.selected.toSet()
        if (ids.isEmpty()) return
        action { repo ->
            if (delete) repo.batchDelete(ids) else repo.batchReinstall(ids)
            _uiState.update { it.copy(selected = emptySet(), notice = "批量请求已提交，请以刷新后的状态为准") }
            refresh()
        }
    }

    fun loadMirrors() = action { repo -> _uiState.update { it.copy(mirrors = repo.mirrors()) } }
    fun closeMirrors() { _uiState.update { it.copy(mirrors = null) } }
    fun saveMirrors(value: DepMirrors) = action { repo ->
        repo.saveMirrors(value)
        _uiState.update { it.copy(mirrors = null, notice = "镜像源已保存") }
    }

    fun export(resolver: ContentResolver, uri: Uri, type: String, version: String) = action { repo ->
        withContext(Dispatchers.IO) {
            (resolver.openOutputStream(uri, "wt") ?: error("无法打开导出文件")).use { repo.export(type, version, it) }
        }
        _uiState.update { it.copy(notice = "依赖清单已导出") }
    }

    init {
        refresh()
        listSystemPackages()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update { it.copy(phase = DepsUiState.Phase.Error, errorMessage = "Deps 后端未装配") }
            return
        }
        val generation = ++refreshGeneration
        _uiState.update { it.copy(phase = DepsUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val items = repo.list()
                if (generation != refreshGeneration) return@launch
                _uiState.update { it.copy(deps = items, selected = it.selected.intersect(items.map { item -> item.id }.toSet()), phase = DepsUiState.Phase.Loaded, errorMessage = null) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                if (generation != refreshGeneration) return@launch
                _uiState.update { it.copy(phase = DepsUiState.Phase.Error, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "加载依赖失败，请重试") }
            }
        }
    }

    fun listSystemPackages() {
        val repo = repository ?: run {
            _uiState.update { it.copy(systemPhase = DepsUiState.Phase.Error, errorMessage = "Deps 后端未装配") }
            return
        }
        val generation = ++systemRefreshGeneration
        _uiState.update { it.copy(systemPhase = DepsUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val items = repo.listSystemPackages()
                if (generation != systemRefreshGeneration) return@launch
                _uiState.update { it.copy(systemPackages = items, systemPhase = DepsUiState.Phase.Loaded, errorMessage = null) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                if (generation != systemRefreshGeneration) return@launch
                _uiState.update { it.copy(systemPhase = DepsUiState.Phase.Error, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "加载系统包失败") }
            }
        }
    }

    fun installSystemPackages(packages: List<String>) {
        if (packages.isEmpty()) return
        action { repo ->
            repo.installSystemPackages(packages)
            _uiState.update { it.copy(notice = "安装系统包请求已提交") }
            listSystemPackages()
        }
    }

    fun uninstallSystemPackage(name: String) {
        action { repo ->
            repo.uninstallSystemPackage(name)
            _uiState.update { it.copy(notice = "卸载系统包请求已提交") }
            listSystemPackages()
        }
    }

    fun restoreCache() {
        action { repo ->
            repo.restoreCache()
            _uiState.update { it.copy(restoreCacheRequested = true, notice = "缓存恢复已提交") }
            listSystemPackages()
        }
    }

    fun uninstall(id: Long) {
        if (_uiState.value.operatingId != null) return
        val repo = repository ?: run { _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }; return }
        _uiState.update { it.copy(operatingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.uninstall(id)
                _uiState.update { it.copy(operatingId = null, notice = "卸载请求已提交") }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(operatingId = null, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "卸载失败，请重试") }
            }
        }
    }

    fun reinstall(id: Long) {
        if (_uiState.value.operatingId != null) return
        val repo = repository ?: run { _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }; return }
        _uiState.update { it.copy(operatingId = id, errorMessage = null) }
        viewModelScope.launch {
            try {
                repo.reinstall(id)
                _uiState.update { it.copy(operatingId = null, notice = "重装请求已提交") }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(operatingId = null, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "重装失败，请重试") }
            }
        }
    }

    fun install(request: DepInstallRequest): Boolean {
        if (_uiState.value.submittingInstall) return false
        if (!request.isValid) { _uiState.update { it.copy(errorMessage = "包名不能为空") }; return false }
        val repo = repository ?: run { _uiState.update { it.copy(errorMessage = "Deps 后端未装配") }; return false }
        _uiState.update { it.copy(submittingInstall = true, errorMessage = null, notice = null) }
        viewModelScope.launch {
            try {
                repo.install(request)
                _uiState.update { it.copy(submittingInstall = false, notice = "安装请求已提交") }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(submittingInstall = false, errorMessage = error.message?.takeIf(String::isNotBlank) ?: "安装失败，请重试") }
            }
        }
        return true
    }

    fun consumeNotice() { _uiState.update { it.copy(notice = null) } }
    fun clearError() { _uiState.update { it.copy(errorMessage = null) } }
}
