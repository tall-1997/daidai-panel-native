package com.daidai.daidai_app

import android.content.Intent
import android.net.Uri
import androidx.core.content.ContextCompat
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.EventChannel
import java.util.concurrent.Executors
import org.json.JSONArray
import org.json.JSONObject

class MainActivity : FlutterActivity() {
    private val ROOT_CHANNEL = "com.daidai.app/root"
    private val INSTALL_CHANNEL = "com.daidai.panel/app_install"
    private val LOCAL_HOST_CHANNEL = "com.daidai.panel/local_host"
    private val LOCAL_HOST_EVENTS = "com.daidai.panel/local_host/events"
    private val updateExecutor = Executors.newSingleThreadExecutor()
    private val localPanelClient by lazy { LocalPanelServiceClient(applicationContext) }
    @Volatile
    private var activityDestroyed = false
    private var localHostEventSink: EventChannel.EventSink? = null

    // ==========================================================================
    // 【阶段5 · 去 Flutter 重构准备】MethodChannel 迁移去向与删除顺序（仅标注，不删除，留集成步骤）
    //
    //  本类为 FlutterActivity。阶段5 将整体切换到 Compose 原生 UI（见 docs/deflutter-plan.md）。
    //  以下 4 个 MethodChannel / EventChannel 的去向如下，集成步骤可按顺序删除：
    //
    //  1) com.daidai.app/root  (ROOT_CHANNEL)
    //     → 死通道，Dart 无任何调用，仅返回 notImplemented()。阶段5 【直接删除】，无替代实现。
    //
    //  2) com.daidai.panel/app_install  (INSTALL_CHANNEL)
    //     → 迁移到 ApkInstaller（阶段5-A1 并行抽取，见下方 app_install handler 标注）。
    //     Dart 侧 installApk / getInstalledApkInfo / getRuntimeAbi / applyPatch 的调用将改为
    //     Compose 层直调 ApkInstaller。阶段 5 【删除整个 INSTALL_CHANNEL 注册块】。
    //
    //  3) com.daidai.panel/local_host  (LOCAL_HOST_CHANNEL)
    //     → 迁移到 Compose PanelCoreController 同进程直调
    //       (app/android/app/src/main/kotlin/com/daidai/daidai_app/ui/data/localcore/PanelCoreController.kt)。
    //     ensureStarted/getStatus/restart/stop/setPersistentSchedulingEnabled/openBrowserInWeb 等
    //     方法由 Compose ViewModel 直调 localPanelClient。阶段 5 【删除该 channel 注册块】。
    //
    //  4) com.daidai.panel/local_host/events  (LOCAL_HOST_EVENTS, EventChannel)
    //     → 迁移到 PanelCoreController.status 的 StateFlow 订阅，替代 Flutter StreamSubscription。
    //     阶段 5 【删除该 EventChannel 注册块及 localHostEventSink 字段】。
    //
    //  并发注意：阶段 5-A1（ApkInstaller 抽取）与本文件并行修改。本文件仅在 A1 完成前为
    //  app_install 保留原调用；A1 抽完后集成步骤把该 handler 替换为 ApkInstaller 调用。
    // ==========================================================================

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        // 【阶段 5 删除】root 通道为死通道（NOT_IMPLEMENTED），Dart 无任何调用，无替代实现。集成步骤直接删除本块。
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, ROOT_CHANNEL).setMethodCallHandler { call, result ->
            when (RootMethodChannelPolicy.disposition(call.method)) {
                RootMethodDisposition.NOT_IMPLEMENTED -> result.notImplemented()
            }
        }

        // 【阶段 5 删除】app_install 通道迁移已完成：A1 已抽取 ApkInstaller，本 handler 全部改为 ApkInstaller 直调（installApk/installedApkInfo/currentAbi/applyPatch）。阶段 5 集成步骤直接删除本块即可。
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, INSTALL_CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "installApk" -> {
                    val path = call.argument<String>("path")
                    if (path != null) {
                        updateExecutor.execute {
                            try {
                                ApkInstaller.installApk(applicationContext, path)
                                runOnUiThread { result.success(null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("INSTALL_ERROR", e.message, null) }
                            }
                        }
                    } else {
                        result.error("INVALID_ARGS", "Path is required", null)
                    }
                }
                "getInstalledApkInfo" -> {
                    updateExecutor.execute {
                        try {
                            val info = ApkInstaller.installedApkInfo(applicationContext)
                            runOnUiThread { result.success(info) }
                        } catch (e: Exception) {
                            runOnUiThread { result.error("APK_INFO_ERROR", e.message, null) }
                        }
                    }
                }
                "getRuntimeAbi" -> result.success(ApkInstaller.currentAbi())
                "applyPatch" -> {
                    val patchPath = call.argument<String>("patchPath")
                    val outputName = call.argument<String>("outputName")
                    if (patchPath.isNullOrBlank() || outputName.isNullOrBlank()) {
                        result.error("INVALID_ARGS", "Patch path and output name are required", null)
                        return@setMethodCallHandler
                    }
                    updateExecutor.execute {
                        try {
                            val outputFile = ApkInstaller.applyPatch(applicationContext, patchPath, outputName)
                            runOnUiThread { result.success(mapOf("path" to outputFile.absolutePath)) }
                        } catch (e: Exception) {
                            runOnUiThread { result.error("PATCH_ERROR", e.message, null) }
                        }
                    }
                }
                else -> {
                    result.notImplemented()
                }
            }
        }

        // 【阶段 5 迁移】local_host 通道迁移到 Compose PanelCoreController 同进程直调（ui/data/localcore/PanelCoreController.kt）；逻辑保留。集成步骤删除本块。
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, LOCAL_HOST_CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "ensureStarted" -> invokeLocalPanel(result, localPanelClient::ensureStarted)
                "getStatus" -> invokeLocalPanel(result, localPanelClient::status)
                "restart" -> invokeLocalPanel(result, localPanelClient::restart, emitAfter = true)
                "stop" -> {
                    invokeLocalPanel(result, localPanelClient::stop, emitAfter = true)
                }
                "setPersistentSchedulingEnabled" -> {
                    val enabled = call.argument<Boolean>("enabled") == true
                    if (enabled) {
                        ContextCompat.startForegroundService(
                            this,
                            Intent(this, LocalPanelHostService::class.java).apply {
                                action = LocalPanelHostService.ACTION_ENABLE_PERSISTENT
                            },
                        )
                    }
                    invokeLocalPanel(
                        result,
                        { callback ->
                            localPanelClient.setPersistentSchedulingEnabled(enabled, callback)
                        },
                        emitAfter = true,
                    )
                }
                "openBrowserPanel" -> localPanelClient.createBrowserUrl { callResult ->
                    runOnUiThread {
                        if (activityDestroyed) return@runOnUiThread
                        callResult.fold(
                            onSuccess = { url ->
                                runCatching {
                                    startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                                }.fold(
                                    onSuccess = {
                                        beginPanelSession()
                                        result.success(url)
                                    },
                                    onFailure = { result.error("BROWSER_UNAVAILABLE", "无法打开设备浏览器", null) },
                                )
                            },
                            onFailure = { result.error("LOCAL_WEB_UNAVAILABLE", it.message, null) },
                        )
                    }
                }
                else -> result.notImplemented()
            }
        }

        // 【阶段 5 迁移】local_host/events (EventChannel) 迁移到 PanelCoreController.status 的 StateFlow 订阅，替代 Flutter StreamSubscription。集成步骤删除本块。
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, LOCAL_HOST_EVENTS).setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
                    localHostEventSink = events
                    emitLocalHostStatus()
                }

                override fun onCancel(arguments: Any?) {
                    localHostEventSink = null
                }
            }
        )
    }

    override fun onDestroy() {
        activityDestroyed = true
        localPanelClient.close()
        updateExecutor.shutdownNow()
        super.onDestroy()
    }

    private fun beginPanelSession() {
        if (LocalPanelHostService.isPersistentSchedulingEnabled(this)) return
        ContextCompat.startForegroundService(
            this,
            Intent(this, LocalPanelHostService::class.java).apply {
                action = LocalPanelHostService.ACTION_PANEL_SESSION_START
            },
        )
    }

    private fun invokeLocalPanel(
        channelResult: MethodChannel.Result,
        operation: ((Result<String>) -> Unit) -> Unit,
        emitAfter: Boolean = false,
    ) {
        operation { callResult ->
            runOnUiThread {
                if (activityDestroyed) return@runOnUiThread
                callResult.fold(
                    onSuccess = { raw ->
                        val status = localHostStatus(raw)
                        channelResult.success(status)
                        if (emitAfter) localHostEventSink?.success(status)
                    },
                    onFailure = { error ->
                        channelResult.success(localHostFailure(error))
                    },
                )
            }
        }
    }

    private fun localHostStatus(raw: String): Map<String, Any?> = runCatching {
        jsonObjectToMap(JSONObject(raw))
    }.getOrElse { error ->
        localHostFailure(IllegalStateException("Invalid local panel service response", error))
            .toMutableMap()
            .apply { this["failure_stage"] = "invalid_service_response" }
    }

    private fun localHostFailure(error: Throwable? = null): Map<String, Any?> = mapOf(
        "phase" to "failed",
        "base_url" to "",
        "instance_id" to "",
        "core_version" to "kotlin-local-fallback",
        "schema_version" to 0,
        "failure_stage" to "binder",
        "message" to (error?.message?.takeIf(String::isNotBlank) ?: "Local panel service unavailable"),
        "host_error_type" to (error?.javaClass?.simpleName ?: "Unknown"),
        "foreground_service_enabled" to false,
    )

    private fun jsonObjectToMap(value: JSONObject): Map<String, Any?> =
        value.keys().asSequence().associateWith { key ->
            when (val item = value.opt(key)) {
                JSONObject.NULL, null -> null
                is JSONObject -> jsonObjectToMap(item)
                is JSONArray -> jsonArrayToList(item)
                else -> item
            }
        }

    private fun jsonArrayToList(value: JSONArray): List<Any?> =
        (0 until value.length()).map { index ->
            when (val item = value.opt(index)) {
                JSONObject.NULL, null -> null
                is JSONObject -> jsonObjectToMap(item)
                is JSONArray -> jsonArrayToList(item)
                else -> item
            }
        }

    private fun emitLocalHostStatus() {
        localPanelClient.status { callResult ->
            runOnUiThread {
                if (activityDestroyed) return@runOnUiThread
                localHostEventSink?.success(
                    callResult.fold(
                        onSuccess = ::localHostStatus,
                        onFailure = { localHostFailure(it) },
                    )
                )
            }
        }
    }

}
