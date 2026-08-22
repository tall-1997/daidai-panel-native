package com.daidai.daidai_app

import java.io.File
import org.json.JSONObject

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

    private val nodeRequireCompatiblePackageSpecs = mapOf(
        "uuid" to "uuid@8.3.2",
        "axios" to "axios@0.27.2",
        "node-fetch" to "node-fetch@2.7.0",
        "got" to "got@11.8.6",
        "chalk" to "chalk@4.1.2",
        "ora" to "ora@5.4.1",
        "execa" to "execa@5.1.1",
        "nanoid" to "nanoid@3.3.7",
        "p-limit" to "p-limit@3.1.0",
        "p-queue" to "p-queue@6.6.2",
        "p-retry" to "p-retry@4.6.2",
        "p-timeout" to "p-timeout@4.1.0",
        "quick-lru" to "quick-lru@5.1.1",
        "yocto-queue" to "yocto-queue@0.1.0",
        "is-stream" to "is-stream@2.0.1",
        "is-port-reachable" to "is-port-reachable@3.1.0",
        "make-dir" to "make-dir@3.1.0",
        "find-up" to "find-up@5.0.0",
        "locate-path" to "locate-path@6.0.0",
        "path-exists" to "path-exists@4.0.0",
        "camelcase" to "camelcase@6.3.0",
        "decamelize" to "decamelize@4.0.0",
        "supports-color" to "supports-color@8.1.1",
        "file-type" to "file-type@16.5.4",
        "mime" to "mime@3.0.0",
        "strip-ansi" to "strip-ansi@6.0.1",
        "string-width" to "string-width@4.2.3",
        "wrap-ansi" to "wrap-ansi@7.0.0",
        "cli-truncate" to "cli-truncate@2.1.0",
        "boxen" to "boxen@5.1.2",
        "open" to "open@8.4.2",
        "del" to "del@6.1.1",
        "globby" to "globby@11.1.0",
        "cheerio" to "cheerio@1.0.0-rc.12",
        "undici" to "undici@5.28.5",
        "ws" to "ws@7.5.10",
        "tough-cookie" to "tough-cookie@4.1.4",
        "form-data" to "form-data@4.0.0",
        "https-proxy-agent" to "https-proxy-agent@5.0.1",
        "http-proxy-agent" to "http-proxy-agent@5.0.0",
        "socks-proxy-agent" to "socks-proxy-agent@7.0.0",
        "hpagent" to "hpagent@1.2.0",
        "tunnel" to "tunnel@0.0.6",
        "tunnel-agent" to "tunnel-agent@0.6.0",
        "request" to "request@2.88.2",
        "request-promise" to "request-promise@4.2.6",
        "request-promise-native" to "request-promise-native@1.0.9",
        "crypto-js" to "crypto-js@4.2.0",
        "md5" to "md5@2.3.0",
        "js-md5" to "js-md5@0.7.3",
        "qs" to "qs@6.11.2",
        "query-string" to "query-string@7.1.3",
        "querystring" to "querystring@0.2.1",
        "moment" to "moment@2.29.4",
        "dayjs" to "dayjs@1.11.10",
        "lodash" to "lodash@4.17.21",
        "dotenv" to "dotenv@16.4.5",
        "yaml" to "yaml@2.3.4",
        "js-yaml" to "js-yaml@4.1.0",
        "adm-zip" to "adm-zip@0.5.10",
        "node-rsa" to "node-rsa@1.1.1",
        "rsa-pem-from-mod-exp" to "rsa-pem-from-mod-exp@0.8.5",
        "iconv-lite" to "iconv-lite@0.6.3",
        "date-fns" to "date-fns@2.30.0",
        "csv-parse" to "csv-parse@5.5.6",
        "fast-xml-parser" to "fast-xml-parser@4.3.6",
        "xml2js" to "xml2js@0.6.2",
        "jsonwebtoken" to "jsonwebtoken@9.0.2",
        "jimp" to "jimp@0.22.12",
        "fs-extra" to "fs-extra@11.2.0",
        "data-uri-to-buffer" to "data-uri-to-buffer@3.0.1",
        "fetch-blob" to "fetch-blob@2.1.2",
        "formdata-polyfill" to "formdata-polyfill@4.0.10",
    )

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

    fun ensureNodePackageManifest(nodeDir: File) {
        nodeDir.mkdirs()
        val manifest = File(nodeDir, "package.json")
        val validManifest = manifest.isFile && runCatching {
            val json = JSONObject(manifest.readText())
            json.optString("name").isNotBlank()
        }.getOrDefault(false)
        if (!validManifest) {
            manifest.writeText(
                JSONObject()
                    .put("name", "daidai-android-node-deps")
                    .put("private", true)
                    .put("version", "1.0.0")
                    .toString(2) + "\n",
            )
        }

        val lock = File(nodeDir, "package-lock.json")
        if (lock.isFile && runCatching { JSONObject(lock.readText()); true }.getOrDefault(false).not()) {
            lock.renameTo(File(nodeDir, "package-lock.json.broken-${System.currentTimeMillis()}"))
        }
    }

    fun nodeInstallPackageSpec(packageName: String): String {
        val value = packageName.trim()
        if (value.isEmpty() || nodePackageSpecHasExplicitVersionOrSource(value)) return value
        val normalized = normalizedName("nodejs", value)
        return nodeRequireCompatiblePackageSpecs[normalized] ?: value
    }

    fun nodeInstallCompatibilityNotice(packageName: String): String {
        val value = packageName.trim()
        if (value.isEmpty()) return ""
        if (nodePackageSpecHasExplicitVersionOrSource(value)) return "[Node.js 依赖] 已按指定版本或来源安装：$value"
        val installSpec = nodeInstallPackageSpec(value)
        return if (installSpec != value) "[Node.js 依赖] $value 已命中 CommonJS 兼容映射，将安装：$installSpec"
        else "[Node.js 依赖] $value：未命中兼容映射，将按 npm 默认版本安装。"
    }

    private fun nodePackageSpecHasExplicitVersionOrSource(spec: String): Boolean {
        val value = spec.trim()
        if (value.isEmpty()) return false
        val lower = value.lowercase()
        val sourcePrefixes = listOf(
            "file:", "link:", "workspace:", "npm:", "http://", "https://",
            "git+", "git://", "ssh://", "github:", "gitlab:", "bitbucket:",
        )
        if (sourcePrefixes.any(lower::startsWith)) return true
        if (value.startsWith("@")) {
            val slash = value.indexOf('/')
            if (slash < 0) return false
            return value.lastIndexOf('@') > slash
        }
        return value.lastIndexOf('@') > 0
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
        if (requested != null) {
            if (type == "nodejs" && requested.matches(Regex("\\d+(?:\\.\\d+)?"))) {
                return installedVersion == requested || installedVersion.startsWith("$requested.")
            }
            return requested == installedVersion
        }
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
