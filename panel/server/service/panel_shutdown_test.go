package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/robfig/cron/v3"
)

// 这一组锁的是面板关停（server/main.go 的 shutdownPanel）落在 service 包的契约：
// 关停有上限、被关停打断的执行怎么结算（D15）、前置钩子随关停终止、两个 cron 调度器不无限等、
// 启动时清理上次留下的任务临时文件。

// hookMarkersCheckable 判断这台机器能不能真跑钩子脚本去验「标记文件写没写」。
// Windows 上的 Git Bash 对路径的处理与 Linux 不同（见 requireUsableBash），只在 Linux 上验标记文件；
// 「[执行后置脚本]」这类提示行在哪都验。
func hookMarkersCheckable() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := exec.LookPath("bash")
	return err == nil
}

// newFailureNotifyProbe 建一个指向本地 httptest 服务的 webhook 渠道，返回渠道 id 与收到的请求数。
func newFailureNotifyProbe(t *testing.T) (uint, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	channel := &model.NotifyChannel{
		Name:    "关停通知探针",
		Type:    "webhook",
		Config:  fmt.Sprintf(`{"url":%q}`, server.URL),
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notify channel: %v", err)
	}
	return channel.ID, hits
}

// writePostHookMarkers 配好三种后置脚本（任务后置脚本、全局 task_after.sh、extra.sh），各自往脚本目录写一个标记文件。
func writePostHookMarkers(t *testing.T, task *model.Task) []string {
	t.Helper()
	scriptsDir := config.C.Data.ScriptsDir
	after := "echo after > task-after.flag\n"
	task.TaskAfter = &after
	if err := database.DB.Model(task).Update("task_after", after).Error; err != nil {
		t.Fatalf("save task_after: %v", err)
	}
	for _, hook := range []struct{ name, flag string }{
		{"task_after.sh", "global-after.flag"},
		{"extra.sh", "extra.flag"},
	} {
		content := fmt.Sprintf("echo after > %s\n", hook.flag)
		if err := os.WriteFile(filepath.Join(scriptsDir, hook.name), []byte(content), 0o755); err != nil {
			t.Fatalf("write %s: %v", hook.name, err)
		}
	}
	return []string{
		filepath.Join(scriptsDir, "task-after.flag"),
		filepath.Join(scriptsDir, "global-after.flag"),
		filepath.Join(scriptsDir, "extra.flag"),
	}
}

func readSettledLogContent(t *testing.T, logID uint) string {
	t.Helper()
	var row model.TaskLog
	if err := database.DB.First(&row, logID).Error; err != nil {
		t.Fatalf("reload task log %d: %v", logID, err)
	}
	content, err := DecompressFromBase64(row.Content)
	if err != nil {
		t.Fatalf("decompress task log %d: %v", logID, err)
	}
	return content
}

// 研究 §6.1-3：关停有硬上限。runTask 卡住（模拟逃出进程组、攥着输出管道的孙进程：进程不登记、不理停止请求）时，
// ShutdownSchedulerV2 也必须在约 3 秒内返回（#159 起结算等待从 4 秒缩到 3 秒，TERM 宽限挪到了 HaltSchedulerV2 里；
// 这里没有登记进程，不受 TERM 宽限影响）—— worker 与执行两段等待共用一个截止时间，剩余时间算成 0 也不能变成无限等。
// 截止时还没结算的执行：库里由 MarkActiveTasksInterrupted 标成中断，注入脚本的凭据被吊销
// （结算里那句吊销等不到了，泄漏在临时目录里的凭据 7 天有效、重启后照样能用）。
func TestShutdownSchedulerV2IsBoundedWhenRunIsStuck(t *testing.T) {
	testutil.SetupTestEnv(t)

	oldScheduler, oldExecutor := globalScheduler.Load(), globalExecutor.Load()
	t.Cleanup(func() {
		globalScheduler.Store(oldScheduler)
		globalExecutor.Store(oldExecutor)
	})

	if err := os.WriteFile(filepath.Join(config.C.Data.ScriptsDir, "probe.js"), []byte("console.log('probe')\n"), 0o644); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
	task := &model.Task{Name: "关停时卡住的执行", Command: "node probe.js", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	reached := make(chan struct{})
	release := newOnceCloser(t)
	var reachOnce sync.Once
	var scriptEnv map[string]string
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		reachOnce.Do(func() {
			scriptEnv = copyScriptEnv(envVars)
			close(reached)
		})
		<-release.ch
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	scheduler := NewSchedulerV2(SchedulerConfig{WorkerCount: 1, QueueSize: 8, RateInterval: time.Millisecond}, executor)
	globalScheduler.Store(scheduler)
	globalExecutor.Store(executor)
	scheduler.Start()
	if err := scheduler.RunNow(task.ID); err != nil {
		t.Fatalf("run task: %v", err)
	}
	waitChan(t, reached, release.close, "任务迟迟没有进入执行")
	claims := mustParseScriptToken(t, scriptEnv)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		ShutdownSchedulerV2()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		release.close()
		t.Fatal("ShutdownSchedulerV2 never returned while a run was stuck; the shared deadline must bound both waits")
	}
	// 上界取 5 秒：等待本身共用 3 秒的截止时间，截止后还要写几次库（吊销凭据、标中断）。
	// 两段各等 3 秒（共 6 秒）或剩余时间算成 0 变成无限等，都超过这个上界。
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ShutdownSchedulerV2 must return within about 3s when a run is stuck, took %s", elapsed)
	}

	stored := reloadServiceTask(t, task.ID)
	if stored.Status == model.TaskStatusRunning || stored.Status == model.TaskStatusQueued {
		t.Fatalf("stuck run must be marked interrupted (task left active, status %v)", stored.Status)
	}
	if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("interrupted run must be recorded as failed(%d), got %v", model.RunFailed, stored.LastRunStatus)
	}
	var logRow model.TaskLog
	if err := database.DB.Where("task_id = ?", task.ID).Order("id DESC").First(&logRow).Error; err != nil {
		t.Fatalf("reload task log: %v", err)
	}
	if !strings.Contains(logRow.Content, "面板正在关闭或重启") {
		t.Fatalf("interrupted run log must explain the shutdown, got %q", logRow.Content)
	}
	var blocked model.TokenBlocklist
	if err := database.DB.Where("jti = ?", claims.ID).First(&blocked).Error; err != nil {
		t.Fatalf("script token of the unsettled run must be revoked at the shutdown deadline: %v", err)
	}

	// 放行卡住的那次执行，等它在库还开着时结算完，免得它在用例收尾之后才去写一个已经关掉的库。
	release.close()
	if !executor.Wait(10 * time.Second) {
		t.Fatal("released run did not settle")
	}
}

// 研究 §6.1-4 + D15：被面板关停打断的执行跳过三种后置脚本、不发失败通知，日志写明两行「[面板正在关闭，…]」，
// last_run_status 仍记失败（不是手动停止）。先跑一遍不关停的对照，证明后置脚本与通知这条链在用例里确实是通的，
// 否则「没跑后置脚本 / 没收到通知」的断言没有区分度。
func TestHaltedRunSkipsPostHooksAndFailureNotify(t *testing.T) {
	cases := []struct {
		name string
		halt bool
	}{
		{name: "对照：普通失败照常跑后置脚本、发失败通知", halt: false},
		{name: "关停打断：跳过后置脚本、不发失败通知", halt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			channelID, hits := newFailureNotifyProbe(t)

			executor := NewTaskExecutor()
			task, req, taskLog := newRunningTaskReq(t, tc.name, func(tk *model.Task) {
				tk.NotifyOnFailure = true
				tk.NotificationChannelID = &channelID
			})
			markers := writePostHookMarkers(t, task)

			previous := runCommandWithPlanFunc
			runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
				onOutput("task output before failure\n")
				if tc.halt {
					// 模拟面板此刻开始关停：任务进程被整组杀掉，这一轮按失败返回。
					executor.StopAllRunningTasks()
				}
				return &ScriptResult{ReturnCode: 1}, nil, nil
			}
			t.Cleanup(func() { runCommandWithPlanFunc = previous })

			tinyLog, err := NewTinyLog(fmt.Sprintf("halt-post-hooks-%v", tc.halt))
			if err != nil {
				t.Fatalf("create tiny log: %v", err)
			}
			executor.runTask(req, taskLog, tinyLog)

			content := readSettledLogContent(t, taskLog.ID)
			stored := reloadServiceTask(t, task.ID)
			if stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
				t.Fatalf("run must settle as failed(%d) in both cases, got %v", model.RunFailed, stored.LastRunStatus)
			}

			if !tc.halt {
				if !strings.Contains(content, "[执行后置脚本]") {
					t.Fatalf("control run must start post hooks, log=%q", content)
				}
				if hookMarkersCheckable() {
					for _, marker := range markers {
						if _, err := os.Stat(marker); err != nil {
							t.Errorf("control run should have run the post hook that writes %s: %v (log=%q)", filepath.Base(marker), err, content)
						}
					}
				}
				deadline := time.Now().Add(5 * time.Second)
				for hits.Load() == 0 && time.Now().Before(deadline) {
					time.Sleep(20 * time.Millisecond)
				}
				if hits.Load() == 0 {
					t.Fatal("control run should send the failure notification; the probe got nothing, so the halted case would prove nothing")
				}
				return
			}

			if strings.Contains(content, "[执行后置脚本]") {
				t.Fatalf("halted run must not start the task post script, log=%q", content)
			}
			for _, marker := range markers {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Errorf("halted run must not run post hooks, but %s exists (err=%v)", filepath.Base(marker), err)
				}
			}
			for _, line := range []string{"[面板正在关闭，任务已中断]", "[面板正在关闭，跳过后置脚本]"} {
				if !containsLine(content, line) {
					t.Errorf("halted run log must contain the standalone line %q, log=%q", line, content)
				}
				if !isPanelMetaLine(line) {
					t.Errorf("%q must be recognised by isPanelMetaLine so it never crowds out the success excerpt", line)
				}
			}
			time.Sleep(500 * time.Millisecond)
			if got := hits.Load(); got != 0 {
				t.Fatalf("halted run must not send the failure notification (D15), probe got %d request(s)", got)
			}
		})
	}
}

// 研究 §6.1-5（回归）：手动停止的行为完全不变 —— 照常跑后置脚本、结算成已终止，日志里没有任何「面板正在关闭」。
func TestManualStopStillRunsPostHooks(t *testing.T) {
	testutil.SetupTestEnv(t)

	executor := NewTaskExecutor()
	task, req, taskLog := newRunningTaskReq(t, "手动停止仍跑后置脚本", nil)
	markers := writePostHookMarkers(t, task)

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		if !executor.StopTask(task.ID) {
			t.Error("StopTask should find the run inside its executing window")
		}
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	tinyLog, err := NewTinyLog("manual-stop-post-hooks")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	executor.runTask(req, taskLog, tinyLog)

	content := readSettledLogContent(t, taskLog.ID)
	if !strings.Contains(content, "[执行后置脚本]") {
		t.Fatalf("manual stop must still run post hooks, log=%q", content)
	}
	if strings.Contains(content, "[面板正在关闭，") {
		t.Fatalf("manual stop must not print shutdown notices, log=%q", content)
	}
	if hookMarkersCheckable() {
		for _, marker := range markers {
			if _, err := os.Stat(marker); err != nil {
				t.Errorf("manual stop should still run the post hook that writes %s: %v", filepath.Base(marker), err)
			}
		}
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("manual stop must settle as aborted(%d), got %v", model.RunAborted, stored.LastRunStatus)
	}
}

// 核实漏项 7：前置钩子原来不在执行器的任何登记里。关停时正跑着一个长前置钩子，会等满预算，
// 二进制 / Magisk 部署下面板退出后钩子还成了孤儿。现在：正在跑的前置钩子随关停按进程组终止；
// 关停开始后（包括关停后才开窗的执行）不再启动新的前置钩子。
func TestHaltKillsRunningPreHookAndSkipsNewOnes(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	scriptsDir := config.C.Data.ScriptsDir
	startedFlag := filepath.Join(scriptsDir, "pre-hook-started.flag")
	finishedFlag := filepath.Join(scriptsDir, "pre-hook-finished.flag")

	var calls atomic.Int32
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		calls.Add(1)
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	task, req, taskLog := newRunningTaskReq(t, "关停时正在跑的前置钩子", func(tk *model.Task) {
		before := "echo started > pre-hook-started.flag\nsleep 30\necho finished > pre-hook-finished.flag\n"
		tk.TaskBefore = &before
	})
	tinyLog, err := NewTinyLog("halt-pre-hook")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, tinyLog)
	}()

	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(startedFlag); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pre hook never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	haltAt := time.Now()
	executor.StopAllRunningTasks()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("runTask is still blocked in the pre hook: a running pre hook must be killed when the panel shuts down")
	}
	if elapsed := time.Since(haltAt); elapsed > 5*time.Second {
		t.Fatalf("running pre hook should be killed right away on shutdown, runTask took %s to settle", elapsed)
	}
	if _, err := os.Stat(finishedFlag); !os.IsNotExist(err) {
		t.Fatalf("pre hook must have been killed before it finished, but %s exists", filepath.Base(finishedFlag))
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("halted run must not start the task process, entry was called %d time(s)", got)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("halted run must settle as failed(%d), got %v", model.RunFailed, stored.LastRunStatus)
	}
	content := readSettledLogContent(t, taskLog.ID)
	for _, line := range []string{"[面板正在关闭，取消后续执行]", "[面板正在关闭，任务已中断]", "[面板正在关闭，跳过后置脚本]"} {
		if !containsLine(content, line) {
			t.Errorf("halted run log must contain the standalone line %q, log=%q", line, content)
		}
	}

	// 关停之后才开窗的执行：任务专属前置脚本与全局 task_before.sh 一个都不启动。
	afterHaltFlag := filepath.Join(scriptsDir, "pre-hook-after-halt.flag")
	globalBeforeFlag := filepath.Join(scriptsDir, "global-before-after-halt.flag")
	if err := os.WriteFile(filepath.Join(scriptsDir, "task_before.sh"), []byte("echo ran > global-before-after-halt.flag\n"), 0o755); err != nil {
		t.Fatalf("write task_before.sh: %v", err)
	}
	_, req2, taskLog2 := newRunningTaskReq(t, "关停之后才开窗的执行", func(tk *model.Task) {
		before := "echo ran > pre-hook-after-halt.flag\n"
		tk.TaskBefore = &before
	})
	tinyLog2, err := NewTinyLog("halt-pre-hook-after")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	executor.runTask(req2, taskLog2, tinyLog2)
	for _, flag := range []string{afterHaltFlag, globalBeforeFlag} {
		if _, err := os.Stat(flag); !os.IsNotExist(err) {
			t.Errorf("no pre hook may start once the panel is shutting down, but %s exists", filepath.Base(flag))
		}
	}
	// 只看标记文件不够：钩子起来那一下撞上关停会被立刻杀掉，来不及写标记。日志里连「[执行前置脚本]」都不该出现。
	if content2 := readSettledLogContent(t, taskLog2.ID); strings.Contains(content2, "[执行前置脚本]") {
		t.Errorf("a run opened after the halt must not even start its pre script, log=%q", content2)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("a run opened after the halt must not start the task process, entry was called %d time(s)", got)
	}
}

// 关停时这次执行已经跑完主进程、正在跑后置钩子（关停前就开始了，halting 拦不住）：同样按进程组终止。
// 原来后置钩子不在任何登记里：关停白等它跑完，4 秒截止后这次执行只能被标成中断、真实输出丢失，
// 二进制部署下面板退出后钩子还成了孤儿（复核端到端实测：task_after.sh 里的 sleep 31 在面板退出后还活着）。
// 钩子把子进程放后台再 wait：只杀 bash 本身的话 sleep 会活下来，这里要验整组都被杀掉。
// 手动停止照旧不碰钩子，见 TestManualStopStillRunsPostHooks。
func TestHaltKillsRunningPostHook(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	scriptsDir := config.C.Data.ScriptsDir
	startedFlag := filepath.Join(scriptsDir, "post-hook-started.flag")
	finishedFlag := filepath.Join(scriptsDir, "post-hook-finished.flag")
	childPIDFile := filepath.Join(scriptsDir, "post-hook-child.pid")

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		onOutput("main process output\n")
		return &ScriptResult{ReturnCode: 0}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	executor := NewTaskExecutor()
	task, req, taskLog := newRunningTaskReq(t, "关停时正在跑的后置钩子", func(tk *model.Task) {
		after := "sleep 30 &\necho $! > post-hook-child.pid\necho started > post-hook-started.flag\nwait\necho finished > post-hook-finished.flag\n"
		tk.TaskAfter = &after
	})
	tinyLog, err := NewTinyLog("halt-post-hook")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, tinyLog)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(startedFlag); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("post hook never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(childPIDFile)
	if err != nil {
		t.Fatalf("read post hook child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse post hook child pid %q: %v", raw, err)
	}

	haltAt := time.Now()
	executor.StopAllRunningTasks()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("runTask is still blocked in the post hook: a post hook running when the panel shuts down must be killed")
	}
	if elapsed := time.Since(haltAt); elapsed > 5*time.Second {
		t.Fatalf("running post hook should be killed right away on shutdown, runTask took %s to settle", elapsed)
	}
	if _, err := os.Stat(finishedFlag); !os.IsNotExist(err) {
		t.Fatalf("post hook must have been killed before it finished, but %s exists", filepath.Base(finishedFlag))
	}
	// 子进程被整组杀掉后由 init 收走；/proc 里查不到或只剩僵尸都算已经死了。
	childAlive := func() bool {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
		if err != nil {
			return false
		}
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		return len(fields) > 0 && fields[0] != "Z" && fields[0] != "X"
	}
	for deadline := time.Now().Add(3 * time.Second); childAlive() && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if childAlive() {
		_ = exec.Command("kill", "-9", strconv.Itoa(childPID)).Run()
		t.Fatalf("the post hook's child (pid %d) survived the shutdown: the hook must be killed as a whole process group", childPID)
	}
	// 这次执行在 runTask 里正常结算（不是被关停截止时的兜底标成中断）：真实输出还在，结算结束行写出。
	if stored := reloadServiceTask(t, task.ID); stored.Status == model.TaskStatusRunning || stored.Status == model.TaskStatusQueued {
		t.Fatalf("run must settle inside runTask, task left active with status %v", stored.Status)
	}
	content := readSettledLogContent(t, taskLog.ID)
	for _, want := range []string{"main process output", "[执行后置脚本]", "=== 执行结束"} {
		if !strings.Contains(content, want) {
			t.Errorf("settled log should contain %q, log=%q", want, content)
		}
	}
}

// 核实漏项 2：真实进程被关停整组杀掉时，「[脚本进程被信号终止：…]」那行必须带结尾换行，
// 否则后面的「[面板正在关闭，任务已中断]」粘在它行尾，isPanelMetaLine 按行首认不出来。
// 这条走真实的 bash 子进程与 runSingleCommand（describeTerminationSignal 只在 Unix 上有输出）。
// 结算后的日志逐行检查：凡是「[面板正在关闭，」「[脚本进程被信号终止：」开头的行都被 isPanelMetaLine 认出，
// 而且「[面板正在关闭，」只能出现在行首。
func TestShutdownInterruptNoticesAreStandalonePanelMetaLines(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	scriptsDir := config.C.Data.ScriptsDir
	if err := os.WriteFile(filepath.Join(scriptsDir, "interrupt_probe.sh"), []byte("echo probe-start\nsleep 30\necho probe-end\n"), 0o755); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
	task := &model.Task{Name: "关停打断真实进程", Command: "task interrupt_probe.sh", TaskType: model.TaskTypeManual, Status: model.TaskStatusRunning, Timeout: 120}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	runningStatus := model.LogStatusRunning
	taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	plan, err := ParseCommandExecutionPlan(task.Command, scriptsDir)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan}
	tinyLog, err := NewTinyLog("interrupt-notice-lines")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}

	executor := NewTaskExecutor()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, tinyLog)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for !executor.HasRunningProcess(task.ID) {
		if time.Now().After(deadline) {
			t.Fatal("task process was never registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	executor.StopAllRunningTasks()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("runTask did not settle after the task process group was killed")
	}

	content := readSettledLogContent(t, taskLog.ID)
	if !strings.Contains(content, "[脚本进程被信号终止：") {
		t.Fatalf("expected the real signal-termination hint in the log (this case must exercise runSingleCommand), log=%q", content)
	}
	for _, line := range []string{"[面板正在关闭，任务已中断]", "[面板正在关闭，跳过后置脚本]"} {
		if !containsLine(content, line) {
			t.Errorf("%q must stand on its own line, log=%q", line, content)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if idx := strings.Index(line, "[面板正在关闭，"); idx > 0 {
			t.Errorf("shutdown notice glued to the previous output: %q", line)
		}
		if strings.HasPrefix(line, "[面板正在关闭，") || strings.HasPrefix(line, "[脚本进程被信号终止：") {
			if !isPanelMetaLine(line) {
				t.Errorf("panel line %q is not recognised by isPanelMetaLine", line)
			}
		}
	}
}

// 设计 §5.2：SignalStop 等正在跑的 cron 回调最多 1 秒。回调要读写库，唯一的连接被长事务占着时会一直卡住，
// 而关停的下一步（杀任务）排在它后面；原来是无限等。
func TestSchedulerV2SignalStopDoesNotWaitForStuckCronCallback(t *testing.T) {
	scheduler := NewSchedulerV2(SchedulerConfig{WorkerCount: 1, QueueSize: 1, RateInterval: time.Millisecond}, nil)
	entered := make(chan struct{})
	release := newOnceCloser(t)
	var enterOnce sync.Once
	scheduler.cron.Schedule(cron.Every(time.Second), cron.FuncJob(func() {
		enterOnce.Do(func() { close(entered) })
		<-release.ch
	}))
	scheduler.Start()
	waitChan(t, entered, release.close, "cron callback never started")

	start := time.Now()
	stopped := make(chan struct{})
	go func() {
		scheduler.SignalStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		release.close()
		t.Fatal("SignalStop is still waiting for a stuck cron callback")
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("SignalStop should give up on a stuck cron callback after about 1s, took %s", elapsed)
	}
	if err := scheduler.Enqueue(&ExecutionRequest{TaskID: 1}); err == nil {
		t.Fatal("scheduler must refuse new work after SignalStop")
	}
}

// 研究 §6.1-6：备份调度、订阅调度关停时不再无限等正在跑的作业，最多等到给定的截止时间（不取消作业）；
// 没有作业在跑时立刻返回，而不是傻等到截止时间。
func TestCronSchedulersShutdownWithinDeadline(t *testing.T) {
	cases := []struct {
		name     string
		install  func(t *testing.T) *cron.Cron
		shutdown func(deadline time.Time)
	}{
		{
			name: "备份调度",
			install: func(t *testing.T) *cron.Cron {
				previous := globalBackupScheduler
				t.Cleanup(func() { globalBackupScheduler = previous })
				scheduler := &BackupScheduler{cron: cron.New(cron.WithSeconds())}
				globalBackupScheduler = scheduler
				return scheduler.cron
			},
			shutdown: ShutdownBackupScheduler,
		},
		{
			name: "订阅调度",
			install: func(t *testing.T) *cron.Cron {
				previous := globalSubscriptionScheduler
				t.Cleanup(func() { globalSubscriptionScheduler = previous })
				scheduler := &SubscriptionScheduler{cron: cron.New(cron.WithSeconds()), entryMap: make(map[uint]cron.EntryID)}
				globalSubscriptionScheduler = scheduler
				return scheduler.cron
			},
			shutdown: ShutdownSubscriptionScheduler,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"：作业在跑时只等到截止时间", func(t *testing.T) {
			c := tc.install(t)
			entered := make(chan struct{})
			release := newOnceCloser(t)
			var enterOnce sync.Once
			var finished atomic.Bool
			c.Schedule(cron.Every(time.Second), cron.FuncJob(func() {
				enterOnce.Do(func() { close(entered) })
				<-release.ch
				finished.Store(true)
			}))
			c.Start()
			waitChan(t, entered, release.close, "scheduled job never started")

			start := time.Now()
			done := make(chan struct{})
			go func() {
				tc.shutdown(time.Now().Add(100 * time.Millisecond))
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				release.close()
				t.Fatal("scheduler shutdown is still waiting for the running job past its deadline")
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("scheduler shutdown should return at its deadline (100ms), took %s", elapsed)
			}
			if finished.Load() {
				t.Fatal("the running job must not have been cut short or completed yet; shutdown should just stop waiting")
			}
			// 作业没被取消：放行后照常跑完。
			release.close()
			deadline := time.Now().Add(5 * time.Second)
			for !finished.Load() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !finished.Load() {
				t.Fatal("the running job should keep running after shutdown gave up waiting")
			}
		})
		t.Run(tc.name+"：没有作业在跑时立刻返回", func(t *testing.T) {
			c := tc.install(t)
			c.Start()
			start := time.Now()
			tc.shutdown(time.Now().Add(5 * time.Second))
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("idle scheduler shutdown must not wait for the deadline, took %s", elapsed)
			}
		})
	}
}

// 关停第 3 步 ShutdownSchedulerV2 把全局调度器置 nil 之后，第 4 步还在等正在跑的订阅拉取（最多到关停开始后 6.5 秒）。
// 这段时间里拉取走到自动添加任务，原来 GetSchedulerV2().AddJob 对 nil 解引用：手动拉取的协程没有 recover，
// 整个面板崩溃退出（退出码 2，关库、删 PID 文件都没走）；定时拉取被 cron 的 Recover 兜住，但这次拉取的收尾丢了。
// ddp sub pull 进程里调度器本来就是 nil，需要自动添加任务时同样崩。
// 现在：不 panic，任务行照常落库（面板下次启动时 InitSchedulerV2 按库注册），同步日志照常记「自动添加任务」。
func TestSubscriptionSyncAfterSchedulerShutdownStillCreatesTasks(t *testing.T) {
	testutil.SetupTestEnv(t)
	oldScheduler, oldExecutor := globalScheduler.Load(), globalExecutor.Load()
	t.Cleanup(func() {
		globalScheduler.Store(oldScheduler)
		globalExecutor.Store(oldExecutor)
	})

	if err := model.SetConfig("auto_add_cron", "true"); err != nil {
		t.Fatalf("set auto_add_cron: %v", err)
	}
	saveDir := "shutdown_sync_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create scripts root: %v", err)
	}
	script := "/**\n * cron 5 8 * * * demo.js\n */\nconst $ = new Env('Demo');\n"
	if err := os.WriteFile(filepath.Join(scriptsRoot, "demo.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}
	sub := &model.Subscription{
		Name:            "关停时还在跑的拉取",
		Type:            model.SubTypeGitRepo,
		URL:             "https://github.com/example/shutdown-sync.git",
		SaveDir:         saveDir,
		Enabled:         true,
		AutoAddTaskMode: model.SubTaskSyncEnabled,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// 按 shutdownPanel 的顺序走到「调度器已关」：HaltSchedulerV2 → ShutdownSchedulerV2。
	InitSchedulerV2()
	HaltSchedulerV2()
	ShutdownSchedulerV2()
	if GetSchedulerV2() != nil {
		t.Fatal("ShutdownSchedulerV2 should leave the global scheduler nil; this case would prove nothing otherwise")
	}

	var logs []string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("subscription sync must not panic after the scheduler is shut down: %v", r)
			}
		}()
		syncSubscriptionTasks(sub, func(line string) { logs = append(logs, line) })
	}()

	var tasks []model.Task
	if err := queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks).Error; err != nil {
		t.Fatalf("query tasks by label: %v", err)
	}
	wantCommand := "task " + filepath.Join(saveDir, "demo.js")
	if len(tasks) != 1 || strings.TrimSpace(tasks[0].Command) != wantCommand || tasks[0].CronExpression != "5 8 * * *" {
		t.Fatalf("the auto-added task row must still be stored (registered on next startup), got %+v", tasks)
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "[自动添加任务] ") {
		t.Fatalf("sync log should still report the auto-added task, got:\n%s", joined)
	}
}

// 调度器为 nil 时 AddJob / UpdateJob / RemoveJob 直接返回（与 HasJob / ScheduledEntryCount / Enqueue 一致），不能空指针 panic。
func TestSchedulerV2NilReceiverAddAndRemoveJob(t *testing.T) {
	var scheduler *SchedulerV2
	task := &model.Task{ID: 42, Name: "nil 调度器", Command: "echo 1", CronExpression: "5 8 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled}
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("AddJob on a nil scheduler should return nil, got %v", err)
	}
	if err := scheduler.UpdateJob(task); err != nil {
		t.Fatalf("UpdateJob on a nil scheduler should return nil, got %v", err)
	}
	scheduler.RemoveJob(task.ID)
	if scheduler.HasJob(task.ID) || scheduler.ScheduledEntryCount(task.ID) != -1 {
		t.Fatal("a nil scheduler must not report any job")
	}
}

// containsLine 判断 content 里有没有恰好等于 line 的一行。
func containsLine(content, line string) bool {
	for _, got := range strings.Split(content, "\n") {
		if strings.TrimRight(got, "\r") == line {
			return true
		}
	}
	return false
}
