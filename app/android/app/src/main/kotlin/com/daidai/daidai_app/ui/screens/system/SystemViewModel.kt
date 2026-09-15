package com.daidai.daidai_app.ui.screens.system

import android.net.Uri
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.BackupRecord
import com.daidai.daidai_app.data.model.BackupSchedule
import com.daidai.daidai_app.data.model.HealthCheckResult
import com.daidai.daidai_app.data.model.RestoreProgress
import com.daidai.daidai_app.data.repository.BackupRepository
import com.daidai.daidai_app.data.repository.SystemRepository
import com.daidai.daidai_app.data.repository.SystemRepository.AppInfo
import com.daidai.daidai_app.data.repository.SshKey
import com.daidai.daidai_app.data.model.PlatformTokenInfo
import com.daidai.daidai_app.data.model.User
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.io.File

/** 系统管理（备份 + 健康检查）的可观察状态。 */
data class SystemUiState(
    val backups: List<BackupRecord> = emptyList(),
    val backupPhase: BackupPhase = BackupPhase.Loading,
    val errorMessage: String? = null,
    /** 是否正在创建新备份（禁用重复点击）。 */
    val creatingBackup: Boolean = false,
    /** 正在删除的备份名（用于按钮级 loading 提示），null 表示无删除进行中。 */
    val deletingBackupName: String? = null,
    /** 最近一次 创建/删除 操作的反馈文案（成功后展示）。 */
    val actionMessage: String? = null,
    /** 健康检查结果，未检查时为 null。 */
    val health: HealthCheckResult? = null,
    /** 健康检查是否运行中。 */
    val healthRunning: Boolean = false,
    /** SAF 导入备份：上传中标记与文件名。 */
    val uploading: Boolean = false,
    val uploadFileName: String = "",
    /** 上传进度 0..1；null 表示总量未知（不确定进度）。 */
    val uploadProgress: Float? = null,
    /** SAF 导出备份：下载中标记与目标缓存文件（供 CreateDocument 选择器接续）。 */
    val downloadingName: String? = null,
    val downloadProgress: Float? = null,
    val pendingDownloadFile: File? = null,
    val pendingDownloadName: String = "",
    /** 恢复进行中（危险操作，UI 先二次确认）。 */
    val restoring: Boolean = false,
    /** 最近一次恢复进度快照。 */
    val restore: RestoreProgress? = null,
    /** 当前定时备份计划配置（由 loadBackupSchedule 加载）。 */
    val backupSchedule: BackupSchedule? = null,
    /** 是否正在加载定时备份计划。 */
    val scheduleLoading: Boolean = false,
    /** 是否正在保存定时备份计划。 */
    val scheduleSaving: Boolean = false,
) {
    enum class BackupPhase {
        Loading,
        Loaded,
        Error,
    }
    val users: List<User> = emptyList(),
    val usersLoading: Boolean = false,
    val usersErrorMessage: String? = null,
    val sshKeys: List<SshKey> = emptyList(),
    val sshKeysLoading: Boolean = false,
    val sshKeysErrorMessage: String? = null,
    val platformTokens: List<PlatformTokenInfo> = emptyList(),
    val platformTokensLoading: Boolean = false,
    val platformTokensErrorMessage: String? = null,
    val apps: List<AppInfo> = emptyList(),
    val appsLoading: Boolean = false,
    val appsErrorMessage: String? = null,
    val smtpConfig: Map<String, Any> = emptyMap(),
    val smtpTesting: Boolean = false,
    val smtpTestResult: String? = null,

}

/**
 * 阶段 4-5 系统管理 ViewModel：通过 [BackupRepository] 提供
 * 备份列表 / 新建 / 删除 以及健康检查的读写能力，暴露为 [StateFlow]。
 *
 * 备份读取走 [BackupRepository.listBackups]；写操作（创建/删除）在完成后自动刷新列表。
 */
class SystemViewModel(
    private val repository: BackupRepository,
    private val systemRepository: SystemRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(SystemUiState())
    val uiState: StateFlow<SystemUiState> = _uiState.asStateFlow()

    init {
        refreshBackups()
    }

    /** 刷新备份列表。 */
    fun refreshBackups() {
        viewModelScope.launch {
            _uiState.update { it.copy(backupPhase = SystemUiState.BackupPhase.Loading, errorMessage = null) }
            try {
                val backups = repository.listBackups()
                _uiState.update {
                    it.copy(backups = backups, backupPhase = SystemUiState.BackupPhase.Loaded)
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        backupPhase = SystemUiState.BackupPhase.Error,
                        errorMessage = friendly(error, "加载备份列表失败，请重试"),
                    )
                }
            }
        }
    }

    /** 新建备份（type 为备份类型："full" 全量或 "incremental" 增量）。成功后自动刷新列表。 */
    fun createBackup(name: String? = null, type: String = "full") {
        if (_uiState.value.creatingBackup) return
        viewModelScope.launch {
            _uiState.update { it.copy(creatingBackup = true, errorMessage = null) }
            try {
                val result = repository.createBackup(name = name, type = type)
                _uiState.update {
                    it.copy(
                        creatingBackup = false,
                        actionMessage = result.message.ifBlank { "备份创建成功" },
                    )
                }
                refreshBackups()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        creatingBackup = false,
                        errorMessage = friendly(error, "创建备份失败"),
                    )
                }
            }
        }
    }

    /** 加载定时备份计划配置。 */
    fun loadBackupSchedule() {
        if (_uiState.value.scheduleLoading) return
        viewModelScope.launch {
            _uiState.update { it.copy(scheduleLoading = true) }
            try {
                val schedule = repository.getBackupSchedule()
                _uiState.update { it.copy(backupSchedule = schedule, scheduleLoading = false) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update { it.copy(scheduleLoading = false) }
            }
        }
    }

    /** 保存定时备份计划配置。 */
    fun saveBackupSchedule(frequency: String, time: String, enabled: Boolean) {
        if (_uiState.value.scheduleSaving) return
        viewModelScope.launch {
            _uiState.update { it.copy(scheduleSaving = true, errorMessage = null) }
            try {
                val result = repository.setBackupSchedule(frequency, time, enabled)
                _uiState.update {
                    it.copy(
                        scheduleSaving = false,
                        actionMessage = result.message.ifBlank { "定时备份设置已保存" },
                    )
                }
                loadBackupSchedule()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        scheduleSaving = false,
                        errorMessage = friendly(error, "保存定时备份设置失败"),
                    )
                }
            }
        }
    }

    /** 删除指定备份。成功后自动刷新列表。 */
    fun deleteBackup(record: BackupRecord) {
        if (_uiState.value.deletingBackupName != null) return
        viewModelScope.launch {
            _uiState.update { it.copy(deletingBackupName = record.name, errorMessage = null) }
            try {
                val result = repository.deleteBackup(record.name)
                _uiState.update {
                    it.copy(
                        deletingBackupName = null,
                        actionMessage = result.message.ifBlank { "已删除 ${record.name}" },
                    )
                }
                refreshBackups()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        deletingBackupName = null,
                        errorMessage = friendly(error, "删除备份失败"),
                    )
                }
            }
        }
    }

    /** 立即重新执行一次健康检查。 */
    fun runHealthCheck() {
        if (_uiState.value.healthRunning) return
        viewModelScope.launch {
            _uiState.update { it.copy(healthRunning = true, errorMessage = null) }
            try {
                val health = repository.runHealthCheck()
                _uiState.update { it.copy(health = health, healthRunning = false) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(healthRunning = false, errorMessage = friendly(error, "健康检查失败"))
                }
            }
        }
    }

    /** 读取最近一次健康检查快照（页面加载时）。 */
    fun loadHealthSnapshot() {
        if (_uiState.value.healthRunning || _uiState.value.health != null) return
        viewModelScope.launch {
            try {
                val health = repository.healthCheckSnapshot()
                _uiState.update { it.copy(health = health) }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                // 快照缺失时静默，允许用户点击“立即检查”
                _uiState.update { it.copy(health = null) }
            }
        }
    }

    /** 清除一次性反馈文案。 */
    fun consumeActionMessage() {
        _uiState.update { it.copy(actionMessage = null) }
    }

    // ---- C8 备份上传 / 下载 / 恢复 ----

    /**
     * 从 SAF [uri] 上传备份。
     * 服务端限制 512MB；先复制到缓存文件再 multipart 上传，带进度回调。
     */
    fun uploadBackupFromUri(uri: Uri, displayName: String? = null) {
        if (_uiState.value.uploading) return
        viewModelScope.launch {
            _uiState.update {
                it.copy(
                    uploading = true,
                    uploadFileName = displayName.orEmpty(),
                    uploadProgress = 0f,
                    errorMessage = null,
                )
            }
            try {
                val staged = repository.stageUriToCache(uri, displayName)
                _uiState.update { it.copy(uploadFileName = staged.name) }
                val result = repository.uploadBackup(staged) { written, total ->
                    val progress = if (total > 0) (written.toFloat() / total).coerceIn(0f, 1f) else null
                    _uiState.update { it.copy(uploadProgress = progress) }
                }
                _uiState.update {
                    it.copy(
                        uploading = false,
                        uploadProgress = null,
                        actionMessage = result.message.ifBlank { "备份上传成功" },
                    )
                }
                refreshBackups()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        uploading = false,
                        uploadProgress = null,
                        errorMessage = friendly(error, "上传备份失败"),
                    )
                }
            }
        }
    }

    /**
     * 下载备份到缓存文件。完成后把 [SystemUiState.pendingDownloadFile] 交给 UI，
     * UI 通过 SAF CreateDocument 选择器把文件转存到用户指定位置。
     */
    fun downloadBackup(record: BackupRecord) {
        if (_uiState.value.downloadingName != null) return
        viewModelScope.launch {
            _uiState.update {
                it.copy(
                    downloadingName = record.name,
                    downloadProgress = 0f,
                    errorMessage = null,
                )
            }
            try {
                val target = File(
                    repository.downloadCacheDir(),
                    "${System.currentTimeMillis()}-${record.name.substringAfterLast('/')}",
                )
                repository.downloadBackup(record.name, target) { written, total ->
                    val progress = if (total > 0) (written.toFloat() / total).coerceIn(0f, 1f) else null
                    _uiState.update { it.copy(downloadProgress = progress) }
                }
                _uiState.update {
                    it.copy(
                        downloadingName = null,
                        downloadProgress = null,
                        pendingDownloadFile = target,
                        pendingDownloadName = record.name,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        downloadingName = null,
                        downloadProgress = null,
                        errorMessage = friendly(error, "下载备份失败"),
                    )
                }
            }
        }
    }

    /** UI 通过 SAF 选择器拿到目标 [uri] 后调用，把缓存文件转存并清理。 */
    fun finishDownload(target: Uri) {
        val file = _uiState.value.pendingDownloadFile ?: return
        val name = _uiState.value.pendingDownloadName
        viewModelScope.launch {
            val ok = repository.copyFileToUri(file, target)
            _uiState.update {
                it.copy(
                    pendingDownloadFile = null,
                    pendingDownloadName = "",
                    actionMessage = if (ok) "已保存备份「$name」" else null,
                    errorMessage = if (ok) null else "保存到所选位置失败",
                )
            }
            runCatching { file.delete() }
        }
    }

    /** 用户取消 SAF 保存对话框时清理。 */
    fun cancelPendingDownload() {
        val file = _uiState.value.pendingDownloadFile
        _uiState.update { it.copy(pendingDownloadFile = null, pendingDownloadName = "") }
        runCatching { file?.delete() }
    }

    /**
     * 恢复备份（危险操作，UI 必须先弹二次确认）。
     * 后端 409 表示仍有活跃任务；恢复期间轮询进度。
     */
    private var restorePollJob: Job? = null

    fun restoreBackup(record: BackupRecord, password: String = "") {
        if (_uiState.value.restoring) return
        viewModelScope.launch {
            _uiState.update {
                it.copy(
                    restoring = true,
                    errorMessage = null,
                    restore = RestoreProgress(active = true, status = "running", filename = record.name, stage = "requesting"),
                )
            }
            restorePollJob = launch { pollRestoreProgress() }
            try {
                val result = repository.restoreBackup(record.name, password)
                _uiState.update {
                    it.copy(
                        restoring = false,
                        actionMessage = result.message.ifBlank { "恢复完成" },
                        restore = it.restore?.copy(active = false, status = "completed", percent = 100),
                    )
                }
                refreshBackups()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        restoring = false,
                        errorMessage = friendly(error, "恢复失败"),
                        restore = it.restore?.copy(active = false, status = "failed", error = error.message.orEmpty()),
                    )
                }
            } finally {
                restorePollJob?.cancel()
                restorePollJob = null
            }
        }
    }

    private suspend fun pollRestoreProgress() {
        val deadline = System.currentTimeMillis() + 120_000L
        while (System.currentTimeMillis() < deadline) {
            val progress = runCatching { repository.restoreProgress() }.getOrNull()
            if (progress != null) {
                _uiState.update { it.copy(restore = progress) }
                if (progress.finished) return
            }
            delay(1000)
        }
    }

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf(String::isNotBlank) ?: fallback
}

    private val systemRepository: SystemRepository by lazy {
        SystemRepository(application)
    }

    fun loadUsers() = viewModelScope.launch {
        _uiState.update { it.copy(loading = true, error = null) }
        runCatching {
            systemRepository.loadUsers()
        }.onSuccess {
            loadUsersState.value = it
        }.onFailure {
            _uiState.update { state -> state.copy(loading = false, error = it.localizedMessage ?: "加载用户失败") }
        }
    }

    fun addUser(username: String, password: String, role: String) = viewModelScope.launch {
        runCatching {
            systemRepository.addUser(username, password, role)
        }.onSuccess {
            loadUsers()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "添加用户失败") }
        }
    }

    fun deleteUser(userId: Long) = viewModelScope.launch {
        runCatching {
            systemRepository.deleteUser(userId)
        }.onSuccess {
            loadUsers()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "删除用户失败") }
        }
    }

    fun loadSshKeys() = viewModelScope.launch {
        runCatching {
            systemRepository.loadSshKeys()
        }.onSuccess {
            _sshKeysState.value = it
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "加载 SSH 密钥失败") }
        }
    }

    fun addSshKey(name: String, key: String) = viewModelScope.launch {
        runCatching {
            systemRepository.addSshKey(name, key)
        }.onSuccess {
            loadSshKeys()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "添加 SSH 密钥失败") }
        }
    }

    fun deleteSshKey(keyId: Long) = viewModelScope.launch {
        runCatching {
            systemRepository.deleteSshKey(keyId)
        }.onSuccess {
            loadSshKeys()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "删除 SSH 密钥失败") }
        }
    }

    fun loadPlatformTokens() = viewModelScope.launch {
        runCatching {
            systemRepository.loadPlatformTokens()
        }.onSuccess {
            _platformTokensState.value = it
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "加载平台令牌失败") }
        }
    }

    fun addPlatformToken(name: String, token: String) = viewModelScope.launch {
        runCatching {
            systemRepository.addPlatformToken(name, token)
        }.onSuccess {
            loadPlatformTokens()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "添加平台令牌失败") }
        }
    }

    fun deletePlatformToken(tokenId: Long) = viewModelScope.launch {
        runCatching {
            systemRepository.deletePlatformToken(tokenId)
        }.onSuccess {
            loadPlatformTokens()
        }.onFailure {
            _uiState.update { state -> state.copy(error = it.localizedMessage ?: "删除平台令牌失败") }
        }
    }

    fun updateSmtpConfig(host: String, port: Int, ssl: Boolean, username: String, password: String) = viewModelScope.launch {
        _uiState.update { it.copy(loading = true, error = null) }
        runCatching {
            systemRepository.updateSmtpConfig(host, port, ssl, username, password)
        }.onSuccess {
            _uiState.update { it.copy(loading = false) }
        }.onFailure {
            _uiState.update { state -> state.copy(loading = false, error = it.localizedMessage ?: "更新 SMTP 配置失败") }
        }
    }

    fun testSmtpConfig() = viewModelScope.launch {
        _uiState.update { it.copy(loading = true, error = null) }
        runCatching {
            systemRepository.testSmtpConfig()
        }.onSuccess { msg ->
            _uiState.update { it.copy(loading = false) }
        }.onFailure {
            _uiState.update { state -> state.copy(loading = false, error = it.localizedMessage ?: "SMTP 测试失败") }
        }
    }

    fun loadApps() = viewModelScope.launch {
        val repo = systemRepository ?: return@launch
        _uiState.update { it.copy(appsLoading = true, appsErrorMessage = null) }
        runCatching {
            val data = repo.loadApps()
            _uiState.update { it.copy(apps = data, appsLoading = false) }
        }.onFailure {
            _uiState.update { state -> state.copy(appsLoading = false, appsErrorMessage = it.localizedMessage ?: "加载应用失败") }
        }
    }

    fun updateAppEnabled(appId: Long, enabled: Boolean) = viewModelScope.launch {
        val repo = systemRepository ?: return@launch
        runCatching {
            repo.updateAppEnabled(appId, enabled)
            _uiState.update { it.copy(actionMessage = if (enabled) "应用已启用" else "应用已禁用") }
        }.onFailure {
            _uiState.update { state -> state.copy(errorMessage = it.localizedMessage ?: "更新应用状态失败") }
        }
    }

    private var loadUsersState = MutableStateFlow<List<com.daidai.daidai_app.data.model.User>>(emptyList())
    private var _sshKeysState = MutableStateFlow<List<com.daidai.daidai_app.data.model.SshKey>>(emptyList())
    private var _platformTokensState = MutableStateFlow<List<com.daidai.daidai_app.data.model.PlatformTokenInfo>>(emptyList())
}
    }

    private var loadUsersState = MutableStateFlow<List<com.daidai.daidai_app.data.model.User>>(emptyList())
    private var _sshKeysState = MutableStateFlow<List<com.daidai.daidai_app.data.model.SshKey>>(emptyList())
    private var _platformTokensState = MutableStateFlow<List<com.daidai.daidai_app.data.model.PlatformTokenInfo>>(emptyList())

}