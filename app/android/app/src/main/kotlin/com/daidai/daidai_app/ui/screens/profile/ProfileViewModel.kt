package com.daidai.daidai_app.ui.screens.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.ProfileInfo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.launch
import okhttp3.MultipartBody
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/** 个人中心页的可观察状态。 */
sealed interface ProfileUiState {
    /** 加载中。 */
    data object Loading : ProfileUiState

    /** 加载成功：展示用户信息 / 版本 / 退出登录入口。 */
    data class Success(
        val profile: ProfileInfo,
        /** 是否具备展示“退出登录”入口的登录态（存在令牌的用户）。 */
        val canLogout: Boolean,
    ) : ProfileUiState

    /** 加载失败（未登录 / 网络异常等），尽量回退展示登录用户名。 */
    data class Error(
        val message: String,
        val fallbackUsername: String = "",
        val canLogout: Boolean = false,
    ) : ProfileUiState
}

/**
 * 个人中心 ViewModel：拉取「当前登录用户」信息与面板版本号，产出
 * Loading / Success / Error 三态 [ProfileUiState]。
 *
 * 数据来源（自包含，网络请求内联于本类，不新增仓库文件）：
 *  - 用户信息：`GET {serverUrl}/api/v1/auth/user`（Bearer / 本地令牌鉴权），
 *    返回 `{"user": {...}}`，含 username / role / avatar_url / enabled。
 *  - 版本号：`GET {serverUrl}/api/v1/system/public-version`（公开端点）。
 *
 * 与 UsersViewModel 约定一致：面对后端缺失时绝不伪造用户信息。未登录或
 * 请求失败时进入 Error 态（保留可用的登录用户名 / 令牌态），供 UI 回退展示。
 */
class ProfileViewModel(
    private val baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
) : ViewModel() {
    private val normalizedBaseUrl: String = baseUrl.trim().trimEnd('/')

    /** 账号操作结果提示（UI 消费后经 consumeNotice 复位）。 */
    private val _notice = MutableStateFlow<String?>(null)
    val notice: StateFlow<String?> = _notice.asStateFlow()
    fun consumeNotice() { _notice.value = null }

    /** 修改自己的登录密码（PUT /api/auth/password）。 */
    fun changePassword(oldPassword: String, newPassword: String) {
        viewModelScope.launch {
            try {
                com.daidai.daidai_app.data.remote.PanelRequests.execute(
                    "PUT",
                    "$normalizedBaseUrl/api/auth/password",
                    json = JSONObject().put("old_password", oldPassword).put("new_password", newPassword).toString(),
                    accessToken = accessToken,
                    localToken = localToken,
                )
                _notice.value = "密码已更新"
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _notice.value = error.message?.takeIf(String::isNotBlank) ?: "修改密码失败"
            }
        }
    }

    /** 修改自己的用户名（PUT /api/auth/username）。 */
    fun changeUsername(username: String) {
        viewModelScope.launch {
            try {
                com.daidai.daidai_app.data.remote.PanelRequests.execute(
                    "PUT",
                    "$normalizedBaseUrl/api/auth/username",
                    json = JSONObject().put("username", username).toString(),
                    accessToken = accessToken,
                    localToken = localToken,
                )
                _notice.value = "用户名已更新"
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _notice.value = error.message?.takeIf(String::isNotBlank) ?: "修改用户名失败"
            }
        }
    }

    /** 上传头像（multipart 字段名 avatar，≤5MB，对齐服务端校验）。 */
    fun uploadAvatar(bytes: ByteArray, filename: String) {
        viewModelScope.launch {
            try {
                val mediaType = okhttp3.MediaType.parse("application/octet-stream")
                val part = MultipartBody.Part.createFormData(
                    "avatar", filename,
                    okhttp3.RequestBody.create(mediaType, bytes),
                )
                val body = MultipartBody.Builder().setType(MultipartBody.FORM).addPart(part).build()
                val builder = okhttp3.Request.Builder().url("$normalizedBaseUrl/api/auth/avatar").post(body)
                accessToken?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
                localToken?.takeIf { it.isNotBlank() }?.let { builder.header("x-daidai-local-token", it) }
                com.daidai.daidai_app.data.remote.PanelRequests.sharedClient.newCall(builder.build()).execute().use { response ->
                    val text = response.body?.string().orEmpty()
                    if (!response.isSuccessful) throw com.daidai.daidai_app.data.remote.PanelApiException(response.code, text)
                }
                _notice.value = "头像已上传"
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _notice.value = error.message?.takeIf(String::isNotBlank) ?: "头像上传失败"
            }
        }
    }

    /** 删除头像（DELETE /api/auth/avatar）。 */
    fun deleteAvatar() {
        viewModelScope.launch {
            try {
                com.daidai.daidai_app.data.remote.PanelRequests.execute(
                    "DELETE", "$normalizedBaseUrl/api/auth/avatar",
                    accessToken = accessToken, localToken = localToken,
                )
                _notice.value = "头像已删除"
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                _notice.value = error.message?.takeIf(String::isNotBlank) ?: "头像删除失败"
            }
        }
    }

    private val _uiState = MutableStateFlow<ProfileUiState>(ProfileUiState.Loading)
    val uiState: StateFlow<ProfileUiState> = _uiState.asStateFlow()

    init {
        refresh()
    }

    /** 重新拉取用户信息与版本。 */
    fun refresh() {
        _uiState.value = ProfileUiState.Loading
        viewModelScope.launch {
            val hasCredential = accessToken?.isNotBlank() == true || localToken?.isNotBlank() == true
            if (normalizedBaseUrl.isBlank()) {
                _uiState.value = ProfileUiState.Error(
                    message = "尚未配置服务器地址，请先在“配置服务器”页面填写",
                    canLogout = false,
                )
                return@launch
            }
            val versionDeferred = async { fetchVersion() }
            val profileDeferred = async { fetchCurrentUser() }
            val version = runCatching { versionDeferred.await() }.getOrDefault("")
            val user = try {
                profileDeferred.await()
            } catch (error: Exception) {
                if (error is kotlinx.coroutines.CancellationException) throw error
                // 未登录或拉取失败：回退到登录态展示。
                if (hasCredential) {
                    _uiState.value = ProfileUiState.Error(
                        message = error.message?.takeIf { it.isNotBlank() }
                            ?: "个人资料加载失败，请重试",
                        canLogout = true,
                    )
                } else {
                    _uiState.value = ProfileUiState.Error(
                        message = "尚未登录，请先登录后再查看个人资料",
                        canLogout = false,
                    )
                }
                return@launch
            }
            _uiState.value = ProfileUiState.Success(
                profile = user.copy(version = version),
                canLogout = user.hasIdentity,
            )
        }
    }

    /** 退出登录：刷新为未登录状态（清除逻辑由集成方在 ProfileScreen.onLogout 处理）。 */
    fun logout() {
        _uiState.value = ProfileUiState.Error(
            message = "尚未登录，请先登录后再查看个人资料",
            canLogout = false,
        )
    }

    /** 拉取当前登录用户（GET /api/v1/auth/user）。 */
    private suspend fun fetchCurrentUser(): ProfileInfo = withContext(Dispatchers.IO) {
        val body = execute("GET", "$normalizedBaseUrl/api/v1/auth/user")
        val json = JSONObject(body)
        ProfileInfo.fromAuthUser(json.optJSONObject("user"), version = "")
    }

    /** 拉取面板版本（GET /api/v1/system/public-version，公开端点）。 */
    private suspend fun fetchVersion(): String = withContext(Dispatchers.IO) {
        val body = execute("GET", "$normalizedBaseUrl/api/v1/system/public-version")
        ProfileInfo.extractVersion(body)
    }

    /** 与 PanelUsersRepository 一致的鉴权请求构造：Bearer / 本地令牌二选一。 */
    private suspend fun execute(method: String, url: String): String {
        val builder = Request.Builder().url(url)
        accessToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("Authorization", "Bearer $it") }
        localToken?.takeIf { it.isNotBlank() }
            ?.let { builder.header("X-Daidai-Local-Token", it) }
        builder.method(method, null)
        httpClient.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = runCatching {
                    val parsed = JSONObject(body)
                    parsed.optString("error").takeIf(String::isNotEmpty)
                        ?: parsed.optString("message")
                }.getOrNull()?.takeIf { it.isNotEmpty() }
                throw ProfileApiException(response.code, body, message)
            }
            return body
        }
    }

    private companion object {
        val httpClient: OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }
}

/** HTTP 失败：保留状态码、原始响应与可读错误描述。 */
class ProfileApiException(
    val statusCode: Int,
    val responseBody: String,
    val serverMessage: String?,
) : RuntimeException(serverMessage?.takeIf(String::isNotEmpty) ?: "个人资料请求失败（HTTP $statusCode）")
