package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/model"
)

func TestGetRelativeLogPathForTaskUsesReadableSafeDirectory(t *testing.T) {
	task := &model.Task{
		ID:      12,
		Name:    "京东 签到/测试:*?",
		Command: "task scripts/jd.py",
	}

	got := GetRelativeLogPathForTask(task)
	dir := strings.Split(filepath.ToSlash(got), "/")[0]

	if !strings.HasPrefix(dir, "task_12_") {
		t.Fatalf("expected readable task dir prefix, got %q", dir)
	}
	if strings.ContainsAny(dir, `<>:"/\|?*`) {
		t.Fatalf("expected unsafe filesystem chars to be removed, got %q", dir)
	}
	if strings.Contains(dir, " ") {
		t.Fatalf("expected spaces to be normalized, got %q", dir)
	}
}

func TestGetRelativeLogPathForTaskFallsBackToScriptName(t *testing.T) {
	task := &model.Task{
		ID:      8,
		Name:    "  ",
		Command: "task scripts/my job.py",
	}

	got := GetRelativeLogPathForTask(task)
	dir := strings.Split(filepath.ToSlash(got), "/")[0]

	if !strings.HasPrefix(dir, "task_8_my_job.py") {
		t.Fatalf("expected script name fallback, got %q", dir)
	}
}

func TestListLogFilesReadsReadableAndLegacyTaskDirs(t *testing.T) {
	logDir := t.TempDir()
	writeTestLog(t, logDir, "task_7_签到任务", "new.log", "new")
	writeTestLog(t, logDir, "task_7", "legacy.log", "legacy")
	writeTestLog(t, logDir, "task_71_其他任务", "wrong.log", "wrong")

	files := ListLogFiles(7, logDir)
	if len(files) != 2 {
		t.Fatalf("expected 2 task logs, got %d: %#v", len(files), files)
	}

	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	if !paths["task_7_签到任务/new.log"] {
		t.Fatalf("expected readable log dir to be listed, got %#v", paths)
	}
	if !paths["task_7/legacy.log"] {
		t.Fatalf("expected legacy log dir to be listed, got %#v", paths)
	}
	if paths["task_71_其他任务/wrong.log"] {
		t.Fatalf("did not expect another task id to be listed, got %#v", paths)
	}
}

func TestResolveTaskLogPathSupportsFullPathAndRejectsOtherTask(t *testing.T) {
	logDir := t.TempDir()
	writeTestLog(t, logDir, "task_7_签到任务", "run.log", "new")
	writeTestLog(t, logDir, "task_71_其他任务", "run.log", "wrong")

	got, err := ResolveTaskLogPath(7, "task_7_签到任务/run.log", logDir)
	if err != nil {
		t.Fatalf("resolve full path: %v", err)
	}
	if got != "task_7_签到任务/run.log" {
		t.Fatalf("unexpected path: %q", got)
	}

	if _, err := ResolveTaskLogPath(7, "task_71_其他任务/run.log", logDir); err == nil {
		t.Fatalf("expected another task id path to be rejected")
	}
}

func writeTestLog(t *testing.T, logDir, taskDir, name, content string) {
	t.Helper()
	dir := filepath.Join(logDir, taskDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
}
