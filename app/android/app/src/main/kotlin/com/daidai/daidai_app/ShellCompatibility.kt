package com.daidai.daidai_app

/** Conservative, side-effect-free Bash syntax scanner for scripts and task shell commands. */
internal object ShellCompatibility {
    internal data class Scan(
        val requiresBash: Boolean,
        val matchedRules: List<String>,
    )

    private data class Rule(val name: String, val pattern: Regex)

    private val commandPrefix = "(?:^|[;&|{(]\\s*|\\b(?:if|then|while|until|do|else)\\s+)\\s*"
    private val rules = listOf(
        Rule("double-bracket", Regex("(?:^|[;&|({\\s])\\[\\[(?=\\s|$)")),
        Rule("arithmetic-command", Regex("(?<!\\$)\\(\\(")),
        Rule("regex-match", Regex("=~")),
        Rule("array-definition", Regex("\\b[A-Za-z_][A-Za-z0-9_]*=\\(")),
        Rule("array-assignment", Regex("\\b[A-Za-z_][A-Za-z0-9_]*\\[[^\\r\\n\\]]+\\]=")),
        Rule("array-expansion", Regex("\\$\\{#?[A-Za-z_][A-Za-z0-9_]*\\[[^}]+]}")),
        Rule("bash-source", Regex("(?:\\$\\{?|\\b)BASH_SOURCE(?:\\b|\\[)")),
        Rule("declare-typeset", Regex("$commandPrefix(?:declare|typeset)\\b")),
        Rule("local-array", Regex("${commandPrefix}local\\s+-[A-Za-z]*[aA][A-Za-z]*\\b")),
        Rule("mapfile-readarray", Regex("$commandPrefix(?:mapfile|readarray)\\b")),
        Rule("shopt", Regex("${commandPrefix}shopt\\b")),
        Rule("process-substitution", Regex("<\\(|>\\(")),
        Rule(
            "brace-expansion",
            Regex("\\{(?:-?[0-9]{1,10}\\.\\.-?[0-9]{1,10}(?:\\.\\.-?[0-9]{1,10})?|[A-Za-z]\\.\\.[A-Za-z]|[^{}\\s,]+(?:,[^{}\\s,]+)+)}"),
        ),
    )
    private val bashShebang = Regex("^#!.*\\bbash\\b")

    fun scan(source: String): Scan {
        if (source.isEmpty()) return Scan(requiresBash = false, matchedRules = emptyList())

        val matches = linkedSetOf<String>()
        source.lineSequence().forEachIndexed { index, rawLine ->
            if (index == 0 && bashShebang.containsMatchIn(rawLine)) matches += "bash-shebang"
            val line = maskLiteralsAndComments(rawLine)
            rules.forEach { rule ->
                if (rule.pattern.containsMatchIn(line)) matches += rule.name
            }
        }
        return Scan(requiresBash = matches.isNotEmpty(), matchedRules = matches.toList())
    }

    private fun maskLiteralsAndComments(line: String): String {
        val masked = StringBuilder(line.length)
        var index = 0
        var quote = '\u0000'
        while (index < line.length) {
            val char = line[index]
            when (quote) {
                '\'' -> {
                    masked.append(' ')
                    if (char == '\'') quote = '\u0000'
                    index++
                }
                '"' -> {
                    when {
                        char == '"' -> {
                            masked.append(' ')
                            quote = '\u0000'
                            index++
                        }
                        char == '\\' -> {
                            masked.append("  ")
                            index += if (index + 1 < line.length) 2 else 1
                        }
                        char == '$' -> index = appendExpansion(line, index, masked)
                        else -> {
                            masked.append(' ')
                            index++
                        }
                    }
                }
                else -> {
                    when {
                        char == '#' && isCommentStart(line, index) -> {
                            repeat(line.length - index) { masked.append(' ') }
                            index = line.length
                        }
                        char == '\'' || char == '"' -> {
                            masked.append(' ')
                            quote = char
                            index++
                        }
                        char == '\\' -> {
                            masked.append("  ")
                            index += if (index + 1 < line.length) 2 else 1
                        }
                        else -> {
                            masked.append(char)
                            index++
                        }
                    }
                }
            }
        }
        return masked.toString()
    }

    private fun appendExpansion(line: String, start: Int, output: StringBuilder): Int {
        if (start + 1 >= line.length) {
            output.append('$')
            return start + 1
        }
        val next = line[start + 1]
        val end = when {
            next == '{' -> line.indexOf('}', startIndex = start + 2).let { if (it < 0) line.length else it + 1 }
            next == '(' -> matchingParenthesisEnd(line, start + 1)
            next == '_' || next.isLetter() -> {
                var cursor = start + 2
                while (cursor < line.length && (line[cursor] == '_' || line[cursor].isLetterOrDigit())) cursor++
                cursor
            }
            else -> start + 2
        }
        output.append(line, start, end)
        return end
    }

    private fun matchingParenthesisEnd(line: String, open: Int): Int {
        var depth = 0
        var cursor = open
        while (cursor < line.length) {
            when (line[cursor]) {
                '(' -> depth++
                ')' -> if (--depth == 0) return cursor + 1
            }
            cursor++
        }
        return line.length
    }

    private fun isCommentStart(line: String, index: Int): Boolean =
        index == 0 || line[index - 1].isWhitespace() || line[index - 1] in ";|&()"
}
