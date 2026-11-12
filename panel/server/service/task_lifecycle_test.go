package service

import (
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestRecoverAbandonedActiveTasksClearsStaleRunningTask(t *testing.T) {
	testutil.SetupTestEnv(t)
	database.EnsureColumns()

	now := time.Now().Add(-2 * time.Minute)
	failedStatus := model.LogStatusRunning
	pid := 12345
	task := &model.Task{
		Name:           "stale running cron",
		Command:        "python task.py",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusRunning,
		LastRunAt:      &now,
		PID:            &pid,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskLog := &model.TaskLog{
		TaskID:    task.ID,
		Status:    &failedStatus,
		StartedAt: now,
	}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	count := RecoverAbandonedActiveTasks("面板上次异常退出，任务已中断")
	if count != 1 {
		t.Fatalf("expected 1 recovered task, got %d", count)
	}

	var updated model.Task
	if err := database.DB.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.Status != model.TaskStatusEnabled {
		t.Fatalf("expected stale cron task to return to enabled, got %v", updated.Status)
	}
	if updated.PID != nil {
		t.Fatalf("expected pid to be cleared, got %#v", updated.PID)
	}
	if updated.LastRunStatus == nil || *updated.LastRunStatus != model.RunFailed {
		t.Fatalf("expected failed run status, got %#v", updated.LastRunStatus)
	}
	if updated.LastRunningTime == nil || *updated.LastRunningTime <= 0 {
		t.Fatalf("expected positive running time, got %#v", updated.LastRunningTime)
	}

	var updatedLog model.TaskLog
	if err := database.DB.First(&updatedLog, taskLog.ID).Error; err != nil {
		t.Fatalf("reload task log: %v", err)
	}
	if updatedLog.Status == nil || *updatedLog.Status != model.LogStatusFailed {
		t.Fatalf("expected failed log status, got %#v", updatedLog.Status)
	}
	if updatedLog.EndedAt == nil {
		t.Fatal("expected ended_at to be recorded")
	}
	if !strings.Contains(updatedLog.Content, "任务已中断") {
		t.Fatalf("expected interruption reason in log content, got %q", updatedLog.Content)
	}
}

func TestMarkActiveTasksInterruptedUsesSchedulerRegistration(t *testing.T) {
	testutil.SetupTestEnv(t)
	database.EnsureColumns()

	previousScheduler := globalScheduler.Load()
	globalScheduler.Store(NewSchedulerV2(SchedulerConfig{WorkerCount: 1, QueueSize: 10, RateInterval: time.Hour}, nil))
	t.Cleanup(func() {
		globalScheduler.Store(previousScheduler)
	})

	enabledManual := &model.Task{
		Name:     "registered manual",
		Command:  "echo hi",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(enabledManual).Error; err != nil {
		t.Fatalf("create enabled manual task: %v", err)
	}
	if err := globalScheduler.Load().AddJob(enabledManual); err != nil {
		t.Fatalf("register manual task: %v", err)
	}
	if err := database.DB.Model(enabledManual).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("mark manual task running: %v", err)
	}

	count := MarkActiveTasksInterrupted("面板关闭，任务已中断")
	// running 任务即使 running 日志缺失（未建或被删）也按确实跑过结算——
	// H4 契约（handler 层 TestDeletingRunningLogThenRecoveryCountsTwice 钉住同一条）。
	if count != 1 {
		t.Fatalf("expected 1 interrupted task, got %d", count)
	}

	var updated model.Task
	if err := database.DB.First(&updated, enabledManual.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.Status != model.TaskStatusEnabled {
		t.Fatalf("expected registered manual task to return enabled, got %v", updated.Status)
	}
}

func TestMarkActiveTasksInterruptedResetsQueuedTaskWithoutRunHistory(t *testing.T) {
	// 排队中的任务从未真正执行（执行线程还没为它写过 running 日志）：结算只复位 status，
	// 不写 last_run_status / last_running_time、不新建中断日志，
	// 避免冷启动把没跑过的任务误标成停止/失败。
	testutil.SetupTestEnv(t)
	database.EnsureColumns()

	now := time.Now().Add(-1 * time.Minute)
	pid := 99999
	task := &model.Task{
		Name:      "queued without run history",
		Command:   "echo hi",
		TaskType:  model.TaskTypeManual,
		Status:    model.TaskStatusQueued,
		LastRunAt: &now,
		PID:       &pid,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	var logCountBefore int64
	database.DB.Model(&model.TaskLog{}).Where("task_id = ?", task.ID).Count(&logCountBefore)

	count := MarkActiveTasksInterrupted("面板关闭，任务已中断")
	if count != 0 {
		t.Fatalf("expected 0 interrupted tasks for queued task, got %d", count)
	}

	var updated model.Task
	if err := database.DB.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.Status != model.TaskStatusEnabled {
		t.Fatalf("expected queued task to return to enabled, got %v", updated.Status)
	}
	if updated.PID != nil {
		t.Fatalf("expected pid to be cleared, got %#v", updated.PID)
	}
	if updated.LastRunStatus != nil && *updated.LastRunStatus != 0 {
		t.Fatalf("expected last_run_status untouched for queued task, got %#v", updated.LastRunStatus)
	}
	if updated.LastRunningTime != nil && *updated.LastRunningTime != 0 {
		t.Fatalf("expected last_running_time untouched for queued task, got %#v", updated.LastRunningTime)
	}

	var logCountAfter int64
	database.DB.Model(&model.TaskLog{}).Where("task_id = ?", task.ID).Count(&logCountAfter)
	if logCountAfter != logCountBefore {
		t.Fatalf("expected no new log entries for queued task, got %d (before: %d)", logCountAfter, logCountBefore)
	}
}

func TestRecoverAbandonedActiveTasksSettlesRunningTaskWithoutRunningLog(t *testing.T) {
	// H4 契约（handler 层 TestDeletingRunningLogThenRecoveryCountsTwice）：运行中的任务即使
	// running 日志缺失（被删或未建）也按确实跑过结算，并另建一行失败日志；
	// 同一次执行在趋势里算两次是已接受的例外。
	testutil.SetupTestEnv(t)
	database.EnsureColumns()

	now := time.Now().Add(-1 * time.Minute)
	pid := 4242
	task := &model.Task{
		Name:      "running without log",
		Command:   "echo hi",
		TaskType:  model.TaskTypeManual,
		Status:    model.TaskStatusRunning,
		LastRunAt: &now,
		PID:       &pid,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	count := RecoverAbandonedActiveTasks("面板上次异常退出，任务已中断")
	if count != 1 {
		t.Fatalf("expected 1 recovered task, got %d", count)
	}

	var updated model.Task
	if err := database.DB.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.Status != model.TaskStatusEnabled {
		t.Fatalf("expected running task to return to enabled, got %v", updated.Status)
	}
	if updated.PID != nil {
		t.Fatalf("expected pid to be cleared, got %#v", updated.PID)
	}
	if updated.LastRunStatus == nil || *updated.LastRunStatus != model.RunFailed {
		t.Fatalf("expected failed run status, got %#v", updated.LastRunStatus)
	}

	var logs []model.TaskLog
	database.DB.Where("task_id = ?", task.ID).Find(&logs)
	if len(logs) != 1 {
		t.Fatalf("expected exactly 1 fallback interrupted log, got %d", len(logs))
	}
	if logs[0].Status == nil || *logs[0].Status != model.LogStatusFailed {
		t.Fatalf("expected failed log status, got %#v", logs[0].Status)
	}
	if !strings.Contains(logs[0].Content, "任务已中断") {
		t.Fatalf("expected interruption reason in fallback log content, got %q", logs[0].Content)
	}
}
