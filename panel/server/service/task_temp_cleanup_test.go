package service

import (
	"os"
	"path/filepath"
	"testing"
)

// 启动时清理上次留下的任务临时文件：只删本面板建的那几类前缀、形状也要对得上，别的一律不碰。
// 这些用例只在 t.TempDir() 里跑清理，绝不碰真实的系统临时目录（并行的其它测试包也在那里建同前缀的文件）。
func TestCleanupLeakedTaskTempEntriesRemovesOnlyPanelPrefixes(t *testing.T) {
	dir := t.TempDir()

	mustMkdir := func(name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		return path
	}
	mustWrite := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	// 面板留下的三类：任务运行时环境目录（含 env.json）、前置钩子环境快照目录、实时日志落盘文件。
	runtimeDir := mustMkdir("daidai-runtime-123456")
	mustWrite(filepath.Join(runtimeDir, "env.json"))
	hookEnvDir := mustMkdir("daidai-hook-env-654321")
	mustWrite(filepath.Join(hookEnvDir, "hook-env.dump"))
	tinyLogFile := filepath.Join(dir, "daidai-log-7_1700000000000000000-998877.log")
	mustWrite(tinyLogFile)

	// 不能碰的：前缀对但形状不对、后缀不对、别的面板临时目录、前缀不在开头、别的程序的文件。
	keep := []string{}
	runtimeAsFile := filepath.Join(dir, "daidai-runtime-is-a-file")
	mustWrite(runtimeAsFile)
	keep = append(keep, runtimeAsFile)
	keep = append(keep, mustMkdir("daidai-log-is-a-dir.log"))
	wrongSuffix := filepath.Join(dir, "daidai-log-7_1-2.txt")
	mustWrite(wrongSuffix)
	keep = append(keep, wrongSuffix)
	keep = append(keep, mustMkdir("daidai-restore-111"))
	keep = append(keep, mustMkdir("my-daidai-runtime-222"))
	sshKey := filepath.Join(dir, "ssh_key_333")
	mustWrite(sshKey)
	keep = append(keep, sshKey)
	unrelated := mustMkdir("unrelated")
	unrelatedFile := filepath.Join(unrelated, "important.txt")
	mustWrite(unrelatedFile)
	keep = append(keep, unrelatedFile)

	// 同前缀的软链：只可能是别人放的，链接本身与它指向的目录都不能动（Windows 上没权限建软链就跳过这一项）。
	symlink := filepath.Join(dir, "daidai-runtime-symlink")
	if err := os.Symlink(unrelated, symlink); err == nil {
		keep = append(keep, symlink)
	}

	if removed := cleanupLeakedTaskTempEntries(dir); removed != 3 {
		t.Fatalf("expected exactly the 3 panel temp entries to be removed, removed %d", removed)
	}
	for _, gone := range []string{runtimeDir, hookEnvDir, tinyLogFile} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("leftover panel temp entry %s should be removed (err=%v)", filepath.Base(gone), err)
		}
	}
	for _, kept := range keep {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("%s is not a panel temp entry and must be kept: %v", filepath.Base(kept), err)
		}
	}

	// 目录不存在 / 读不了时什么都不做。
	if removed := cleanupLeakedTaskTempEntries(filepath.Join(dir, "missing")); removed != 0 {
		t.Fatalf("missing dir should remove nothing, removed %d", removed)
	}
}

// 「与创建处同一前缀、同一形状」靠这条守：用真实的创建函数各建一个，清理的判定必须认得它们。
// 有人改了创建处的前缀（或把目录改成文件）而忘了改 leakedTaskTempPatterns，这里会红。
func TestLeakedTaskTempPatternsMatchCreators(t *testing.T) {
	type created struct {
		label string
		path  string
	}
	var entries []created

	runtimeDir, _, cleanupRuntime, err := writeManagedRuntimeEnvFile(map[string]string{"PROBE": "1"})
	if err != nil {
		t.Fatalf("writeManagedRuntimeEnvFile: %v", err)
	}
	defer cleanupRuntime()
	entries = append(entries, created{"writeManagedRuntimeEnvFile", runtimeDir})

	shellDir, _, cleanupShell, err := writeManagedRuntimeShellEnvFile(map[string]string{"PROBE": "1"})
	if err != nil {
		t.Fatalf("writeManagedRuntimeShellEnvFile: %v", err)
	}
	defer cleanupShell()
	entries = append(entries, created{"writeManagedRuntimeShellEnvFile", shellDir})

	capture, err := newHookEnvCapture(map[string]string{"PROBE": "1"})
	if err != nil {
		t.Fatalf("newHookEnvCapture: %v", err)
	}
	defer capture.close()
	entries = append(entries, created{"newHookEnvCapture", capture.dir})

	tinyLog, err := NewTinyLog("987_1700000000000000000")
	if err != nil {
		t.Fatalf("NewTinyLog: %v", err)
	}
	defer tinyLog.Close()
	entries = append(entries, created{"NewTinyLog", tinyLog.file.Name()})

	for _, entry := range entries {
		parent, name := filepath.Dir(entry.path), filepath.Base(entry.path)
		dirEntries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatalf("read %s: %v", parent, err)
		}
		matched := false
		for _, dirEntry := range dirEntries {
			if dirEntry.Name() != name {
				continue
			}
			matched = true
			if !isLeakedTaskTempEntry(dirEntry) {
				t.Errorf("%s creates %q, which the startup cleanup does not recognise; keep leakedTaskTempPatterns in sync", entry.label, name)
			}
		}
		if !matched {
			t.Fatalf("%s: created entry %q not found in %s", entry.label, name, parent)
		}
		if parent != filepath.Clean(os.TempDir()) {
			t.Errorf("%s creates its entry under %s, but the startup cleanup only scans os.TempDir() (%s)", entry.label, parent, os.TempDir())
		}
	}
}
