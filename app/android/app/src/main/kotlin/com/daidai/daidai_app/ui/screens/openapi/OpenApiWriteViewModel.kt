package com.daidai.daidai_app.ui.screens.openapi

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.CreateAppPayload
import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.data.model.ResetSecretResult
import com.daidai.daidai_app.data.model.UpdateAppPayload
import com.daidai.daidai_app.data.model.dataObjectOrNull
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * Open API 应用写操作的可观察状态（Compose 原生，补齐-2）。
 *
 * 复用既有模块的 Phase / errorMessage 约定；额外维护：
 *  - [busy]：任一写操作进行中的布尔闸，用于禁用按钮防重复提交；
 *  - [createdApp]：创建成功后后端即将返回的应用（含一次性 secret），详情页据此
 *    进入「展示新密钥」态；
 *  - [resetSecret]：最近一次重置密钥得到的凭据（secret 仅展示一次，随即清空）；
 *  - [mutationSucceeded] + [mutationOp]：写操作成功信号，供父级刷新或回跳。
 */
data class OpenApiWriteUiState(
    val phase: Phase = Phase.Idle,
    val busy: Boolean = false,
    val errorMessage: String? = null,
    /** 创建成功返回的应用（含一次性 app_secret），非 null 表示待展示密钥。 */
    val createdApp: OpenApiApp? = null,
    /** 重置密钥成功后返回的凭据；展示一次后由 UI 调用 [OpenApiWriteViewModel.consumeSecret] 清空。 */
    val resetSecret: ResetSecretResult? = null,
    /** 最近一次写操作是否成功（删除 / 启停 / 更新）。 */
    val mutationSucceeded: Boolean = false,
) {
    enum class Phase {
        Idle,
        Busy,
        Error,
    }
}

/**
 * Open API 应用的写操作 ViewModel：自含 OkHttp client，不依赖既有只读仓库，
 * 也不改动 OpenApiRepository / di 模块。baseUrl 去掉尾部斜杠，认证约定与
 * PanelHttpClient 一致（Bearer accessToken / X-Daidai-Local-Token localToken）。
 */
class OpenApiWriteViewModel(
    private val baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : ViewModel() {

    private val _uiState = MutableStateFlow(OpenApiWriteUiState())
    val uiState: StateFlow<OpenApiWriteUiState> = _uiState.asStateFlow()

    /** 新建应用：POST /api/open-api/apps。成功后把含 secret 的应用放入 [OpenApiWriteUiState.createdApp]。 */
    fun create(payload: CreateAppPayload) = mutate(op = "create") { baseUrl ->
        val body = execute("POST", "$baseUrl/api/open-api/apps", payload.toJson())
        val data = dataObjectOrNull(body)
        val key = data?.optString("app_key").orEmpty()
        val secret = data?.optString("app_secret").orEmpty()
        _uiState.update {
            it.copy(
                createdApp = OpenApiApp(
                    name = payload.name,
                    appKey = key,
                    scopes = payload.scopes,
                    enabled = true,
                    rateLimit = payload.rateLimit,
                ),
                resetSecret = ResetSecretResult(appKey = key, secret = secret),
            )
        }
    }

    /** 重置密钥：POST /api/open-api/apps/:id/reset-secret。 */
    fun resetSecret(appId: Long) = mutate(op = "resetSecret") { baseUrl ->
        val body = execute("POST", "$baseUrl/api/open-api/apps/$appId/reset-secret", null)
        val result = dataObjectOrNull(body)?.let(ResetSecretResult::fromJson) ?: ResetSecretResult()
        _uiState.update { it.copy(resetSecret = result) }
    }

    /** 启停：PUT /api/open-api/apps/:id（enabled/scopes/rate_limit）。 */
    fun toggleEnabled(app: OpenApiApp) = mutate(op = "toggleEnabled") { baseUrl ->
        val payload = UpdateAppPayload(
            enabled = !app.enabled,
            scopes = app.scopes,
            rateLimit = app.rateLimit,
        )
        execute("PUT", "$baseUrl/api/open-api/apps/${app.id}", payload.toJson())
    }

    /** 删除：DELETE /api/open-api/apps/:id。 */
    fun delete(appId: Long) = mutate(op = "delete") { baseUrl ->
        execute("DELETE", "$baseUrl/api/open-api/apps/$appId", null)
    }

    /** 应用列表页拉取到最新数据后，用本方法重置写状态（回读到 OpenApiListScreen）。 */
    fun resetForExternal() {
        _uiState.value = OpenApiWriteUiState()
    }

    /** 消耗一次性密钥：展示完成后清空，避免复用旧密钥。 */
    fun consumeSecret() {
        _uiState.update { it.copy(createdApp = null, resetSecret = null) }
    }

    /** 供 UI 消费 mutationSucceeded 后复位。 */
    fun consumeMutation() {
        _uiState.update { it.copy(mutationSucceeded = false) }
    }

    // ------------------------------------------------------------------
    // 内部工具
    // ------------------------------------------------------------------

    /** 统一写操作外壳：置 busy、捕获异常为 errorMessage、成功置 mutationSucceeded。 */
    private fun mutate(op: String, block: (String) -> Unit) {
        viewModelScope.launch {
            _uiState.update {
                it.copy(busy = true, errorMessage = null, mutationSucceeded = false)
            }
            try {
                withContext(Dispatchers.IO) { block(baseUrl) }
                _uiState.update {
                    it.copy(
                        busy = false,
                        phase = OpenApiWriteUiState.Phase.Idle,
                        mutationSucceeded = true,
                    )
                }
            } catch (error: Throwable) {
                _uiState.update {
                    it.copy(
                        busy = false,
                        phase = OpenApiWriteUiState.Phase.Error,
                        errorMessage = friendly(error, "操作失败：$op"),
                    )
                }
            }
        }
    }

    /** 统一请求执行：注入认证头、按方法装配 body、校验状态码。 */
    private fun execute(method: String, url: String, body: JSONObject?): String {
        val builder = Request.Builder()
            .url(url)
            .apply {
                accessToken?.takeIf { it.isNotBlank() }
                    ?.let { header("Authorization", "Bearer $it") }
                localToken?.takeIf { it.isNotBlank() }
                    ?.let { header("X-Daidai-Local-Token", it) }
            }
        val jsonType = "application/json; charset=utf-8".toMediaType()
        val request = when (method) {
            "POST" -> builder.post(body?.toString().orEmpty().toRequestBody(jsonType)).build()
            "PUT" -> builder.put(body?.toString().orEmpty().toRequestBody(jsonType)).build()
            "DELETE" -> builder.delete().build()
            else -> builder.build()
        }
        httpClient.newCall(request).execute().use { response ->
            val responseBody = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw IllegalStateException(
                    "Open API 写操作失败（HTTP ${response.code}）：${responseBody}",
                )
            }
            return responseBody
        }
    }

    private fun friendly(error: Throwable, fallback: String): String =
        error.message?.takeIf { it.isNotBlank() } ?: fallback

    private companion object {
        fun defaultHttpClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }
}
