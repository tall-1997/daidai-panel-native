package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// disabledManualRunHarness 走真实的 RunNow -> OnTaskExecuting -> RunTask / OnTaskFailed 结算链路（issue #133）。
//
// 同目录其它用例是拿库里的行去喂 ResolveTaskInactiveStatus 近似「结算」，可真实结算读的是请求里那份
// req.Task 内存快照：OnTaskExecuting 用 Updates(map) 写运行中时 GORM 会回写到这份快照上，
// 准备阶段失败 / 请求被放弃时则直接拿 RunNow 装进去的那份。这组用例把这两个前提一起钉住。
// 请求停在一个不启动 worker 的调度器队列里，由用例手动取出来执行；子进程入口换成桩，不依赖本机装没装 node。
type disabledManualRunHarness struct {
	scheduler *SchedulerV2
	task      *model.Task
	req       *ExecutionRequest
}

func newDisabledManualRunHarness(t *testing.T) *disabledManualRunHarness {
	t.Helper()
	testutil.SetupTestEnv(t)

	previousScheduler := globalScheduler.Load()
	scheduler := newStatusGateScheduler(t)
	// 结算时的 !HasJob 兜底与调度注册都认全局调度器。
	globalScheduler.Store(scheduler)
	t.Cleanup(func() { globalScheduler.Store(previousScheduler) })

	// OnTaskExecuting 会真的解析命令，脚本文件必须存在；执行本身由桩接管。
	const scriptName = "disabled_manual_run_probe.js"
	if err := os.WriteFile(filepath.Join(config.C.Data.ScriptsDir, scriptName), []byte("console.log('probe')\n"), 0o644); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	task := &model.Task{
		Name:           "禁用任务端到端手动运行",
		Command:        "node " + scriptName,
		TaskType:       model.TaskTypeCron,
		CronExpression: "0 5 * * *",
		Status:         model.TaskStatusDisabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if err := scheduler.RunNow(task.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	var req *ExecutionRequest
	select {
	case req = <-scheduler.taskQueue:
	default:
		t.Fatal("RunNow 应当把请求放进队列")
	}
	return &disabledManualRunHarness{scheduler: scheduler, task: task, req: req}
}

// enableLikeHandler 复刻 handler.validateAndEnableTask 对排队中 / 运行中任务的处理：
// 撤掉禁用意图、按库里的最新行重新注册调度，不改写 status。
func (h *disabledManualRunHarness) enableLikeHandler(t *testing.T) {
	t.Helper()

	ClearPendingDisable(h.task.ID)
	current := reloadServiceTask(t, h.task.ID)
	if err := h.scheduler.AddJob(&current); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
}

// runWithPause 执行这次请求：子进程跑到一半时调用 midRun，返回后放行并等结算写完。
func (h *disabledManualRunHarness) runWithPause(t *testing.T, midRun func()) {
	t.Helper()

	started := make(chan struct{})
	release := make(chan struct{})
	stubRunCommandWithPlan(t, func(map[string]string) (*ScriptResult, error) {
		close(started)
		<-release
		return &ScriptResult{ReturnCode: 0}, nil
	})

	executor := NewTaskExecutor()
	if err := executor.OnTaskExecuting(h.req); err != nil {
		t.Fatalf("OnTaskExecuting: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.RunTask(h.req)
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("子进程桩迟迟没有被调用")
	}
	if midRun != nil {
		midRun()
	}
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("结算迟迟没有完成")
	}
}

func (h *disabledManualRunHarness) assertSettled(t *testing.T, wantStatus float64, wantEntryCount int) {
	t.Helper()

	stored := reloadServiceTask(t, h.task.ID)
	if stored.Status != wantStatus {
		t.Fatalf("expected settled status %v, got %v", wantStatus, stored.Status)
	}
	if got := h.scheduler.ScheduledEntryCount(h.task.ID); got != wantEntryCount {
		t.Fatalf("expected ScheduledEntryCount=%d（%s），got %d（%s）",
			wantEntryCount, describeEntryCount(wantEntryCount), got, describeEntryCount(got))
	}
	if hasPendingDisable(&stored) {
		t.Fatal("结算之后待禁用标记必须清掉")
	}
}

// 不插手：运行期间开关位是关，跑完落回禁用、标记清掉、调度器里仍然没有它。
func TestDisabledTaskManualRunEndToEndSettlesDisabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	h.runWithPause(t, func() {
		running := reloadServiceTask(t, h.task.ID)
		if running.Status != model.TaskStatusRunning {
			t.Errorf("前置条件不成立：运行期间 status 应为运行中，实际 %v", running.Status)
		}
		if ResolveTaskEnabledSwitch(&running) {
			t.Error("运行中的禁用任务开关位应为关")
		}
	})

	h.assertSettled(t, model.TaskStatusDisabled, -1)
}

// 运行中编辑保存一次（task_mutate 重载出 status=2 再 UpdateJob）：不能被重新注册，跑完仍是禁用。
func TestDisabledTaskManualRunEndToEndEditedWhileRunningStaysDisabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	h.runWithPause(t, func() {
		running := reloadServiceTask(t, h.task.ID)
		if err := h.scheduler.UpdateJob(&running); err != nil {
			t.Errorf("UpdateJob: %v", err)
		}
	})

	h.assertSettled(t, model.TaskStatusDisabled, -1)
}

// 运行中被启用：跑完必须落回启用、定时触发保持注册。
// 启用接口对运行中的任务不改写 status，全靠结算时读到「运行中 + 无标记 + 已注册」落回启用 ——
// 前提是结算读到的 req.Task 快照已经是运行中，停在 RunNow 时的「禁用」的话会被直接判成禁用。
func TestDisabledTaskManualRunEndToEndEnabledWhileRunningSettlesEnabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	h.runWithPause(t, func() {
		h.enableLikeHandler(t)
	})

	h.assertSettled(t, model.TaskStatusEnabled, 1)
}

// 排队中被启用、随后准备阶段就失败（依赖任务上次未成功、脚本被删……）：必须落回启用。
// 启用那一步已经把它注册回调度器；OnTaskFailed 若拿 RunNow 时「禁用」的快照结算，
// 库里就会写成禁用、cron 条目却活着 —— 列表显示禁用中，到点照样被触发。
func TestDisabledTaskEnabledWhileQueuedThenFailsToPrepareSettlesEnabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	h.enableLikeHandler(t)
	NewTaskExecutor().OnTaskFailed(h.req, errors.New("依赖任务 '上游' 上次执行未成功"))

	h.assertSettled(t, model.TaskStatusEnabled, 1)
}

// 排队中没人动、准备阶段失败：照旧落回禁用并清掉标记（对照组）。
func TestDisabledTaskQueuedThenFailsToPrepareSettlesDisabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	NewTaskExecutor().OnTaskFailed(h.req, errors.New("依赖任务 '上游' 上次执行未成功"))

	h.assertSettled(t, model.TaskStatusDisabled, -1)
}

// 请求被放弃执行（releaseQueuedTaskStatus：多实例闸门拦下、延迟后重新入队失败）同一口径：
// 排队中被启用过就回到启用，不能拿 RunNow 时的快照把它写回禁用。
func TestReleaseQueuedDisabledManualRunAfterEnableRestoresEnabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	h.enableLikeHandler(t)
	releaseQueuedTaskStatus(h.req)

	if got := reloadServiceTask(t, h.task.ID).Status; got != model.TaskStatusEnabled {
		t.Fatalf("排队中被启用的任务放弃执行后应回到启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}
	if got := h.scheduler.ScheduledEntryCount(h.task.ID); got != 1 {
		t.Fatalf("expected ScheduledEntryCount=1, got %d（%s）", got, describeEntryCount(got))
	}
}

// 对照组：排队中没人动，放弃执行后回到禁用。
func TestReleaseQueuedDisabledManualRunRestoresDisabled(t *testing.T) {
	h := newDisabledManualRunHarness(t)

	releaseQueuedTaskStatus(h.req)

	if got := reloadServiceTask(t, h.task.ID).Status; got != model.TaskStatusDisabled {
		t.Fatalf("排队中的禁用任务放弃执行后应回到禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}
}
