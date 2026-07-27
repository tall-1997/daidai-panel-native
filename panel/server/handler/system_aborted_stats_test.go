package handler_test

import (
	"net/http"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestSystemDashboardAndStatsReportAbortedSeparately(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "system-aborted-stats", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:     "aborted stats task",
		Command:  "echo ok",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	now := time.Now()
	for _, status := range []int{model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusAborted} {
		status := status
		duration := 1.0
		logRecord := &model.TaskLog{
			TaskID:    task.ID,
			Status:    &status,
			Duration:  &duration,
			StartedAt: now.Add(-time.Duration(status+1) * time.Minute),
			EndedAt:   &now,
		}
		if err := database.DB.Create(logRecord).Error; err != nil {
			t.Fatalf("create task log: %v", err)
		}
	}

	dashboardRec := performJSONRequest(
		engine,
		http.MethodGet,
		"/api/v1/system/dashboard",
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard 200, got %d body=%s", dashboardRec.Code, dashboardRec.Body.String())
	}
	dashboardPayload := decodeJSONMap(t, dashboardRec)
	dashboardData, ok := dashboardPayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dashboard data object, got %#v", dashboardPayload["data"])
	}
	if got := dashboardData["success_logs"]; got != float64(1) {
		t.Fatalf("expected success_logs=1, got %#v", got)
	}
	if got := dashboardData["failed_logs"]; got != float64(1) {
		t.Fatalf("expected failed_logs=1, got %#v", got)
	}
	if got := dashboardData["aborted_logs"]; got != float64(1) {
		t.Fatalf("expected aborted_logs=1, got %#v", got)
	}

	// 每日趋势也必须返回 aborted，前端折线图和环形统计依赖这个字段。
	dailyStats, ok := dashboardData["daily_stats"].([]interface{})
	if !ok || len(dailyStats) == 0 {
		t.Fatalf("expected non-empty daily_stats, got %#v", dashboardData["daily_stats"])
	}
	foundAbortedDay := false
	for _, item := range dailyStats {
		day, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if day["aborted"] == float64(1) {
			foundAbortedDay = true
			break
		}
	}
	if !foundAbortedDay {
		t.Fatalf("expected one daily stat with aborted=1, got %#v", dailyStats)
	}

	statsRec := performJSONRequest(
		engine,
		http.MethodGet,
		"/api/v1/system/stats",
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("expected stats 200, got %d body=%s", statsRec.Code, statsRec.Body.String())
	}
	statsPayload := decodeJSONMap(t, statsRec)
	statsData, ok := statsPayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected stats data object, got %#v", statsPayload["data"])
	}
	logsData, ok := statsData["logs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected logs object, got %#v", statsData["logs"])
	}
	if got := logsData["aborted"]; got != float64(1) {
		t.Fatalf("expected stats logs aborted=1, got %#v", got)
	}
	if got := logsData["success_rate"]; got != float64(50) {
		t.Fatalf("expected success_rate=50 when aborted is excluded, got %#v", got)
	}
}
