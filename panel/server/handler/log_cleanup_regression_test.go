package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestLogCleanupUsesRegisteredRetentionDaysByDefault(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "cleanup-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	if err := model.SetConfig("log_retention_days", "30"); err != nil {
		t.Fatalf("set log_retention_days: %v", err)
	}

	task := model.Task{
		Name:           "cleanup-task",
		Command:        "echo test",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	oldLogStatus := model.LogStatusSuccess
	oldStartedAt := time.Now().AddDate(0, 0, -31)
	recentStartedAt := time.Now().AddDate(0, 0, -10)

	oldLog := model.TaskLog{
		TaskID:    task.ID,
		Status:    &oldLogStatus,
		StartedAt: oldStartedAt,
	}
	recentLog := model.TaskLog{
		TaskID:    task.ID,
		Status:    &oldLogStatus,
		StartedAt: recentStartedAt,
	}

	if err := database.DB.Create(&oldLog).Error; err != nil {
		t.Fatalf("create old log: %v", err)
	}
	if err := database.DB.Create(&recentLog).Error; err != nil {
		t.Fatalf("create recent log: %v", err)
	}

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/logs/clean", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.TaskLog{}).Where("id = ?", oldLog.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected old log to be deleted, remaining=%d", count)
	}

	database.DB.Model(&model.TaskLog{}).Where("id = ?", recentLog.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected recent log to remain, remaining=%d", count)
	}
}
