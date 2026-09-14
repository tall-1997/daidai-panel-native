package com.daidai.daidai_app.ui.screens.system

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.BackupRecord
import com.daidai.daidai_app.data.model.HealthCheckResult
import com.daidai.daidai_app.data.repository.BackupRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

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
) {
    enum class BackupPhase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 阶段 4-5 系统管理 ViewModel：通过 [BackupRepository] 提供
 * 备份列表 / 新建 / 删除 以及健康检查的读写能力，暴露为 [StateFlow]。
 *
 * 备份读取走 [BackupRepository.listBackups]；写操作（创建/删除）在完成后自动刷新列表。
 */
class SystemViewModel(
    private val repository: BackupRepository,
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

    /** 新建备份（默认全量、无密码）。成功后自动刷新列表。 */
    fun createBackup() {
        if (_uiState.value.creatingBackup) return
        viewModelScope.launch {
            _uiState.update { it.copy(creatingBackup = true, errorMessage = null) }
            try {
                val result = repository.createBackup(name = null)
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

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf(String::isNotBlank) ?: fallback
}
