package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestEnsureColumnsAddsTaskSuccessExitCodesToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacyTask := &model.Task{
		Name:             "legacy task",
		Command:          "task legacy.js",
		CronExpression:   "",
		TaskType:         model.TaskTypeManual,
		Status:           model.TaskStatusEnabled,
		SuccessExitCodes: "0,1",
	}
	if err := database.DB.Create(legacyTask).Error; err != nil {
		t.Fatalf("create task before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.Task{}, "SuccessExitCodes"); err != nil {
		t.Fatalf("drop success_exit_codes to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.Task{}, "SuccessExitCodes") {
		t.Fatal("expected simulated legacy database to have no success_exit_codes column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.Task{}, "SuccessExitCodes") {
		t.Fatal("expected EnsureColumns to add success_exit_codes")
	}

	var storedValue string
	if err := database.DB.Raw("SELECT success_exit_codes FROM tasks WHERE id = ?", legacyTask.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read migrated success_exit_codes: %v", err)
	}
	if storedValue != model.DefaultSuccessExitCodes {
		t.Fatalf("expected migrated legacy task default %q, got %q", model.DefaultSuccessExitCodes, storedValue)
	}
}
