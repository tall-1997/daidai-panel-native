package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这里钉的是执行窗口的「起点」：OnTaskExecuting 把状态写成运行中之后、runTask 登记进程之前，
// 中间还有建日志、建实时日志、runTask 开头的环境准备、前置钩子（task_before / task_before.sh）。
// 停止落在这一整段里的任何位置，StopTask 都必须认出这次执行、主进程都不得再启动。
// 真机上前置钩子要走 bash，Windows 上无法确定性地卡在钩子里；这里按生产顺序
// OnTaskExecuting → StopTask → RunTask 直接走一遍，覆盖的正是这段窗口里最早的那一截。

func writeProbeScript(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(config.C.Data.ScriptsDir, "probe.js"), []byte("console.log('probe')\n"), 0o644); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
}

func TestStopAfterPrepareBeforeRunTaskNeverLaunchesProcess(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeProbeScript(t)

	task, req, _ := newRunningTaskReq(t, "停止早于子进程启动", nil)
	req.CommandPlan = nil // 交给 OnTaskExecuting 按真实命令解析

	called := false
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		called = true
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	if err := executor.OnTaskExecuting(req); err != nil {
		t.Fatalf("OnTaskExecuting: %v", err)
	}
	if !executor.StopTask(task.ID) {
		t.Fatal("状态已写成运行中、还没进 runTask：StopTask 必须认出这次执行并返回 true")
	}

	executor.RunTask(req)

	if called {
		t.Fatal("停止请求早于主进程启动，主进程绝不应被启动")
	}
	stored := reloadServiceTask(t, task.ID)
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("被停止的执行应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
	mustAbortedLog(t, task.ID)
	if executor.StopTask(task.ID) {
		t.Fatal("结算完成后执行窗口必须已关闭，StopTask 应返回 false")
	}
}

// 准备阶段打开的执行窗口，在准备失败（OnTaskFailed）时必须关掉，不能只增不减：
// 否则之后对这个空闲任务点停止会被当成「正在执行」，还会把停止请求挂到下一次运行上。
func TestPreparedWindowClosedWhenRunFailsBeforeRunTask(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeProbeScript(t)

	task, req, _ := newRunningTaskReq(t, "准备后失败不留执行窗口", nil)
	req.CommandPlan = nil

	executor := NewTaskExecutor()
	if err := executor.OnTaskExecuting(req); err != nil {
		t.Fatalf("OnTaskExecuting: %v", err)
	}
	executor.OnTaskFailed(req, errors.New("任务调度阶段异常: boom"))

	if executor.StopTask(task.ID) {
		t.Fatal("准备后失败的执行已经结算，执行窗口必须已关闭，StopTask 应返回 false")
	}
}
