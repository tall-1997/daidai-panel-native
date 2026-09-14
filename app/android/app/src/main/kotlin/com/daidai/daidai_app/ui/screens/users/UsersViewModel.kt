package com.daidai.daidai_app.ui.screens.users

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.ResetPasswordPayload
import com.daidai.daidai_app.data.model.User
import com.daidai.daidai_app.data.repository.UsersRepository
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** 用户管理页的可观察状态。 */
data class UsersUiState(
    val users: List<User> = emptyList(),
    val phase: Phase = Phase.Loading,
    val errorMessage: String? = null,
    // 操作反馈
    val actionError: String? = null,
    val actionMessage: String? = null,
    // 正在执行删除 / 重置密码的用户（用于按钮禁用与轻量反馈）
    val userIdInProgress: Long? = null,
) {
    enum class Phase {
        Loading,
        Loaded,
        Error,
    }
}

/**
 * 用户管理模块 ViewModel：拉取用户列表并支持 新建 / 编辑 / 删除 / 重置密码。
 *
 * 复用阶段 0 的 Loading/Loaded/Error 三态思路，并为 CRUD 提供独立的
 * 操作反馈（actionMessage / actionError）。repository 缺省为 null 时有意报
 * 「用户模块未装配」，绝不伪造空列表，与 LoginViewModel / OpenApiViewModel
 * 的约定一致。
 */
class UsersViewModel(
    private val repository: UsersRepository? = null,
) : ViewModel() {
    private val _uiState = MutableStateFlow(UsersUiState())
    val uiState: StateFlow<UsersUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(
                    phase = UsersUiState.Phase.Error,
                    errorMessage = "用户模块后端未装配，请先配置服务器并登录",
                )
            }
            return
        }
        _uiState.update { it.copy(phase = UsersUiState.Phase.Loading, errorMessage = null) }
        viewModelScope.launch {
            try {
                val users = repo.getUsers()
                _uiState.update {
                    it.copy(
                        users = users,
                        phase = UsersUiState.Phase.Loaded,
                        errorMessage = null,
                    )
                }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        phase = UsersUiState.Phase.Error,
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "用户列表加载失败，请重试",
                    )
                }
            }
        }
    }

    /** 新建用户。成功后刷新列表。 */
    fun createUser(username: String, password: String, role: String) {
        val repo = repository ?: return
        viewModelScope.launch {
            try {
                repo.createUser(username, password, role)
                _uiState.update { it.copy(actionError = null, actionMessage = "用户「$username」创建成功") }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "创建用户失败",
                        actionMessage = null,
                    )
                }
            }
        }
    }

    /** 更新用户角色 / 启停。成功后刷新列表。 */
    fun updateUser(id: Long, role: String?, enabled: Boolean?, username: String = "") {
        val repo = repository ?: return
        viewModelScope.launch {
            try {
                repo.updateUser(id, role, enabled)
                _uiState.update { it.copy(actionError = null, actionMessage = "用户「$username」已更新") }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "更新用户失败",
                        actionMessage = null,
                    )
                }
            }
        }
    }

    /** 删除用户。成功后刷新列表。 */
    fun deleteUser(id: Long, username: String) {
        val repo = repository ?: return
        _uiState.update { it.copy(userIdInProgress = id, actionError = null, actionMessage = null) }
        viewModelScope.launch {
            try {
                repo.deleteUser(id)
                _uiState.update { it.copy(userIdInProgress = null, actionError = null, actionMessage = "用户「$username」已删除") }
                refresh()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        userIdInProgress = null,
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "删除用户失败",
                        actionMessage = null,
                    )
                }
            }
        }
    }

    /** 重置指定用户的密码。成功后刷新列表（可选）。 */
    fun resetPassword(id: Long, username: String, newPassword: String) {
        val repo = repository ?: return
        _uiState.update { it.copy(userIdInProgress = id, actionError = null, actionMessage = null) }
        viewModelScope.launch {
            try {
                repo.resetPassword(id, ResetPasswordPayload(newPassword))
                _uiState.update { it.copy(userIdInProgress = null, actionError = null, actionMessage = "用户「$username」密码已重置") }
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _uiState.update {
                    it.copy(
                        userIdInProgress = null,
                        actionError = error.message?.takeIf(String::isNotBlank)
                            ?: "重置密码失败",
                        actionMessage = null,
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
