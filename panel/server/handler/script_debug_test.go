package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestPrepareInlineDebugFileUsesOriginalScriptDirectory(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "demo", "sample.py")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("print('hello')\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	full, workDir, cleanup, err := prepareInlineDebugFile("demo/sample.py", ".py")
	if err != nil {
		t.Fatalf("prepareInlineDebugFile: %v", err)
	}
	defer cleanup()

	expectedDirInfo, err := os.Stat(filepath.Dir(scriptPath))
	if err != nil {
		t.Fatalf("stat expected dir: %v", err)
	}
	actualWorkDirInfo, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("stat actual workDir: %v", err)
	}
	actualFileDirInfo, err := os.Stat(filepath.Dir(full))
	if err != nil {
		t.Fatalf("stat actual file dir: %v", err)
	}

	if !os.SameFile(expectedDirInfo, actualWorkDirInfo) {
		t.Fatalf("expected workDir %q, got %q", filepath.Dir(scriptPath), workDir)
	}
	if !os.SameFile(expectedDirInfo, actualFileDirInfo) {
		t.Fatalf("expected debug file to be created beside source script, got %q", full)
	}
	if !strings.HasPrefix(filepath.Base(full), ".sample.daidai-debug-") {
		t.Fatalf("unexpected debug filename %q", filepath.Base(full))
	}
	if filepath.Ext(full) != ".py" {
		t.Fatalf("expected debug file extension .py, got %q", filepath.Ext(full))
	}
}
