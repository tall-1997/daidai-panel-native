package service

import (
	"encoding/json"
	"strings"
)

func SplitTaskEnvValues(raw string) []string {
	return splitTaskEnvValues(raw)
}

func JoinTaskEnvValues(values []string) string {
	return joinTaskEnvValues(values)
}

func splitTaskEnvValues(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var parsed []string
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return append([]string(nil), parsed...)
		}
	}

	separator := "&"
	if hasUnescapedTaskEnvSeparator(raw, "&&") {
		separator = "&&"
	}

	return splitEscapedTaskEnvValues(raw, separator)
}

func joinTaskEnvValues(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	}

	separator := "&"
	for _, value := range values {
		if strings.Contains(value, "&") {
			separator = "&&"
			break
		}
	}

	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, escapeTaskEnvValue(value, separator))
	}

	return strings.Join(escaped, separator)
}

func hasUnescapedTaskEnvSeparator(raw, separator string) bool {
	if separator == "" {
		return false
	}

	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if strings.HasPrefix(raw[i:], separator) {
			return true
		}
	}

	return false
}

func splitEscapedTaskEnvValues(raw, separator string) []string {
	if separator == "" {
		return []string{raw}
	}

	values := make([]string, 0, strings.Count(raw, separator)+1)
	var current strings.Builder
	escaped := false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if strings.HasPrefix(raw[i:], separator) {
			values = append(values, current.String())
			current.Reset()
			i += len(separator) - 1
			continue
		}

		current.WriteByte(ch)
	}

	if escaped {
		current.WriteByte('\\')
	}

	values = append(values, current.String())
	return values
}

func escapeTaskEnvValue(value, separator string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	switch separator {
	case "&&":
		value = strings.ReplaceAll(value, "&&", "\\&\\&")
	case "&":
		value = strings.ReplaceAll(value, "&", "\\&")
	}
	return value
}
