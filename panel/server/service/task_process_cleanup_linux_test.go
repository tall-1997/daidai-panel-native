//go:build linux

package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这一组锁的是 #159 两处修复在 Linux 上的真实行为（进程组只在 Linux 上成立，Windows 本机跑全绿不算数，要在 WSL / CI 里跑）：
//   - 修复 A：定时 / 手动任务的主命令正常结束后，清理它进程组里留在后台的进程；
//     开机任务、总开关关闭、setsid 起的、钩子留下的、已被停止的执行都不清；真清理了才写一行提示。
//   - 修复 B：超时、手动停止、面板关停，一律先对整组 SIGTERM，宽限期过后还在才 SIGKILL。
//     关停另有两条：被关停打断的执行即便 trap 了 TERM、以 0 退出，也按失败结算；Magisk 部署下关停不给宽限，立即整组 KILL。
//
// 公共写法（设计稿 §4）：
//   - 脚本写进 config.C.Data.ScriptsDir，同步调 executor.runTask（同 panel_shutdown_test.go 的真实进程用例）；
//   - 每条用例用独一无二的 sleep 8641xx 当标记，扫 /proc/*/cmdline 统计存活的标记进程（僵尸不算）；
//   - 断言一律「轮询到期限为止」，期限 3 秒，不用固定 sleep；
//   - 靠 trap 的用例必须等脚本写出就绪标记再发信号：HasRunningProcess 在 trap 装上之前就为真，
//     这时发 TERM，bash 按默认动作直接退出、trap 不执行；
//   - 放到后台的标记进程一律重定向输出，否则断言失败或做突变时，攥着管道的孙进程会让 pumpAndWait 等不到 EOF，
//     用例卡到 go test 的 10 分钟超时，而不是干脆地变红。

// cleanupTestPollLimit 是这组用例「轮询到期限为止」的统一期限。
const cleanupTestPollLimit = 3 * time.Second

// waitBackgroundSleepSnippet 拼在「把 sleep 放到后台」那一行后面：等到 $! 真的 exec 成 sleep 才往下走（最多约 5 秒）。
// 不等的话，主命令退出、清理发生时后台进程可能还停在 fork / nohup / setsid 那一步：
// 用例分不清「被清理了」和「还没起来」，setsid 那条还可能在 setsid() 生效之前就被当成组内进程误伤。
// 执行到 sleep 时，nohup / setsid 都已经生效，sh -c 'trap "" TERM; exec sleep …' 里的忽略 TERM 也已经装上。
const waitBackgroundSleepSnippet = "bg=$!\n" +
	"i=0; while [ \"$(cat /proc/$bg/comm 2>/dev/null)\" != sleep ] && [ $i -lt 250 ]; do sleep 0.02; i=$((i+1)); done\n"

// leftoverNoticePrefix 是修复 A 那行提示的前缀（与 panelMetaLinePrefixes 里登记的一致）。
const leftoverNoticePrefix = "[已结束残留的后台进程："

// markerProcessPIDs 扫 /proc/*/cmdline，返回参数里恰好有 marker 的存活进程（僵尸、已死的不算）。
func markerProcessPIDs(marker string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		matched := false
		for _, arg := range strings.Split(string(raw), "\x00") {
			if arg == marker {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		// 状态字段在最后一个 ')' 之后；Z / X 是已经死了、只等回收的
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) == 0 || fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// waitForCondition 每 20ms 查一次 cond，到期限还不成立返回 false。
func waitForCondition(limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// killMarkerProcessesOnCleanup 在用例结束时杀掉还活着的标记进程，免得用例失败时把它们留给后面的用例。
func killMarkerProcessesOnCleanup(t *testing.T, markers ...string) {
	t.Cleanup(func() {
		for _, marker := range markers {
			for _, pid := range markerProcessPIDs(marker) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// overrideTermGraces 把两个 TERM 宽限期临时调短，用例结束后还原。
// 必须在 runTaskAsync 之前调：t.Cleanup 后进先出，还原要排在「等 runTask 跑完」之后。
func overrideTermGraces(t *testing.T, group, shutdown time.Duration) {
	previousGroup, previousShutdown := groupTermGrace, shutdownTermGrace
	groupTermGrace, shutdownTermGrace = group, shutdown
	t.Cleanup(func() { groupTermGrace, shutdownTermGrace = previousGroup, previousShutdown })
}

// newCleanupRunReq 写好脚本，建一条运行中的任务与日志，返回可直接喂给 executor.runTask 的请求和实时日志。
// 命令默认是「task <scriptFile>」、任务类型默认手动，mutate 可以改任务类型、超时、前置脚本、命令（conc）等。
func newCleanupRunReq(t *testing.T, scriptFile, script string, mutate func(*model.Task)) (*model.Task, *ExecutionRequest, *model.TaskLog, *TinyLog) {
	t.Helper()
	scriptsDir := config.C.Data.ScriptsDir
	if err := os.WriteFile(filepath.Join(scriptsDir, scriptFile), []byte(script), 0o755); err != nil {
		t.Fatalf("write %s: %v", scriptFile, err)
	}
	task := &model.Task{
		Name:     scriptFile,
		Command:  "task " + scriptFile,
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
	plan, err := ParseCommandExecutionPlan(task.Command, scriptsDir)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan, TriggerType: TriggerTypeManual}
	tinyLog, err := NewTinyLog("cleanup-" + strings.TrimSuffix(scriptFile, ".sh"))
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	return task, req, taskLog, tinyLog
}

// runTaskAsync 在协程里跑 executor.runTask，返回跑完时关闭的通道。
// 用例结束时（包括中途失败）把这个执行器上还在跑的任务收掉、等它结算完，免得进程和协程活过用例、去写已经关掉的库。
func runTaskAsync(t *testing.T, executor *TaskExecutor, req *ExecutionRequest, taskLog *model.TaskLog, tinyLog *TinyLog) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.runTask(req, taskLog, tinyLog)
	}()
	t.Cleanup(func() {
		executor.StopAllRunningTasks()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	return done
}

// waitScriptReady 等脚本写出就绪标记、并且执行器已经登记了它的进程：之后发的信号才会落在装好的 trap 上。
func waitScriptReady(t *testing.T, executor *TaskExecutor, taskID uint, flag string) {
	t.Helper()
	if !waitForCondition(15*time.Second, func() bool {
		_, err := os.Stat(flag)
		return err == nil && executor.HasRunningProcess(taskID)
	}) {
		t.Fatalf("脚本迟迟没有写出就绪标记 %s（或进程没有登记）", filepath.Base(flag))
	}
}

// countLeftoverNoticeLines 数日志里以修复 A 提示前缀开头的行。
func countLeftoverNoticeLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, leftoverNoticePrefix) {
			count++
		}
	}
	return count
}

// assertMarkerStaysAlive 断言标记进程已经在跑、并且在接下来 500ms 里没有被结束（不清理的反例用）。
func assertMarkerStaysAlive(t *testing.T, marker, why string) {
	t.Helper()
	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 1 }) {
		t.Fatalf("%s：sleep %s 应当还活着，实际存活 %v", why, marker, markerProcessPIDs(marker))
	}
	if waitForCondition(500*time.Millisecond, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("%s：sleep %s 不该被结束", why, marker)
	}
}

// 修复 A 的正例：主命令把 sleep 放到后台（重定向掉输出、不占管道）后退出，结算之后 3 秒内它就不在了；
// 日志里恰好一行提示；退出码、成功判定与结束行都和改动前一样。定时、手动两种任务类型都清。
func TestLeftoverProcessCleanupKillsBackgroundSleep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		taskType string
		marker   string
	}{
		{name: "定时任务", taskType: model.TaskTypeCron, marker: "864101"},
		{name: "手动任务", taskType: model.TaskTypeManual, marker: "864121"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			requireUsableBash(t)
			killMarkerProcessesOnCleanup(t, tc.marker)

			script := "nohup sleep " + tc.marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo spawned\n"
			task, req, taskLog, tinyLog := newCleanupRunReq(t, "leak.sh", script, func(tk *model.Task) { tk.TaskType = tc.taskType })
			NewTaskExecutor().runTask(req, taskLog, tinyLog)

			if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(tc.marker)) == 0 }) {
				t.Fatalf("主命令结束后，它放到后台的 sleep %s 应在 3 秒内被清理，仍存活 %v", tc.marker, markerProcessPIDs(tc.marker))
			}
			content := readSettledLogContent(t, taskLog.ID)
			if got := countLeftoverNoticeLines(content); got != 1 {
				t.Fatalf("真清理了残留进程时日志里应恰好有一行提示，实际 %d 行，log=%q", got, content)
			}
			for _, line := range strings.Split(content, "\n") {
				if strings.HasPrefix(line, leftoverNoticePrefix) && !isPanelMetaLine(line) {
					t.Fatalf("提示行必须被 isPanelMetaLine 认出，否则会挤进成功通知的摘录：%q", line)
				}
			}
			if !strings.Contains(content, "spawned") || !strings.Contains(content, "退出码 0 ===") {
				t.Fatalf("脚本输出与「退出码 0」的结束行都应照旧，log=%q", content)
			}
			if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunSuccess {
				t.Fatalf("清理残留进程不应改变结算结果，期望成功(%d)，实际 %v", model.RunSuccess, stored.LastRunStatus)
			}
		})
	}
}

// setsid 起的进程自成会话与进程组，不在任务的进程组里：修复 A 不碰它，也不写提示。这是给用户的「常驻进程出路」。
func TestLeftoverProcessCleanupSparesSetsidProcess(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864102"
	killMarkerProcessesOnCleanup(t, marker)

	script := "setsid nohup sleep " + marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo spawned\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "setsid.sh", script, nil)
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	assertMarkerStaysAlive(t, marker, "setsid 起的进程不在任务的进程组里")
	if content := readSettledLogContent(t, taskLog.ID); countLeftoverNoticeLines(content) != 0 {
		t.Fatalf("组里本来就空（setsid 逃出去了）时不该写提示，log=%q", content)
	}
}

// 开机任务按任务类型豁免，不论是开机自动触发还是手动点运行（runTask 本身不看触发方式，两条走同一路径；
// 这对子用例守的是「将来有人改成按触发方式判定」时手动那条变红）。
func TestLeftoverProcessCleanupSkipsStartupTasks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		triggerType string
		marker      string
	}{
		{name: "开机自动触发", triggerType: TriggerTypeStartup, marker: "864103"},
		{name: "手动点运行", triggerType: TriggerTypeManual, marker: "864123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			requireUsableBash(t)
			killMarkerProcessesOnCleanup(t, tc.marker)

			script := "nohup sleep " + tc.marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo spawned\n"
			_, req, taskLog, tinyLog := newCleanupRunReq(t, "startup.sh", script, func(tk *model.Task) { tk.TaskType = model.TaskTypeStartup })
			req.TriggerType = tc.triggerType
			NewTaskExecutor().runTask(req, taskLog, tinyLog)

			if content := readSettledLogContent(t, taskLog.ID); countLeftoverNoticeLines(content) != 0 {
				t.Fatalf("开机任务不清理残留进程，也不该写提示，log=%q", content)
			}
			assertMarkerStaysAlive(t, tc.marker, "开机任务常用来拉起常驻服务")
		})
	}
}

// 总开关关闭时不清理、不写提示。
func TestLeftoverProcessCleanupHonorsSwitch(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864104"
	killMarkerProcessesOnCleanup(t, marker)
	if err := model.SetConfig("cleanup_leftover_processes", "false"); err != nil {
		t.Fatalf("disable cleanup_leftover_processes: %v", err)
	}

	script := "nohup sleep " + marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo spawned\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "switch.sh", script, nil)
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	if content := readSettledLogContent(t, taskLog.ID); countLeftoverNoticeLines(content) != 0 {
		t.Fatalf("总开关关闭时不该写提示，log=%q", content)
	}
	assertMarkerStaysAlive(t, marker, "总开关关闭")
}

// 组里本来就空（主命令没留任何后台进程）时不写提示：先用 processGroupAlive 看一眼，真有进程才清理、才写。
func TestLeftoverProcessCleanupSilentWhenGroupEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	task, req, taskLog, tinyLog := newCleanupRunReq(t, "clean.sh", "echo clean\n", nil)
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	content := readSettledLogContent(t, taskLog.ID)
	if countLeftoverNoticeLines(content) != 0 {
		t.Fatalf("组里本来就空时不该写提示，log=%q", content)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunSuccess {
		t.Fatalf("应结算为成功(%d)，实际 %v", model.RunSuccess, stored.LastRunStatus)
	}
}

// conc 模式下每个账号各走一次 runSingleCommand，各自的进程组各清各的：两个账号各留一个进程，两个都被清掉；
// 两行提示，各自前面有单独占一行的账号前缀。
func TestLeftoverProcessCleanupCoversConcAccounts(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	killMarkerProcessesOnCleanup(t, "864105", "864106")
	if err := database.DB.Create(&model.EnvVar{Name: "LEAK_ACC", Value: "864105&864106", Enabled: true}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	// 每个账号拿到的 LEAK_ACC 就是自己那一段，正好拿来当标记
	script := "nohup sleep \"$LEAK_ACC\" >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo \"spawned-$LEAK_ACC\"\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "leak.sh", script, func(tk *model.Task) { tk.Command = "task leak.sh conc LEAK_ACC" })
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	for _, marker := range []string{"864105", "864106"} {
		if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
			t.Fatalf("conc 模式下账号留下的 sleep %s 也应被清理，仍存活 %v", marker, markerProcessPIDs(marker))
		}
	}
	content := readSettledLogContent(t, taskLog.ID)
	if got := countLeftoverNoticeLines(content); got != 2 {
		t.Fatalf("两个账号各清理一次，应有两行提示，实际 %d 行，log=%q", got, content)
	}
	for _, index := range []string{"1", "2"} {
		if want := "[LEAK_ACC#" + index + "] \n" + leftoverNoticePrefix; !strings.Contains(content, want) {
			t.Fatalf("账号 %s 的提示行前面应有单独占一行的账号前缀，log=%q", index, content)
		}
	}
}

// D3：只覆盖任务主命令，钩子（前置 / 后置脚本、task_before.sh 等）不经过 runSingleCommand，它们留下的进程照旧保留。
func TestLeftoverProcessCleanupLeavesHooksAlone(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864107"
	killMarkerProcessesOnCleanup(t, marker)

	before := "nohup sleep " + marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "main.sh", "echo main\n", func(tk *model.Task) { tk.TaskBefore = &before })
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	assertMarkerStaysAlive(t, marker, "前置脚本留下的进程不归修复 A 管")
	if content := readSettledLogContent(t, taskLog.ID); countLeftoverNoticeLines(content) != 0 {
		t.Fatalf("主命令的进程组本来就空，不该写提示，log=%q", content)
	}
}

// 清理不推迟结算：残留进程忽略 TERM 时，runTask 照样立刻返回（这一刻标记进程还活着），宽限期过后再由补刀协程 KILL。
// 推迟的话，每次执行都会多占宽限期那么久的并发槽位、拖住单实例闸门、虚增耗时。
func TestLeftoverProcessCleanupDoesNotDelaySettlement(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864108"
	killMarkerProcessesOnCleanup(t, marker)
	overrideTermGraces(t, 2*time.Second, shutdownTermGrace)

	script := "nohup sh -c 'trap \"\" TERM; exec sleep " + marker + "' >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "echo spawned\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "stubborn.sh", script, nil)
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	if got := markerProcessPIDs(marker); len(got) != 1 {
		t.Fatalf("runTask 返回的这一刻，忽略 TERM 的残留进程应当还活着（结算不等宽限期），实际存活 %v", got)
	}
	if content := readSettledLogContent(t, taskLog.ID); countLeftoverNoticeLines(content) != 1 {
		t.Fatalf("清理了残留进程，日志里应有一行提示，log=%q", content)
	}
	if !waitForCondition(2*time.Second+cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("宽限期过后补刀协程应整组 KILL，仍存活 %v", markerProcessPIDs(marker))
	}
}

// 已被手动停止的执行，主命令收尾后正常退出（退出码 0），组里还剩忽略 TERM 的后台进程：
// 这个组归停止路径负责（已发过 TERM、安排了 KILL），修复 A 不再发第二个信号、也不写提示，结算为已终止。
func TestLeftoverProcessCleanupSkippedAfterManualStop(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864109"
	killMarkerProcessesOnCleanup(t, marker)
	// 宽限放长一点：主命令收尾退出后、执行器做判定的那一刻，组里必须还剩这个忽略 TERM 的进程，
	// 「判定闭包恒为 true」的突变才会写出提示行、让这条变红
	overrideTermGraces(t, 2*time.Second, shutdownTermGrace)

	// sleep 30 放在就绪标记之前起：标记出现时它已经在组里，TERM 一到就跟着退出，不会在 trap 收尾后还留着
	script := "nohup sh -c 'trap \"\" TERM; exec sleep " + marker + "' >/dev/null 2>&1 &\n" +
		waitBackgroundSleepSnippet +
		"trap 'exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > stop-ready.flag\n" +
		"wait\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "stopped.sh", script, nil)
	executor := NewTaskExecutor()
	done := runTaskAsync(t, executor, req, taskLog, tinyLog)

	waitScriptReady(t, executor, task.ID, filepath.Join(config.C.Data.ScriptsDir, "stop-ready.flag"))
	if !executor.StopTask(task.ID) {
		t.Fatal("StopTask 应当认出这次正在执行的任务")
	}
	waitChan(t, done, nil, "手动停止后 runTask 迟迟没有结算")

	content := readSettledLogContent(t, taskLog.ID)
	if countLeftoverNoticeLines(content) != 0 {
		t.Fatalf("已被手动停止的执行不该再由修复 A 清理、写提示（这个组归停止路径负责），log=%q", content)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("手动停止应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
}

// 修复 B：超时先对整组发 SIGTERM，trap 了 TERM 的脚本能把收尾做完（写出标记文件）；超时提示照旧。
func TestTimeoutSendsSigtermBeforeSigkill(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	// sleep 30 放在就绪标记之前起、并重定向输出：标记出现时它已经在组里，TERM 一到就跟着退出
	script := "trap 'echo done > timeout-term.flag; exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > timeout-ready.flag\n" +
		"wait\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "timeout_trap.sh", script, func(tk *model.Task) { tk.Timeout = 1 })
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	scriptsDir := config.C.Data.ScriptsDir
	if _, err := os.Stat(filepath.Join(scriptsDir, "timeout-ready.flag")); err != nil {
		t.Fatalf("脚本在超时之前应已装好 trap、写出就绪标记：%v", err)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "timeout-term.flag")); err != nil {
		t.Fatalf("超时应先发 SIGTERM，让 trap 了 TERM 的脚本完成收尾；没有收尾标记说明直接 SIGKILL 了：%v", err)
	}
	content := readSettledLogContent(t, taskLog.ID)
	if !strings.Contains(content, "[任务超时，已在 1 秒后终止]") {
		t.Fatalf("超时提示应照旧，log=%q", content)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("超时应结算为失败(%d)，实际 %v", model.RunFailed, stored.LastRunStatus)
	}
}

// 忽略 TERM 的整组在宽限期过后被 SIGKILL：能正常结算，进程不在了，耗时不少于「超时 + 宽限期」。
func TestTimeoutKillsTermIgnoringGroupAfterGrace(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864111"
	killMarkerProcessesOnCleanup(t, marker)
	overrideTermGraces(t, 500*time.Millisecond, shutdownTermGrace)

	// sleep 继承了「忽略 TERM」
	script := "trap '' TERM\necho ready > ignore-ready.flag\nsleep " + marker + "\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "timeout_ignore.sh", script, func(tk *model.Task) { tk.Timeout = 1 })
	start := time.Now()
	NewTaskExecutor().runTask(req, taskLog, tinyLog)
	elapsed := time.Since(start)

	if elapsed < time.Second+500*time.Millisecond {
		t.Fatalf("忽略 TERM 时应等满宽限期再 KILL，耗时至少 1.5 秒，实际 %s", elapsed)
	}
	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("宽限期过后整组应被 KILL，仍存活 %v", markerProcessPIDs(marker))
	}
	if stored := reloadServiceTask(t, task.ID); stored.Status == model.TaskStatusRunning || stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("应正常结算为失败(%d)，实际 status=%v last_run_status=%v", model.RunFailed, stored.Status, stored.LastRunStatus)
	}
}

// 超时杀整组与任务类型、总开关都无关：开机任务超时，留在后台的进程同样被结束（开机豁免只管修复 A）。
func TestTimeoutStillKillsWholeGroupForStartupTask(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864113"
	killMarkerProcessesOnCleanup(t, marker)
	overrideTermGraces(t, time.Second, shutdownTermGrace)

	script := "nohup sleep " + marker + " >/dev/null 2>&1 &\n" + waitBackgroundSleepSnippet + "sleep 30\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "startup_timeout.sh", script, func(tk *model.Task) {
		tk.TaskType = model.TaskTypeStartup
		tk.Timeout = 1
	})
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("开机任务超时同样整组终止，后台的 sleep %s 仍存活 %v", marker, markerProcessPIDs(marker))
	}
	if content := readSettledLogContent(t, taskLog.ID); !strings.Contains(content, "[任务超时，已在 1 秒后终止]") {
		t.Fatalf("超时提示应照旧，log=%q", content)
	}
}

// DD2：超时分支等的是「整组退空」，不是 outcome。主命令收到 TERM 就退出、输出管道随之关闭（outcome 立刻就到），
// 组里却还留着不攥管道、又忽略 TERM 的后台进程：只等 outcome 的实现会放过它，而超时之后修复 A 不再清理，它就一直留下来。
// 这里要求宽限期过后它同样被整组 KILL（「超时杀整组」的语义不变）。
// 上面几条超时用例里忽略 TERM 的进程都攥着输出管道，区分不出这两种实现；突变成「TERM 后只等 outcome」时只有这条变红。
func TestTimeoutKillsTermIgnoringBackgroundOutsideThePipe(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864112"
	killMarkerProcessesOnCleanup(t, marker)
	overrideTermGraces(t, 500*time.Millisecond, shutdownTermGrace)

	// 后台进程重定向掉输出（不攥管道）并忽略 TERM；前台 sleep 30 是默认动作，收到 TERM 就和 bash 一起退出。
	// 超时给 2 秒：超时前脚本要先等后台 sleep 起来，WSL 负载高时 1 秒偏紧（同 TestTermLetsParentCloseDetachedChild）
	script := "nohup sh -c 'trap \"\" TERM; exec sleep " + marker + "' >/dev/null 2>&1 &\n" +
		waitBackgroundSleepSnippet +
		"echo ready > timeout-bg-ready.flag\n" +
		"sleep 30\n"
	_, req, taskLog, tinyLog := newCleanupRunReq(t, "timeout_bg_ignore.sh", script, func(tk *model.Task) { tk.Timeout = 2 })
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	if _, err := os.Stat(filepath.Join(config.C.Data.ScriptsDir, "timeout-bg-ready.flag")); err != nil {
		t.Fatalf("超时之前，忽略 TERM 的后台进程应已起来、脚本写出就绪标记：%v", err)
	}
	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("超时要等整组退空、宽限期过后整组 KILL：不攥管道又忽略 TERM 的后台 sleep %s 仍存活 %v —— 像是只等了 outcome", marker, markerProcessPIDs(marker))
	}
	if content := readSettledLogContent(t, taskLog.ID); !strings.Contains(content, "[任务超时，已在 2 秒后终止]") {
		t.Fatalf("超时提示应照旧，log=%q", content)
	}
}

// 修复 B：手动停止立即返回（补刀在单独的协程里），整组先收到 TERM，trap 了 TERM 的脚本能完成收尾，结算为已终止。
func TestStopTaskReturnsImmediatelyAndSendsSigterm(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	// sleep 30 放在就绪标记之前起、并重定向输出：标记出现时它已经在组里，TERM 一到就跟着退出
	script := "trap 'echo done > stop-term.flag; exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > stop-ready.flag\n" +
		"wait\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "stop_trap.sh", script, nil)
	executor := NewTaskExecutor()
	done := runTaskAsync(t, executor, req, taskLog, tinyLog)

	scriptsDir := config.C.Data.ScriptsDir
	waitScriptReady(t, executor, task.ID, filepath.Join(scriptsDir, "stop-ready.flag"))
	start := time.Now()
	if !executor.StopTask(task.ID) {
		t.Fatal("StopTask 应当认出这次正在执行的任务")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("StopTask 不应阻塞等宽限期，应在 200ms 内返回，实际 %s", elapsed)
	}
	termFlag := filepath.Join(scriptsDir, "stop-term.flag")
	if !waitForCondition(cleanupTestPollLimit, func() bool {
		_, err := os.Stat(termFlag)
		return err == nil
	}) {
		t.Fatal("停止应先发 SIGTERM，让 trap 了 TERM 的脚本完成收尾；3 秒内没有收尾标记")
	}
	waitChan(t, done, nil, "手动停止后 runTask 迟迟没有结算")
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("手动停止应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
}

// 忽略 TERM 的任务被手动停止：停止那一刻它还活着（不是立即 KILL），宽限期过后被整组 KILL，执行照常结算。
func TestStopTaskKillsTermIgnoringGroupAfterGrace(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	const marker = "864115"
	killMarkerProcessesOnCleanup(t, marker)
	overrideTermGraces(t, 500*time.Millisecond, shutdownTermGrace)

	script := "trap '' TERM\necho ready > stop-ignore-ready.flag\nsleep " + marker + "\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "stop_ignore.sh", script, nil)
	executor := NewTaskExecutor()
	done := runTaskAsync(t, executor, req, taskLog, tinyLog)

	waitScriptReady(t, executor, task.ID, filepath.Join(config.C.Data.ScriptsDir, "stop-ignore-ready.flag"))
	// 就绪标记写在 sleep 之前，再等它真的跑起来，下面「停止那一刻还活着」的断言才有意义
	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 1 }) {
		t.Fatalf("写出就绪标记后，sleep %s 应当很快跑起来", marker)
	}
	if !executor.StopTask(task.ID) {
		t.Fatal("StopTask 应当认出这次正在执行的任务")
	}
	if got := markerProcessPIDs(marker); len(got) != 1 {
		t.Fatalf("停止那一刻只发了 TERM（被忽略），进程应当还活着；实际存活 %v", got)
	}
	if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
		t.Fatalf("宽限期过后整组应被 KILL，仍存活 %v", markerProcessPIDs(marker))
	}
	waitChan(t, done, nil, "进程被 KILL 后 runTask 迟迟没有结算")
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunAborted {
		t.Fatalf("手动停止应结算为已终止(%d)，实际 %v", model.RunAborted, stored.LastRunStatus)
	}
}

// 修复 B 的关停：所有组一起收到 TERM，共用一个宽限截止时间，到点还在的统一 KILL。
// 两个忽略 TERM 的组：共享截止约 1 秒返回；「每组各等一遍」的错误实现会变成约 2 秒。trap 了 TERM 的那组能完成收尾。
func TestShutdownTermsAllGroupsWithinSharedGrace(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	killMarkerProcessesOnCleanup(t, "864116", "864126")
	overrideTermGraces(t, groupTermGrace, time.Second)

	scriptsDir := config.C.Data.ScriptsDir
	executor := NewTaskExecutor()
	type shutdownCase struct {
		task *model.Task
		done <-chan struct{}
		flag string
	}
	var cases []shutdownCase
	for _, item := range []struct{ file, script, flag string }{
		{"shutdown_a.sh", "trap 'echo done > shutdown-a-term.flag; exit 0' TERM\nsleep 30 >/dev/null 2>&1 &\necho ready > shutdown-a-ready.flag\nwait\n", "shutdown-a-ready.flag"},
		{"shutdown_b.sh", "trap '' TERM\necho ready > shutdown-b-ready.flag\nsleep 864116\n", "shutdown-b-ready.flag"},
		{"shutdown_c.sh", "trap '' TERM\necho ready > shutdown-c-ready.flag\nsleep 864126\n", "shutdown-c-ready.flag"},
	} {
		task, req, taskLog, tinyLog := newCleanupRunReq(t, item.file, item.script, nil)
		cases = append(cases, shutdownCase{task: task, done: runTaskAsync(t, executor, req, taskLog, tinyLog), flag: item.flag})
	}
	for _, c := range cases {
		waitScriptReady(t, executor, c.task.ID, filepath.Join(scriptsDir, c.flag))
	}

	start := time.Now()
	count := executor.StopAllRunningTasks()
	elapsed := time.Since(start)
	if count != 3 {
		t.Fatalf("应终止 3 个进程组，实际 %d", count)
	}
	if elapsed >= 1500*time.Millisecond {
		t.Fatalf("所有组应共用 1 秒的宽限截止时间（约 1 秒返回），实际 %s —— 像是每组各等了一遍", elapsed)
	}
	if elapsed < 800*time.Millisecond {
		t.Fatalf("有忽略 TERM 的组时应等到宽限截止再 KILL，实际只用了 %s", elapsed)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "shutdown-a-term.flag")); err != nil {
		t.Fatalf("trap 了 TERM 的那组应在宽限期内完成收尾：%v", err)
	}
	for _, marker := range []string{"864116", "864126"} {
		if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(marker)) == 0 }) {
			t.Fatalf("宽限截止后忽略 TERM 的组应被 KILL，sleep %s 仍存活 %v", marker, markerProcessPIDs(marker))
		}
	}
	for _, c := range cases {
		waitChan(t, c.done, nil, "关停后 runTask 迟迟没有结算")
	}
}

// 被面板关停打断的执行一律按失败结算（runStopHalt 的契约），即便脚本 trap 了 TERM、收完尾以 0 退出：
// 修复 B 起关停先发 SIGTERM，这类脚本的退出码会被判成成功——不拦的话记成成功、不写「任务已中断」、还发成功通知。
// 与手动停止（trap 后以 0 退出照样判已终止）同一思路：看这次执行收到的停止种类，不看退出码。
// 成功、失败通知都开着，用本地 webhook 探针确认一条都不发（D15）。
// MaxRetries 设成 1：这一轮照常走失败分支，下一轮开头被停止检查拦下、写「取消后续执行」，不再起新进程（与改前立即 KILL 时一样）。
func TestShutdownSettlesTermTrappedZeroExitAsInterrupted(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	// 探针数的是这个渠道收到的所有请求，成功通知、失败通知都算
	channelID, hits := newFailureNotifyProbe(t)

	// sleep 30 放在就绪标记之前起、并重定向输出：标记出现时它已经在组里，TERM 一到就跟着退出
	script := "trap 'echo done > halt-term.flag; exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > halt-ready.flag\n" +
		"wait\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "halt_graceful.sh", script, func(tk *model.Task) {
		tk.NotifyOnSuccess = true
		tk.NotifyOnFailure = true
		tk.NotificationChannelID = &channelID
		tk.MaxRetries = 1
	})
	executor := NewTaskExecutor()
	done := runTaskAsync(t, executor, req, taskLog, tinyLog)

	scriptsDir := config.C.Data.ScriptsDir
	waitScriptReady(t, executor, task.ID, filepath.Join(scriptsDir, "halt-ready.flag"))
	executor.StopAllRunningTasks()
	waitChan(t, done, nil, "关停后 runTask 迟迟没有结算")

	// 先确认用例真的走到了「收到 TERM、在 trap 里收尾后以 0 退出」这条路，否则下面几条断言证明不了什么
	if _, err := os.Stat(filepath.Join(scriptsDir, "halt-term.flag")); err != nil {
		t.Fatalf("关停应先发 SIGTERM，让脚本在 trap 里收尾后以 0 退出；没有收尾标记：%v", err)
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("被面板关停打断的执行应按失败结算(%d)，退出码 0 也一样，实际 %v", model.RunFailed, stored.LastRunStatus)
	}
	content := readSettledLogContent(t, taskLog.ID)
	for _, line := range []string{"[面板正在关闭，取消后续执行]", "[面板正在关闭，任务已中断]", "[面板正在关闭，跳过后置脚本]"} {
		if !containsLine(content, line) {
			t.Fatalf("日志里应有单独一行 %q，log=%q", line, content)
		}
	}
	// 通知是异步发的：等一会儿再看探针（同 TestHaltedRunSkipsPostHooksAndFailureNotify）
	time.Sleep(500 * time.Millisecond)
	if got := hits.Load(); got != 0 {
		t.Fatalf("被面板关停打断的执行不发任何通知（D15），探针却收到 %d 次请求", got)
	}
}

// 上一条的反面：主命令在关停之前就已经成功返回（关停落在后置脚本里），这次执行照旧记成功——
// 停止种类是在主命令返回那一刻读的，那时还是 runStopNone。正在跑的后置脚本被整组终止，日志不写「任务已中断」。
func TestShutdownAfterMainCommandSucceededStillSettlesAsSuccess(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	// 后置脚本写出就绪标记后阻塞，关停时它正在跑。sleep 30 放在就绪标记之前起、并重定向输出：
	// 标记出现时它已经在组里，TERM 一到就和 bash 一起退出
	after := "sleep 30 >/dev/null 2>&1 &\necho started > post-ready.flag\nwait\n"
	task, req, taskLog, tinyLog := newCleanupRunReq(t, "main_ok.sh", "echo main-done\n", func(tk *model.Task) { tk.TaskAfter = &after })
	executor := NewTaskExecutor()
	done := runTaskAsync(t, executor, req, taskLog, tinyLog)

	readyFlag := filepath.Join(config.C.Data.ScriptsDir, "post-ready.flag")
	if !waitForCondition(15*time.Second, func() bool {
		_, err := os.Stat(readyFlag)
		return err == nil
	}) {
		t.Fatal("后置脚本迟迟没有写出就绪标记")
	}
	executor.StopAllRunningTasks()
	waitChan(t, done, nil, "关停后 runTask 迟迟没有结算")

	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunSuccess {
		t.Fatalf("主命令在关停之前已经成功返回，应照旧记成功(%d)，实际 %v", model.RunSuccess, stored.LastRunStatus)
	}
	content := readSettledLogContent(t, taskLog.ID)
	if !strings.Contains(content, "main-done") || strings.Contains(content, "[面板正在关闭，任务已中断]") {
		t.Fatalf("主命令的输出应在、且成功的执行不写「任务已中断」，log=%q", content)
	}
}

// Magisk 部署下面板关停不给 TERM 宽限，所有进程组立即整组 KILL（与 v3.3.5 一致）；其它部署照旧共享宽限。
// 模块管理器动作按钮的「停止」是 kill -TERM 面板、sleep 2、还在就 kill -KILL（Magisk/action.sh）：
// SignalStop 最多 1 秒加 2 秒宽限装不进去，面板先被 KILL，忽略 TERM 的任务进程组成了孤儿；action.sh 在线升级改不到。
// 部署判断认 service.sh export 的 DAIDAI_MAGISK_SHELL_VERSION。环境变量都用 t.Setenv 设、子用例结束自动还原，
// 这组用例不调 t.Parallel，不会和同包别的用例同时跑。宽限显式设成 2 秒（同默认值），计时断言不随默认值漂移。
func TestShutdownKillsImmediatelyOnMagisk(t *testing.T) {
	for _, tc := range []struct {
		name       string
		magisk     bool
		marker     string
		wantSignal string
	}{
		{name: "Magisk：立即整组 KILL", magisk: true, marker: "864131", wantSignal: "killed(9)"},
		{name: "其它部署：照旧等 TERM 宽限", magisk: false, marker: "864132", wantSignal: "terminated(15)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			requireUsableBash(t)
			killMarkerProcessesOnCleanup(t, tc.marker)
			// 另一个标志先清空：Magisk 那条要证明的是「认 DAIDAI_MAGISK_SHELL_VERSION」
			t.Setenv("DAIDAI_MAGISK_MODULE", "")
			if tc.magisk {
				t.Setenv("DAIDAI_MAGISK_SHELL_VERSION", "1")
			} else {
				t.Setenv("DAIDAI_MAGISK_SHELL_VERSION", "")
				if IsMagiskModuleRuntime() {
					t.Skip("当前机器上存在 Magisk 模块标记文件，模拟不出非 Magisk 部署")
				}
			}
			overrideTermGraces(t, groupTermGrace, 2*time.Second)

			// 组里有一个忽略 TERM 的后台进程（重定向掉输出、不攥管道），它让「照旧等宽限」的计时有区分度；
			// 主进程 bash 自己对 TERM 是默认动作，它被哪个信号结束，说明关停有没有先发 TERM：
			// Magisk 下只有 SIGKILL（killed(9)，与 v3.3.5 一致），其它部署先被 SIGTERM 结束（terminated(15)）。
			script := "nohup sh -c 'trap \"\" TERM; exec sleep " + tc.marker + "' >/dev/null 2>&1 &\n" +
				waitBackgroundSleepSnippet +
				"echo ready > magisk-ready.flag\n" +
				"sleep 30\n"
			task, req, taskLog, tinyLog := newCleanupRunReq(t, "magisk_ignore.sh", script, nil)
			executor := NewTaskExecutor()
			done := runTaskAsync(t, executor, req, taskLog, tinyLog)

			waitScriptReady(t, executor, task.ID, filepath.Join(config.C.Data.ScriptsDir, "magisk-ready.flag"))
			if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(tc.marker)) == 1 }) {
				t.Fatalf("写出就绪标记后，忽略 TERM 的 sleep %s 应当已经在跑，实际存活 %v", tc.marker, markerProcessPIDs(tc.marker))
			}

			start := time.Now()
			if count := executor.StopAllRunningTasks(); count != 1 {
				t.Fatalf("应终止 1 个进程组，实际 %d", count)
			}
			elapsed := time.Since(start)
			if tc.magisk && elapsed >= 500*time.Millisecond {
				t.Fatalf("Magisk 部署下关停不给 TERM 宽限、应立即整组 KILL（远小于 2 秒），实际用了 %s", elapsed)
			}
			if !tc.magisk && elapsed < 1500*time.Millisecond {
				t.Fatalf("非 Magisk 部署照旧先 TERM、等满 2 秒的共享宽限再 KILL（组里有忽略 TERM 的进程），实际只用了 %s", elapsed)
			}
			if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(tc.marker)) == 0 }) {
				t.Fatalf("忽略 TERM 的进程最终都应被整组 KILL，sleep %s 仍存活 %v", tc.marker, markerProcessPIDs(tc.marker))
			}
			waitChan(t, done, nil, "关停后 runTask 迟迟没有结算")
			if content := readSettledLogContent(t, taskLog.ID); !strings.Contains(content, "[脚本进程被信号终止："+tc.wantSignal) {
				t.Fatalf("主进程应被 %s 结束（Magisk 下关停不发 TERM、直接 KILL；其它部署先发 TERM），log=%q", tc.wantSignal, content)
			}
		})
	}
}

// R2 最后一条（对应 Puppeteer 默认 detached:true 模式）：父进程把子进程放进独立的会话 / 进程组，自己 trap TERM 去关掉它。
// 先 TERM 的话父进程有机会收尾，0 残留；直接对组 KILL 的话父进程来不及，独立组里的子进程成了孤儿。
// 超时、手动停止、关停三条路径都要成立。突变：任一路径改回直接 KillProcessGroup，对应子用例变红。
func TestTermLetsParentCloseDetachedChild(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker string
		stop   string
	}{
		{name: "超时", marker: "864117", stop: "timeout"},
		{name: "手动停止", marker: "864118", stop: "stop_task"},
		{name: "面板关停", marker: "864119", stop: "shutdown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			requireUsableBash(t)
			killMarkerProcessesOnCleanup(t, tc.marker)

			// 非交互 bash 里 setsid 后台作业的 $! 就是 sleep 本身（setsid 不是组长时直接 setsid() 再 exec），它自成会话与进程组
			script := "setsid sleep " + tc.marker + " >/dev/null 2>&1 &\n" +
				waitBackgroundSleepSnippet +
				"child=$bg\n" +
				"trap 'kill $child; exit 0' TERM\n" +
				"echo ready > detached-ready.flag\n" +
				"wait\n"
			task, req, taskLog, tinyLog := newCleanupRunReq(t, "detached.sh", script, func(tk *model.Task) {
				if tc.stop == "timeout" {
					// 给 2 秒：超时前脚本要先等后台 sleep 起来、再装好 trap，WSL 负载高时 1 秒偏紧
					tk.Timeout = 2
				}
			})
			executor := NewTaskExecutor()
			done := runTaskAsync(t, executor, req, taskLog, tinyLog)

			if tc.stop != "timeout" {
				waitScriptReady(t, executor, task.ID, filepath.Join(config.C.Data.ScriptsDir, "detached-ready.flag"))
				if tc.stop == "stop_task" {
					if !executor.StopTask(task.ID) {
						t.Fatal("StopTask 应当认出这次正在执行的任务")
					}
				} else {
					executor.StopAllRunningTasks()
				}
			}
			waitChan(t, done, nil, "runTask 迟迟没有结算")

			if !waitForCondition(cleanupTestPollLimit, func() bool { return len(markerProcessPIDs(tc.marker)) == 0 }) {
				t.Fatalf("父进程收到 TERM 后应自己关掉独立进程组里的子进程，sleep %s 仍存活 %v —— 像是直接 SIGKILL 了父进程", tc.marker, markerProcessPIDs(tc.marker))
			}
		})
	}
}
