package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这一组是对「停止落在进程登记之前」修复的对抗复查用例（与 manual_stop_executing_window_test.go 同一套桩法）。
// 每条锁一个修复后仍然存在的缝：已登记进程被停后重试照跑、重试等待期间停止不生效、
// 多实例下停止误伤后来的实例 / 同批被停的实例结算不一致、关机不拦执行窗口、残留停止标记串到下一次运行。
// 用例只用修复前就存在的符号，能在修复前后各跑一次看红→绿。

// envWithoutStopHelper 返回去掉 DDP_STOP_HELPER 的环境，让 helper 用例立即返回、子进程马上退出。
func envWithoutStopHelper() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DDP_STOP_HELPER=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// logStatusOf 读回指定日志行的状态。多实例用例同一任务有多条日志，必须按日志 id 各查各的。
func logStatusOf(t *testing.T, logID uint) int {
	t.Helper()
	var row model.TaskLog
	if err := database.DB.First(&row, logID).Error; err != nil {
		t.Fatalf("reload task log %d: %v", logID, err)
	}
	if row.Status == nil {
		t.Fatalf("task log %d has nil status", logID)
	}
	return *row.Status
}

// newExtraRunReq 给同一个任务再造一次执行（独立日志行 + 独立请求），模拟 allow_multiple_instances 下的第二个实例。
func newExtraRunReq(t *testing.T, task *model.Task) (*ExecutionRequest, *model.TaskLog) {
	t.Helper()
	runningStatus := model.LogStatusRunning
	taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create extra task log: %v", err)
	}
	snapshot := *task
	plan := &CommandExecutionPlan{
		Interpreter: "node",
		FullPath:    filepath.Join(config.C.Data.ScriptsDir, "probe.js"),
		Mode:        commandModeNormal,
	}
	return &ExecutionRequest{TaskID: task.ID, Task: &snapshot, TaskLogID: taskLog.ID, CommandPlan: plan}, taskLog
}

// onceCloser 让「放行」通道在断言失败提前退出时也能被 Cleanup 关掉，不留阻塞的 goroutine。
type onceCloser struct {
	ch   chan struct{}
	once sync.Once
}

func newOnceCloser(t *testing.T) *onceCloser {
	c := &onceCloser{ch: make(chan struct{})}
	t.Cleanup(c.close)
	return c
}

func (c *onceCloser) close() { c.once.Do(func() { close(c.ch) }) }

// A. 已登记进程被停止后，重试循环不得再启动新进程。
// 修复前后 StopTask 走「已登记进程」分支时都只杀进程、不置停止请求，被杀的这一轮算失败后照常重试到 MaxRetries。
func TestStopRegisteredProcessEndsRetryLoop(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "已登记进程停止后不再重试", func(tk *model.Task) {
		tk.MaxRetries = 2
		tk.RetryInterval = 0
	})
	helper := startKillableHelper(t)

	registered := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			return &ScriptResult{ReturnCode: 1}, nil, nil
		}
		for _, start := range onProcessStart {
			start(helper)
		}
		close(registered)
		_, _ = helper.Wait()
		// 固定按失败返回：没有停止守卫时这里一定会触发重试，用例才有区分度。
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, registered, nil, "进程迟迟没有被登记")
	if !executor.StopTask(task.ID) {
		t.Fatal("已登记进程的 StopTask 必须返回 true")
	}
	waitChan(t, done, nil, "runTask 迟迟没有结算完成")

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("停止后不应再启动新进程（重试），子进程入口却被调用了 %d 次", got)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("被停止的执行应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
	mustAbortedLog(t, task.ID)
}

// B. 停止落在重试等待期间：上一轮进程已经结束却还留在进程表里，StopTask 走「已登记进程」分支。
// 修复前后都不置停止请求，等待结束后照常启动下一轮；这里还要求等待被打断，不必白等完整个重试间隔。
func TestStopDuringRetryWaitCancelsPendingRetry(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "重试等待中停止", func(tk *model.Task) {
		tk.MaxRetries = 1
		tk.RetryInterval = 3
	})

	firstDone := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			return &ScriptResult{ReturnCode: 1}, nil, nil
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestExecutorStopHelperProcess$")
		cmd.Env = envWithoutStopHelper()
		if err := cmd.Start(); err != nil {
			t.Errorf("start quick helper: %v", err)
			close(firstDone)
			return &ScriptResult{ReturnCode: 1}, nil, nil
		}
		for _, start := range onProcessStart {
			start(cmd.Process) // 登记后它很快自己退出，但会一直留在进程表里直到结算
		}
		_ = cmd.Wait()
		close(firstDone)
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, firstDone, nil, "第一轮迟迟没有结束")
	stopAt := time.Now()
	if !executor.StopTask(task.ID) {
		t.Fatal("重试等待期间任务仍在执行，StopTask 必须返回 true")
	}
	waitChan(t, done, nil, "runTask 迟迟没有结算完成")
	settleDelay := time.Since(stopAt)

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("重试等待期间被停止，不应再启动下一轮，子进程入口却被调用了 %d 次", got)
	}
	if settleDelay > 2*time.Second {
		t.Fatalf("停止应打断重试等待，结算却在停止后 %s 才完成（重试间隔 3 秒）", settleDelay)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("被停止的执行应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
}

// C. 多实例：停止只作用于「停止那一刻正在执行」的实例。停止之后才开始的新实例必须照常执行、照常结算，
// 被停的实例必须结算为已终止。修复后的停止请求按任务 id 挂着、直到所有实例都结束才清，
// 于是后来的实例一进循环就被当成已停止跳出，还会抢走那笔按任务 id 的手动停止标记。
func TestStopDoesNotLeakIntoLaterConcurrentInstance(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, reqA, logA := newRunningTaskReq(t, "多实例：停止不误伤后来的实例", func(tk *model.Task) {
		tk.AllowMultipleInstances = true
	})
	reqB, logB := newExtraRunReq(t, task)
	helper := startKillableHelper(t)

	reachedA := make(chan struct{})
	releaseA := newOnceCloser(t)
	var mu sync.Mutex
	calls := 0
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			return &ScriptResult{ReturnCode: 0}, nil, nil // 实例 B：正常成功
		}
		close(reachedA) // 实例 A：进入执行、进程还没登记
		<-releaseA.ch
		for _, start := range onProcessStart {
			start(helper)
		}
		state, _ := helper.Wait()
		code := 0
		if state != nil {
			code = state.ExitCode()
		}
		return &ScriptResult{ReturnCode: code}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		executor.runTask(reqA, logA, nil)
	}()

	waitChan(t, reachedA, releaseA.close, "实例 A 的子进程桩迟迟没有被调用")
	if !executor.StopTask(task.ID) {
		releaseA.close()
		t.Fatal("实例 A 在执行窗口内，StopTask 必须返回 true")
	}

	// 停止之后才开始的实例 B：A 还卡在窗口里。
	executor.runTask(reqB, logB, nil)

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Errorf("停止之后才开始的实例 B 必须照常启动进程，子进程入口却只被调用了 %d 次", gotCalls)
	}
	if got := logStatusOf(t, logB.ID); got != model.LogStatusSuccess {
		t.Errorf("实例 B 不受停止影响，应结算为成功(%d)，实际 %d", model.LogStatusSuccess, got)
	}

	releaseA.close()
	waitChan(t, doneA, nil, "实例 A 迟迟没有结算完成")
	if got := logStatusOf(t, logA.ID); got != model.LogStatusAborted {
		t.Errorf("被停止的实例 A 应结算为已终止(%d)，实际 %d", model.LogStatusAborted, got)
	}
}

// D. 多实例：一次停止同时命中两个正在执行的实例，两个都必须结算为已终止。
// 按任务 id 的手动停止标记只有一笔，先结算的那个读即清，后结算的那个就被记成普通失败。
func TestStopHittingTwoInstancesSettlesBothAborted(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, reqA, logA := newRunningTaskReq(t, "多实例：同批被停都记已终止", func(tk *model.Task) {
		tk.AllowMultipleInstances = true
	})
	reqB, logB := newExtraRunReq(t, task)
	helpers := []*os.Process{startKillableHelper(t), startKillableHelper(t)}

	reached := make(chan struct{}, 2)
	release := newOnceCloser(t)
	var mu sync.Mutex
	calls := 0
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 2 {
			return &ScriptResult{ReturnCode: 1}, nil, nil
		}
		helper := helpers[n-1]
		reached <- struct{}{}
		<-release.ch
		for _, start := range onProcessStart {
			start(helper)
		}
		state, _ := helper.Wait()
		code := 0
		if state != nil {
			code = state.ExitCode()
		}
		return &ScriptResult{ReturnCode: code}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	var wg sync.WaitGroup
	for _, pair := range []struct {
		req *ExecutionRequest
		log *model.TaskLog
	}{{reqA, logA}, {reqB, logB}} {
		pair := pair
		wg.Add(1)
		go func() {
			defer wg.Done()
			executor.runTask(pair.req, pair.log, nil)
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-reached:
		case <-time.After(15 * time.Second):
			release.close()
			t.Fatal("两个实例迟迟没有都进入执行")
		}
	}
	if !executor.StopTask(task.ID) {
		release.close()
		t.Fatal("两个实例都在执行窗口内，StopTask 必须返回 true")
	}
	release.close()

	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()
	waitChan(t, allDone, nil, "两个实例迟迟没有都结算完成")

	for name, logRow := range map[string]*model.TaskLog{"A": logA, "B": logB} {
		if got := logStatusOf(t, logRow.ID); got != model.LogStatusAborted {
			t.Errorf("实例 %s 被同一次停止命中，应结算为已终止(%d)，实际 %d", name, model.LogStatusAborted, got)
		}
	}
}

// E. 面板关闭 / 重启：StopAllRunningTasks 只杀已登记进程。落在执行窗口里的执行随后照样启动进程，
// 被杀的执行照样按失败重试、再起新进程——面板退出后这些子进程（独立进程组）会变成孤儿继续跑。
// 关机中断不是手动停止：结算口径保持失败，不能被记成已终止。
func TestStopAllRunningTasksHaltsExecutingWindowAndRetries(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "关机中断执行窗口", func(tk *model.Task) {
		tk.MaxRetries = 2
		tk.RetryInterval = 0
	})
	helper := startKillableHelper(t)

	reached := make(chan struct{})
	release := newOnceCloser(t)
	var mu sync.Mutex
	calls := 0
	var waitDur time.Duration
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			return &ScriptResult{ReturnCode: 1}, nil, nil
		}
		close(reached)
		<-release.ch
		for _, start := range onProcessStart {
			start(helper)
		}
		began := time.Now()
		_, _ = helper.Wait()
		mu.Lock()
		waitDur = time.Since(began)
		mu.Unlock()
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, reached, release.close, "子进程桩迟迟没有被调用")
	executor.StopAllRunningTasks()
	release.close()
	waitChan(t, done, nil, "runTask 迟迟没有结算完成")

	mu.Lock()
	gotCalls, gotWait := calls, waitDur
	mu.Unlock()
	if gotWait > 2*time.Second {
		t.Errorf("关机后才登记的进程应被立刻杀掉，helper.Wait 却等了 %s", gotWait)
	}
	if gotCalls != 1 {
		t.Errorf("关机中断后不应再重试启动新进程，子进程入口却被调用了 %d 次", gotCalls)
	}
	stored := reloadServiceTask(t, task.ID)
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Errorf("关机中断按失败结算(%d)，不是手动停止，实际 %v", model.RunFailed, stored.LastRunStatus)
	}
}

// F. 没有任何执行在跑时落下的手动停止标记（例如网页停止一个由 `ddp task run` 在另一个进程里跑起来的任务：
// 面板执行器里没有这次执行，handler 按库里的 PID 兜底时照样 MarkManualStop），不得串到下一次运行。
// endExecuting 的兜底只清「执行窗口还开着时」落下的标记，这笔清不到，下一次正常执行会被记成已终止、成功通知也被吞掉。
func TestStaleManualStopMarkDoesNotAbortNextRun(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "残留停止标记不串到下一次", nil)
	executor := NewTaskExecutor()
	if executor.StopTask(task.ID) {
		t.Fatal("没有任何执行时 StopTask 应返回 false")
	}
	MarkManualStop(task.ID) // handler 的 PID 兜底路径：目标进程不归这个执行器管
	t.Cleanup(func() { consumeManualStop(task.ID) })

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor.runTask(req, taskLog, nil)

	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunSuccess {
		t.Fatalf("这次执行没有被停止过，应结算为成功(%d)，实际 %v", model.RunSuccess, stored.LastRunStatus)
	}
	if got := logStatusOf(t, taskLog.ID); got != model.LogStatusSuccess {
		t.Fatalf("日志应结算为成功(%d)，实际 %d", model.LogStatusSuccess, got)
	}
}

// G. 执行器因停止 / 关机打进任务日志的提示行也是面板元信息行，必须登记到 panelMetaLinePrefixes（契约见该变量注释）。
func TestExecutorStopNoticeLinesAreRegisteredPanelMeta(t *testing.T) {
	for _, line := range []string{
		"[任务已被手动停止，取消后续执行]",
		"[任务已被手动停止，终止刚启动的进程]",
		"[面板正在关闭，取消后续执行]",
		"[面板正在关闭，终止刚启动的进程]",
	} {
		if !isPanelMetaLine(line) {
			t.Errorf("面板提示行 %q 没有登记到 panelMetaLinePrefixes", line)
		}
	}
}
