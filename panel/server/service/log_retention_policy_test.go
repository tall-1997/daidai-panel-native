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

// createTaskWithRetention 建一个任务；days 为 nil 表示不设任务级保留天数（跟随全局）。
func createTaskWithRetention(t *testing.T, name string, days *int) uint {
	t.Helper()
	task := &model.Task{Name: name, Command: "echo x", CronExpression: "0 0 * * *", LogRetentionDays: days}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	return task.ID
}

// seedTaskLogFile 建一条 task_logs 行并在磁盘上造出对应的日志文件，返回文件绝对路径。
func seedTaskLogFile(t *testing.T, taskID uint, relPath string, startedAt time.Time) string {
	t.Helper()
	rel := writeTaskLogFile(t, relPath)
	createTaskLogWithPath(t, taskID, startedAt, nil, &rel)
	return filepath.Join(config.C.Data.LogDir, filepath.FromSlash(relPath))
}

func assertFileGone(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s removed, stat err=%v", label, err)
	}
}

func assertFileKept(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s kept, stat err=%v", label, err)
	}
}

// 一个任务设 1 天、一个跟随全局 30 天，各按各自的 cutoff 清；任务已删的孤儿行走全局。
func TestCleanLogsByRetentionPolicyGroupsByTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	oneDay := 1
	fastTask := createTaskWithRetention(t, "每分钟任务", &oneDay)
	normalTask := createTaskWithRetention(t, "普通任务", nil)

	now := time.Now()
	fastOld := seedTaskLogFile(t, fastTask, "task_1_fast/old.log", now.AddDate(0, 0, -3))
	fastRecent := seedTaskLogFile(t, fastTask, "task_1_fast/recent.log", now.Add(-2*time.Hour))
	normalMid := seedTaskLogFile(t, normalTask, "task_2_normal/mid.log", now.AddDate(0, 0, -3))
	normalOld := seedTaskLogFile(t, normalTask, "task_2_normal/old.log", now.AddDate(0, 0, -40))
	// 没有造「任务已删、日志行还在」的孤儿行：task_logs.task_id 上有外键约束，
	// 测试库里插不进不存在的 task_id。代码侧它和 normalTask 走的是同一条 task_id NOT IN 分支。

	records, files := CleanLogsByRetentionPolicy(30)
	if records != 2 {
		t.Fatalf("expected 2 TaskLog rows removed, got %d", records)
	}
	if files != 2 {
		t.Fatalf("expected 2 log files removed, got %d", files)
	}

	if got := countTaskLogs(t); got != 2 {
		t.Fatalf("expected 2 TaskLog rows kept, got %d", got)
	}
	// 3 天前的日志：设了 1 天的任务该删，跟随全局 30 天的任务该留——同一个 cutoff 下结果相反，
	// 这正是分组清理生效的证据。
	assertFileGone(t, fastOld, "fast/old.log")
	assertFileKept(t, fastRecent, "fast/recent.log")
	assertFileKept(t, normalMid, "normal/mid.log")
	assertFileGone(t, normalOld, "normal/old.log")
}

// 任务级天数比全局长的时候，扫盘那一步不能把它的文件删掉（扫盘天数取 max，见 CleanLogsByRetentionPolicy）。
func TestCleanLogsByRetentionPolicyKeepsFilesOfLongerRetentionTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	thirtyDays := 30
	taskID := createTaskWithRetention(t, "归档任务", &thirtyDays)

	now := time.Now()
	kept := seedTaskLogFile(t, taskID, "task_3_archive/ten-days.log", now.AddDate(0, 0, -10))
	// 把文件时间也退到 10 天前：不退的话 ModTime 扫描压根碰不到它，这条用例就成了空跑。
	old := now.AddDate(0, 0, -10)
	if err := os.Chtimes(kept, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	CleanLogsByRetentionPolicy(7)
	if countTaskLogs(t) != 1 {
		t.Fatalf("expected the row to survive the task-level 30-day retention")
	}
	assertFileKept(t, kept, "archive/ten-days.log")
}

// 手动清理刻意不看任务级天数：用户点「保留最近 N 天」是一次性指令，
// 掺进任务级天数就会删掉用户刚说要留的日志。
func TestCleanLogsOlderThanIgnoresPerTaskRetention(t *testing.T) {
	testutil.SetupTestEnv(t)

	oneDay := 1
	taskID := createTaskWithRetention(t, "每分钟任务", &oneDay)

	now := time.Now()
	kept := seedTaskLogFile(t, taskID, "task_4_fast/three-days.log", now.AddDate(0, 0, -3))

	if records, _ := CleanLogsOlderThan(30); records != 0 {
		t.Fatalf("expected no rows removed when cleaning with an explicit 30-day window, got %d", records)
	}
	assertFileKept(t, kept, "fast/three-days.log")
}
