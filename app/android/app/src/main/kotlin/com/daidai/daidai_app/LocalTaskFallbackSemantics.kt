package com.daidai.daidai_app

import java.util.concurrent.TimeUnit
import java.util.stream.BaseStream

internal object LocalTaskFallbackSemantics {
    const val MAX_DEPENDENCY_INSTALLS = 5

    data class DependencyCandidate(val type: String, val name: String)

    private val pythonMissing = Regex("(?:ModuleNotFoundError|ImportError):\\s*No module named\\s*['\"]([^'\"]+)['\"]")
    private val nodeMissing = Regex("(?:Cannot find module|Error \\[ERR_MODULE_NOT_FOUND].*?)\\s*['\"]([^'\"]+)['\"]")

    fun detectMissingDependency(runtime: String, output: String): DependencyCandidate? {
        return when (runtime.lowercase()) {
            "python" -> pythonMissing.find(output)?.groupValues?.get(1)
                ?.substringBefore('.')
                ?.takeIf(::isInstallableName)
                ?.let { DependencyCandidate("python", it) }
            "nodejs" -> nodeMissing.find(output)?.groupValues?.get(1)
                ?.takeIf { isInstallableName(it) && !it.startsWith(".") && !it.startsWith("/") }
                ?.let { DependencyCandidate("nodejs", it) }
            else -> null
        }
    }

    fun cursor(rawCursor: String?, lastEventId: String?): Long =
        (rawCursor?.toLongOrNull() ?: lastEventId?.toLongOrNull() ?: 0L).coerceAtLeast(0L)

    fun linesAfterCursor(lines: List<String>, cursor: Long, latestCursor: Long = lines.size.toLong()): List<Pair<Long, String>> {
        val firstCursor = (latestCursor - lines.size + 1L).coerceAtLeast(1L)
        return lines.mapIndexed { index, line -> (firstCursor + index) to line }
            .filter { it.first > cursor }
    }

    fun applyRuntimeEnvironment(target: MutableMap<String, String>, runtime: Map<String, String>) {
        target.putAll(runtime)
    }

    fun nextDependency(
        runtime: String,
        output: String,
        attempted: Set<DependencyCandidate>,
        installCount: Int,
    ): DependencyCandidate? {
        if (installCount >= MAX_DEPENDENCY_INSTALLS) return null
        return detectMissingDependency(runtime, output)?.takeUnless(attempted::contains)
    }

    private fun isInstallableName(name: String): Boolean =
        name.isNotBlank() && name.matches(Regex("[A-Za-z0-9@][A-Za-z0-9_./@-]*"))
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
