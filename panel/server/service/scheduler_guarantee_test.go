package service

import (
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestShouldPauseLowPriorityWorkOnlyPausesBackgroundTriggers(t *testing.T) {
	t.Cleanup(func() {
		ConfigureSchedulerGuarantee(SchedulerGuaranteeSnapshot{State: SchedulerGuaranteeSystemCompensation, ReasonCode: "reset"})
	})
	ConfigureSchedulerGuarantee(SchedulerGuaranteeSnapshot{
		State:        SchedulerGuaranteeResourceLimited,
		ReasonCode:   "battery_low",
		Intervention: "charge device",
	})

	guarantee, paused := ShouldPauseLowPriorityWork(TriggerTypeCron)
	if !paused || guarantee.ReasonCode != "battery_low" {
		t.Fatalf("expected cron pause with battery_low, got paused=%v guarantee=%+v", paused, guarantee)
	}
	_, paused = ShouldPauseLowPriorityWork(TriggerTypeManual)
	if paused {
		t.Fatalf("manual work must remain runnable under resource protection")
	}
}

func TestTaskExecutorFailsCronOperationWhenResourceLimited(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Cleanup(func() {
		ConfigureSchedulerGuarantee(SchedulerGuaranteeSnapshot{State: SchedulerGuaranteeSystemCompensation, ReasonCode: "reset"})
	})
	ConfigureSchedulerGuarantee(SchedulerGuaranteeSnapshot{
		State:      SchedulerGuaranteeResourceLimited,
		ReasonCode: "storage_low",
	})

	task := &model.Task{Name: "cron", Command: "echo ok", CronExpression: "0 * * * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled, SuccessExitCodes: model.DefaultSuccessExitCodes}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	operationID, err := NewTaskRunOperation(task.ID)
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}

	executor := NewTaskExecutor()
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TriggerType: TriggerTypeCron}
	err = executor.OnTaskExecuting(req)
	if err == nil || !strings.Contains(err.Error(), "storage_low") {
		t.Fatalf("expected storage_low pause error, got %v", err)
	}

	var operation model.Operation
	if err := database.DB.First(&operation, "id = ?", operationID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if operation.State != model.OperationStateFailed || operation.ErrorCode != "storage_low" {
		t.Fatalf("expected failed storage_low operation, got %+v", operation)
	}
}
