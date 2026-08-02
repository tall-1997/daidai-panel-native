package com.daidai.daidai_app

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.Process
import androidx.core.app.NotificationCompat
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class LocalPanelHostService : Service() {
    companion object {
        const val ACTION_ENABLE_PERSISTENT = "com.daidai.daidai_app.LOCAL_PANEL_ENABLE_PERSISTENT"
        const val ACTION_DISABLE_PERSISTENT = "com.daidai.daidai_app.LOCAL_PANEL_DISABLE_PERSISTENT"
        const val ACTION_RECOVER_PERSISTENT = "com.daidai.daidai_app.LOCAL_PANEL_RECOVER_PERSISTENT"
        const val EXTRA_RECOVERY_TRIGGER = "recovery_trigger"
        const val PERSISTENT_PREFS_NAME = "local_panel_persistent_foreground"
        const val PREF_PERSISTENT_ENABLED = "enabled"
        private const val CHANNEL_ID = "local_panel_scheduler"
        private const val NOTIFICATION_ID = 5700

        fun isPersistentSchedulingEnabled(context: android.content.Context): Boolean =
            context.getSharedPreferences(PERSISTENT_PREFS_NAME, android.content.Context.MODE_PRIVATE)
                .getBoolean(PREF_PERSISTENT_ENABLED, false)
    }

    private val localToken: String = PanelProcessLocalToken.value
    private lateinit var persistentPolicy: PersistentForegroundPolicy
    private lateinit var recoveryCoordinator: PersistentCoreRecoveryCoordinator
    private lateinit var networkRecovery: LocalPanelNetworkRecovery
    @Volatile
    private var recoveryFailure: Map<String, Any>? = null
    @Volatile
    private var lastRecoveryTrigger: String = "app-start"
    @Volatile
    private var destroyed = false
    private val binder = object : ILocalPanelService.Stub() {
        override fun ensureStarted(): String = encodeWithState(
            recordCoreResult(LocalPanelRuntime.tryEnsureStarted(applicationContext, localToken)),
        )

        override fun status(): String = encodeWithState(
            recordCoreResult(LocalPanelRuntime.status(localToken) - "local_token"),
        )

        override fun restart(): String = encodeWithState(
            recordCoreResult(LocalPanelRuntime.restart(applicationContext, localToken)),
        )

        override fun stop(): String = encodeWithState(explicitlyStopCore() - "local_token")

        override fun setPersistentSchedulingEnabled(enabled: Boolean): String {
            setPersistentScheduling(enabled)
            return status()
        }
    }

    override fun onCreate() {
        super.onCreate()
        runCatching {
            val crashDir = File(filesDir, "panel-crash-logs")
            crashDir.mkdirs()
            val pid = Process.myPid()
            val timestamp = SimpleDateFormat("yyyy-MM-dd_HH-mm-ss", Locale.US).format(Date())
            val marker = File(crashDir, "oncreate-${pid}-${timestamp}.started")
            marker.writeText("pid=${pid};process=${System.getProperty("os.arch", "unknown")};started_at=${System.currentTimeMillis()}")
        }
        try {
            createNotificationChannel()
            // Initialize panel runtime in background to avoid blocking UI
            // Start fallback server immediately, then do heavy Python init
            Thread {
                try {
                    // First: start fallback HTTP server (fast)
                    LocalPanelRuntime.ensureStarted(applicationContext, localToken)
                    // Second: preload Python and Node runtimes in background
                    AndroidPythonRuntime.preload(applicationContext)
                    AndroidNodeRuntime.preload(applicationContext)
                } catch (_: Exception) { }
            }.start()
            recoveryCoordinator = PersistentCoreRecoveryCoordinator(
                runner = ExecutorCoreRecoveryTaskRunner(),
                runtime = { LocalPanelRuntime.ensureStarted(applicationContext, localToken) },
                onResult = ::handleRecoveryResult,
            )
            networkRecovery = LocalPanelNetworkRecovery(applicationContext) {
                lastRecoveryTrigger = "network"
                LocalPanelRecoveryTriggers.reconcileWhenNetworkRestored(applicationContext)
            }
            persistentPolicy = PersistentForegroundPolicy(readPersistentSelection())
            applyPersistentAction(persistentPolicy.recoveryAction())
            LocalPanelRecoveryTriggers.schedulePeriodicReconciliation(applicationContext)
        } catch (error: Throwable) {
            runCatching {
                val crashDir = File(filesDir, "panel-crash-logs")
                crashDir.mkdirs()
                val sw = StringWriter()
                error.printStackTrace(PrintWriter(sw))
                val log = File(crashDir, "oncreate-crash-${Process.myPid()}.txt")
                log.writeText("type=${error.javaClass.name}\nmessage=${error.message}\n${sw}")
            }
            throw error
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent == null) {
            lastRecoveryTrigger = "process-recovery"
            restorePersistentSelection()
            return if (persistentPolicy.enabled) START_STICKY else START_NOT_STICKY
        }
        if (intent.action == ACTION_ENABLE_PERSISTENT) {
            lastRecoveryTrigger = "app-start"
            setPersistentScheduling(true)
            return START_STICKY
        }
        if (intent.action == ACTION_DISABLE_PERSISTENT) {
            setPersistentScheduling(false)
            return START_NOT_STICKY
        }
        if (intent.action == ACTION_RECOVER_PERSISTENT) {
            lastRecoveryTrigger = intent.getStringExtra(EXTRA_RECOVERY_TRIGGER) ?: "process-recovery"
            restorePersistentSelection()
            return if (persistentPolicy.enabled) START_STICKY else START_NOT_STICKY
        }
        return START_NOT_STICKY
    }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        destroyed = true
        networkRecovery.stop()
        recoveryCoordinator.close()
        // The Core belongs to the :panel process, not this Service instance.
        // A replacement Service must never let an old onDestroy stop its Core.
        check(PanelCoreLifecyclePolicy.onServiceDestroyed() == PanelCoreLifecyclePolicy.Action.KEEP_RUNNING)
        super.onDestroy()
    }

    private fun encodeWithState(status: Map<String, Any>): String = GoCoreResultMapper.encode(
        mergeLocalPanelHostStatus(
            coreStatus = status,
            foregroundServiceEnabled = persistentPolicy.foregroundActive,
            schedulerStatus = AndroidSchedulerHostStatus.status(
                applicationContext,
                persistentPolicy.foregroundActive,
                lastRecoveryTrigger,
            ),
            recoveryFailure = recoveryFailure,
        ),
    )

    @Synchronized
    private fun setPersistentScheduling(enabled: Boolean) {
        val preferences = getSharedPreferences(PERSISTENT_PREFS_NAME, MODE_PRIVATE)
        check(preferences.edit().putBoolean(PREF_PERSISTENT_ENABLED, enabled).commit())
        applyPersistentAction(persistentPolicy.update(enabled))
    }

    @Synchronized
    private fun restorePersistentSelection() {
        val selectionAction = persistentPolicy.update(readPersistentSelection())
        applyPersistentAction(
            if (persistentPolicy.enabled) persistentPolicy.recoveryAction() else selectionAction,
        )
    }

    private fun readPersistentSelection(): Boolean =
        getSharedPreferences(PERSISTENT_PREFS_NAME, MODE_PRIVATE)
            .getBoolean(PREF_PERSISTENT_ENABLED, false)

    private fun applyPersistentAction(action: PersistentForegroundPolicy.Action) {
        when (action) {
            PersistentForegroundPolicy.Action.START_FOREGROUND ->
                startForeground(NOTIFICATION_ID, buildNotification(starting = true)).also {
                    recoveryFailure = null
                    networkRecovery.start()
                    recoveryCoordinator.recoverAfterForegroundStarted()
                }
            PersistentForegroundPolicy.Action.STOP_FOREGROUND -> {
                networkRecovery.stop()
                recoveryCoordinator.cancelPending()
                recoveryFailure = null
                stopForeground(STOP_FOREGROUND_REMOVE)
                check(
                    PanelCoreLifecyclePolicy.onPersistentDisabled() ==
                        PanelCoreLifecyclePolicy.Action.KEEP_RUNNING
                )
                stopSelf()
            }
            PersistentForegroundPolicy.Action.NONE -> Unit
        }
    }

    private fun handleRecoveryResult(result: Result<Map<String, Any>>) {
        if (destroyed) return
        val status = result.getOrNull()
        val ready = status?.get("phase") == "ready"
        recoveryFailure = if (ready) {
            null
        } else {
            mapOf(
                "recovery_phase" to "failed",
                "recovery_failure_stage" to "persistent_recovery",
                "recovery_message" to "Embedded core recovery failed",
            )
        }
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(NOTIFICATION_ID, buildNotification(failed = !ready))
    }

    private fun recordCoreResult(status: Map<String, Any>): Map<String, Any> {
        if (status["phase"] == "ready" && recoveryFailure != null) {
            recoveryFailure = null
            if (!destroyed && persistentPolicy.foregroundActive) {
                getSystemService(NotificationManager::class.java)
                    .notify(NOTIFICATION_ID, buildNotification())
            }
        }
        return status
    }

    private fun explicitlyStopCore(): Map<String, Any> =
        when (PanelCoreLifecyclePolicy.onExplicitStop()) {
            PanelCoreLifecyclePolicy.Action.STOP_CORE -> LocalPanelRuntime.stop(localToken)
            PanelCoreLifecyclePolicy.Action.KEEP_RUNNING -> LocalPanelRuntime.status(localToken)
        }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ID,
                "本地面板调度",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "保持本地面板任务调度和依赖操作运行"
            }
        )
    }

    private fun buildNotification(
        starting: Boolean = false,
        failed: Boolean = false,
    ) = NotificationCompat.Builder(this, CHANNEL_ID)
        .setSmallIcon(R.mipmap.ic_launcher)
        .setContentTitle("呆呆面板本地服务")
        .setContentText(
            when {
                failed -> "本地面板核心恢复失败，请打开应用重试"
                AndroidResourceProtection.evaluate(AndroidResourceProtection.snapshot(this)).state == "resource_limited" ->
                    "本地面板运行中，低优先级任务已受资源保护"
                starting -> "正在恢复本地面板核心"
                else -> "本地面板与任务调度宿主正在运行"
            }
        )
        .setOngoing(true)
        .setContentIntent(
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, MainActivity::class.java),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )
        )
        .addAction(
            0,
            "停止",
            PendingIntent.getService(
                this,
                1,
                Intent(this, LocalPanelHostService::class.java).apply {
                    action = ACTION_DISABLE_PERSISTENT
                },
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )
        )
        .build()
}

internal fun mergeLocalPanelHostStatus(
    coreStatus: Map<String, Any>,
    foregroundServiceEnabled: Boolean,
    schedulerStatus: Map<String, Any>,
    recoveryFailure: Map<String, Any>?,
): Map<String, Any> = coreStatus.toMutableMap().apply {
    this["foreground_service_enabled"] = foregroundServiceEnabled
    putAll(schedulerStatus)
    recoveryFailure?.filterKeys { it.startsWith("recovery_") }?.let(::putAll)
}
