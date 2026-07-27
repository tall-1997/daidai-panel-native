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

	previousScheduler := globalScheduler
	globalScheduler = NewSchedulerV2(SchedulerConfig{WorkerCount: 1, QueueSize: 10, RateInterval: time.Hour}, nil)
	t.Cleanup(func() {
		globalScheduler = previousScheduler
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
	if err := globalScheduler.AddJob(enabledManual); err != nil {
		t.Fatalf("register manual task: %v", err)
	}
	if err := database.DB.Model(enabledManual).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("mark manual task running: %v", err)
	}

	count := MarkActiveTasksInterrupted("面板关闭，任务已中断")
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
