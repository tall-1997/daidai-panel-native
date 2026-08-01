package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

type recordingProcessSupervisor struct {
	delegate ProcessSupervisor

	mu      sync.Mutex
	specs   []ProcessSpec
	streams []string
}

func (supervisor *recordingProcessSupervisor) Start(ctx context.Context, spec ProcessSpec, onEvent func(ProcessEvent)) (SupervisedProcess, error) {
	supervisor.mu.Lock()
	supervisor.specs = append(supervisor.specs, spec)
	supervisor.mu.Unlock()

	return supervisor.delegate.Start(ctx, spec, func(event ProcessEvent) {
		supervisor.mu.Lock()
		supervisor.streams = append(supervisor.streams, event.Stream)
		supervisor.mu.Unlock()
		if onEvent != nil {
			onEvent(event)
		}
	})
}

func TestAndroidGoTaskExecutionPersistsEnvironmentOutputAndStructuredArgv(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("create native runtime dir: %v", err)
	}
	runtimePath := filepath.Join(nativeDir, "libyaegi_go_exec.so")
	fixture := `#!/bin/sh
printf 'stdout-env=%s\n' "$ANDROID_TASK_TOKEN"
printf 'stdout-args=%s|%s|%s\n' "$1" "$2" "$3"
printf 'stderr-env=%s\n' "$ANDROID_TASK_TOKEN" >&2
`
	if err := os.WriteFile(runtimePath, []byte(fixture), 0o755); err != nil {
		t.Fatalf("write runtime fixture: %v", err)
	}
	restoreLocator := SetRuntimeLocatorForTest(NewManifestRuntimeLocator(nativeDir, RuntimeManifest{
		Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: filepath.Base(runtimePath)}},
	}))
	defer restoreLocator()

	recorder := &recordingProcessSupervisor{delegate: DefaultProcessSupervisor{}}
	restoreSupervisor := SetProcessSupervisor(recorder)
	defer restoreSupervisor()

	envVar := &model.EnvVar{Name: "ANDROID_TASK_TOKEN", Value: "fixture-value", Enabled: true}
	if err := database.DB.Create(envVar).Error; err != nil {
		t.Fatalf("create task environment variable: %v", err)
	}
	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "worker.go")
	if err := os.WriteFile(scriptPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go task: %v", err)
	}
	task := &model.Task{
		Name:             "android go fixture",
		Command:          `task worker.go -- "value with spaces" "semi;colon"`,
		CronExpression:   "0 0 * * *",
		TaskType:         model.TaskTypeManual,
		Status:           model.TaskStatusEnabled,
		Timeout:          10,
		SuccessExitCodes: model.DefaultSuccessExitCodes,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	executor := NewTaskExecutor()
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TriggerType: TriggerTypeManual}
	if err := executor.OnTaskExecuting(req); err != nil {
		t.Fatalf("execute task: %v", err)
	}
	if !executor.Wait(10 * time.Second) {
		t.Fatal("timed out waiting for task execution")
	}

	recorder.mu.Lock()
	specs := append([]ProcessSpec{}, recorder.specs...)
	streams := append([]string{}, recorder.streams...)
	recorder.mu.Unlock()
	if len(specs) != 1 {
		t.Fatalf("expected one supervised process, got %d", len(specs))
	}
	wantArgv := []string{runtimePath, scriptPath, "value with spaces", "semi;colon"}
	if !reflect.DeepEqual(specs[0].Argv, wantArgv) {
		t.Fatalf("expected structured argv %#v, got %#v", wantArgv, specs[0].Argv)
	}
	if !containsString(specs[0].Env, "ANDROID_TASK_TOKEN=fixture-value") {
		t.Fatalf("expected task environment in process spec, got %#v", specs[0].Env)
	}
	if !containsString(streams, "stdout") || !containsString(streams, "stderr") {
		t.Fatalf("expected stdout and stderr process events, got %#v", streams)
	}

	var taskLog model.TaskLog
	if err := database.DB.First(&taskLog, req.TaskLogID).Error; err != nil {
		t.Fatalf("load task log: %v", err)
	}
	plain, err := TaskLogPlainContent(&taskLog)
	if err != nil {
		t.Fatalf("read task log detail: %v", err)
	}
	for _, expected := range []string{
		"stdout-env=fixture-value",
		"stdout-args=" + scriptPath + "|value with spaces|semi;colon",
		"stderr-env=fixture-value",
	} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("expected task log detail to contain %q, got %q", expected, plain)
		}
	}
	if taskLog.LogPath == nil {
		t.Fatal("expected persisted task log path")
	}
	fileContent, err := ReadLogFile(*taskLog.LogPath, config.C.Data.LogDir)
	if err != nil {
		t.Fatalf("read persisted task log file: %v", err)
	}
	if fileContent != plain {
		t.Fatalf("expected log detail and file content to match\ndetail: %q\nfile: %q", plain, fileContent)
	}
	fromCursor, nextCursor, err := ReadTaskLogFromCursor(&taskLog, 0)
	if err != nil {
		t.Fatalf("read task log for SSE: %v", err)
	}
	if fromCursor != plain || nextCursor != int64(len(plain)) || taskLog.LogCursor != nextCursor {
		t.Fatalf("unexpected SSE cursor result: content=%q next=%d persisted=%d", fromCursor, nextCursor, taskLog.LogCursor)
	}
}

func TestSupervisedRuntimeCommandPreservesLogTruncationSemantics(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	runtimePath := filepath.Join(root, "runtime.sh")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\nprintf '1234567890'\n"), 0o755); err != nil {
		t.Fatalf("write runtime fixture: %v", err)
	}
	restoreLocator := SetRuntimeLocatorForTest(NewManifestRuntimeLocator(root, RuntimeManifest{
		Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: filepath.Base(runtimePath)}},
	}))
	defer restoreLocator()

	plan := &CommandExecutionPlan{
		Interpreter: RuntimeIDYaegiGo,
		FullPath:    filepath.Join(root, "worker.go"),
		WorkDir:     root,
	}
	var streamed strings.Builder
	result, _, err := runSingleCommand(plan, 10, nil, 5, func(chunk string) {
		streamed.WriteString(chunk)
	})
	if err != nil {
		t.Fatalf("run supervised command: %v", err)
	}
	const expected = "12345\n[日志已截断，超过最大大小限制]"
	if !result.Truncated || result.Output != expected || streamed.String() != expected {
		t.Fatalf("unexpected truncation result: truncated=%v output=%q streamed=%q", result.Truncated, result.Output, streamed.String())
	}
}
