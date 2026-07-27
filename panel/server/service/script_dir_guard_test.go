package service

import (
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
)

func TestShouldIgnoreScriptEntryName(t *testing.T) {
	if !ShouldIgnoreScriptEntryName("node_modules") {
		t.Fatal("expected node_modules to be ignored")
	}
	if !ShouldIgnoreScriptEntryName("__pycache__") {
		t.Fatal("expected __pycache__ to be ignored")
	}
	if !ShouldIgnoreScriptEntryName("%SystemDrive%") {
		t.Fatal("expected %SystemDrive% to be ignored")
	}
	if ShouldIgnoreScriptEntryName("demo") {
		t.Fatal("expected normal directory not to be ignored")
	}
}

func TestShouldIgnoreScriptPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "%SystemDrive%", "ProgramData", "demo.db")
	if !ShouldIgnoreScriptPath(root, target) {
		t.Fatal("expected quarantined subtree path to be ignored")
	}

	normal := filepath.Join(root, "jobs", "demo.py")
	if ShouldIgnoreScriptPath(root, normal) {
		t.Fatal("expected normal script path not to be ignored")
	}
}

func TestShouldIgnoreScriptRelativePath(t *testing.T) {
	if !ShouldIgnoreScriptRelativePath("%SystemDrive%/ProgramData/test.db") {
		t.Fatal("expected quarantined relative path to be ignored")
	}
	if ShouldIgnoreScriptRelativePath("demo/regression.py") {
		t.Fatal("expected normal relative path not to be ignored")
	}
}

func TestQuarantineUnexpectedScriptEntriesOnStartup(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	dataDir := t.TempDir()
	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "%SystemDrive%", "ProgramData"), 0o755); err != nil {
		t.Fatalf("mkdir polluted scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "%SystemDrive%", "ProgramData", "demo.db"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write polluted file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "regression.py"), []byte("print('ok')"), 0o644); err != nil {
		t.Fatalf("write normal script: %v", err)
	}

	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir

	QuarantineUnexpectedScriptEntriesOnStartup()

	if _, err := os.Stat(filepath.Join(scriptsDir, "%SystemDrive%")); !os.IsNotExist(err) {
		t.Fatalf("expected polluted directory to be moved away, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "regression.py")); err != nil {
		t.Fatalf("expected normal script to stay in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "quarantine", "scripts", "%SystemDrive%", "ProgramData", "demo.db")); err != nil {
		t.Fatalf("expected polluted directory to be quarantined: %v", err)
	}
}
