package service

import (
	"errors"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func reloadServiceTask(t *testing.T, id uint) model.Task {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("reload task %d: %v", id, err)
	}
	return task
}

// issue #133：禁用任务被手动运行，排队 / 运行期间都不能被重新注册进调度器，停止或跑完都落回禁用。
//
// 原来「跑完回到禁用」只靠「调度器里没有这条任务」反推。运行期间只要编辑保存一次
// （handler 重载出 status=0.5/2 再 UpdateJob），AddJob 就会把它挂回去，跑完被结算成启用、cron 条目也是活的。
// RunNow 现在在入队前打待禁用标记，这条用例把整条链路钉死。
func TestSchedulerV2RunNowKeepsDisabledTaskDisabledThroughItsRun(t *testing.T) {
	testutil.SetupTestEnv(t)

	previousScheduler := globalScheduler.Load()
	scheduler := newStatusGateScheduler(t)
	globalScheduler.Store(scheduler)
	t.Cleanup(func() { globalScheduler.Store(previousScheduler) })

	cases := []struct {
		name           string
		taskType       string
		cronExpression string
	}{
		{name: "禁用的定时任务被手动运行", taskType: model.TaskTypeCron, cronExpression: "0 5 * * *"},
		{name: "禁用的手动任务被手动运行", taskType: model.TaskTypeManual},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			task := mustCreateScheduledTask(t, testCase.name, testCase.taskType, model.TaskStatusDisabled, testCase.cronExpression)
			t.Cleanup(func() { ClearPendingDisable(task.ID) })

			if err := scheduler.RunNow(task.ID); err != nil {
				t.Fatalf("RunNow 返回错误: %v", err)
			}

			// 排队中：开关位是关；此刻被停止 / 放弃执行也必须落回禁用。
			queued := reloadServiceTask(t, task.ID)
			if queued.Status != model.TaskStatusQueued {
				t.Fatalf("前置条件不成立：RunNow 之后应处于排队中，实际 %v", queued.Status)
			}
			if !pendingDisableInEffect(&queued) {
				t.Fatal("RunNow 必须给禁用中的任务打待禁用标记")
			}
			if ResolveTaskEnabledSwitch(&queued) {
				t.Fatal("排队中的禁用任务开关位应为关")
			}
			if got := ResolveTaskInactiveStatus(&queued); got != model.TaskStatusDisabled {
				t.Fatalf("排队中的禁用任务被停止后应落回禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
			}

			// 排队期间编辑保存一次：不能被注册。
			if err := scheduler.UpdateJob(&queued); err != nil {
				t.Fatalf("UpdateJob 返回错误: %v", err)
			}
			if got := scheduler.ScheduledEntryCount(task.ID); got != -1 {
				t.Fatalf("排队中的禁用任务被编辑保存后不能注册进调度器，期望 -1（%s），实际 %d（%s）",
					describeEntryCount(-1), got, describeEntryCount(got))
			}

			// 执行器接手，status 变成运行中；此刻再编辑保存一次。
			if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).
				Update("status", model.TaskStatusRunning).Error; err != nil {
				t.Fatalf("把任务置为运行中失败: %v", err)
			}
			running := reloadServiceTask(t, task.ID)
			if err := scheduler.UpdateJob(&running); err != nil {
				t.Fatalf("UpdateJob 返回错误: %v", err)
			}
			if got := scheduler.ScheduledEntryCount(task.ID); got != -1 {
				t.Fatalf("运行中的禁用任务被编辑保存后不能注册进调度器，期望 -1（%s），实际 %d（%s）",
					describeEntryCount(-1), got, describeEntryCount(got))
			}
			if ResolveTaskEnabledSwitch(&running) {
				t.Fatal("运行中的禁用任务开关位应为关")
			}
			if got := ResolveTaskInactiveStatus(&running); got != model.TaskStatusDisabled {
				t.Fatalf("这次跑完应结算为禁用(%v)，实际 %v —— 任务被静默重新启用了", model.TaskStatusDisabled, got)
			}
		})
	}
}

// RunNow 只给禁用中的任务打标记，启用任务手动运行不受影响。
func TestSchedulerV2RunNowDoesNotMarkEnabledTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)
	task := mustCreateScheduledTask(t, "启用任务被手动运行", model.TaskTypeCron, model.TaskStatusEnabled, "0 5 * * *")
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if err := scheduler.RunNow(task.ID); err != nil {
		t.Fatalf("RunNow 返回错误: %v", err)
	}
	stored := reloadServiceTask(t, task.ID)
	if hasPendingDisable(&stored) {
		t.Fatal("启用任务手动运行不该被打待禁用标记，否则跑完会被结算成禁用")
	}
}

// 入队失败时这次运行根本没发生、也就不会有结算：RunNow 刚打的标记必须撤回，不能一直挂在任务上。
func TestSchedulerV2RunNowRollsBackPendingDisableWhenEnqueueFails(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)
	// 调度器停掉之后 Enqueue 一律返回 scheduler stopped。
	scheduler.SignalStop()

	task := mustCreateScheduledTask(t, "入队失败的禁用任务", model.TaskTypeManual, model.TaskStatusDisabled, "")
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if err := scheduler.RunNow(task.ID); err == nil {
		t.Fatal("调度器已停止时 RunNow 应返回错误")
	}
	stored := reloadServiceTask(t, task.ID)
	if stored.Status != model.TaskStatusDisabled {
		t.Fatalf("入队失败不该改动任务状态，期望禁用(%v)，实际 %v", model.TaskStatusDisabled, stored.Status)
	}
	if hasPendingDisable(&stored) {
		t.Fatal("入队失败时 RunNow 刚打的待禁用标记必须撤回")
	}
}

// 准备阶段就失败（依赖任务上次未成功、脚本不存在……）也是这次运行的最终结算：
// 落回禁用，并把 RunNow 打的待禁用标记清掉，与 runTask 结算块同一口径。
func TestTaskExecutorOnTaskFailedSettlesManualRunOfDisabledTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	previousScheduler := globalScheduler.Load()
	scheduler := newStatusGateScheduler(t)
	globalScheduler.Store(scheduler)
	t.Cleanup(func() { globalScheduler.Store(previousScheduler) })

	task := mustCreateScheduledTask(t, "准备阶段失败的禁用任务", model.TaskTypeManual, model.TaskStatusDisabled, "")
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if err := scheduler.RunNow(task.ID); err != nil {
		t.Fatalf("RunNow 返回错误: %v", err)
	}

	var req *ExecutionRequest
	select {
	case req = <-scheduler.taskQueue:
	default:
		t.Fatal("RunNow 应当把请求放进队列")
	}

	NewTaskExecutor().OnTaskFailed(req, errors.New("依赖任务 '上游' 上次执行未成功"))

	stored := reloadServiceTask(t, task.ID)
	if stored.Status != model.TaskStatusDisabled {
		t.Fatalf("准备阶段失败的禁用任务应落回禁用(%v)，实际 %v", model.TaskStatusDisabled, stored.Status)
	}
	if hasPendingDisable(&stored) {
		t.Fatal("结算落成禁用后待禁用标记应被清掉")
	}
}

// 标记只在排队中 / 运行中作数：status 已经是明确的启用时，残留标记不能把它挡在调度器外面。
func TestSchedulerV2AddJobIgnoresStalePendingDisableOnEnabledTask(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)
	task := mustCreateScheduledTask(t, "带残留标记的启用任务", model.TaskTypeCron, model.TaskStatusEnabled, "0 5 * * *")
	MarkPendingDisable(task.ID)
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("AddJob 返回错误: %v", err)
	}
	if got := scheduler.ScheduledEntryCount(task.ID); got != 1 {
		t.Fatalf("status 为启用时残留标记不该作数，期望注册 1 条触发条目，实际 %d（%s）", got, describeEntryCount(got))
	}

	// 同一笔标记在运行中是作数的：这正是「运行中被禁用、等这次跑完再生效」的语义。
	task.Status = model.TaskStatusRunning
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("AddJob 返回错误: %v", err)
	}
	if got := scheduler.ScheduledEntryCount(task.ID); got != -1 {
		t.Fatalf("运行中的任务带待禁用标记时不能注册，期望 -1，实际 %d（%s）", got, describeEntryCount(got))
	}
}
