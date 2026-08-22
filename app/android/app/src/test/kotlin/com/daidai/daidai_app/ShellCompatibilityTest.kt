package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShellCompatibilityTest {
    @Test fun acceptsEmptyAndPosixShellInput() {
        assertCompatible("")
        assertCompatible("#!/bin/sh\nname='世界'\nprintf '%s\\n' \"\$name\"\nvalue=\$((value + 1))")
    }

    @Test fun ignoresCommentsAndCommonQuotedLiterals() {
        val source = """
            # [[ comment ]] and values=(one two)
            echo '(( count++ )) <(date) {1..3}'
            printf '%s\n' "declare -a items; shopt; value =~ pattern"
            echo 中文内容 # mapfile -t values
            printf '%s\n' foo[[bar 'items[0]=value'
        """.trimIndent()

        assertCompatible(source)
    }

    @Test fun preservesEffectiveExpansionsInsideDoubleQuotes() {
        assertEquals(
            listOf("array-expansion", "bash-source"),
            ShellCompatibility.scan("source=\"\${BASH_SOURCE[0]}\"; printf '%s' \"\${items[@]}\"").matchedRules,
        )
    }

    @Test fun detectsRequestedBashisms() {
        val cases = linkedMapOf(
            "bash-shebang" to "#!/usr/bin/env bash\necho ok",
            "double-bracket" to "if [[ -n \$value ]]; then echo yes; fi",
            "arithmetic-command" to "((count++))",
            "regex-match" to "[[ \$value =~ ^ok ]];",
            "array-definition" to "items=(one two)",
            "array-assignment" to "items[0]=one",
            "array-expansion" to "printf '%s' \"\${items[@]}\"",
            "bash-source" to "echo \"\${BASH_SOURCE[0]}\"",
            "declare-typeset" to "declare -A lookup=()",
            "local-array" to "local -a values=()",
            "mapfile-readarray" to "mapfile -t lines < input",
            "shopt" to "shopt -s nullglob",
            "process-substitution" to "diff <(sort a) <(sort b)",
            "brace-expansion" to "printf '%s' {1..5}",
        )

        cases.forEach { (rule, source) ->
            val result = ShellCompatibility.scan(source)
            assertTrue("Expected Bash requirement for $rule", result.requiresBash)
            assertTrue("Expected rule $rule in ${result.matchedRules}", rule in result.matchedRules)
        }
    }

    @Test fun detectsAlternativeForms() {
        assertRule("declare-typeset", "typeset -i count=1")
        assertRule("declare-typeset", "build() { declare -a values=(); }")
        assertRule("local-array", "local -A lookup=()")
        assertRule("local-array", "if local -ra values=(); then :; fi")
        assertRule("mapfile-readarray", "readarray lines < input")
        assertRule("process-substitution", "tee >(consumer)")
        assertRule("brace-expansion", "echo {alpha,beta}")
        assertRule("brace-expansion", "echo {a..z}")
    }

    private fun assertCompatible(source: String) {
        val result = ShellCompatibility.scan(source)
        assertFalse(result.requiresBash)
        assertEquals(emptyList<String>(), result.matchedRules)
    }

    private fun assertRule(rule: String, source: String) {
        val result = ShellCompatibility.scan(source)
        assertTrue(result.requiresBash)
        assertTrue(rule in result.matchedRules)
    }
}
