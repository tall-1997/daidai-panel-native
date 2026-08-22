package com.daidai.daidai_app

import java.io.File

/** Conservative, side-effect-free source scanner used before launching QingLong-style scripts. */
internal object ScriptCompatibility {
    const val MAX_AUTO_INSTALLS = 8
    const val INSTALL_TIMEOUT_SECONDS = 90L

    internal data class Scan(
        val pythonPackages: Set<String> = emptySet(),
        val nodePackages: Set<String> = emptySet(),
        val missingCompanionFiles: Set<String> = emptySet(),
    )

    private val pythonPackageMap = mapOf(
        "yaml" to "pyyaml", "bs4" to "beautifulsoup4", "crypto" to "pycryptodome",
        "requests" to "requests", "httpx" to "httpx", "lxml" to "lxml", "pandas" to "pandas",
        "numpy" to "numpy", "dateutil" to "python-dateutil", "dotenv" to "python-dotenv",
    )
    private val pythonBuiltins = setOf(
        "abc", "argparse", "asyncio", "base64", "binascii", "collections", "concurrent", "contextlib",
        "csv", "datetime", "decimal", "email", "enum", "functools", "glob", "hashlib", "heapq", "hmac",
        "html", "http", "importlib", "inspect", "io", "itertools", "json", "logging", "math", "multiprocessing",
        "os", "pathlib", "pickle", "platform", "queue", "random", "re", "secrets", "shlex", "shutil", "signal",
        "socket", "sqlite3", "ssl", "statistics", "string", "struct", "subprocess", "sys", "tempfile", "threading",
        "time", "traceback", "types", "typing", "unittest", "urllib", "uuid", "warnings", "xml", "zipfile", "notify",
    )
    private val nodeBuiltins = setOf(
        "assert", "buffer", "child_process", "cluster", "console", "crypto", "dns", "events", "fs", "http",
        "https", "module", "net", "os", "path", "perf_hooks", "process", "querystring", "readline", "stream",
        "string_decoder", "timers", "tls", "tty", "url", "util", "v8", "vm", "worker_threads", "zlib", "notify", "sendNotify",
    )
    private val allowedNodePackages = setOf(
        "axios", "crypto-js", "got", "lodash", "moment", "dayjs", "dotenv", "node-fetch", "tough-cookie",
        "cheerio", "qs", "form-data", "iconv-lite", "https-proxy-agent", "http-proxy-agent", "socks-proxy-agent",
        "request", "request-promise", "request-promise-native", "md5", "js-md5", "js-yaml", "yaml", "xml2js",
        "fast-xml-parser", "jsonwebtoken", "fs-extra", "csv-parse", "date-fns", "undici", "ws",
    )
    private val nodeInstallHintPattern = Regex("\\bnpm[ \\t]+(?:i|install|add)[ \\t]+((?:@?[A-Za-z0-9][A-Za-z0-9_./@+-]*)(?:[ \\t]+@?[A-Za-z0-9][A-Za-z0-9_./@+-]*)*)")
    private val pythonInstallHintPattern = Regex("\\bpip3?\\s+install\\s+([A-Za-z][A-Za-z0-9_.@-]*)")

    fun scan(file: File): Scan {
        val text = runCatching { file.readText() }.getOrDefault("")
        return when (file.extension.lowercase()) {
            "py" -> scanPython(text, file.parentFile ?: File("."))
            "js", "mjs", "cjs", "ts" -> scanNode(text, file.parentFile ?: File("."))
            else -> Scan()
        }
    }

    internal fun scanPython(text: String, directory: File): Scan {
        val packages = linkedSetOf<String>()
        val importPattern = Regex("^(?:from\\s+([A-Za-z_][\\w.]*)\\s+import\\b|import\\s+(.+))")
        pythonInstallHintPattern.findAll(text).forEach { match ->
            val requested = match.groupValues[1].substringBefore(',').trim()
            if (requested.isNotBlank()) packages += requested
        }
        text.lineSequence().filter { it.isNotBlank() && !it.first().isWhitespace() }.forEach { line ->
            val clean = line.substringBefore('#').trim()
            val match = importPattern.find(clean) ?: return@forEach
            val names = if (match.groupValues[1].isNotEmpty()) listOf(match.groupValues[1])
            else match.groupValues[2].split(',').map { it.trim().substringBefore(" as ") }
            names.map { it.substringBefore('.') }.filter { it.isNotBlank() && it !in pythonBuiltins }.forEach { module ->
                if (!File(directory, "$module.py").isFile && !File(directory, module).isDirectory) {
                    pythonPackageMap[module.lowercase()]?.let(packages::add) // Unknown names are deliberately not installed.
                }
            }
        }
        return Scan(pythonPackages = packages)
    }

    internal fun scanNode(text: String, directory: File): Scan {
        val packages = linkedSetOf<String>()
        val missing = linkedSetOf<String>()
        val requirePattern = Regex("\\brequire\\s*\\(\\s*(['\"])([^'\"]+)\\1\\s*\\)")
        val importPattern = Regex("(?:^|[;\\n])\\s*import\\s+(?:[^'\"]+?\\s+from\\s+)?(['\"])([^'\"]+)\\1")
        val dynamicImportPattern = Regex("\\bimport\\s*\\(\\s*(['\"])([^'\"]+)\\1\\s*\\)")
        fun addPackage(name: String) {
            val root = if (name.startsWith('@')) name.split('/').take(2).joinToString("/") else name.substringBefore('/')
            if (root.removePrefix("node:") !in nodeBuiltins && root in allowedNodePackages) packages += root
        }
        nodeInstallHintPattern.findAll(text).forEach { match ->
            match.groupValues[1].split(Regex("\\s+")).map(String::trim).filter(String::isNotEmpty).forEach { name ->
                if (!name.startsWith("-")) addPackage(name)
            }
        }
        requirePattern.findAll(text).forEach { match ->
            val name = match.groupValues[2]
            if (name.startsWith(".") || name.startsWith("/")) {
                if (!relativeModuleExists(directory, name)) missing += name
            } else {
                addPackage(name)
            }
        }
        importPattern.findAll(text).forEach { match ->
            val name = match.groupValues[2]
            if (name.startsWith(".") || name.startsWith("/")) {
                if (!relativeModuleExists(directory, name)) missing += name
            } else {
                addPackage(name)
            }
        }
        dynamicImportPattern.findAll(text).forEach { match ->
            val name = match.groupValues[2]
            if (name.startsWith(".") || name.startsWith("/")) {
                if (!relativeModuleExists(directory, name)) missing += name
            } else {
                addPackage(name)
            }
        }
        return Scan(nodePackages = packages, missingCompanionFiles = missing)
    }

    private fun relativeModuleExists(directory: File, request: String): Boolean {
        val target = File(directory, request)
        return target.isFile || target.isDirectory || File(target.path + ".js").isFile || File(target.path + ".json").isFile ||
            File(target, "index.js").isFile || File(target, "package.json").isFile
    }
}
