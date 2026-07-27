package handler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestStopTaskMarksRunningLogAborted(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-stop-outcome", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tests := []struct {
		name string
	}{
		{name: "手动停止"},
		{name: "定时停止兜底同口径"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startedAt := time.Now().Add(-time.Minute)
			task := &model.Task{
				Name:     tt.name,
				Command:  "echo running",
				TaskType: model.TaskTypeManual,
				Status:   model.TaskStatusRunning,
			}
			if err := database.DB.Create(task).Error; err != nil {
				t.Fatalf("create task: %v", err)
			}
			runningStatus := model.LogStatusRunning
			logRecord := &model.TaskLog{
				TaskID:    task.ID,
				Status:    &runningStatus,
				StartedAt: startedAt,
			}
			if err := database.DB.Create(logRecord).Error; err != nil {
				t.Fatalf("create task log: %v", err)
			}

			rec := performJSONRequest(
				engine,
				http.MethodPut,
				fmt.Sprintf("/api/v1/tasks/%d/stop", task.ID),
				`{}`,
				map[string]string{"Authorization": "Bearer " + token},
				"",
			)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}

			var updatedLog model.TaskLog
			if err := database.DB.First(&updatedLog, logRecord.ID).Error; err != nil {
				t.Fatalf("reload task log: %v", err)
			}
			if updatedLog.Status == nil || *updatedLog.Status != model.LogStatusAborted {
				t.Fatalf("expected aborted log status, got %#v", updatedLog.Status)
			}
			if updatedLog.EndedAt == nil {
				t.Fatalf("expected ended_at after stop")
			}

			var updatedTask model.Task
			if err := database.DB.First(&updatedTask, task.ID).Error; err != nil {
				t.Fatalf("reload task: %v", err)
			}
			if updatedTask.LastRunStatus == nil || *updatedTask.LastRunStatus != model.RunAborted {
				t.Fatalf("expected task last_run_status aborted, got %#v", updatedTask.LastRunStatus)
			}
		})
	}
}
