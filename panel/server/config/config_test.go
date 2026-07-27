package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesRelativeDataPathsToAbsolute(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	configPath := filepath.Join(tmp, "config.yaml")
	body := `server:
  port: 5701
  mode: test
database:
  path: data/test.db
jwt:
  secret: unit-test-secret
data:
  dir: ./data
  scripts_dir: data/scripts
  log_dir: ./data/logs
cors:
  origins: []
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Cleanup(func() { C = nil })

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !filepath.IsAbs(cfg.Data.Dir) {
		t.Fatalf("expected Data.Dir to be absolute, got %q", cfg.Data.Dir)
	}
	if !filepath.IsAbs(cfg.Data.ScriptsDir) {
		t.Fatalf("expected Data.ScriptsDir to be absolute, got %q", cfg.Data.ScriptsDir)
	}
	if !filepath.IsAbs(cfg.Data.LogDir) {
		t.Fatalf("expected Data.LogDir to be absolute, got %q", cfg.Data.LogDir)
	}
	if !filepath.IsAbs(cfg.Database.Path) {
		t.Fatalf("expected Database.Path to be absolute, got %q", cfg.Database.Path)
	}

	expectedScripts := filepath.Join(tmp, "data", "scripts")
	if cfg.Data.ScriptsDir != expectedScripts {
		t.Fatalf("expected ScriptsDir=%q, got %q", expectedScripts, cfg.Data.ScriptsDir)
	}
	expectedDB := filepath.Join(tmp, "data", "test.db")
	if cfg.Database.Path != expectedDB {
		t.Fatalf("expected Database.Path=%q, got %q", expectedDB, cfg.Database.Path)
	}
}

// 回归测试：v2.2.5 → v2.2.6 数据丢失事故的核心场景——cwd 与 config.yaml 不在
// 同一目录时（如 systemd 错配 WorkingDirectory、Docker WORKDIR 与挂载点错位、
// Windows 双击 exe 但 config 不在 cwd 等），相对路径必须按 config 所在目录
// 解析，而不是按 cwd 解析；否则 sqlite 会落到错位置，旧库变成"消失"。
func TestLoadResolvesRelativeDatabasePathBasedOnConfigDir(t *testing.T) {
	configDir := t.TempDir()
	otherCwd := t.TempDir()
	t.Chdir(otherCwd) // 故意让 cwd ≠ configDir

	configPath := filepath.Join(configDir, "config.yaml")
	body := `server:
  port: 5701
  mode: test
database:
  path: Dumb-Panel/daidai.db
jwt:
  secret: unit-test-secret
data:
  dir: ./Dumb-Panel
  scripts_dir: ./Dumb-Panel/scripts
  log_dir: ./Dumb-Panel/logs
cors:
  origins: []
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Cleanup(func() { C = nil })

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	expectedDB := filepath.Join(configDir, "Dumb-Panel", "daidai.db")
	if cfg.Database.Path != expectedDB {
		t.Fatalf("Database.Path should resolve relative to config dir (%q), got %q (cwd was %q)",
			expectedDB, cfg.Database.Path, otherCwd)
	}
	expectedDataDir := filepath.Join(configDir, "Dumb-Panel")
	if cfg.Data.Dir != expectedDataDir {
		t.Fatalf("Data.Dir should resolve relative to config dir (%q), got %q", expectedDataDir, cfg.Data.Dir)
	}

	// 反向断言：绝对不能基于 cwd 解析
	wrongDB := filepath.Join(otherCwd, "Dumb-Panel", "daidai.db")
	if cfg.Database.Path == wrongDB {
		t.Fatalf("Database.Path must NOT resolve to cwd-based %q", wrongDB)
	}
}

func TestLoadKeepsAbsolutePathsUnchanged(t *testing.T) {
	configDir := t.TempDir()
	absDataDir := filepath.Join(t.TempDir(), "explicit-data")
	absDBPath := filepath.Join(t.TempDir(), "explicit.db")

	configPath := filepath.Join(configDir, "config.yaml")
	body := `server:
  port: 5701
  mode: test
database:
  path: ` + absDBPath + `
jwt:
  secret: unit-test-secret
data:
  dir: ` + absDataDir + `
  scripts_dir: ` + filepath.Join(absDataDir, "scripts") + `
  log_dir: ` + filepath.Join(absDataDir, "logs") + `
cors:
  origins: []
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Cleanup(func() { C = nil })

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Database.Path != absDBPath {
		t.Fatalf("absolute Database.Path should be untouched: want %q got %q", absDBPath, cfg.Database.Path)
	}
	if cfg.Data.Dir != absDataDir {
		t.Fatalf("absolute Data.Dir should be untouched: want %q got %q", absDataDir, cfg.Data.Dir)
	}
}

func TestResolveDataPathLeavesAbsoluteUntouched(t *testing.T) {
	tmp := t.TempDir()
	resolved := resolveDataPath("/anchor", tmp)
	if resolved != filepath.Clean(tmp) {
		t.Fatalf("expected absolute path to stay as %q, got %q", filepath.Clean(tmp), resolved)
	}
}

func TestResolveDataPathJoinsRelativeOnBaseDir(t *testing.T) {
	base := filepath.FromSlash("/anchor/here")
	got := resolveDataPath(base, "Dumb-Panel/daidai.db")
	want := filepath.Join(base, "Dumb-Panel", "daidai.db")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
