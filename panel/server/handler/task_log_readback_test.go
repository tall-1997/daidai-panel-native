package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestCompletedTaskLogDetailAndSSEReadSamePersistedOutput(t *testing.T) {
	testutil.SetupTestEnv(t)
	task := &model.Task{
		Name:             "completed log fixture",
		Command:          "task fixture.go",
		CronExpression:   "0 0 * * *",
		TaskType:         model.TaskTypeManual,
		Status:           model.TaskStatusEnabled,
		SuccessExitCodes: model.DefaultSuccessExitCodes,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	plain := "stdout-line\nstderr-line\n"
	status := model.LogStatusSuccess
	endedAt := time.Now()
	logRecord := &model.TaskLog{
		TaskID:    task.ID,
		Content:   plain,
		Status:    &status,
		LogCursor: int64(len(plain)),
		StartedAt: endedAt.Add(-time.Second),
		EndedAt:   &endedAt,
	}
	if err := database.DB.Create(logRecord).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(task.ID), 10)}}
	NewTaskHandler().LatestLog(ctx)
	if recorder.Code != 200 {
		t.Fatalf("expected log detail status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode log detail: %v", err)
	}
	if detail["content"] != plain {
		t.Fatalf("expected persisted log detail %q, got %#v", plain, detail["content"])
	}

	sseRecorder := httptest.NewRecorder()
	sseContext, _ := gin.CreateTestContext(sseRecorder)
	sseContext.Request = httptest.NewRequest(http.MethodGet, "/?cursor="+strconv.Itoa(len("stdout-line\n")), nil)
	sseContext.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(task.ID), 10)}}
	NewLogHandler().Stream(sseContext)
	sse := sseRecorder.Body.String()
	if !strings.Contains(sse, "id: "+strconv.Itoa(len(plain))) || !strings.Contains(sse, "data: stderr-line") {
		t.Fatalf("expected SSE payload with persisted cursor and stderr, got %q", sse)
	}
	if strings.Contains(sse, "data: stdout-line") || !strings.Contains(sse, "event: done\ndata: finished") {
		t.Fatalf("expected cursor-resumed completed SSE payload, got %q", sse)
	}
}
