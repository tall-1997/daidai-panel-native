package handler_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// TestPanelLogKeepsTailAndWholeFileTotal 锁住 #159 修复 E：GET /system/panel-log 改成环形缓冲只留尾部之后，
// 返回内容与改动前「整份读进来、筛选、取最后 lines 行」逐项相同——logs 逐行一致，total 仍是全文件命中筛选的行数。
// 用例里的 oracle 就是改动前的算法。覆盖：绕圈多次与不绕圈、按级别、按关键词、无命中（logs 为 null、total 0）、
// lines 越界回落 100、关键词 + 级别组合；文件不存在时响应体是 {"data":{"logs":[]}}、没有 total。
func TestPanelLogKeepsTailAndWholeFileTotal(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "panel-log-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}
	logPath := filepath.Join(config.C.Data.Dir, "panel.log")

	// 文件不存在：这一支没改，响应体原样是 {"data":{"logs":[]}}，没有 total 与 level。
	rec := performJSONRequest(engine, http.MethodGet, "/api/v1/system/panel-log", `{}`, auth, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"data":{"logs":[]}}` {
		t.Fatalf("panel.log 不存在时应回 200 {\"data\":{\"logs\":[]}}，实际 %d %s", rec.Code, rec.Body.String())
	}

	// 真实形状：级别前缀在行首。25003 行里每 7 行一条 ERROR、每 11 行一条带「更新」的 INFO，
	// 末尾再加一行 70KiB 的长行（在 scanner 1MiB 的单行上限以内）。
	var builder strings.Builder
	for i := 0; i < 25003; i++ {
		switch {
		case i%7 == 0:
			fmt.Fprintf(&builder, "[ERROR] 2026/10/09 10:00:00 boom %05d\n", i)
		case i%11 == 0:
			fmt.Fprintf(&builder, "[INFO] 2026/10/09 10:00:00 更新 %05d\n", i)
		default:
			fmt.Fprintf(&builder, "[INFO] 2026/10/09 10:00:00 line %05d\n", i)
		}
	}
	builder.WriteString("[INFO] 2026/10/09 10:00:00 long " + strings.Repeat("x", 70*1024) + "\n")
	if err := os.WriteFile(logPath, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write panel.log: %v", err)
	}

	// oracle：改动前的算法——整份读进来、按级别与关键词筛选、取最后 lines 行；一行都没命中时是 nil（JSON 为 null）。
	oracle := func(t *testing.T, lines int, keyword, level string) ([]string, int) {
		t.Helper()
		file, err := os.Open(logPath)
		if err != nil {
			t.Fatalf("open panel.log: %v", err)
		}
		defer file.Close()
		var all []string
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if level != "" && !service.MatchPanelLogLevel(line, level) {
				continue
			}
			if keyword == "" || strings.Contains(line, keyword) {
				all = append(all, line)
			}
		}
		start := len(all) - lines
		if start < 0 {
			start = 0
		}
		return all[start:], len(all)
	}

	// lines 是请求里原样带的参数，effectiveLines 是接口实际用的行数（≤0 或 >10000 回落 100）。
	cases := []struct {
		name           string
		lines          string
		keyword, level string
		effectiveLines int
		wantNull       bool
	}{
		{"默认 100 行", "100", "", "", 100, false},
		{"10000 行（环形缓冲绕好几圈）", "10000", "", "", 10000, false},
		{"只看 error", "200", "", "error", 200, false},
		{"关键词更新（命中不足 lines，不绕圈）", "10000", "更新", "", 10000, false},
		{"关键词无命中", "100", "nomatch-keyword", "", 100, true},
		{"lines=0 回落 100", "0", "", "", 100, false},
		{"lines=20000 回落 100", "20000", "", "", 100, false},
		{"关键词与级别组合", "3000", "boom", "error", 3000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{}
			query.Set("lines", tc.lines)
			if tc.keyword != "" {
				query.Set("keyword", tc.keyword)
			}
			if tc.level != "" {
				query.Set("level", tc.level)
			}
			rec := performJSONRequest(engine, http.MethodGet, "/api/v1/system/panel-log?"+query.Encode(), `{}`, auth, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Data struct {
					Logs  json.RawMessage `json:"logs"`
					Total int             `json:"total"`
					Level string          `json:"level"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			wantLogs, wantTotal := oracle(t, tc.effectiveLines, tc.keyword, tc.level)
			wantRaw, err := json.Marshal(wantLogs)
			if err != nil {
				t.Fatalf("marshal oracle logs: %v", err)
			}
			// 按 JSON 原文比：行内容、行序、条数，以及「无命中时是 null」都要和改动前一致。
			if string(payload.Data.Logs) != string(wantRaw) {
				t.Fatalf("logs 应与改动前逐项相同：期望 %d 行（JSON %d 字节），实际 JSON %d 字节",
					len(wantLogs), len(wantRaw), len(payload.Data.Logs))
			}
			if tc.wantNull && string(payload.Data.Logs) != "null" {
				t.Fatalf("一行都没命中时 logs 应为 null，实际 %s", payload.Data.Logs)
			}
			if payload.Data.Total != wantTotal {
				t.Fatalf("total 应是全文件命中筛选的行数 %d，实际 %d", wantTotal, payload.Data.Total)
			}
			if payload.Data.Level != tc.level {
				t.Fatalf("level 应原样回传 %q，实际 %q", tc.level, payload.Data.Level)
			}
		})
	}
}
