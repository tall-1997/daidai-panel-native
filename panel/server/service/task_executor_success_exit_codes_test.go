package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestTaskExecutorAppliesConfiguredSuccessExitCodes(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found")
	}

	for _, tt := range []struct {
		name             string
		successExitCodes string
		wantRunStatus    int
		wantLogStatus    int
		wantNote         bool
	}{
		{name: "default keeps exit code one failed", wantRunStatus: model.RunFailed, wantLogStatus: model.LogStatusFailed},
		{name: "configured exit code one succeeds", successExitCodes: "0,1", wantRunStatus: model.RunSuccess, wantLogStatus: model.LogStatusSuccess, wantNote: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)

			scriptPath := filepath.Join(config.C.Data.ScriptsDir, "exit-one.js")
			if err := os.WriteFile(scriptPath, []byte("console.log('business completed'); process.exit(1);\n"), 0o644); err != nil {
				t.Fatalf("write script: %v", err)
			}

			task := &model.Task{
				Name:             tt.name,
				Command:          "node exit-one.js",
				TaskType:         model.TaskTypeManual,
				Status:           model.TaskStatusRunning,
				SuccessExitCodes: tt.successExitCodes,
				Timeout:          30,
			}
			if err := database.DB.Create(task).Error; err != nil {
				t.Fatalf("create task: %v", err)
			}

			runningStatus := model.LogStatusRunning
			taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
			if err := database.DB.Create(taskLog).Error; err != nil {
				t.Fatalf("create task log: %v", err)
			}
			tinyLog, err := NewTinyLog("success-exit-codes")
			if err != nil {
				t.Fatalf("create tiny log: %v", err)
			}

			plan, err := ParseCommandExecutionPlan(task.Command, config.C.Data.ScriptsDir)
			if err != nil {
				t.Fatalf("parse execution plan: %v", err)
			}
			req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan}
			NewTaskExecutor().runTask(req, taskLog, tinyLog)

			var storedTask model.Task
			if err := database.DB.First(&storedTask, task.ID).Error; err != nil {
				t.Fatalf("reload task: %v", err)
			}
			if storedTask.LastRunStatus == nil || *storedTask.LastRunStatus != tt.wantRunStatus {
				t.Fatalf("expected run status %d, got %#v", tt.wantRunStatus, storedTask.LastRunStatus)
			}

			var storedLog model.TaskLog
			if err := database.DB.First(&storedLog, taskLog.ID).Error; err != nil {
				t.Fatalf("reload task log: %v", err)
			}
			if storedLog.Status == nil || *storedLog.Status != tt.wantLogStatus {
				t.Fatalf("expected log status %d, got %#v", tt.wantLogStatus, storedLog.Status)
			}
			content, err := DecompressFromBase64(storedLog.Content)
			if err != nil {
				t.Fatalf("decompress task log: %v", err)
			}
			if !strings.Contains(content, "退出码 1") {
				t.Fatalf("expected raw exit code in log, got %q", content)
			}
			if got := strings.Contains(content, "已按任务配置判定成功"); got != tt.wantNote {
				t.Fatalf("expected compatibility note=%v, got log %q", tt.wantNote, content)
			}
		})
	}
}
