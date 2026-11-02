package service

import (
	"os"
	"sync"
	"testing"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这两条钉的是对抗复查里补上的两处：
//   - 结算只摘自己这次登记的进程（原来是 delete(runningProcesses, taskID) 整条删，
//     多实例下先结算的那次会把另一次还在跑的进程一起抹掉，之后对这个任务点停止就找不到进程可杀）；
//   - 执行窗口在任何结束路径上都必须关掉，崩溃也不例外（窗口只增不减的话，空闲任务会被永远当成正在执行）。
//
// 桩法与同目录 manual_stop_executing_window*_test.go 一致，复用那边的 helper。

// TestSettleOnlyReleasesItsOwnProcesses：多实例下先结算的那次执行不得摘掉另一次的进程。
func TestSettleOnlyReleasesItsOwnProcesses(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, reqA, logA := newRunningTaskReq(t, "多实例：结算只摘自己的进程", func(tk *model.Task) {
		tk.AllowMultipleInstances = true
	})
	reqB, logB := newExtraRunReq(t, task)

	helpers := []*os.Process{startKillableHelper(t), startKillableHelper(t)}
	releases := []*onceCloser{newOnceCloser(t), newOnceCloser(t)}
	reached := []chan struct{}{make(chan struct{}), make(chan struct{})}

	var mu sync.Mutex
	calls := 0
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 2 {
			return &ScriptResult{ReturnCode: 0}, nil, nil
		}
		for _, start := range onProcessStart {
			start(helpers[n-1]) // 真正登记进程
		}
		close(reached[n-1])
		<-releases[n-1].ch
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	// 先起 A、等它登记完再起 B，这样桩里的 n 与 A/B 一一对应。
	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		executor.runTask(reqA, logA, nil)
	}()
	waitChan(t, reached[0], releases[0].close, "实例 A 迟迟没有登记进程")

	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		executor.runTask(reqB, logB, nil)
	}()
	waitChan(t, reached[1], releases[1].close, "实例 B 迟迟没有登记进程")

	// A 先跑完结算：它只该摘掉自己那个 pid。
	releases[0].close()
	waitChan(t, doneA, releases[1].close, "实例 A 迟迟没有结算完成")

	if !executor.HasRunningProcess(task.ID) {
		releases[1].close()
		t.Fatal("实例 A 结算只应摘掉自己登记的进程，实例 B 的进程必须还登记着；整条删的话之后对这个任务点停止就没进程可杀了")
	}
	if !executor.StopTask(task.ID) {
		releases[1].close()
		t.Fatal("实例 B 仍在执行，StopTask 必须返回 true")
	}

	releases[1].close()
	waitChan(t, doneB, nil, "实例 B 迟迟没有结算完成")

	if got := logStatusOf(t, logA.ID); got != model.LogStatusSuccess {
		t.Errorf("实例 A 没有被停止过，应结算为成功(%d)，实际 %d", model.LogStatusSuccess, got)
	}
	if got := logStatusOf(t, logB.ID); got != model.LogStatusAborted {
		t.Errorf("实例 B 被停止，应结算为已终止(%d)，实际 %d", model.LogStatusAborted, got)
	}
}

// TestPanicDuringRunClosesExecutingWindow：执行中崩溃同样要关掉执行窗口，
// 否则这个任务会一直被当成「正在执行」——对它点停止永远返回 true，停止请求还会挂到下一次运行上。
func TestPanicDuringRunClosesExecutingWindow(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "崩溃不留执行窗口", nil)

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		panic("boom inside task execution")
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	executor.runTask(req, taskLog, nil) // runTask 内部有 recover，崩溃不会抛出来

	if executor.StopTask(task.ID) {
		t.Fatal("崩溃结算之后执行窗口必须已关闭，StopTask 应返回 false")
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("崩溃应结算为失败(%d)，实际 %v", model.RunFailed, stored.LastRunStatus)
	}
}
