package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这一组端到端用例锁的是「停止落在执行窗口里」的竞态：runTask 已经把状态写成运行中、
// 进程还没登记（onStart 之前）时点停止。修复前 StopTask 找不到已登记进程直接返回 false、
// 什么也不做，脚本照常跑完、被结算成成功；修复后 StopTask 记下停止请求并返回 true，
// onStart 一登记就杀、重试循环见到就跳出，本次执行按手动停止结算为已终止。
//
// 走的是真实执行链路：子进程入口换成桩（runCommandWithPlanFunc），不依赖本机装没装 node/bash。
// 关键点：现网共享桩 stubRunCommandWithPlan 压根不回调 onStart，所以「桩阻塞中」= 进程未登记，
// 正好就是这段竞态窗口。用例只依赖修复前就存在的符号，因此能在修复前后各跑一次，看红→绿。

// TestExecutorStopHelperProcess 是「可被杀掉的真实子进程」：父测试用它验证 onStart 真的 kill 了进程。
// 没有 DDP_STOP_HELPER=1 时立即返回，当作一条空跑用例，不影响正常 go test。
func TestExecutorStopHelperProcess(t *testing.T) {
	if os.Getenv("DDP_STOP_HELPER") != "1" {
		return
	}
	// 睡足够久：正常应被父测试 kill；没被 kill 时才靠这个自退时长兜底，避免留孤儿。
	time.Sleep(5 * time.Second)
	os.Exit(0)
}

// startKillableHelper 拉起一个真实的、会阻塞的子进程，返回其 *os.Process 供 onStart 登记 / kill。
func startKillableHelper(t *testing.T) *os.Process {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecutorStopHelperProcess$")
	cmd.Env = append(os.Environ(), "DDP_STOP_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start killable helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process
}

// newRunningTaskReq 造一条「已经处于运行中」的手动任务 + 运行中日志 + 执行请求，
// 直接喂给 executor.runTask（与 task_executor_script_token_test.go 同款做法）。
func newRunningTaskReq(t *testing.T, name string, mutate func(*model.Task)) (*model.Task, *ExecutionRequest, *model.TaskLog) {
	t.Helper()

	task := &model.Task{
		Name:     name,
		Command:  "node probe.js",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusRunning,
	}
	if mutate != nil {
		mutate(task)
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	runningStatus := model.LogStatusRunning
	taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	plan := &CommandExecutionPlan{
		Interpreter: "node",
		FullPath:    filepath.Join(config.C.Data.ScriptsDir, "probe.js"),
		Mode:        commandModeNormal,
	}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan}
	return task, req, taskLog
}

func waitChan(t *testing.T, ch <-chan struct{}, onFail func(), msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		if onFail != nil {
			onFail()
		}
		t.Fatal(msg)
	}
}

func mustAbortedLog(t *testing.T, taskID uint) {
	t.Helper()
	var logRow model.TaskLog
	if err := database.DB.Where("task_id = ?", taskID).Order("id DESC").First(&logRow).Error; err != nil {
		t.Fatalf("reload task log: %v", err)
	}
	if logRow.Status == nil || *logRow.Status != model.LogStatusAborted {
		t.Fatalf("expected log status Aborted(%d), got %v", model.LogStatusAborted, logRow.Status)
	}
}

// ① 停止发生在进程登记之前：桩先阻塞在 channel 上（进程未登记），测试调 StopTask 后放行，
// 断言 onStart 登记进程后立刻把它杀掉、结算为已终止、绝不记成成功。
func TestStopBeforeProcessRegistrationKillsAndSettlesAborted(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "停止早于进程登记", nil)
	helper := startKillableHelper(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	var waitDur time.Duration

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		close(reached) // 已进入执行、还没登记进程
		<-release      // 等测试先调 StopTask
		for _, start := range onProcessStart {
			start(helper) // 登记进程；修复后 onStart 应立刻杀掉 helper
		}
		began := time.Now()
		state, _ := helper.Wait() // 被杀就立刻返回；没被杀则要等 helper 自己退出（5s）
		waitDur = time.Since(began)
		code := 0
		if state != nil {
			code = state.ExitCode()
		}
		return &ScriptResult{ReturnCode: code}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, reached, func() { close(release) }, "子进程桩迟迟没有被调用")

	if executor.HasRunningProcess(task.ID) {
		close(release)
		t.Fatal("onStart 还没被调用，此刻不应有已登记进程")
	}
	if !executor.StopTask(task.ID) {
		close(release)
		t.Fatal("停止落在执行窗口内，StopTask 必须返回 true")
	}
	close(release)

	waitChan(t, done, nil, "runTask 迟迟没有结算完成")

	stored := reloadServiceTask(t, task.ID)
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("被停止的执行应结算为已终止(%d)，实际 %v；绝不能记成成功", model.RunAborted, stored.LastRunStatus)
	}
	mustAbortedLog(t, task.ID)
	if waitDur > 2*time.Second {
		t.Fatalf("onStart 应立刻杀掉进程，helper.Wait 却等了 %s（说明没被及时杀）", waitDur)
	}
}

// ② 停止落在执行窗口内后，重试循环不得再启动新进程（前置钩子阶段共用同一停止守卫）。
// 桩每次都返回失败，若没有停止守卫会一直重试到 MaxRetries；断言停止后只启动过一次、结算为已终止。
func TestStopWithinExecutingWindowSkipsRemainingRetries(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "停止后不再重试", func(tk *model.Task) {
		tk.MaxRetries = 3
		tk.RetryInterval = 0
	})

	reached := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(reached)
			<-release
		}
		// 一直返回失败：有停止守卫时会被跳出，没有守卫时会重试到 MaxRetries。
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, reached, func() { close(release) }, "子进程桩迟迟没有被调用")
	if !executor.StopTask(task.ID) {
		close(release)
		t.Fatal("停止落在执行窗口内，StopTask 必须返回 true")
	}
	close(release)
	waitChan(t, done, nil, "runTask 迟迟没有结算完成")

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("停止后不应再启动新进程，子进程入口却被调用了 %d 次", got)
	}
	stored := reloadServiceTask(t, task.ID)
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("被停止的执行应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
}

// ③ 结算后停止请求 / 执行标记必须清干净：同一 executor、同一任务 id 再跑一次普通任务，
// 断言这次不受上次停止影响、正常成功。
func TestExecutingWindowStopDoesNotLeakIntoNextRun(t *testing.T) {
	testutil.SetupTestEnv(t)

	executor := NewTaskExecutor()

	// 第一次：在执行窗口内被停止，结算为已终止。
	task, req, taskLog := newRunningTaskReq(t, "先被停止再复跑", nil)
	helper := startKillableHelper(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		close(reached)
		<-release
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()
	waitChan(t, reached, func() { close(release) }, "第一次执行的子进程桩迟迟没有被调用")
	if !executor.StopTask(task.ID) {
		close(release)
		t.Fatal("第一次停止应命中执行窗口，StopTask 必须返回 true")
	}
	close(release)
	waitChan(t, done, nil, "第一次执行迟迟没有结算完成")

	first := reloadServiceTask(t, task.ID)
	if first.LastRunStatus == nil || *first.LastRunStatus != model.RunAborted {
		t.Fatalf("第一次应结算为已终止(%d)，实际 %v", model.RunAborted, first.LastRunStatus)
	}

	// 第二次：同一 executor、同一任务 id，普通成功执行；不应残留上次的停止请求 / 执行标记。
	first.Status = model.TaskStatusRunning
	database.DB.Model(&first).Update("status", model.TaskStatusRunning)
	runningStatus := model.LogStatusRunning
	secondLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(secondLog).Error; err != nil {
		t.Fatalf("create second task log: %v", err)
	}
	plan := &CommandExecutionPlan{Interpreter: "node", FullPath: filepath.Join(config.C.Data.ScriptsDir, "probe.js"), Mode: commandModeNormal}
	secondReq := &ExecutionRequest{TaskID: task.ID, Task: &first, TaskLogID: secondLog.ID, CommandPlan: plan}

	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor.runTask(secondReq, secondLog, nil)

	second := reloadServiceTask(t, task.ID)
	if second.LastRunStatus == nil || *second.LastRunStatus != model.RunSuccess {
		t.Fatalf("复跑不应受上次停止影响，应成功(%d)，实际 %v", model.RunSuccess, second.LastRunStatus)
	}
}

// ④ 回归：已登记进程的停止路径行为不变。桩这次真的回调 onStart 登记进程并阻塞，
// 测试等到进程登记后再调 StopTask，断言进程被杀、结算为已终止。这条在修复前后都应当通过。
func TestStopRegisteredProcessRegressionUnchanged(t *testing.T) {
	testutil.SetupTestEnv(t)

	task, req, taskLog := newRunningTaskReq(t, "已登记进程停止回归", nil)
	helper := startKillableHelper(t)

	registered := make(chan struct{})
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		for _, start := range onProcessStart {
			start(helper) // 真正登记进程，走已登记进程停止路径
		}
		close(registered)
		state, _ := helper.Wait() // 被 StopTask 杀掉后返回
		code := 0
		if state != nil {
			code = state.ExitCode()
		}
		return &ScriptResult{ReturnCode: code}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, nil)
	}()

	waitChan(t, registered, nil, "进程迟迟没有被登记")
	if !executor.HasRunningProcess(task.ID) {
		t.Fatal("onStart 已回调，进程应已登记")
	}
	if !executor.StopTask(task.ID) {
		t.Fatal("已登记进程的 StopTask 必须返回 true")
	}

	waitChan(t, done, nil, "runTask 迟迟没有结算完成")

	stored := reloadServiceTask(t, task.ID)
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("已登记进程被停止应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
	mustAbortedLog(t, task.ID)
}
