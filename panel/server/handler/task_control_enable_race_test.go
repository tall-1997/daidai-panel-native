package handler

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

type enableRaceInFlightCase struct {
	name   string
	status float64
}

var enableRaceInFlightCases = []enableRaceInFlightCase{
	{name: "运行中", status: model.TaskStatusRunning},
	{name: "排队中", status: model.TaskStatusQueued},
}

// setupEnableRace 起全局调度器（AddJob 登记用）和一个不启动 worker 的调度器（只用来走真实的 RunNow：
// 打待禁用标记、把库里改成排队中，请求永远停在它自己的队列里不会被执行）。
func setupEnableRace(t *testing.T) (*service.SchedulerV2, *service.SchedulerV2) {
	t.Helper()

	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	global := service.GetSchedulerV2()
	if global == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	idle := service.NewSchedulerV2(service.SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    16,
		RateInterval: time.Hour,
	}, nil)
	t.Cleanup(idle.Stop)
	return global, idle
}

// createDisabledTaskInFlight 造一个被手动运行的禁用任务，库里停在 status。
func createDisabledTaskInFlight(t *testing.T, idle *service.SchedulerV2, name string, status float64) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:           name,
		Command:        "echo enable race",
		TaskType:       model.TaskTypeCron,
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusDisabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	// 待禁用标记只活在内存里、跨用例存活，用完一律清掉。
	t.Cleanup(func() { service.ClearPendingDisable(task.ID) })

	if err := idle.RunNow(task.ID); err != nil {
		t.Fatalf("run disabled task %q: %v", name, err)
	}
	writeTaskStatusForEnableRace(t, task.ID, status)
	return task
}

func writeTaskStatusForEnableRace(t *testing.T, id uint, status float64) {
	t.Helper()

	if err := database.DB.Model(&model.Task{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		t.Fatalf("set status %v for task %d: %v", status, id, err)
	}
}

func loadTaskForEnableRace(t *testing.T, id uint) model.Task {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("load task %d: %v", id, err)
	}
	return task
}

// S1 复查的 PLAUSIBLE 竞态：Enable 先读到 status=0.5 / 2，本次执行恰好在读库之后结算成禁用
// （标记还在；或者撤标记之后、AddJob 之前走了 !HasJob 兜底），handler 却按旧快照只做「撤标记 + AddJob」，
// 结果库里是禁用、定时触发已经注册。这里把库里的行直接改成 0 来模拟「刚被结算成禁用」，再拿旧快照走这条分支。
func TestValidateAndEnableTaskPullsBackRowSettledDisabledMidEnable(t *testing.T) {
	global, idle := setupEnableRace(t)

	for _, inFlight := range enableRaceInFlightCases {
		t.Run(inFlight.name, func(t *testing.T) {
			task := createDisabledTaskInFlight(t, idle, "启用竞态-已结算-"+inFlight.name, inFlight.status)

			// Enable handler 读到的快照：还是排队中 / 运行中。
			snapshot := loadTaskForEnableRace(t, task.ID)
			if snapshot.Status != inFlight.status {
				t.Fatalf("前置条件不成立：快照应为 %v，实际 %v", inFlight.status, snapshot.Status)
			}

			// 本次执行恰好在读库之后结算：标记还在，落成禁用并清掉标记（runTask 结算块做的事）。
			writeTaskStatusForEnableRace(t, task.ID, model.TaskStatusDisabled)
			service.ClearPendingDisable(task.ID)

			if err := validateAndEnableTask(&snapshot); err != nil {
				t.Fatalf("validateAndEnableTask: %v", err)
			}

			stored := loadTaskForEnableRace(t, task.ID)
			if stored.Status != model.TaskStatusEnabled {
				t.Fatalf("启用落在结算之后，库里应被拉回启用(%v)，实际 %v —— 库里禁用、定时触发却已注册", model.TaskStatusEnabled, stored.Status)
			}
			if got := global.ScheduledEntryCount(task.ID); got != 1 {
				t.Fatalf("启用后应注册 1 条定时触发，实际 ScheduledEntryCount=%d", got)
			}
			if snapshot.Status != model.TaskStatusEnabled {
				t.Fatalf("启用接口拿这份快照回响应，拉回之后快照也应是启用(%v)，实际 %v", model.TaskStatusEnabled, snapshot.Status)
			}
		})
	}
}

// 条件更新只拉回「刚被结算成禁用」的行：库里还是排队中 / 运行中时一律不碰，
// 只撤标记、重新注册，跑完由结算自然落回启用（运行中不能直接写启用的原因见 validateAndEnableTask 的注释）。
func TestValidateAndEnableTaskLeavesInFlightRowUntouched(t *testing.T) {
	global, idle := setupEnableRace(t)

	for _, inFlight := range enableRaceInFlightCases {
		t.Run(inFlight.name, func(t *testing.T) {
			task := createDisabledTaskInFlight(t, idle, "启用竞态-未结算-"+inFlight.name, inFlight.status)
			snapshot := loadTaskForEnableRace(t, task.ID)

			if err := validateAndEnableTask(&snapshot); err != nil {
				t.Fatalf("validateAndEnableTask: %v", err)
			}

			stored := loadTaskForEnableRace(t, task.ID)
			if stored.Status != inFlight.status {
				t.Fatalf("库里还是%s时启用不能改写 status，期望 %v，实际 %v", inFlight.name, inFlight.status, stored.Status)
			}
			if snapshot.Status != inFlight.status {
				t.Fatalf("快照 status 不应被改写，期望 %v，实际 %v", inFlight.status, snapshot.Status)
			}
			if service.HasPendingDisable(&stored) {
				t.Fatal("启用必须撤掉 RunNow 打的待禁用标记")
			}
			if got := global.ScheduledEntryCount(task.ID); got != 1 {
				t.Fatalf("启用后应注册 1 条定时触发，实际 ScheduledEntryCount=%d", got)
			}
			if got := service.ResolveTaskInactiveStatus(&stored); got != model.TaskStatusEnabled {
				t.Fatalf("这次跑完应结算为启用(%v)，实际 %v", model.TaskStatusEnabled, got)
			}
		})
	}
}
