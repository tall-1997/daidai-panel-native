package service

import (
	"bufio"
	"io"
	"log"
	"regexp"
	"strings"
	"time"
)

const (
	PanelLogLevelDebug = "debug"
	PanelLogLevelInfo  = "info"
	PanelLogLevelWarn  = "warn"
	PanelLogLevelError = "error"
)

var panelLogLinePattern = regexp.MustCompile(`^\[(DEBUG|INFO|WARN|ERROR)\]\s+`)
var panelGINLinePattern = regexp.MustCompile(`^\[GIN\]\s+\d{4}/\d{2}/\d{2}\s+-\s+\d{2}:\d{2}:\d{2}\s+\|\s+(\d{3})\s+\|\s+(.+?)\s+\|\s+([^|]+?)\s+\|\s+([A-Z]+)\s+\"([^\"]+)\"$`)
var panelLogLevelPriority = map[string]int{
	PanelLogLevelDebug: 10,
	PanelLogLevelInfo:  20,
	PanelLogLevelWarn:  30,
	PanelLogLevelError: 40,
}

type PanelLogFilterWriter struct {
	dst io.Writer
}

func NewPanelLogFilterWriter(dst io.Writer) *PanelLogFilterWriter {
	return &PanelLogFilterWriter{dst: dst}
}

func (w *PanelLogFilterWriter) Write(p []byte) (int, error) {
	text := string(p)
	if shouldSuppressPanelStartupLog(text) {
		return len(p), nil
	}

	lines := splitLogPayloadLines(text)
	if len(lines) == 0 {
		return len(p), nil
	}

	var builder strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		builder.WriteString(formatPanelLogLine(trimmed))
		builder.WriteByte('\n')
	}

	if builder.Len() == 0 {
		return len(p), nil
	}
	_, err := w.dst.Write([]byte(builder.String()))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func splitLogPayloadLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 && strings.TrimSpace(text) != "" {
		lines = append(lines, text)
	}
	return lines
}

func formatPanelLogLine(line string) string {
	if panelLogLinePattern.MatchString(line) {
		return line
	}
	if compacted, ok := compactGINLogLine(line); ok {
		return compacted
	}
	level := detectPanelLogLevel(line)
	return "[" + strings.ToUpper(level) + "] " + line
}

func compactGINLogLine(line string) (string, bool) {
	match := panelGINLinePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 6 {
		return "", false
	}

	statusCode := strings.TrimSpace(match[1])
	clientIP := strings.TrimSpace(match[3])
	method := strings.TrimSpace(match[4])
	path := strings.TrimSpace(match[5])
	level := logLevelForStatusCode(statusCode)

	ts := time.Now().Format("2006-01-02 15:04:05")
	return "[" + strings.ToUpper(level) + "] " + ts + " [" + clientIP + "] " + method + " " + path + " 状态=" + statusCode, true
}

func normalizeGINLatency(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "")
	return raw
}

func logLevelForStatusCode(statusCode string) string {
	switch {
	case strings.HasPrefix(statusCode, "5"):
		return PanelLogLevelError
	case strings.HasPrefix(statusCode, "4"):
		return PanelLogLevelWarn
	default:
		return PanelLogLevelInfo
	}
}

func detectPanelLogLevel(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(lower, " panic"), strings.Contains(lower, "fatal"), strings.Contains(lower, " failed"), strings.Contains(lower, " error"), strings.Contains(lower, "无法"), strings.Contains(lower, "失败"):
		return PanelLogLevelError
	case strings.Contains(lower, "warn"), strings.Contains(lower, "warning"), strings.Contains(lower, "超时"), strings.Contains(lower, "不可用"), strings.Contains(lower, "跳过"):
		return PanelLogLevelWarn
	case strings.Contains(lower, "debug"), strings.Contains(lower, "trace"), strings.Contains(lower, "scanner"), strings.Contains(lower, "probe"):
		return PanelLogLevelDebug
	default:
		return PanelLogLevelInfo
	}
}

func NewGINLoggerWriter(dst io.Writer) io.Writer {
	return &ginAccessLogWriter{dst: dst}
}

type ginAccessLogWriter struct {
	dst io.Writer
}

func (w *ginAccessLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" {
		return len(p), nil
	}

	if compacted, ok := compactGINLogLine(text); ok {
		w.dst.Write([]byte(compacted + "\n"))
		return len(p), nil
	}

	w.dst.Write([]byte(strings.TrimRight(text, "\r\n") + "\n"))
	return len(p), nil
}

func ParsePanelLogLineLevel(line string) string {
	match := panelLogLinePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) >= 2 {
		return strings.ToLower(strings.TrimSpace(match[1]))
	}
	return detectPanelLogLevel(line)
}

func MatchPanelLogLevel(line, minimumLevel string) bool {
	minimumLevel = strings.ToLower(strings.TrimSpace(minimumLevel))
	if minimumLevel == "" {
		return true
	}

	currentLevel := ParsePanelLogLineLevel(line)
	currentPriority, currentOK := panelLogLevelPriority[currentLevel]
	minimumPriority, minimumOK := panelLogLevelPriority[minimumLevel]
	if !currentOK || !minimumOK {
		return currentLevel == minimumLevel
	}

	return currentPriority >= minimumPriority
}

func NewPanelLogger(dst io.Writer) *log.Logger {
	return log.New(NewPanelLogFilterWriter(dst), "", log.LstdFlags)
}

func shouldSuppressPanelStartupLog(text string) bool {
	markers := []string{
		"database connected:",
		"added missing column:",
		"column check completed",
		"scheduler v2 started:",
		"scheduler v2 initialized with",
		"scheduler v2 enqueued",
		"subscription scheduler initialized with",
		"resource watcher started",
		"server starting on",
	}

	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}

	return false
}
