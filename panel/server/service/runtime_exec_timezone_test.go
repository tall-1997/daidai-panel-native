package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"daidai-panel/testutil"
)

func TestWindowsPythonPOSIXTimezoneUsesCurrentIANATimezoneOffset(t *testing.T) {
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		timezone string
		want     string
	}{
		{name: "中国标准时间", timezone: "Asia/Shanghai", want: "CST-8"},
		{name: "UTC", timezone: "UTC", want: "UTC0"},
		{name: "负偏移夏令时", timezone: "America/New_York", want: "EDT4"},
		{name: "半小时偏移", timezone: "Asia/Kolkata", want: "IST-5:30"},
		{name: "数字缩写兜底", timezone: "Pacific/Marquesas", want: "DDT9:30"},
		{name: "四字母缩写", timezone: "Europe/Paris", want: "CES-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowsPythonPOSIXTimezone(tt.timezone, now)
			if err != nil {
				t.Fatalf("convert timezone %s: %v", tt.timezone, err)
			}
			if got != tt.want {
				t.Fatalf("expected %s => %q, got %q", tt.timezone, tt.want, got)
			}
		})
	}
}

func TestWindowsPythonPOSIXTimezoneRejectsInvalidIANAName(t *testing.T) {
	if _, err := windowsPythonPOSIXTimezone("Bad/Zone", time.Now()); err == nil {
		t.Fatal("expected invalid IANA timezone to return an error")
	}
}

func TestBuildPythonBootstrapProcessEnvOnlyConvertsTimezoneOnWindows(t *testing.T) {
	testutil.SetupTestEnv(t)

	envVars := map[string]string{
		"PATH": os.Getenv("PATH"),
		"TZ":   "Asia/Shanghai",
	}

	pythonEnv := buildPythonBootstrapProcessEnv(envVars)
	wantPythonTZ := "Asia/Shanghai"
	if runtime.GOOS == "windows" {
		wantPythonTZ = "CST-8"
	}
	if got := testProcessEnvValue(pythonEnv, "TZ"); got != wantPythonTZ {
		t.Fatalf("expected Python startup TZ %q, got %q", wantPythonTZ, got)
	}
	if got := envVars["TZ"]; got != "Asia/Shanghai" {
		t.Fatalf("expected task env TZ to remain IANA name, got %q", got)
	}

	nodeEnv := buildBootstrapProcessEnv(envVars)
	if got := testProcessEnvValue(nodeEnv, "TZ"); got != "Asia/Shanghai" {
		t.Fatalf("expected Node startup TZ to remain Asia/Shanghai, got %q", got)
	}
}

func TestWindowsPythonBootstrapsKeepBeijingTimeAndExposeIANAName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Python CRT compatibility regression")
	}
	testutil.SetupTestEnv(t)

	pythonBin, err := exec.LookPath("python")
	if err != nil {
		t.Skipf("python not found: %v", err)
	}
	if err := exec.Command(pythonBin, "--version").Run(); err != nil {
		t.Skipf("python is present but not usable: %v", err)
	}

	envVars := map[string]string{
		"PATH": os.Getenv("PATH"),
		"TZ":   "Asia/Shanghai",
	}
	_, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		t.Fatalf("write runtime env file: %v", err)
	}
	defer cleanup()

	pythonSourceDir := t.TempDir()
	scriptPath := filepath.Join(pythonSourceDir, "timezone_check.py")
	modulePath := filepath.Join(pythonSourceDir, "timezone_module_check.py")
	script := `import datetime, json, os
now = datetime.datetime.now().astimezone()
print(json.dumps({"tz": os.environ.get("TZ"), "offset": now.strftime("%z")}, ensure_ascii=False))
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write Python timezone check: %v", err)
	}
	if err := os.WriteFile(modulePath, []byte(script), 0o600); err != nil {
		t.Fatalf("write Python module timezone check: %v", err)
	}

	// Python 必须以 CRT 可识别的固定偏移启动，bootstrap 再恢复脚本可见的 IANA 名称。
	tests := []struct {
		name string
		args []string
	}{
		{name: "脚本", args: []string{"-u", "-c", pythonEnvBootstrap, envFile, scriptPath, ""}},
		{name: "模块", args: []string{"-u", "-c", pythonModuleEnvBootstrap, envFile, "timezone_module_check", pythonSourceDir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(pythonBin, tt.args...)
			cmd.Env = buildPythonBootstrapProcessEnv(envVars)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Python timezone check failed: %v, output=%s", err, string(out))
			}

			var got struct {
				Timezone string `json:"tz"`
				Offset   string `json:"offset"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
				t.Fatalf("decode Python output %q: %v", string(out), err)
			}
			if got.Timezone != "Asia/Shanghai" {
				t.Fatalf("expected script-visible TZ Asia/Shanghai, got %q", got.Timezone)
			}
			if got.Offset != "+0800" {
				t.Fatalf("expected Python local offset +0800, got %q", got.Offset)
			}
		})
	}
}

func TestWindowsNodeKeepsIANATimezone(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Node timezone regression")
	}
	testutil.SetupTestEnv(t)

	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not found: %v", err)
	}
	cmd := exec.Command(nodeBin, "-e", `process.stdout.write(JSON.stringify({tz: process.env.TZ, offset: new Date().getTimezoneOffset()}))`)
	cmd.Env = buildBootstrapProcessEnv(map[string]string{
		"PATH": os.Getenv("PATH"),
		"TZ":   "Asia/Shanghai",
	})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Node timezone check failed: %v, output=%s", err, string(out))
	}

	var got struct {
		Timezone string `json:"tz"`
		Offset   int    `json:"offset"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode Node output %q: %v", string(out), err)
	}
	if got.Timezone != "Asia/Shanghai" {
		t.Fatalf("expected Node TZ Asia/Shanghai, got %q", got.Timezone)
	}
	if got.Offset != -480 {
		t.Fatalf("expected Node timezone offset -480 minutes, got %d", got.Offset)
	}
}

func testProcessEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
