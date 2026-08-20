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

func TestShouldHideScriptTreeEntryName(t *testing.T) {
	for _, name := range []string{".git", ".GIT", " .git ", ".svn", ".hg", ".bzr", "node_modules", "__pycache__"} {
		if !ShouldHideScriptTreeEntryName(name) {
			t.Fatalf("expected %q to be hidden from the script tree", name)
		}
	}

	// v2.2.17 起 dotfile 必须保持可见，隐藏名单不能扩大成“一刀切隐藏隐藏文件”。
	for _, name := range []string{".env", ".hidden-dir", ".hidden-file", ".github", "demo.py"} {
		if ShouldHideScriptTreeEntryName(name) {
			t.Fatalf("expected %q to stay visible in the script tree", name)
		}
	}

	// 守住启动期隔离：QuarantineUnexpectedScriptEntriesOnStartup 复用 ShouldIgnoreScriptEntryName，
	// 命中即 os.Rename 物理搬走，一旦它对 .git 返回 true，脚本根目录本身是 git 仓库时仓库会被搬走。
	for _, name := range []string{".git", ".svn", ".hg", ".bzr"} {
		if ShouldIgnoreScriptEntryName(name) {
			t.Fatalf("ShouldIgnoreScriptEntryName(%q) must stay false, startup quarantine reuses it", name)
		}
	}
}

func TestShouldHideScriptTreeRelativePath(t *testing.T) {
	hidden := []string{
		".git",
		".git/config",
		"SmallWorld/.git/config",
		"SmallWorld/.git",
		"a/b/.GIT/objects/ab/cdef",
		"SmallWorld/node_modules/pkg/index.js",
	}
	for _, relPath := range hidden {
		if !ShouldHideScriptTreeRelativePath(relPath) {
			t.Fatalf("expected %q to be hidden", relPath)
		}
	}

	visible := []string{"", ".", "/", "SmallWorld/demo.py", ".hidden-dir/.env", "gitlab/config"}
	for _, relPath := range visible {
		if ShouldHideScriptTreeRelativePath(relPath) {
			t.Fatalf("expected %q to stay visible", relPath)
		}
	}
}

func TestShouldHideScriptTreePath(t *testing.T) {
	root := t.TempDir()

	if !ShouldHideScriptTreePath(root, filepath.Join(root, "SmallWorld", ".git", "config")) {
		t.Fatal("expected nested .git/config to be hidden")
	}
	if !ShouldHideScriptTreePath(root, filepath.Join(root, ".git", "HEAD")) {
		t.Fatal("expected top-level .git/HEAD to be hidden")
	}
	if ShouldHideScriptTreePath(root, filepath.Join(root, "SmallWorld", "demo.py")) {
		t.Fatal("expected normal script path to stay visible")
	}
	// 路径不在脚本目录内时不做判定，交给上游的穿越校验处理
	if ShouldHideScriptTreePath(root, filepath.Join(filepath.Dir(root), "outside", ".git")) {
		t.Fatal("expected path outside scripts dir not to be treated as hidden")
	}
}

func TestQuarantineDoesNotMoveGitRepository(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	dataDir := t.TempDir()
	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir

	QuarantineUnexpectedScriptEntriesOnStartup()

	if _, err := os.Stat(filepath.Join(scriptsDir, ".git", "config")); err != nil {
		t.Fatalf("expected .git to stay in place, stat err=%v", err)
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
