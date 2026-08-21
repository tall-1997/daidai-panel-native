package handler

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestTaskLogFilesIncludePersistedLogID(t *testing.T) {
	testutil.SetupTestEnv(t)
	task := &model.Task{Name: "file contract", Command: "task fixture.go", CronExpression: "0 0 * * *", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled, SuccessExitCodes: model.DefaultSuccessExitCodes}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	relPath := filepath.ToSlash(filepath.Join("task_"+strconv.FormatUint(uint64(task.ID), 10), "run.log"))
	fullPath := filepath.Join(config.C.Data.LogDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("output"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	status := model.LogStatusSuccess
	logRecord := &model.TaskLog{TaskID: task.ID, Status: &status, LogPath: &relPath, StartedAt: time.Now()}
	if err := database.DB.Create(logRecord).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(task.ID), 10)}}
	NewTaskHandler().LogFiles(ctx)
	var files []struct {
		Filename  string `json:"filename"`
		Path      string `json:"path"`
		LogID     uint   `json:"log_id"`
		Size      int64  `json:"size"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) != 1 || files[0].LogID != logRecord.ID || files[0].Filename != "run.log" || files[0].Path != relPath || files[0].Size != 6 || files[0].CreatedAt == "" {
		t.Fatalf("unexpected log file contract: %#v", files)
	}
}
