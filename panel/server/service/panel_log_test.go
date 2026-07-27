package service

import (
	"strings"
	"testing"
)

func TestMatchPanelLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		level string
		want  bool
	}{
		{name: "info matches info threshold", line: "[INFO] started", level: "info", want: true},
		{name: "warn matches info threshold", line: "[WARN] warn text", level: "info", want: true},
		{name: "debug filtered by info threshold", line: "[DEBUG] debug text", level: "info", want: false},
		{name: "error matches warn threshold", line: "[ERROR] boom", level: "warn", want: true},
		{name: "warn filtered by error threshold", line: "[WARN] not error", level: "error", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchPanelLogLevel(tt.line, tt.level); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestFormatPanelLogLineCompactsGINAccessLog(t *testing.T) {
	line := `[GIN] 2026/05/11 - 23:14:59 | 200 |    8.576817ms | 116.162.227.223 | GET      "/api/tasks?page=1&page_size=20"`
	got := formatPanelLogLine(line)
	if !strings.HasPrefix(got, "[INFO] ") {
		t.Fatalf("expected line to start with [INFO], got %q", got)
	}
	for _, part := range []string{"[116.162.227.223]", "GET", "/api/tasks?page=1&page_size=20", "状态=200"} {
		if !strings.Contains(got, part) {
			t.Fatalf("expected line to contain %q, got %q", part, got)
		}
	}
}
