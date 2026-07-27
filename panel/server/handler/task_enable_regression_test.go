package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

func TestEnableTaskAcceptsMultipleCronExpressions(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-enable-multi", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := model.Task{
		Name:           "multi cron task",
		Command:        "echo ok",
		CronExpression: "55 59 9 * * *\n55 59 17 * * *\n55 59 19 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusDisabled,
		Timeout:        300,
		RetryInterval:  60,
	}
	if err := database.DB.Select("*").Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodPut,
		fmt.Sprintf("/api/v1/tasks/%d/enable", task.ID),
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	if scheduler := service.GetSchedulerV2(); scheduler == nil || !scheduler.HasJob(task.ID) {
		t.Fatalf("expected enabled task to be registered in scheduler")
	}
}

func TestBatchEnableSkipsInvalidCronAndEnablesValidMultiCronTask(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-batch-enable", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	validTask := model.Task{
		Name:           "valid multi cron task",
		Command:        "echo valid",
		CronExpression: "0 0 12 * * *\n0 0 18 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusDisabled,
		Timeout:        300,
		RetryInterval:  60,
	}
	if err := database.DB.Select("*").Create(&validTask).Error; err != nil {
		t.Fatalf("create valid task: %v", err)
	}

	invalidTask := model.Task{
		Name:           "invalid task",
		Command:        "echo invalid",
		CronExpression: "invalid cron",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusDisabled,
		Timeout:        300,
		RetryInterval:  60,
	}
	if err := database.DB.Select("*").Create(&invalidTask).Error; err != nil {
		t.Fatalf("create invalid task: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/batch/enable",
		fmt.Sprintf(`{"task_ids":[%d,%d]}`, validTask.ID, invalidTask.ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var reloadedValid model.Task
	if err := database.DB.First(&reloadedValid, validTask.ID).Error; err != nil {
		t.Fatalf("reload valid task: %v", err)
	}
	if reloadedValid.Status != model.TaskStatusEnabled {
		t.Fatalf("expected valid task enabled, got status=%v", reloadedValid.Status)
	}

	var reloadedInvalid model.Task
	if err := database.DB.First(&reloadedInvalid, invalidTask.ID).Error; err != nil {
		t.Fatalf("reload invalid task: %v", err)
	}
	if reloadedInvalid.Status != model.TaskStatusDisabled {
		t.Fatalf("expected invalid task to stay disabled, got status=%v", reloadedInvalid.Status)
	}
}
