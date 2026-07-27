package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestScriptCommandParts(t *testing.T) {
	parts, err := scriptCommandParts(".py", "demo.py")
	if err != nil {
		t.Fatalf("expected python command, got error: %v", err)
	}
	if len(parts) != 3 || parts[0] != "python" || parts[1] != "-u" || parts[2] != "demo.py" {
		t.Fatalf("unexpected command parts: %#v", parts)
	}
}

func TestScriptCommandPartsSupportsGo(t *testing.T) {
	parts, err := scriptCommandParts(".go", "demo.go")
	if err != nil {
		t.Fatalf("expected go command, got error: %v", err)
	}
	if len(parts) != 3 || parts[0] != "go" || parts[1] != "run" || parts[2] != "demo.go" {
		t.Fatalf("unexpected go command parts: %#v", parts)
	}
}

func TestScriptCommandPartsSupportsMJS(t *testing.T) {
	parts, err := scriptCommandParts(".mjs", "demo.mjs")
	if err != nil {
		t.Fatalf("expected mjs command, got error: %v", err)
	}
	if len(parts) != 2 || parts[0] != "node" || parts[1] != "demo.mjs" {
		t.Fatalf("unexpected mjs command parts: %#v", parts)
	}
}

func TestScriptCommandPartsRejectsUnsupportedExtension(t *testing.T) {
	if _, err := scriptCommandParts(".rb", "demo.rb"); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}

func TestScriptLanguageExtMapSupportsGo(t *testing.T) {
	if got := scriptLanguageExtMap["go"]; got != ".go" {
		t.Fatalf("expected go language map to .go, got %q", got)
	}
}

func TestScriptLanguageExtMapSupportsNodeMJS(t *testing.T) {
	if got := scriptLanguageExtMap["node"]; got != ".mjs" {
		t.Fatalf("expected node language map to .mjs, got %q", got)
	}
}

func TestDebugRunFinishDoesNotOverrideStoppedStatus(t *testing.T) {
	exitCode := -1
	run := &debugRun{
		Logs:     []string{"before"},
		Done:     true,
		ExitCode: &exitCode,
		Status:   "stopped",
	}

	run.finish(1, nil, 0.25)

	if run.Status != "stopped" {
		t.Fatalf("expected stopped status to be preserved, got %q", run.Status)
	}
	if !run.Done {
		t.Fatal("expected done flag to stay true")
	}
	if got := len(run.Logs); got != 1 {
		t.Fatalf("expected finish to avoid appending logs for stopped run, got %d entries", got)
	}
}

func requireUsableBash(t *testing.T) {
	t.Helper()

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
	if err := exec.Command(bashPath, "--version").Run(); err != nil {
		t.Skipf("bash is present but not usable: %v", err)
	}
}

func TestNewScriptCommandLoadsLargeShellEnvFromFile(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "large-env.sh")
	outputPath := filepath.Join(config.C.Data.ScriptsDir, "large-env.out")
	if err := os.WriteFile(scriptPath, []byte(`printf '%s' "${#BIG_ENV}" > large-env.out`+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd, cleanup, err := newScriptCommand(
		"bash",
		scriptPath,
		nil,
		config.C.Data.ScriptsDir,
		map[string]string{"BIG_ENV": strings.Repeat("x", 3*1024*1024)},
	)
	if err != nil {
		t.Fatalf("new script command: %v", err)
	}
	defer cleanup()

	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "BIG_ENV=") {
			t.Fatalf("large env must not be passed through process environment")
		}
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run script: %v: %s", err, out)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got := string(content); got != "3145728" {
		t.Fatalf("expected large env length 3145728, got %q", got)
	}
}

func TestNewScriptCommandDoesNotExportLargeShellEnvToChildren(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)
	if _, err := exec.LookPath("mktemp"); err != nil {
		t.Skipf("mktemp unavailable: %v", err)
	}

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "large-env-child.sh")
	outputPath := filepath.Join(config.C.Data.ScriptsDir, "large-env-child.out")
	script := strings.Join([]string{
		`tmp="$(mktemp)"`,
		`printf '%s:%s' "${#BIG_ENV}" "$SMALL_ENV" > large-env-child.out`,
		`rm -f "$tmp"`,
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd, cleanup, err := newScriptCommand(
		"bash",
		scriptPath,
		nil,
		config.C.Data.ScriptsDir,
		map[string]string{
			"BIG_ENV":   strings.Repeat("x", 3*1024*1024),
			"SMALL_ENV": "ok",
		},
	)
	if err != nil {
		t.Fatalf("new script command: %v", err)
	}
	defer cleanup()

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run script with child process: %v: %s", err, out)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got := string(content); got != "3145728:ok" {
		t.Fatalf("expected large env and small env in shell, got %q", got)
	}
}
