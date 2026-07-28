package com.daidai.daidai_app

import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Build
import android.os.Environment
import android.os.StatFs

data class AndroidResourceSnapshot(
    val batteryPercent: Int,
    val batteryCharging: Boolean,
    val thermalStatus: String,
    val lowMemory: Boolean,
    val availableMemoryBytes: Long,
    val availableStorageBytes: Long,
)

data class AndroidResourceGuarantee(
    val state: String,
    val reasonCode: String,
    val intervention: String,
)

object AndroidResourceProtection {
    private const val LOW_BATTERY_PERCENT = 15
    private const val MIN_AVAILABLE_STORAGE_BYTES = 512L * 1024L * 1024L

    fun snapshot(context: Context): AndroidResourceSnapshot {
        val battery = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = battery?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = battery?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
        val status = battery?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) ?: -1
        val charging = status == BatteryManager.BATTERY_STATUS_CHARGING ||
            status == BatteryManager.BATTERY_STATUS_FULL
        val batteryPercent = if (level >= 0 && scale > 0) level * 100 / scale else 100

        val memory = ActivityManager.MemoryInfo()
        context.getSystemService(ActivityManager::class.java).getMemoryInfo(memory)

        val storage = StatFs(Environment.getDataDirectory().absolutePath)
        return AndroidResourceSnapshot(
            batteryPercent = batteryPercent,
            batteryCharging = charging,
            thermalStatus = thermalStatus(context),
            lowMemory = memory.lowMemory,
            availableMemoryBytes = memory.availMem,
            availableStorageBytes = storage.availableBytes,
        )
    }

    fun evaluate(snapshot: AndroidResourceSnapshot): AndroidResourceGuarantee {
        if (snapshot.availableStorageBytes < MIN_AVAILABLE_STORAGE_BYTES) {
            return AndroidResourceGuarantee(
                state = "resource_limited",
                reasonCode = "storage_low",
                intervention = "释放至少 512MB 存储后恢复低优先级调度",
            )
        }
        if (snapshot.lowMemory) {
            return AndroidResourceGuarantee(
                state = "resource_limited",
                reasonCode = "memory_low",
                intervention = "关闭占用内存的应用后恢复低优先级调度",
            )
        }
        if (snapshot.thermalStatus in setOf("severe", "critical", "emergency", "shutdown")) {
            return AndroidResourceGuarantee(
                state = "resource_limited",
                reasonCode = "thermal_${snapshot.thermalStatus}",
                intervention = "等待设备降温后恢复低优先级调度",
            )
        }
        if (!snapshot.batteryCharging && snapshot.batteryPercent in 0 until LOW_BATTERY_PERCENT) {
            return AndroidResourceGuarantee(
                state = "resource_limited",
                reasonCode = "battery_low",
                intervention = "充电或提高电量后恢复低优先级调度",
            )
        }
        return AndroidResourceGuarantee(
            state = "foreground_continuous",
            reasonCode = "ok",
            intervention = "",
        )
    }

    private fun thermalStatus(context: Context): String {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return "unknown"
        return when (context.getSystemService(android.os.PowerManager::class.java).currentThermalStatus) {
            android.os.PowerManager.THERMAL_STATUS_NONE -> "none"
            android.os.PowerManager.THERMAL_STATUS_LIGHT -> "light"
            android.os.PowerManager.THERMAL_STATUS_MODERATE -> "moderate"
            android.os.PowerManager.THERMAL_STATUS_SEVERE -> "severe"
            android.os.PowerManager.THERMAL_STATUS_CRITICAL -> "critical"
            android.os.PowerManager.THERMAL_STATUS_EMERGENCY -> "emergency"
            android.os.PowerManager.THERMAL_STATUS_SHUTDOWN -> "shutdown"
            else -> "unknown"
        }
    }
}
