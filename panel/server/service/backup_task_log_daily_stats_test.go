package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 本文件覆盖 #158 执行趋势计数（task_log_daily_stats）在备份 / 恢复里的去向：
//   - 备份：勾「日志」时清单带 data.task_log_daily_stats（day 升序），与日志同一个读事务里取；
//   - 恢复勾「日志」（青龙除外）：计数整体换成备份里的（老备份没有这个键 = 清零），写回用 upsert；
//   - 只勾「任务」或青龙导入：现存日志先并入计数再清空，计数表不清零。
// archivedDailyStats / createTaskForLog / countTaskLogs 等夹具在同包的其它测试文件里。

// dailyStatsSeedLogs 在 2026-10-01（东八区）造 2 条成功、1 条失败日志，返回任务 id。
func dailyStatsSeedLogs(t *testing.T) uint {
	t.Helper()
	taskID := createTaskForLog(t)
	success, failed := model.LogStatusSuccess, model.LogStatusFailed
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	archiveCreateLog(t, taskID, at, &success, nil)
	archiveCreateLog(t, taskID, at.Add(time.Minute), &success, nil)
	archiveCreateLog(t, taskID, at.Add(2*time.Minute), &failed, nil)
	return taskID
}

// dailyStatsInsert 直接往计数表里插一行（模拟以前清理日志时并入的计数）。
func dailyStatsInsert(t *testing.T, stat model.TaskLogDailyStat) {
	t.Helper()
	if err := database.DB.Create(&stat).Error; err != nil {
		t.Fatalf("insert task_log_daily_stats %+v: %v", stat, err)
	}
}

// B1：勾「日志」时清单带 data.task_log_daily_stats，内容与表一致、按 day 升序；只勾「任务」时清单里没有这个键。
func TestBackupManifestCarriesTaskLogDailyStatsWithLogs(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	want := []model.TaskLogDailyStat{
		{Day: "2026-09-20", Success: 100, Failed: 2},
		{Day: "2026-09-21", Success: 3, Aborted: 1, Other: 2},
	}
	// 刻意倒序插入：清单里必须按 day 升序，而不是按插入顺序。
	dailyStatsInsert(t, want[1])
	dailyStatsInsert(t, want[0])

	filePath, err := CreateBackup(BackupCreateOptions{Selection: BackupSelection{Tasks: true, Logs: true}})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	var manifest struct {
		Data struct {
			TaskLogDailyStats []model.TaskLogDailyStat `json:"task_log_daily_stats"`
			TaskLogs          []json.RawMessage        `json:"task_logs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(readBackupManifestJSONForTest(t, filePath), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if !reflect.DeepEqual(manifest.Data.TaskLogDailyStats, want) {
		t.Fatalf("清单里的计数应为 %+v，实际 %+v", want, manifest.Data.TaskLogDailyStats)
	}
	if len(manifest.Data.TaskLogs) != 3 {
		t.Fatalf("清单里应有 3 条日志，实际 %d 条", len(manifest.Data.TaskLogs))
	}

	// 只勾「任务」：计数跟着「日志」勾选项走，清单里不该出现这个键。
	filePath, err = CreateBackup(BackupCreateOptions{Selection: BackupSelection{Tasks: true}})
	if err != nil {
		t.Fatalf("create tasks-only backup: %v", err)
	}
	var tasksOnly struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(readBackupManifestJSONForTest(t, filePath), &tasksOnly); err != nil {
		t.Fatalf("decode tasks-only manifest: %v", err)
	}
	if raw, ok := tasksOnly.Data["task_log_daily_stats"]; ok {
		t.Fatalf("只勾「任务」时清单里不该有 task_log_daily_stats，实际 %s", raw)
	}
}

// B2：恢复勾「日志」：计数整体换成备份里的——本机独有的天消失；备份里同一天出现两次时 upsert 累加，不让整次恢复回滚。
func TestRestoreLogsReplacesTaskLogDailyStats(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	dailyStatsInsert(t, model.TaskLogDailyStat{Day: "2026-09-20", Success: 100, Failed: 2})

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		Selection: BackupSelection{Logs: true},
		Data: BackupPayload{TaskLogDailyStats: []model.TaskLogDailyStat{
			{Day: "2026-08-01", Success: 5},
			{Day: "2026-08-01", Success: 7, Other: 1},
		}},
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore logs: %v", err)
	}

	want := []model.TaskLogDailyStat{{Day: "2026-08-01", Success: 12, Other: 1}}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("计数应只剩备份里的（重复的天合计），期望 %+v，实际 %+v", want, got)
	}
	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("备份里没有日志，恢复后日志应为 0 行，实际 %d 行", n)
	}
}

// B3：只勾「任务」：现存日志要清掉（任务 id 会重排），先并入计数；原有计数保留。
func TestRestoreTasksOnlyArchivesExistingLogs(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	dailyStatsInsert(t, model.TaskLogDailyStat{Day: "2026-09-20", Success: 100, Failed: 2})

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		Selection: BackupSelection{Tasks: true},
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore tasks: %v", err)
	}

	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("只勾任务时现存日志应清空，实际 %d 行", n)
	}
	want := []model.TaskLogDailyStat{
		{Day: "2026-09-20", Success: 100, Failed: 2},
		{Day: "2026-10-01", Success: 2, Failed: 1},
	}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("计数应为原有 + 现存日志按天并入，期望 %+v，实际 %+v", want, got)
	}
}

// B4：老格式备份（清单里没有 task_log_daily_stats 这个键）勾「日志」：计数清零，只剩恢复回来的日志，避免重复计数。
func TestRestoreLegacyLogsBackupClearsTaskLogDailyStats(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	dailyStatsInsert(t, model.TaskLogDailyStat{Day: "2026-09-20", Success: 100, Failed: 2})

	var manifest BackupManifest
	legacy := `{"format":"daidai-panel-backup","version":"0.4.0","source":"daidai-panel","selection":{"logs":true},"data":{}}`
	if err := json.Unmarshal([]byte(legacy), &manifest); err != nil {
		t.Fatalf("decode legacy manifest: %v", err)
	}
	if manifest.Data.TaskLogDailyStats != nil {
		t.Fatalf("前提不成立：老备份解出来的计数应为 nil，实际 %+v", manifest.Data.TaskLogDailyStats)
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore legacy logs: %v", err)
	}

	if got := archivedDailyStats(t); len(got) != 0 {
		t.Fatalf("老备份勾「日志」恢复后计数应清零，实际 %+v", got)
	}
}

// B5：青龙导入（Source=qinglong）即使识别出了日志文件（Logs=true），也按「只勾任务」处理：
// 现存日志并入计数后清空，计数表不清零——青龙包里没有呆呆面板的执行历史，清零只会丢信息。
func TestQingLongImportKeepsTaskLogDailyStats(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	dailyStatsInsert(t, model.TaskLogDailyStat{Day: "2026-09-20", Success: 100, Failed: 2})

	// 最小的青龙解包目录：data/config + data/db 让 resolveQingLongDataDir 认得出来，data/log 下放一个日志文件，
	// 这样提交之后的 restoreLogFiles（拷青龙日志文件）也能走通、整个恢复返回 nil。
	extractedDir := t.TempDir()
	for _, dir := range []string{"config", "db", "log"} {
		if err := os.MkdirAll(filepath.Join(extractedDir, "data", dir), 0o755); err != nil {
			t.Fatalf("mkdir qinglong %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "data", "log", "a.log"), []byte("qinglong log\n"), 0o644); err != nil {
		t.Fatalf("write qinglong log: %v", err)
	}

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "qinglong",
		Selection: BackupSelection{Tasks: true, Logs: true},
	}
	if err := restoreBackupManifest(manifest, extractedDir); err != nil {
		t.Fatalf("restore qinglong manifest: %v", err)
	}

	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("青龙导入后现存日志应清空，实际 %d 行", n)
	}
	want := []model.TaskLogDailyStat{
		{Day: "2026-09-20", Success: 100, Failed: 2},
		{Day: "2026-10-01", Success: 2, Failed: 1},
	}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("青龙导入不应清零计数、现存日志应并入，期望 %+v，实际 %+v", want, got)
	}
}

// B6：备份 → 本机又清理一次（日志并入计数）→ 从备份包恢复「任务 + 日志」：日志与计数都回到备份那一刻，
// 清理时并入的那一天不会和恢复回来的日志算两遍。恢复走真实的解包路径（restoreArchiveBytes），顺带证明 tgz 往返不丢计数。
func TestBackupRestoreRoundTripDoesNotDoubleCount(t *testing.T) {
	testutil.SetupTestEnv(t)
	dailyStatsSeedLogs(t)
	dailyStatsInsert(t, model.TaskLogDailyStat{Day: "2026-09-20", Success: 100, Failed: 2})
	statsAtBackup := archivedDailyStats(t)
	logsAtBackup := countTaskLogs(t)

	filePath, err := CreateBackup(BackupCreateOptions{Selection: BackupSelection{Tasks: true, Logs: true}})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	// 备份之后本机又清掉一次：日志并入计数后清空，计数多出 2026-10-01。
	if _, _, err := deleteTaskLogsInTx(t, "1 = 1"); err != nil {
		t.Fatalf("clean logs after backup: %v", err)
	}
	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("前提不成立：清理后日志应为 0 行，实际 %d 行", n)
	}
	if got := archivedDailyStats(t); len(got) != len(statsAtBackup)+1 {
		t.Fatalf("前提不成立：清理后计数应多出一天，实际 %+v", got)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	if err := restoreArchiveBytes(raw); err != nil {
		t.Fatalf("restore backup archive: %v", err)
	}

	if n := countTaskLogs(t); n != logsAtBackup {
		t.Fatalf("恢复后日志应回到备份那一刻的 %d 行，实际 %d 行", logsAtBackup, n)
	}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, statsAtBackup) {
		t.Fatalf("恢复后计数应回到备份那一刻 %+v（清理时并入的那天不能再算一遍），实际 %+v", statsAtBackup, got)
	}
}
