package service

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestSchedulerV2AddJobRegistersEnabledManualTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)

	task := &model.Task{
		Name:     "manual task",
		Command:  "echo hi",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("add manual task job: %v", err)
	}
	if !scheduler.HasJob(task.ID) {
		t.Fatal("expected enabled manual task to be registered for state restoration")
	}
}

func TestSchedulerV2EnqueueStartupTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	startupTask := &model.Task{
		Name:     "startup task",
		Command:  "echo boot",
		TaskType: model.TaskTypeStartup,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(startupTask).Error; err != nil {
		t.Fatalf("create startup task: %v", err)
	}
	disabledStartupTask := &model.Task{
		Name:     "disabled startup task",
		Command:  "echo no",
		TaskType: model.TaskTypeStartup,
		Status:   model.TaskStatusDisabled,
	}
	if err := database.DB.Create(disabledStartupTask).Error; err != nil {
		t.Fatalf("create disabled startup task: %v", err)
	}

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)

	if err := scheduler.AddJob(startupTask); err != nil {
		t.Fatalf("register startup task: %v", err)
	}

	count := scheduler.EnqueueStartupTasks()
	if count != 1 {
		t.Fatalf("expected 1 startup task to be enqueued, got %d", count)
	}
	if got := len(scheduler.taskQueue); got != 1 {
		t.Fatalf("expected queue length 1, got %d", got)
	}

	var updated model.Task
	if err := database.DB.First(&updated, startupTask.ID).Error; err != nil {
		t.Fatalf("reload startup task: %v", err)
	}
	if updated.Status != model.TaskStatusQueued {
		t.Fatalf("expected startup task status queued, got %v", updated.Status)
	}
}

func TestSchedulerV2EnqueueStartupTasksOnlyOncePerDay(t *testing.T) {
	testutil.SetupTestEnv(t)

	today := time.Now().Format("2006-01-02")
	startupTask := &model.Task{
		Name:     "startup daily task",
		Command:  "echo boot once",
		TaskType: model.TaskTypeStartup,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(startupTask).Error; err != nil {
		t.Fatalf("create startup task: %v", err)
	}

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)

	if count := scheduler.EnqueueStartupTasks(); count != 1 {
		t.Fatalf("expected first startup enqueue count 1, got %d", count)
	}

	var updated model.Task
	if err := database.DB.First(&updated, startupTask.ID).Error; err != nil {
		t.Fatalf("reload startup task: %v", err)
	}
	if updated.LastStartupAutoRunDate != today {
		t.Fatalf("expected startup auto run date %q, got %q", today, updated.LastStartupAutoRunDate)
	}

	// 模拟第一次开机运行已经结束：任务状态会回到启用，但当天再次重启面板不应再自动入队。
	if err := database.DB.Model(&model.Task{}).Where("id = ?", startupTask.ID).Update("status", model.TaskStatusEnabled).Error; err != nil {
		t.Fatalf("reset startup task status: %v", err)
	}

	if count := scheduler.EnqueueStartupTasks(); count != 0 {
		t.Fatalf("expected same-day startup enqueue count 0, got %d", count)
	}
	if got := len(scheduler.taskQueue); got != 1 {
		t.Fatalf("expected only the first automatic queue item to remain, got queue length %d", got)
	}

	if err := scheduler.RunNow(startupTask.ID); err != nil {
		t.Fatalf("manual run should ignore startup auto date: %v", err)
	}
	if err := scheduler.RunNow(startupTask.ID); err != nil {
		t.Fatalf("second manual run should ignore startup auto date: %v", err)
	}
	if got := len(scheduler.taskQueue); got != 3 {
		t.Fatalf("expected one automatic item plus two manual items, got queue length %d", got)
	}
}

func TestSchedulerV2EnqueueStartupTasksRunsAgainWhenStoredDateIsOld(t *testing.T) {
	testutil.SetupTestEnv(t)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	startupTask := &model.Task{
		Name:                   "old startup task",
		Command:                "echo boot again",
		TaskType:               model.TaskTypeStartup,
		Status:                 model.TaskStatusEnabled,
		LastStartupAutoRunDate: yesterday,
	}
	if err := database.DB.Create(startupTask).Error; err != nil {
		t.Fatalf("create startup task: %v", err)
	}

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)

	if count := scheduler.EnqueueStartupTasks(); count != 1 {
		t.Fatalf("expected old-date startup task to enqueue again, got %d", count)
	}
}

func TestSchedulerV2RejectsEnqueueAfterStop(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)
	scheduler.Start()
	scheduler.Stop()

	err := scheduler.Enqueue(&ExecutionRequest{
		TaskID: 1,
		Task: &model.Task{
			ID:      1,
			Name:    "stopped task",
			Command: "echo no",
			Status:  model.TaskStatusEnabled,
		},
	})
	if err == nil {
		t.Fatal("expected stopped scheduler to reject enqueue")
	}
}

func TestSchedulerV2StopTaskByScheduleMarksRunningLogAborted(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 本测试只验证定时停止的数据库兜底收口，不需要真实执行器参与。
	oldExecutor := globalExecutor
	globalExecutor = nil
	t.Cleanup(func() {
		globalExecutor = oldExecutor
	})

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)

	tests := []string{"定时停止长驻任务", "定时停止普通运行任务"}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			task := &model.Task{
				Name:     name,
				Command:  "echo running",
				TaskType: model.TaskTypeCron,
				Status:   model.TaskStatusRunning,
			}
			if err := database.DB.Create(task).Error; err != nil {
				t.Fatalf("create task: %v", err)
			}

			runningStatus := model.LogStatusRunning
			logRecord := &model.TaskLog{
				TaskID:    task.ID,
				Status:    &runningStatus,
				StartedAt: time.Now().Add(-time.Minute),
			}
			if err := database.DB.Create(logRecord).Error; err != nil {
				t.Fatalf("create task log: %v", err)
			}

			scheduler.stopTaskBySchedule(task.ID)

			var updatedLog model.TaskLog
			if err := database.DB.First(&updatedLog, logRecord.ID).Error; err != nil {
				t.Fatalf("reload task log: %v", err)
			}
			if updatedLog.Status == nil || *updatedLog.Status != model.LogStatusAborted {
				t.Fatalf("expected aborted log status, got %#v", updatedLog.Status)
			}
			if updatedLog.EndedAt == nil {
				t.Fatalf("expected ended_at after scheduled stop")
			}
		})
	}
}
