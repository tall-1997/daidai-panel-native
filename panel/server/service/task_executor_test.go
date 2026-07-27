package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 回归：v2.2.18 在 task_executor.runTask 中构建任务运行环境时，
// 把 workDir 错传成了 scriptsDir，导致实际脚本所在目录没有进入 PYTHONPATH 前缀。
// 这会让脚本真实运行环境与缺依赖检测/自动安装重试环境脱节，出现“自动安装失效”的表象。
// 这里守卫：任务环境必须优先使用 plan.FullPath 的父目录作为 workDir。
func TestTaskRuntimeEnvUsesPlannedScriptDirectory(t *testing.T) {
	testutil.SetupTestEnv(t)

	taskDir := filepath.Join(config.C.Data.ScriptsDir, "nested", "feature")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(
		taskDir,
		config.C.Data.ScriptsDir,
		nil,
		2*time.Hour,
		"3.11",
	)
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	pythonPath := envMap["PYTHONPATH"]
	if pythonPath == "" {
		t.Fatal("expected PYTHONPATH to be populated")
	}

	parts := strings.Split(pythonPath, string(filepath.ListSeparator))
	if len(parts) == 0 {
		t.Fatalf("expected python path parts, got %q", pythonPath)
	}
	if parts[0] != taskDir {
		t.Fatalf("expected first PYTHONPATH entry to be task dir %q, got %q (all=%v)", taskDir, parts[0], parts)
	}
	if len(parts) < 2 || parts[1] != config.C.Data.ScriptsDir {
		t.Fatalf("expected second PYTHONPATH entry to be scripts dir %q, got %v", config.C.Data.ScriptsDir, parts)
	}
}

func TestTaskExecutorUsesPlannedScriptDirectoryForRuntimeEnv(t *testing.T) {
	testutil.SetupTestEnv(t)

	taskDir := filepath.Join(config.C.Data.ScriptsDir, "jobs", "demo")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	scriptPath := filepath.Join(taskDir, "sample.py")
	if err := os.WriteFile(scriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	task := &model.Task{
		PythonVersion: "3.10",
		Timeout:       30,
	}
	plan := &CommandExecutionPlan{
		Interpreter: "python3.10",
		FullPath:    scriptPath,
	}

	taskWorkDir := config.C.Data.ScriptsDir
	if plan != nil && strings.TrimSpace(plan.FullPath) != "" {
		taskWorkDir = filepath.Dir(plan.FullPath)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(taskWorkDir, config.C.Data.ScriptsDir, task.NotificationChannelID, time.Duration(task.Timeout)*time.Second+time.Hour, task.PythonVersion)
	if err != nil {
		t.Fatalf("build runtime env: %v", err)
	}

	if got := envMap["DAIDAI_PYTHON_VERSION"]; got != "3.10" {
		t.Fatalf("expected DAIDAI_PYTHON_VERSION=3.10, got %q", got)
	}

	parts := strings.Split(envMap["PYTHONPATH"], string(filepath.ListSeparator))
	if len(parts) == 0 || parts[0] != taskDir {
		t.Fatalf("expected task dir %q to lead PYTHONPATH, got %v", taskDir, parts)
	}
	if len(parts) < 2 || parts[1] != config.C.Data.ScriptsDir {
		t.Fatalf("expected scripts dir %q as second PYTHONPATH entry, got %v", config.C.Data.ScriptsDir, parts)
	}
}
