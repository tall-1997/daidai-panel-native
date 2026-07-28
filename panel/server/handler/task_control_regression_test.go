package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func setupTaskExecutionRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-control-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	_ = service.StopSchedulerV2(context.Background())
	if err := service.StartSchedulerV2(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	t.Cleanup(func() {
		_ = service.StopSchedulerV2(context.Background())
	})
	return engine, token
}

func waitForOperationState(t *testing.T, operationID string, wantTerminal bool) model.Operation {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	var op model.Operation
	for time.Now().Before(deadline) {
		if err := database.DB.First(&op, "id = ?", operationID).Error; err == nil {
			if model.IsOperationTerminalState(op.State) == wantTerminal {
				return op
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := database.DB.First(&op, "id = ?", operationID).Error; err != nil {
		t.Fatalf("reload operation %s: %v", operationID, err)
	}
	t.Fatalf("operation %s terminal=%v state=%s", operationID, wantTerminal, op.State)
	return op
}

func waitForTaskPID(t *testing.T, taskID uint) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var task model.Task
		if err := database.DB.First(&task, taskID).Error; err == nil && task.PID != nil && *task.PID > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %d did not start a process", taskID)
}

func decodeOperationID(t *testing.T, recBody []byte) string {
	t.Helper()

	var payload map[string]interface{}
	if err := json.Unmarshal(recBody, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	operationID, ok := payload["operation_id"].(string)
	if !ok || strings.TrimSpace(operationID) == "" {
		t.Fatalf("expected operation_id in response, got %s", string(recBody))
	}
	if _, ok := payload["message"].(string); !ok {
		t.Fatalf("expected message compatibility field, got %s", string(recBody))
	}
	return operationID
}

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

func TestRunTaskReturnsTraceableExecutionOperationID(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	testutil.SetupTestEnv(t)
	engine, token := setupTaskExecutionRouter(t)

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "run-success.sh")
	if err := os.WriteFile(scriptPath, []byte("exit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	task := &model.Task{
		Name:     "traceable run operation",
		Command:  "bash run-success.sh",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
		Timeout:  5,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		fmt.Sprintf("/api/v1/tasks/%d/run", task.ID),
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	operationID := decodeOperationID(t, rec.Body.Bytes())
	if strings.HasPrefix(operationID, "task_run_request_") {
		t.Fatalf("run returned request operation id %q", operationID)
	}

	op := waitForOperationState(t, operationID, true)
	if op.State != model.OperationStateSuccess {
		t.Fatalf("expected run operation success, got %+v", op)
	}
}

func TestStopTaskReturnsCurrentExecutionOperationIDAndKeepsSingleTerminal(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	testutil.SetupTestEnv(t)
	engine, token := setupTaskExecutionRouter(t)

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "run-slow.sh")
	if err := os.WriteFile(scriptPath, []byte("sleep 10\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	task := &model.Task{
		Name:     "stoppable execution operation",
		Command:  "bash run-slow.sh",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
		Timeout:  30,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	runRec := performJSONRequest(
		engine,
		http.MethodPut,
		fmt.Sprintf("/api/v1/tasks/%d/run", task.ID),
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if runRec.Code != http.StatusOK {
		t.Fatalf("expected run 200, got %d body=%s", runRec.Code, runRec.Body.String())
	}
	runOperationID := decodeOperationID(t, runRec.Body.Bytes())
	waitForTaskPID(t, task.ID)

	stopRec := performJSONRequest(
		engine,
		http.MethodPut,
		fmt.Sprintf("/api/v1/tasks/%d/stop", task.ID),
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("expected stop 200, got %d body=%s", stopRec.Code, stopRec.Body.String())
	}
	stopOperationID := decodeOperationID(t, stopRec.Body.Bytes())
	if stopOperationID != runOperationID {
		t.Fatalf("expected stop to return execution operation %q, got %q", runOperationID, stopOperationID)
	}

	op := waitForOperationState(t, stopOperationID, true)
	if op.State != model.OperationStateCanceled {
		t.Fatalf("expected canceled execution operation, got %+v", op)
	}
	time.Sleep(200 * time.Millisecond)
	if err := database.DB.First(&op, "id = ?", stopOperationID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if op.State != model.OperationStateCanceled || op.ErrorCode != "aborted" {
		t.Fatalf("execution terminal state was overwritten: %+v", op)
	}

	var terminalCount int64
	if err := database.DB.Model(&model.Operation{}).
		Where("id = ? AND state IN ?", stopOperationID, model.OperationTerminalStates()).
		Count(&terminalCount).Error; err != nil {
		t.Fatalf("count terminal operations: %v", err)
	}
	if terminalCount != 1 {
		t.Fatalf("expected one execution terminal state, got %d", terminalCount)
	}
}
