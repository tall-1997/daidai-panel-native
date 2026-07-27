package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func countTaskLogs(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := database.DB.Model(&model.TaskLog{}).Count(&n).Error; err != nil {
		t.Fatalf("count task logs: %v", err)
	}
	return n
}

func createTaskLogStartedAt(t *testing.T, taskID uint, startedAt time.Time) {
	t.Helper()
	logEntry := &model.TaskLog{TaskID: taskID, StartedAt: startedAt}
	if err := database.DB.Create(logEntry).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
}

func writeLogFileWithMTime(t *testing.T, name string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(config.C.Data.LogDir, name)
	if err := os.WriteFile(path, []byte("log"), 0o644); err != nil {
		t.Fatalf("write log file %q: %v", name, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %q: %v", name, err)
	}
	return path
}

func createTaskForLog(t *testing.T) uint {
	t.Helper()
	task := &model.Task{Name: "log-owner", Command: "echo x", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

func TestCleanupOldLogsRemovesOldKeepsRecent(t *testing.T) {
	testutil.SetupTestEnv(t)

	taskID := createTaskForLog(t)

	// 默认保留 7 天。
	now := time.Now()
	createTaskLogStartedAt(t, taskID, now.AddDate(0, 0, -10)) // 旧记录，应删
	createTaskLogStartedAt(t, taskID, now.AddDate(0, 0, -1))  // 近 1 天，应留

	oldFile := writeLogFileWithMTime(t, "old.log", now.AddDate(0, 0, -10))
	recentFile := writeLogFileWithMTime(t, "recent.log", now.AddDate(0, 0, -1))

	cleanupOldLogs()

	if got := countTaskLogs(t); got != 1 {
		t.Fatalf("expected 1 task log remaining, got %d", got)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected old.log removed, stat err=%v", err)
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("expected recent.log kept, got err=%v", err)
	}
}

func TestCleanupOldLogsHonorsRetentionDays(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 把保留天数改成 30，则 10 天前的记录与文件应被保留。
	if err := model.SetConfig("log_retention_days", "30"); err != nil {
		t.Fatalf("set log_retention_days: %v", err)
	}

	taskID := createTaskForLog(t)
	now := time.Now()
	createTaskLogStartedAt(t, taskID, now.AddDate(0, 0, -10))
	keptFile := writeLogFileWithMTime(t, "ten-days.log", now.AddDate(0, 0, -10))

	cleanupOldLogs()

	if got := countTaskLogs(t); got != 1 {
		t.Fatalf("expected 1 task log kept under 30-day retention, got %d", got)
	}
	if _, err := os.Stat(keptFile); err != nil {
		t.Fatalf("expected ten-days.log kept under 30-day retention, got err=%v", err)
	}
}
