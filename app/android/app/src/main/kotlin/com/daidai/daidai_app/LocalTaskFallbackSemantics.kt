package com.daidai.daidai_app

import java.util.concurrent.TimeUnit
import java.util.stream.BaseStream

internal object LocalTaskFallbackSemantics {
    const val MAX_DEPENDENCY_INSTALLS = 5

    data class DependencyCandidate(val type: String, val name: String)
    data class TaskCommandPlan(
        val scriptPath: String,
        val scriptArgs: List<String> = emptyList(),
        val mode: String = "normal",
        val timeoutSeconds: Long = 300,
        val envName: String = "",
        val accountSpec: String = "",
    )
    data class TaskEnvironment(val index: Int, val values: Map<String, String>)

    private val pythonMissing = Regex("(?:ModuleNotFoundError|ImportError):\\s*No module named\\s*['\"]([^'\"]+)['\"]")
    private val nodeMissing = Regex("(?:Cannot find module|Error \\[ERR_MODULE_NOT_FOUND].*?)\\s*['\"]([^'\"]+)['\"]")
    private val pythonInstallHint = Regex("\\bpip3?\\s+install\\s+([A-Za-z][A-Za-z0-9_.@-]*)")
    private val nodeInstallHint = Regex("\\bnpm[ \\t]+(?:i|install|add)[ \\t]+((?:@?[A-Za-z0-9][A-Za-z0-9_./@+-]*)(?:[ \\t]+@?[A-Za-z0-9][A-Za-z0-9_./@+-]*)*)")
    private val managedModules = setOf("notify", "sendNotify")
    private val pythonPackageMap = mapOf(
        "bs4" to "beautifulsoup4",
        "crypto" to "pycryptodome",
        "cryptodome" to "pycryptodomex",
        "cv2" to "opencv-python",
        "dateutil" to "python-dateutil",
        "dotenv" to "python-dotenv",
        "pil" to "pillow",
        "socks" to "pysocks",
        "yaml" to "pyyaml",
    )

    fun detectMissingDependency(runtime: String, output: String): DependencyCandidate? {
        return detectMissingDependencies(runtime, output).firstOrNull()
    }

    internal fun pythonImportName(packageName: String): String? {
        val normalized = packageName.trim().substringBefore(';').substringBefore('[')
            .takeWhile { it !in "=<>!~ \t\r\n(," }
            .lowercase().replace(Regex("[-_.]+"), "-")
        val importName = when (normalized) {
        "pycryptodome" -> "Crypto"
        "pycryptodomex" -> "Cryptodome"
        "beautifulsoup4" -> "bs4"
        "python-dateutil" -> "dateutil"
        "python-dotenv" -> "dotenv"
        "opencv-python" -> "cv2"
        "pillow" -> "PIL"
        "pyyaml" -> "yaml"
        else -> normalized.replace('-', '_')
        }
        return importName.takeIf { Regex("[A-Za-z_][A-Za-z0-9_.]*").matches(it) }
    }

    internal fun detectMissingDependencies(runtime: String, output: String): List<DependencyCandidate> {
        val candidates = linkedSetOf<DependencyCandidate>()
        return when (runtime.lowercase()) {
            "python" -> {
                pythonMissing.find(output)?.groupValues?.get(1)
                    ?.substringBefore('.')
                    ?.takeIf { isInstallableName(it) && it !in managedModules }
                    ?.let { candidates += DependencyCandidate("python", pythonInstallPackageName(it)) }
                pythonInstallHint.findAll(output).forEach { match ->
                    match.groupValues[1]
                        .takeIf { isInstallableName(it) && it !in managedModules }
                        ?.let { candidates += DependencyCandidate("python", pythonInstallPackageName(it)) }
                }
                candidates.toList()
            }
            "nodejs" -> {
                nodeMissing.find(output)?.groupValues?.get(1)
                    ?.takeIf { isNodeInstallableName(it) }
                    ?.let { candidates += DependencyCandidate("nodejs", it) }
                nodeInstallHint.findAll(output).forEach { match ->
                    match.groupValues[1].split(Regex("[ \\t]+"))
                        .map(String::trim)
                        .filter(String::isNotEmpty)
                        .filter(::isNodeInstallableName)
                        .forEach { candidates += DependencyCandidate("nodejs", it) }
                }
                candidates.toList()
            }
            else -> emptyList()
        }
    }

    fun cursor(rawCursor: String?, lastEventId: String?): Long =
        (lastEventId?.toLongOrNull() ?: rawCursor?.toLongOrNull() ?: 0L).coerceAtLeast(0L)

    fun linesAfterCursor(lines: List<String>, cursor: Long, latestCursor: Long = lines.size.toLong()): List<Pair<Long, String>> {
        val firstCursor = (latestCursor - lines.size + 1L).coerceAtLeast(1L)
        return lines.mapIndexed { index, line -> (firstCursor + index) to line }
            .filter { it.first > cursor }
    }

    fun applyRuntimeEnvironment(target: MutableMap<String, String>, runtime: Map<String, String>) {
        target.putAll(runtime)
    }

    fun shouldNotify(status: String, notifyFailure: Boolean, notifySuccess: Boolean, notifyAbort: Boolean): Boolean =
        when (status) {
            "success" -> notifySuccess
            "aborted" -> notifyAbort
            else -> notifyFailure
        }

    fun tokenize(command: String): List<String> {
        val tokens = mutableListOf<String>()
        val current = StringBuilder()
        var quote: Char? = null
        var escaped = false
        var tokenStarted = false

        fun flush() {
            if (tokenStarted) tokens += current.toString()
            current.clear()
            tokenStarted = false
        }

        command.forEach { char ->
            when {
                escaped -> {
                    tokenStarted = true
                    if (char == '\'' || char == '"' || char == '\\' || char.isWhitespace()) {
                        current.append(char)
                    } else {
                        current.append('\\').append(char)
                    }
                    escaped = false
                }
                char == '\\' && quote != '\'' -> {
                    tokenStarted = true
                    escaped = true
                }
                quote != null -> {
                    if (char == quote) quote = null else current.append(char)
                }
                char == '\'' || char == '"' -> {
                    tokenStarted = true
                    quote = char
                }
                char.isWhitespace() -> flush()
                else -> {
                    tokenStarted = true
                    current.append(char)
                }
            }
        }
        if (escaped) current.append('\\')
        require(quote == null) { "命令引号未闭合" }
        flush()
        return tokens
    }

    fun parseTaskCommand(command: String, pathExists: (String) -> Boolean): TaskCommandPlan {
        val tokens = tokenize(command)
        require(tokens.firstOrNull() == "task") { "命令格式无效，格式: task [-m 超时] [-l] <脚本路径> [now|conc|desi] [-- 参数]" }

        var index = 1
        var timeoutSeconds = 300L
        while (index < tokens.size) {
            when (tokens[index]) {
                "-m" -> {
                    require(index + 1 < tokens.size) { "缺少 -m 对应的超时时间" }
                    timeoutSeconds = parseTimeoutSeconds(tokens[index + 1])
                    index += 2
                }
                "-l" -> index++
                else -> break
            }
        }

        val remaining = tokens.drop(index)
        val separator = remaining.indexOf("--")
        val taskTokens = if (separator >= 0) remaining.take(separator) else remaining
        val scriptArgs = if (separator >= 0) remaining.drop(separator + 1) else emptyList()
        require(taskTokens.isNotEmpty()) { "命令格式无效，缺少脚本路径" }

        var pathCount = 0
        var scriptPath = ""
        for (count in 1..taskTokens.size) {
            val candidate = taskTokens.take(count).joinToString(" ")
            if (supportedScriptPath(candidate) && pathExists(candidate) && validRemainder(taskTokens.drop(count))) {
                pathCount = count
                scriptPath = candidate
            }
        }
        require(pathCount > 0) {
            val candidate = taskTokens.takeWhile { it !in setOf("now", "conc", "desi") }.joinToString(" ")
            "脚本不存在或路径无效: ${candidate.ifBlank { taskTokens.first() }}"
        }

        val remainder = taskTokens.drop(pathCount)
        if (remainder.isEmpty()) return TaskCommandPlan(scriptPath, scriptArgs, timeoutSeconds = timeoutSeconds)
        return when (remainder[0]) {
            "now" -> {
                require(remainder.size == 1) { "now 模式不支持额外参数，请将脚本参数放在 -- 后" }
                TaskCommandPlan(scriptPath, scriptArgs, "now", timeoutSeconds)
            }
            "conc", "desi" -> {
                require(remainder.size >= 2) { "${remainder[0]} 模式缺少环境变量名称" }
                TaskCommandPlan(scriptPath, scriptArgs, remainder[0], timeoutSeconds, remainder[1], remainder.drop(2).joinToString(" "))
            }
            else -> TaskCommandPlan(scriptPath, remainder + scriptArgs, timeoutSeconds = timeoutSeconds)
        }
    }

    fun taskEnvironments(plan: TaskCommandPlan, environment: Map<String, String>): List<TaskEnvironment> {
        if (plan.mode !in setOf("conc", "desi")) return listOf(TaskEnvironment(0, emptyMap()))
        val rawValue = environment[plan.envName].orEmpty()
        require(rawValue.isNotBlank()) { "环境变量 ${plan.envName} 不存在或为空" }
        val values = splitEnvironmentValues(rawValue)
        val indices = parseAccountSpec(plan.accountSpec, values.size)
        if (plan.mode == "desi") {
            val selected = indices.map { values[it - 1] }
            return listOf(TaskEnvironment(0, mapOf(
                plan.envName to joinEnvironmentValues(selected),
                "envParam" to plan.envName,
                "numParam" to indices.joinToString(" "),
                "TASK_EXEC_MODE" to plan.mode,
                "TASK_ENV_NAME" to plan.envName,
                "TASK_ACCOUNT_SPEC" to indices.joinToString(" "),
            )))
        }
        return indices.map { index -> TaskEnvironment(index, mapOf(
            plan.envName to values[index - 1],
            "envParam" to plan.envName,
            "numParam" to index.toString(),
            "TASK_EXEC_MODE" to plan.mode,
            "TASK_ENV_NAME" to plan.envName,
            "TASK_ACCOUNT_SPEC" to index.toString(),
            "TASK_ACCOUNT_NUMBER" to index.toString(),
        )) }
    }

    internal fun splitEnvironmentValues(raw: String): List<String> {
        val trimmed = raw.trim()
        if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
            runCatching {
                val array = org.json.JSONArray(trimmed)
                return List(array.length()) { array.getString(it) }
            }
        }
        val separator = if (hasUnescapedSeparator(raw, "&&")) "&&" else "&"
        val values = mutableListOf<String>()
        val current = StringBuilder()
        var escaped = false
        var index = 0
        while (index < raw.length) {
            val char = raw[index]
            when {
                escaped -> {
                    current.append(char)
                    escaped = false
                }
                char == '\\' -> escaped = true
                raw.startsWith(separator, index) -> {
                    values += current.toString()
                    current.clear()
                    index += separator.length - 1
                }
                else -> current.append(char)
            }
            index++
        }
        if (escaped) current.append('\\')
        values += current.toString()
        return values
    }

    internal fun joinEnvironmentValues(values: List<String>): String {
        if (values.isEmpty()) return ""
        if (values.size == 1) return values[0]
        val separator = if (values.any { '&' in it }) "&&" else "&"
        return values.joinToString(separator) { value ->
            value.replace("\\", "\\\\").let {
                it.replace("&", "\\&")
            }
        }
    }

    private fun hasUnescapedSeparator(raw: String, separator: String): Boolean {
        var escaped = false
        raw.indices.forEach { index ->
            if (escaped) {
                escaped = false
            } else if (raw[index] == '\\') {
                escaped = true
            } else if (raw.startsWith(separator, index)) {
                return true
            }
        }
        return false
    }

    private fun parseAccountSpec(raw: String, total: Int): List<Int> {
        require(total > 0) { "环境变量账号数量为空" }
        val spec = raw.trim().ifEmpty { "1-max" }
        val result = linkedSetOf<Int>()
        spec.split(Regex("[\\s,]+")).filter(String::isNotEmpty).forEach { token ->
            val separator = listOf('-', '~', '_').firstOrNull(token::contains)
            if (separator == null) {
                result += accountEndpoint(token, total)
            } else {
                val parts = token.split(separator, limit = 2)
                val start = accountEndpoint(parts[0], total)
                val end = accountEndpoint(parts[1], total)
                if (start <= end) (start..end).forEach(result::add) else (start downTo end).forEach(result::add)
            }
        }
        require(result.isNotEmpty()) { "未匹配到有效的账号序号" }
        return result.toList()
    }

    private fun accountEndpoint(raw: String, total: Int): Int {
        val value = if (raw.isBlank() || raw.equals("max", true)) total else raw.toIntOrNull()
        require(value != null && value in 1..total) { "无效的账号序号: $raw" }
        return value
    }

    private fun supportedScriptPath(path: String): Boolean =
        path.substringAfterLast('.', "").lowercase() in setOf("py", "js", "mjs", "ts", "sh", "go")

    private fun validRemainder(tokens: List<String>): Boolean = when (tokens.firstOrNull()) {
        null -> true
        "now" -> tokens.size == 1
        "conc", "desi" -> tokens.size >= 2
        else -> true
    }

    private fun parseTimeoutSeconds(raw: String): Long {
        val value = raw.lowercase()
        require(value.isNotEmpty()) { "超时时间不能为空" }
        val multiplier = when (value.last()) {
            's' -> 1L
            'm' -> 60L
            'h' -> 3600L
            'd' -> 86400L
            else -> 1L
        }
        val number = if (value.last().isLetter()) value.dropLast(1) else value
        return (number.toLongOrNull()?.takeIf { it > 0 } ?: throw IllegalArgumentException("无效的超时时间: $raw")) * multiplier
    }

    fun nextDependency(
        runtime: String,
        output: String,
        attempted: Set<DependencyCandidate>,
        installCount: Int,
    ): DependencyCandidate? {
        if (installCount >= MAX_DEPENDENCY_INSTALLS) return null
        return detectMissingDependencies(runtime, output).firstOrNull { it !in attempted }
    }

    private fun isInstallableName(name: String): Boolean =
        name.isNotBlank() && name.matches(Regex("[A-Za-z0-9@][A-Za-z0-9_./@-]*"))

    private fun isNodeInstallableName(name: String): Boolean =
        isInstallableName(name) && name !in managedModules && !name.startsWith(".") && !name.startsWith("/")

    private fun pythonInstallPackageName(moduleOrPackage: String): String =
        pythonPackageMap[moduleOrPackage.substringBefore('.').lowercase()] ?: moduleOrPackage
}

internal object LocalLogQueryContract {
    data class Query(
        val where: String,
        val args: Array<String>,
        val limit: Int,
        val offset: Int,
    )

    fun build(params: Map<String, String>): Query {
        val clauses = mutableListOf<String>()
        val args = mutableListOf<String>()
        params["task_id"]?.toLongOrNull()?.let {
            clauses += "l.task_id = ?"
            args += it.toString()
        }
        params["status"]?.toIntOrNull()?.let {
            clauses += "l.status = ?"
            args += it.toString()
        }
        params["keyword"]?.trim()?.takeIf(String::isNotEmpty)?.let {
            clauses += "(t.name LIKE ? OR l.content LIKE ?)"
            args += "%$it%"
            args += "%$it%"
        }
        val page = (params["page"]?.toIntOrNull() ?: 1).coerceAtLeast(1)
        val requestedPageSize = params["page_size"]?.toIntOrNull() ?: 20
        val pageSize = if (requestedPageSize in 1..100) requestedPageSize else 20
        return Query(
            where = if (clauses.isEmpty()) "" else " WHERE ${clauses.joinToString(" AND ")}",
            args = args.toTypedArray(),
            limit = pageSize,
            offset = (page - 1) * pageSize,
        )
    }

    fun logFile(taskId: Long, logId: Long, size: Long, createdAt: String): Map<String, Any> {
        val filename = "task-$taskId-$logId.log"
        return mapOf(
            "filename" to filename,
            "path" to "task_$taskId/$filename",
            "log_id" to logId,
            "size" to size,
            "created_at" to createdAt,
        )
    }
}

internal object LocalTaskProcessTerminator {
    fun terminate(process: Process, waitForExit: (Process) -> Boolean = { it.waitFor(1, TimeUnit.SECONDS) }) {
        descendants(process).asReversed().forEach(::destroyHandle)
        process.destroy()
        if (runCatching { !waitForExit(process) }.getOrDefault(true)) process.destroyForcibly()
    }

    private fun descendants(process: Process): List<Any> = runCatching {
        val handleType = Class.forName("java.lang.ProcessHandle")
        val handle = Process::class.java.getMethod("toHandle").invoke(process)
        val stream = handleType.getMethod("descendants").invoke(handle)
        try {
            val iterator = BaseStream::class.java.getMethod("iterator").invoke(stream) as Iterator<*>
            iterator.asSequence().filterNotNull().toList()
        } finally {
            (stream as? AutoCloseable)?.close()
        }
    }.getOrDefault(emptyList())

    private fun destroyHandle(handle: Any) {
        val handleType = Class.forName("java.lang.ProcessHandle")
        runCatching { handleType.getMethod("destroy").invoke(handle) }
        val alive = runCatching { handleType.getMethod("isAlive").invoke(handle) as Boolean }.getOrDefault(false)
        if (alive) runCatching { handleType.getMethod("destroyForcibly").invoke(handle) }
    }
}
