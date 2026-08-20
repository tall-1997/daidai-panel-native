package com.daidai.daidai_app

import android.content.Context
import org.json.JSONObject

object AndroidSchedulerHostStatus {
    fun status(context: Context, foregroundActive: Boolean, recoveryTrigger: String): Map<String, Any> {
        val snapshot = AndroidResourceProtection.snapshot(context)
        val resourceGuarantee = AndroidResourceProtection.evaluate(snapshot)
        val guarantee = if (!foregroundActive && resourceGuarantee.state == "foreground_continuous") {
            AndroidResourceGuarantee("system_compensation", "background_window", "启用持续调度可获得前台调度保障")
        } else {
            resourceGuarantee
        }
        return mapOf(
            "foreground_service_enabled" to foregroundActive,
            "scheduler_host_state" to if (foregroundActive) "foreground_continuous" else "system_compensation",
            "scheduler_recovery_trigger" to recoveryTrigger,
            "scheduler_guarantee_state" to guarantee.state,
            "scheduler_guarantee_reason" to guarantee.reasonCode,
            "scheduler_intervention" to guarantee.intervention,
            "resource_snapshot" to mapOf(
                "battery_percent" to snapshot.batteryPercent,
                "battery_charging" to snapshot.batteryCharging,
                "thermal_status" to snapshot.thermalStatus,
                "low_memory" to snapshot.lowMemory,
                "available_memory_bytes" to snapshot.availableMemoryBytes,
                "available_storage_bytes" to snapshot.availableStorageBytes,
            ),
        )
    }

    fun platformCapabilities(context: Context): JSONObject {
        return platformCapabilities(AndroidResourceProtection.evaluate(AndroidResourceProtection.snapshot(context)))
    }

    fun platformCapabilities(guarantee: AndroidResourceGuarantee): JSONObject {
        val capabilityState = if (guarantee.state == "resource_limited") "disabled" else "enabled"
        val reasonCode = if (guarantee.state == "resource_limited") guarantee.reasonCode else "ANDROID_HOST"
        val capabilities = JSONObject()
        listOf(
            "task_execution",
            "script_execution",
            "dependency_mutation",
            "subscription_pull",
            "system_update",
            "system_restart",
            "backup_mutation",
            "runtime_mutation",
            "notification_dispatch",
        ).forEach { id ->
            capabilities.put(
                id,
                JSONObject()
                    .put("state", capabilityState)
                    .put("reasonCode", reasonCode)
                    .put("adapterId", "android.scheduler-host"),
            )
        }
        return JSONObject()
            .put("version", 1)
            .put("capabilities", capabilities)
    }
}
