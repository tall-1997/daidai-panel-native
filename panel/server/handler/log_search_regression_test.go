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

func TestLogListKeywordSearchMatchesTaskNameInsteadOfContent(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "log-search-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	targetTask := model.Task{
		Name:           "京东签到任务",
		Command:        "echo target",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	otherTask := model.Task{
		Name:           "普通同步任务",
		Command:        "echo other",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}

	if err := database.DB.Create(&targetTask).Error; err != nil {
		t.Fatalf("create target task: %v", err)
	}
	if err := database.DB.Create(&otherTask).Error; err != nil {
		t.Fatalf("create other task: %v", err)
	}

	successStatus := model.LogStatusSuccess
	now := time.Now()
	targetLog := model.TaskLog{
		TaskID:    targetTask.ID,
		Content:   "这条日志内容里没有关键字",
		Status:    &successStatus,
		StartedAt: now,
	}
	otherLog := model.TaskLog{
		TaskID:    otherTask.ID,
		Content:   "日志内容里提到了签到关键字",
		Status:    &successStatus,
		StartedAt: now.Add(-time.Minute),
	}

	if err := database.DB.Create(&targetLog).Error; err != nil {
		t.Fatalf("create target log: %v", err)
	}
	if err := database.DB.Create(&otherLog).Error; err != nil {
		t.Fatalf("create other log: %v", err)
	}

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?keyword=签到", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	if int(payload["total"].(float64)) != 1 {
		t.Fatalf("expected total 1, got %v", payload["total"])
	}

	data, ok := payload["data"].([]interface{})
	if !ok || len(data) != 1 {
		t.Fatalf("expected single log entry, got %#v", payload["data"])
	}

	entry, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected log entry map, got %#v", data[0])
	}

	if entry["task_name"] != targetTask.Name {
		t.Fatalf("expected task_name %q, got %#v", targetTask.Name, entry["task_name"])
	}
}
