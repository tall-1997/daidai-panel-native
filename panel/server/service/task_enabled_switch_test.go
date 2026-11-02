package service

import (
	"testing"
	"time"

	"daidai-panel/model"

	"github.com/robfig/cron/v3"
)

// issue #133：启用开关位与运行态无关（契约 C1 的 enabled）。
// 禁用任务被手动运行时 status 也会走到 0.5 / 2，必须靠待禁用标记（或调度器兜底）把它认回「关」。
func TestResolveTaskEnabledSwitch(t *testing.T) {
	previousScheduler := globalScheduler.Load()
	t.Cleanup(func() { globalScheduler.Store(previousScheduler) })

	// CreatedAt 必须早于打标记的时刻，否则 hasPendingDisable 会把标记当成 id 复用留下的旧账。
	createdAt := time.Now().Add(-time.Minute)
	newTask := func(id uint, status float64) *model.Task {
		return &model.Task{ID: id, Status: status, CreatedAt: createdAt}
	}
	registered := func(id uint) *SchedulerV2 {
		return &SchedulerV2{entryMap: map[uint][]cron.EntryID{id: {1}}}
	}
	emptyScheduler := func() *SchedulerV2 {
		return &SchedulerV2{entryMap: make(map[uint][]cron.EntryID)}
	}

	cases := []struct {
		name      string
		task      *model.Task
		scheduler *SchedulerV2
		mark      bool
		want      bool
	}{
		{name: "nil 任务按关", task: nil, want: false},
		{name: "禁用是关", task: newTask(93001, model.TaskStatusDisabled), want: false},
		{name: "启用是开", task: newTask(93002, model.TaskStatusEnabled), want: true},
		{name: "启用加残留标记仍以 status 为准是开", task: newTask(93003, model.TaskStatusEnabled), mark: true, want: true},
		{name: "运行中加待禁用是关（即使还登记着）", task: newTask(93004, model.TaskStatusRunning), scheduler: registered(93004), mark: true, want: false},
		{name: "排队中加待禁用是关", task: newTask(93005, model.TaskStatusQueued), scheduler: registered(93005), mark: true, want: false},
		{name: "运行中且调度器里没有是关", task: newTask(93006, model.TaskStatusRunning), scheduler: emptyScheduler(), want: false},
		{name: "排队中且调度器里没有是关", task: newTask(93007, model.TaskStatusQueued), scheduler: emptyScheduler(), want: false},
		{name: "运行中且已注册是开", task: newTask(93008, model.TaskStatusRunning), scheduler: registered(93008), want: true},
		{name: "排队中的手动任务空登记是开", task: newTask(93009, model.TaskStatusQueued), scheduler: &SchedulerV2{entryMap: map[uint][]cron.EntryID{93009: {}}}, want: true},
		{name: "运行中且没有调度器时无从判断按开", task: newTask(93010, model.TaskStatusRunning), want: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			globalScheduler.Store(testCase.scheduler)
			if testCase.mark {
				MarkPendingDisable(testCase.task.ID)
				t.Cleanup(func() { ClearPendingDisable(testCase.task.ID) })
			}
			if got := ResolveTaskEnabledSwitch(testCase.task); got != testCase.want {
				t.Fatalf("expected enabled switch %v, got %v", testCase.want, got)
			}
		})
	}
}

// issue #133：排队中的禁用任务（RunNow 打了待禁用标记）被停止 / 放弃执行时，结算必须落回禁用。
// 排队中刻意不接 !HasJob 兜底：没有标记时照旧回到启用，行为与改动前一致。
func TestResolveTaskInactiveStatusQueuedHonorsPendingDisable(t *testing.T) {
	withoutGlobalScheduler(t)

	task := &model.Task{ID: 93101, Status: model.TaskStatusQueued, CreatedAt: time.Now().Add(-time.Minute)}
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
		t.Fatalf("未标记的排队中任务应照旧回到启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}

	MarkPendingDisable(task.ID)
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
		t.Fatalf("带待禁用标记的排队中任务应落回禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}

	ClearPendingDisable(task.ID)
	globalScheduler.Store(&SchedulerV2{entryMap: make(map[uint][]cron.EntryID)})
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
		t.Fatalf("排队中不该接 !HasJob 兜底：没有标记时即使调度器里没有它也应回到启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}
}
