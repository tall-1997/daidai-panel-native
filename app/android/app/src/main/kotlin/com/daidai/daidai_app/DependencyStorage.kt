package com.daidai.daidai_app

import java.io.File

internal object DependencyStorage {
    const val PYTHON_VERSION = "3.14"
    const val NODE_VERSION = "18.20.4"
    const val MAX_CACHE_BYTES = 64L * 1024 * 1024
    const val MAX_BACKUPS = 5
    const val MAX_TASK_LOGS = 500
    const val MAX_SCRIPT_RUNS = 200
    const val TEMP_MAX_AGE_MILLIS = 24L * 60 * 60 * 1000

    private data class LockEntry(val lock: Any = Any(), var references: Int = 0)

    private val installLocks = mutableMapOf<String, LockEntry>()
    private val installLocksGuard = Any()

    fun pythonSitePackages(filesDir: File, version: String = PYTHON_VERSION): File =
        File(filesDir, "deps/python/$version/site-packages")

    fun npmCache(filesDir: File): File = File(filesDir, "deps/cache/npm")

    fun normalizedName(type: String, spec: String): String {
        val value = spec.trim()
        require(!value.startsWith("-")) { "依赖名称不能以选项前缀开头" }
        val packageName = when (type) {
            "python" -> value.substringBefore(';').substringBefore('[')
                .takeWhile { it !in "=<>!~ \t\r\n(," }
            "nodejs" -> if (value.startsWith("@")) {
                val slash = value.indexOf('/')
                val versionAt = if (slash >= 0) value.indexOf('@', slash) else -1
                if (versionAt > 0) value.substring(0, versionAt) else value
            } else value.substringBeforeLast('@', value)
            else -> value
        }
        return if (type == "python") packageName.lowercase().replace(Regex("[-_.]+"), "-")
        else packageName.lowercase()
    }

    fun requestedVersion(type: String, spec: String): String? {
        val value = spec.trim()
        return when (type) {
            "python" -> Regex("^.*?===?\\s*([^;\\s]+)").matchEntire(value)?.groupValues?.get(1)
            "nodejs" -> {
                val start = if (value.startsWith("@")) value.indexOf('@', value.indexOf('/').coerceAtLeast(0)) else value.lastIndexOf('@')
                if (start > 0) value.substring(start + 1).takeIf { it.isNotBlank() && it != "latest" } else null
            }
            else -> null
        }
    }

    fun satisfies(type: String, spec: String, installedVersion: String?): Boolean {
        if (installedVersion == null) return false
        val requested = requestedVersion(type, spec)
        if (requested != null) return requested == installedVersion
        if (type == "python" && Regex("[<>=!~]").containsMatchIn(spec)) return false
        return true
    }

    fun <T> withInstallLock(type: String, spec: String, runtimeVersion: String, action: () -> T): T {
        val key = "$type\u0000${normalizedName(type, spec)}\u0000$runtimeVersion"
        val entry = synchronized(installLocksGuard) {
            installLocks.getOrPut(key) { LockEntry() }.also { it.references++ }
        }
        return try {
            synchronized(entry.lock) { action() }
        } finally {
            synchronized(installLocksGuard) {
                entry.references--
                if (entry.references == 0) installLocks.remove(key, entry)
            }
        }
    }

    fun trimDirectory(root: File, maxBytes: Long, now: Long = System.currentTimeMillis()): Long {
        if (!root.exists()) return 0
        val files = root.walkBottomUp().filter { it.isFile }.sortedBy { it.lastModified() }.toList()
        var total = files.sumOf { it.length() }
        for (file in files) {
            if (total <= maxBytes) break
            val length = file.length()
            if (file.delete()) total -= length
        }
        root.walkBottomUp().filter { it.isDirectory && it != root && it.list().isNullOrEmpty() }.forEach { it.delete() }
        return total.coerceAtLeast(0)
    }

    fun removeExpired(root: File, maxAgeMillis: Long, now: Long = System.currentTimeMillis()) {
        if (!root.exists()) return
        root.listFiles().orEmpty().filter { now - it.lastModified() > maxAgeMillis }.forEach {
            if (it.isDirectory) it.deleteRecursively() else it.delete()
        }
    }

    fun retainNewest(root: File, maxFiles: Int) {
        root.listFiles().orEmpty().filter { it.isFile }.sortedByDescending { it.lastModified() }
            .drop(maxFiles).forEach { it.delete() }
    }
}
