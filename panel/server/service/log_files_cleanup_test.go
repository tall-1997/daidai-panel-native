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

// writeTaskLogFile 按真实形状（task_<ID>_<名字>/时间戳.log）在 LogDir 下造一个日志文件，
// 返回入库用的相对路径 —— 斜杠分隔，与 GetRelativeLogPathForTask 的产物一致。
func writeTaskLogFile(t *testing.T, relPath string) string {
	t.Helper()
	full := filepath.Join(config.C.Data.LogDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte("log body"), 0o644); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
	return relPath
}

func createTaskLogWithPath(t *testing.T, taskID uint, startedAt time.Time, status *int, relPath *string) uint {
	t.Helper()
	entry := &model.TaskLog{TaskID: taskID, StartedAt: startedAt, Status: status, LogPath: relPath}
	if err := database.DB.Create(entry).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	return entry.ID
}

func TestDeleteLogFilesForRecordsSkipsFilesStillBeingWritten(t *testing.T) {
	testutil.SetupTestEnv(t)
	logDir := config.C.Data.LogDir

	// Linux 上删一个已经 open 的日志文件不会报错，之后所有输出都写进被 unlink 的 inode，
	// 任务跑完日志凭空消失。这道保护就是为了挡住它（issue #144）。
	openRel := "task_1_demo/open.log"
	closedRel := writeTaskLogFile(t, "task_1_demo/closed.log")

	openFull := filepath.Join(logDir, filepath.FromSlash(openRel))
	if err := GetLogStreamManager().Write(openFull, "still running\n"); err != nil {
		t.Fatalf("open log stream: %v", err)
	}
	defer GetLogStreamManager().CloseStream(openFull)

	removed := DeleteLogFilesForRecords([]string{openRel, closedRel}, logDir)
	if removed != 1 {
		t.Fatalf("expected exactly 1 file removed, got %d", removed)
	}
	if _, err := os.Stat(openFull); err != nil {
		t.Fatalf("expected the file still being written to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(logDir, filepath.FromSlash(closedRel))); !os.IsNotExist(err) {
		t.Fatalf("expected closed.log removed, stat err=%v", err)
	}

	// 这是「这一轮跳过」而不是「永远不删」：流关掉之后下一轮就该收走。
	GetLogStreamManager().CloseStream(openFull)
	if removed := DeleteLogFilesForRecords([]string{openRel}, logDir); removed != 1 {
		t.Fatalf("expected the released file removed on the next pass, got %d", removed)
	}
}

func TestDeleteLogFilesForRecordsIgnoresEmptyAndMissingPaths(t *testing.T) {
	testutil.SetupTestEnv(t)
	logDir := config.C.Data.LogDir

	// 空 log_path 来自「正文直接存在 content 列里」的老记录；文件不存在则是用户手工删过。
	// 两者都必须安静跳过，不能让整批清理中断或 panic。
	if removed := DeleteLogFilesForRecords([]string{"", "   ", "task_9_gone/never-existed.log"}, logDir); removed != 0 {
		t.Fatalf("expected 0 files removed, got %d", removed)
	}

	// config 还没初始化时 logDir 会是空串，同样不能炸。
	if removed := DeleteLogFilesForRecords([]string{"task_9_gone/x.log"}, ""); removed != 0 {
		t.Fatalf("expected 0 files removed with empty logDir, got %d", removed)
	}
}

func TestCleanLogsOlderThanRemovesNullStatusRowsAndTheirFiles(t *testing.T) {
	testutil.SetupTestEnv(t)

	taskID := createTaskForLog(t)
	now := time.Now()

	// status 为 NULL 的历史行是主力场景（task_logs.status 是可空列）。
	// WHERE 若只写 status <> 2，SQL 三值逻辑下 NULL <> 2 求值为 NULL 而非 TRUE，
	// 这一行会被静默漏掉、永远清不干净 —— 本用例就是钉死这一点的。
	oldRel := writeTaskLogFile(t, "task_1_demo/old.log")
	recentRel := writeTaskLogFile(t, "task_1_demo/recent.log")
	oldID := createTaskLogWithPath(t, taskID, now.AddDate(0, 0, -10), nil, &oldRel)
	recentID := createTaskLogWithPath(t, taskID, now.AddDate(0, 0, -1), nil, &recentRel)

	records, files := CleanLogsOlderThan(7)
	if records != 1 {
		t.Fatalf("expected 1 record removed, got %d", records)
	}
	// 两个文件的 ModTime 都是刚写的「现在」，CleanOldLogs 的扫盘拿不到它们，
	// 所以这个 1 只可能来自按 log_path 删文件那条新路径。
	if files != 1 {
		t.Fatalf("expected 1 file removed, got %d", files)
	}

	var count int64
	database.DB.Model(&model.TaskLog{}).Where("id = ?", oldID).Count(&count)
	if count != 0 {
		t.Fatalf("expected the NULL-status old row deleted, remaining=%d", count)
	}
	database.DB.Model(&model.TaskLog{}).Where("id = ?", recentID).Count(&count)
	if count != 1 {
		t.Fatalf("expected the recent row kept, remaining=%d", count)
	}

	if _, err := os.Stat(filepath.Join(config.C.Data.LogDir, filepath.FromSlash(oldRel))); !os.IsNotExist(err) {
		t.Fatalf("expected old.log removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(config.C.Data.LogDir, filepath.FromSlash(recentRel))); err != nil {
		t.Fatalf("expected recent.log kept, stat err=%v", err)
	}
}

func TestCleanLogsOlderThanKeepsRunningLogs(t *testing.T) {
	testutil.SetupTestEnv(t)

	taskID := createTaskForLog(t)
	runningStatus := model.LogStatusRunning
	rel := writeTaskLogFile(t, "task_1_demo/running.log")
	runningID := createTaskLogWithPath(t, taskID, time.Now().AddDate(0, 0, -10), &runningStatus, &rel)

	// 一次跑了十天以上的任务很罕见但确实存在（长驻脚本）。清理不能把它这次执行的
	// 日志行连同正在写的文件一起抽掉 —— 行这一层靠 status <> running，文件那一层靠 IsStreamOpen。
	records, _ := CleanLogsOlderThan(7)
	if records != 0 {
		t.Fatalf("expected the running log row to be kept, removed=%d", records)
	}

	var count int64
	database.DB.Model(&model.TaskLog{}).Where("id = ?", runningID).Count(&count)
	if count != 1 {
		t.Fatalf("expected the running row kept, remaining=%d", count)
	}
	if _, err := os.Stat(filepath.Join(config.C.Data.LogDir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("expected running.log kept, stat err=%v", err)
	}
}

func TestRemoveTaskLogDirsCollectsRenamedHistoryOnly(t *testing.T) {
	testutil.SetupTestEnv(t)
	logDir := config.C.Data.LogDir

	// 同一个任务改过名会留下多个目录（task_7 是老格式，task_7_xxx 是带名字的新格式），
	// 删任务时必须一起收走；task_70_other 只是前缀看着像，绝不能被误删。
	writeTaskLogFile(t, "task_7/a.log")
	writeTaskLogFile(t, "task_7_old-name/b.log")
	writeTaskLogFile(t, "task_7_new-name/c.log")
	writeTaskLogFile(t, "task_70_other/d.log")

	RemoveTaskLogDirs(7, logDir)

	for _, dir := range []string{"task_7", "task_7_old-name", "task_7_new-name"} {
		if _, err := os.Stat(filepath.Join(logDir, dir)); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err=%v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(logDir, "task_70_other", "d.log")); err != nil {
		t.Fatalf("expected task_70_other untouched, stat err=%v", err)
	}
}

func TestRemoveTaskLogDirsKeepsDirWithFileStillBeingWritten(t *testing.T) {
	testutil.SetupTestEnv(t)
	logDir := config.C.Data.LogDir

	openFull := filepath.Join(logDir, "task_8_busy", "open.log")
	if err := GetLogStreamManager().Write(openFull, "still running\n"); err != nil {
		t.Fatalf("open log stream: %v", err)
	}
	defer GetLogStreamManager().CloseStream(openFull)

	RemoveTaskLogDirs(8, logDir)

	if _, err := os.Stat(openFull); err != nil {
		t.Fatalf("expected the busy task log dir kept, stat err=%v", err)
	}
}
