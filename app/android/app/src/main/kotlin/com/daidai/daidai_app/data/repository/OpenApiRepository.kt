package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.data.model.parseOpenApiApps
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.TimeUnit

/**
 * Open API 应用的只读数据源契约。
 *
 * 与既有 `PanelHttpClient` 认证约定保持一致：
 *  - 远程面板使用 `Authorization: Bearer <accessToken>`
 *  - 托管本地面板使用 `X-Daidai-Local-Token: <localToken>`
 * 二者均未提供时按未认证请求发出。
 */
interface OpenApiRepository {
    /** 拉取全部 Open API 应用列表。只读，不修改任何数据。 */
    suspend fun getApps(): List<OpenApiApp>
}

/**
 * 读写 `GET {baseUrl}/api/open-api/apps` 的默认实现。
 *
 * 独立于 [com.daidai.daidai_app.data.remote.PanelHttpClient] 的自包含只读客户端：
 * 仅负责本轮模块的拉取，不改动既有网络层。baseUrl 应已去掉尾部斜杠。
 */
class PanelOpenApiRepository(
    baseUrl: String,
    private val accessToken: String? = null,
    private val localToken: String? = null,
    private val httpClient: OkHttpClient = defaultHttpClient(),
) : OpenApiRepository {

    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    override suspend fun getApps(): List<OpenApiApp> = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url("$baseUrl/api/open-api/apps")
            .apply {
                accessToken?.takeIf { it.isNotBlank() }
                    ?.let { header("Authorization", "Bearer $it") }
                localToken?.takeIf { it.isNotBlank() }
                    ?.let { header("X-Daidai-Local-Token", it) }
            }
            .get()
            .build()

        httpClient.newCall(request).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw IllegalStateException(
                    "Open API 应用加载失败（HTTP ${response.code}）：$body",
                )
            }
            parseOpenApiApps(body)
        }
    }

    private companion object {
        fun defaultHttpClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }
}