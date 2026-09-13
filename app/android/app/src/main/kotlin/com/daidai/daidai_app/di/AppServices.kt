package com.daidai.daidai_app.di

import android.content.Context
import com.daidai.daidai_app.data.localcore.PanelCoreController
import com.daidai.daidai_app.data.remote.AuthInitRequest
import com.daidai.daidai_app.data.remote.AuthLoginRequest
import com.daidai.daidai_app.data.remote.PanelHttpClient
import com.daidai.daidai_app.data.repository.PanelConfigRepository
import com.daidai.daidai_app.data.repository.PanelConnectionMode
import com.daidai.daidai_app.ui.screens.LoginRepository
import com.daidai.daidai_app.ui.screens.LoginResult
import com.daidai.daidai_app.ui.screens.ServerConfigRepository
import com.daidai.daidai_app.ui.screens.ServerMode

/**
 * 阶段 1 应用级装配（wire）。
 *
 * 把网络层 [PanelHttpClient] 与存储层 [PanelConfigRepository] 生产为
 * LoginScreen / ServerConfigScreen 需要的默认 Repository 实现，并通过对
 * [PanelCoreController] 的调用把“托管本地模式”接到 LocalPanelRuntime 的真实网络。
 *
 * - 无参/缺省参数路径（AppNavHost 不传 repository 时）由 UI 层经
 *   [serverConfigRepository] / [loginRepository] 拉取生产实现。
 * - 幂等：多个 UI 实例共享同一份配置读取与运行时状态，不重复创建。
 */
object AppServices {

    @Volatile
    private var panelConfig: PanelConfigRepository? = null
    @Volatile
    private var serverConfigRepository: ServerConfigRepository? = null
    @Volatile
    private var loginRepository: LoginRepository? = null

    private val lock = Any()

    /** 幂等初始化：注入 application Context，初始化本地面板控制台与 Keystore 配置存储。 */
    fun initialize(context: Context) {
        synchronized(lock) {
            if (panelConfig != null) return
            val appContext = context.applicationContext
            PanelCoreController.initialize(appContext)
            panelConfig = PanelConfigRepository(appContext)
        }
    }

    /** 面板配置存储（serverUrl / mode / accessToken / localToken）。 */
    fun configRepository(context: Context): PanelConfigRepository {
        initialize(context)
        return checkNotNull(panelConfig) { "AppServices.configRepository unavailable" }
    }

    /** ServerConfigScreen 默认实现：把选中的来源与其远程地址持久化到配置存储。 */
    fun serverConfigRepository(context: Context): ServerConfigRepository {
        serverConfigRepository?.let { return it }
        synchronized(lock) {
            if (serverConfigRepository == null) {
                serverConfigRepository = PersistentServerConfigRepository(configRepository(context))
            }
            return checkNotNull(serverConfigRepository)
        }
    }

    /** LoginScreen 默认实现：按配置来源路由到远程 http 或托管本地的 http，并持久化 accessToken。 */
    fun loginRepository(context: Context): LoginRepository {
        loginRepository?.let { return it }
        synchronized(lock) {
            if (loginRepository == null) {
                loginRepository = PanelConnectLoginRepository(configRepository(context))
            }
            return checkNotNull(loginRepository)
        }
    }

    /**
     * ServerConfigRepository 生产实现：把 ServerMode 映射到
     * [PanelConnectionMode]，并持久化远程地址 / 模式选择。
     */
    private class PersistentServerConfigRepository(
        private val configRepository: PanelConfigRepository,
    ) : ServerConfigRepository {
        override suspend fun save(mode: ServerMode, remoteUrl: String?) {
            val connectionMode = when (mode) {
                ServerMode.Remote -> PanelConnectionMode.REMOTE
                ServerMode.ManagedLocal -> PanelConnectionMode.MANAGED_LOCAL
            }
            configRepository.setMode(connectionMode)
            if (connectionMode == PanelConnectionMode.REMOTE) {
                configRepository.setServerUrl(remoteUrl.orEmpty())
            }
        }
    }

    /**
     * LoginRepository 生产实现：根据已持久化的连接模式路由到远程服务或
     * 托管本地服务（经 [PanelCoreController.ensureStarted] 获取 baseUrl + localToken）。
     * 登录成功后把 access_token 写入配置存储。
     */
    private class PanelConnectLoginRepository(
        private val configRepository: PanelConfigRepository,
        private val coreController: PanelCoreController = PanelCoreController,
    ) : LoginRepository {

        override suspend fun checkInit(): Boolean = client().checkInit().needInit

        override suspend fun init(username: String, password: String) {
            client().init(AuthInitRequest(username, password))
        }

        override suspend fun login(username: String, password: String): LoginResult {
            val http = client()
            val response = http.login(AuthLoginRequest(username, password))
            val token = response.accessToken
                ?: throw IllegalStateException("登录响应未包含访问令牌（access_token），请检查面板认证接口")
            configRepository.setAccessToken(token)
            return LoginResult(accessToken = token, username = response.user?.username ?: username)
        }

        private suspend fun client(): PanelHttpClient {
            val config = configRepository.getConfig()
            return when (config.mode) {
                PanelConnectionMode.MANAGED_LOCAL -> {
                    val status = coreController.ensureStarted()
                    val baseUrl = status.baseUrl
                    if (baseUrl.isNullOrBlank()) {
                        throw IllegalStateException("本地服务未能启动：${status.message ?: "未知原因"}")
                    }
                    PanelHttpClient(baseUrl = baseUrl, localToken = status.localToken)
                }
                PanelConnectionMode.REMOTE -> {
                    val baseUrl = config.serverUrl
                    if (baseUrl.isBlank()) {
                        throw IllegalStateException("尚未配置远程服务地址，请先在“配置服务器”页面填写")
                    }
                    PanelHttpClient(baseUrl = baseUrl)
                }
            }
        }
    }
}
