package com.daidai.daidai_app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

object LocalPanelRecoveryTriggers {
    const val ACTION_PROCESS_RECOVERY = "com.daidai.daidai_app.LOCAL_PANEL_PROCESS_RECOVERY"
    const val ACTION_NETWORK_RESTORED = "com.daidai.daidai_app.LOCAL_PANEL_NETWORK_RESTORED"
    private const val UNIQUE_PERIODIC = "local-panel-periodic-reconciliation"

    fun reconcileNow(context: Context, trigger: String) {
        val request = OneTimeWorkRequestBuilder<LocalPanelRecoveryWorker>()
            .setInputData(androidx.work.workDataOf("trigger" to trigger))
            .build()
        WorkManager.getInstance(context.applicationContext).enqueueUniqueWork(
            "local-panel-recovery-$trigger",
            ExistingWorkPolicy.REPLACE,
            request,
        )
    }

    fun reconcileWhenNetworkRestored(context: Context) {
        val request = OneTimeWorkRequestBuilder<LocalPanelRecoveryWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setInputData(androidx.work.workDataOf("trigger" to "network"))
            .build()
        WorkManager.getInstance(context.applicationContext).enqueueUniqueWork(
            "local-panel-network-recovery",
            ExistingWorkPolicy.REPLACE,
            request,
        )
    }

    fun schedulePeriodicReconciliation(context: Context) {
        if (!LocalPanelHostService.isPersistentSchedulingEnabled(context)) {
            cancelPeriodicReconciliation(context)
            return
        }
        val request = PeriodicWorkRequestBuilder<LocalPanelRecoveryWorker>(15, TimeUnit.MINUTES)
            .setInputData(androidx.work.workDataOf("trigger" to "periodic"))
            .build()
        WorkManager.getInstance(context.applicationContext).enqueueUniquePeriodicWork(
            UNIQUE_PERIODIC,
            ExistingPeriodicWorkPolicy.UPDATE,
            request,
        )
    }

    fun cancelPeriodicReconciliation(context: Context) {
        WorkManager.getInstance(context.applicationContext).cancelUniqueWork(UNIQUE_PERIODIC)
    }
}

class LocalPanelRecoveryWorker(
    context: Context,
    params: WorkerParameters,
) : Worker(context, params) {
    override fun doWork(): Result {
        if (!LocalPanelHostService.isPersistentSchedulingEnabled(applicationContext)) {
            return Result.success()
        }
        val trigger = inputData.getString("trigger") ?: "periodic"
        val intent = Intent(applicationContext, LocalPanelHostService::class.java).apply {
            action = LocalPanelHostService.ACTION_RECOVER_PERSISTENT
            putExtra(LocalPanelHostService.EXTRA_RECOVERY_TRIGGER, trigger)
        }
        return runCatching {
            ContextCompat.startForegroundService(applicationContext, intent)
            Result.success()
        }.getOrDefault(Result.retry())
    }
}

class LocalPanelRecoveryReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        when (intent?.action) {
            Intent.ACTION_BOOT_COMPLETED -> LocalPanelRecoveryTriggers.reconcileNow(context, "boot")
            LocalPanelRecoveryTriggers.ACTION_PROCESS_RECOVERY -> LocalPanelRecoveryTriggers.reconcileNow(context, "process-recovery")
            LocalPanelRecoveryTriggers.ACTION_NETWORK_RESTORED -> LocalPanelRecoveryTriggers.reconcileWhenNetworkRestored(context)
        }
        if (LocalPanelHostService.isPersistentSchedulingEnabled(context)) {
            LocalPanelRecoveryTriggers.schedulePeriodicReconciliation(context)
        } else {
            LocalPanelRecoveryTriggers.cancelPeriodicReconciliation(context)
        }
    }
}
