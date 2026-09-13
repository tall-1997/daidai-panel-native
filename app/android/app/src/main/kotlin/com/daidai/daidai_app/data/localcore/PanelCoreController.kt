package com.daidai.daidai_app.data.localcore

import android.content.Context
import com.daidai.daidai_app.LocalPanelRuntime
import com.daidai.daidai_app.PanelProcessLocalToken
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 同进程 LocalPanelRuntime 的稳定门面。
 *
 * 该控制器不通过 Flutter MethodChannel 调用，也不推断运行成功；每次操作都把
 * LocalPanelRuntime 返回的 phase/base_url/local_token 映射到 [status]。调用方应先用
 * [initialize] 注入 application Context；未注入时 ensure/restart 会返回诊断状态。
 */
object PanelCoreController {
    private val lock = Any()
    private val _status = MutableStateFlow(LocalPanelStatus())
    private var applicationContext: Context? = null

    /** 当前本地面板运行状态，UI 可安全订阅。 */
    val status: StateFlow<LocalPanelStatus> = _status.asStateFlow()

    /** 配置同进程运行时所需的 Context。不会启动运行时。 */
    fun initialize(context: Context) {
        synchronized(lock) {
            applicationContext = context.applicationContext
        }
        refreshStatus()
    }

    /** 读取 LocalPanelRuntime 的真实状态并发布。 */
    fun refreshStatus(): LocalPanelStatus = synchronized(lock) {
        publish(LocalPanelRuntime.status(PanelProcessLocalToken.value))
    }

    /** 确保本地面板运行时已启动，并发布其真实结果。 */
    fun ensureStarted(): LocalPanelStatus = synchronized(lock) {
        val context = applicationContext
            ?: return publish(diagnostic("LocalPanelController requires initialize(context) before ensureStarted"))
        runCatching {
            LocalPanelRuntime.tryEnsureStarted(context, PanelProcessLocalToken.value)
        }.getOrElse { error ->
            diagnostic("LocalPanelRuntime.ensureStarted failed: ${error.message ?: error::class.java.simpleName}")
        }.let(::publish)
    }

    /** 重启本地面板运行时，并发布其真实结果。 */
    fun restart(): LocalPanelStatus = synchronized(lock) {
        val context = applicationContext
            ?: return publish(diagnostic("LocalPanelController requires initialize(context) before restart"))
        runCatching {
            LocalPanelRuntime.restart(context, PanelProcessLocalToken.value)
        }.getOrElse { error ->
            diagnostic("LocalPanelRuntime.restart failed: ${error.message ?: error::class.java.simpleName}")
        }.let(::publish)
    }

    /** 停止本地面板运行时，并发布停止结果。 */
    fun stop(): LocalPanelStatus = synchronized(lock) {
        runCatching {
            LocalPanelRuntime.stop(PanelProcessLocalToken.value)
        }.getOrElse { error ->
            diagnostic("LocalPanelRuntime.stop failed: ${error.message ?: error::class.java.simpleName}")
        }.let(::publish)
    }

    private fun publish(raw: Map<String, Any>): LocalPanelStatus {
        val mapped = LocalPanelStatus(
            fallbackMode = modeFor(raw["phase"] as? String),
            baseUrl = raw["base_url"] as? String,
            localToken = raw["local_token"] as? String ?: PanelProcessLocalToken.value,
            message = raw["message"] as? String,
        )
        _status.value = mapped
        return mapped
    }

    private fun diagnostic(message: String): Map<String, Any> = mapOf(
        "phase" to "failed",
        "base_url" to "",
        "local_token" to PanelProcessLocalToken.value,
        "message" to message,
    )

    private fun modeFor(phase: String?): PanelMode = when (phase?.lowercase()) {
        "ready", "started", "running" -> PanelMode.STARTED
        "stopped" -> PanelMode.STOPPED
        else -> PanelMode.DIAGNOSTIC
    }
}
