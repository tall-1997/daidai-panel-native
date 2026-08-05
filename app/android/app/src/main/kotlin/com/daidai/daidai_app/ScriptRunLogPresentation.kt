package com.daidai.daidai_app

/** Pure presentation helpers shared by the local HTTP response and JVM tests. */
internal object ScriptRunLogPresentation {
    private val errorLine = Regex(
        "(?:Error|Exception|Traceback|FAILED|Failure|fatal|SyntaxError|ModuleNotFoundError|ImportError|TypeError|ValueError|RuntimeError|NameError|KeyError|PermissionError|No such file)",
        RegexOption.IGNORE_CASE,
    )

    fun errorSummary(logs: List<String>, failed: Boolean): String? {
        if (!failed) return null
        val nonBlank = logs.map(String::trim).filter(String::isNotEmpty)
        if (nonBlank.isEmpty()) return null

        // Python tracebacks conclude with the most useful exception line. Prefer it over
        // launcher's generic exit-code footer, while never inspecting process environment.
        val tracebackIndex = nonBlank.indexOfLast { it.startsWith("Traceback (most recent call last)") }
        if (tracebackIndex >= 0) {
            nonBlank.subList(tracebackIndex + 1, nonBlank.size).asReversed().firstOrNull {
                errorLine.containsMatchIn(it) && !isGenericFooter(it)
            }?.let { return it }
        }
        return nonBlank.asReversed().firstOrNull {
            errorLine.containsMatchIn(it) && !isGenericFooter(it)
        } ?: nonBlank.asReversed().firstOrNull { !isGenericFooter(it) } ?: nonBlank.last()
    }

    private fun isGenericFooter(line: String): Boolean =
        line.matches(Regex("Script failed with exit code \\d+", RegexOption.IGNORE_CASE))
}
