package service

import (
	"testing"
	"time"

	"daidai-panel/model"
)

// withoutGlobalScheduler 把全局调度器暂时摘掉再还原。
// ResolveTaskInactiveStatus 的兜底分支会去问 GetSchedulerV2().HasJob，
// 而同一个包里别的用例可能留下一个全局调度器，会让下面这些断言变得不确定。
// 这几条用例要验的是「显式标记」这条路径，所以把兜底分支摘干净。
func withoutGlobalScheduler(t *testing.T) {
	t.Helper()
	previous := globalScheduler.Load()
	globalScheduler.Store(nil)
	t.Cleanup(func() { globalScheduler.Store(previous) })
}

// newRunningTask 造一个「已经存在了一分钟」的运行中任务。
// CreatedAt 必须给，而且要早于打标记的时刻：待禁用标记带着打标记的时间，
// 判定时要求「标记晚于任务创建」，CreatedAt 留零值的话一律不命中（见 hasPendingDisable）。
func newRunningTask(id uint) *model.Task {
	return &model.Task{
		ID:        id,
		Status:    model.TaskStatusRunning,
		CreatedAt: time.Now().Add(-time.Minute),
	}
}

// 「运行中被禁用」的意图必须以显式标记为准，不能再靠 !HasJob 反推。
//
// 背景：禁用一个正在运行的任务时，面板只摘掉 cron entry、不改 status，
// 等本次执行结算时才落成禁用。这个「等一下再生效」原来是靠 ResolveTaskInactiveStatus 里的
// !HasJob 反推的，但中途只要有任何一次 AddJob 把 entry 加回来（用户顺手编辑保存一下就会），
// 这个意图就丢了，任务跑完会变回「已启用」。
func TestResolveTaskInactiveStatusHonorsPendingDisableMark(t *testing.T) {
	withoutGlobalScheduler(t)

	task := newRunningTask(92001)

	// 没打标记时，运行中的任务结算回启用态。
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
		t.Fatalf("未标记待禁用的运行中任务应结算为启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}

	MarkPendingDisable(task.ID)
	t.Cleanup(func() { ClearPendingDisable(task.ID) })
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
		t.Fatalf("标记了待禁用的运行中任务应结算为禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}

	// 只读不清：ResolveTaskInactiveStatus 会被多处调用，其中 releaseQueuedTaskStatus 只是先算一个
	// 目标状态、最后未必真的写回去。读即清的话那一次白算就会把用户的禁用意图吃掉，
	// 等这次执行真正结束时标记已经没了，任务又变回启用。
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
		t.Fatalf("待禁用标记必须一直有效到用户重新启用，第二次结算仍应为禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}
}

// 重新启用要撤掉还没生效的禁用意图，否则会出现「刚点了启用，跑完又变回禁用」。
func TestClearPendingDisableCancelsIntent(t *testing.T) {
	withoutGlobalScheduler(t)

	task := newRunningTask(92002)

	MarkPendingDisable(task.ID)
	ClearPendingDisable(task.ID)

	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
		t.Fatalf("撤销禁用意图后应结算为启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}
}

// 标记按任务隔离，不能串到别的任务上。
func TestPendingDisableMarkIsolatedPerTask(t *testing.T) {
	withoutGlobalScheduler(t)

	taskA := newRunningTask(92003)
	taskB := newRunningTask(92004)

	MarkPendingDisable(taskA.ID)
	t.Cleanup(func() { ClearPendingDisable(taskA.ID) })

	if got := ResolveTaskInactiveStatus(taskB); got != model.TaskStatusEnabled {
		t.Fatalf("任务 B 未标记待禁用，不应被任务 A 的标记影响，实际 %v", got)
	}

	if !hasPendingDisable(taskA) {
		t.Fatalf("任务 A 应命中自己的待禁用标记")
	}
}

// 标记只活在内存里、按任务 id 存，而 SQLite 的自增 id 在删掉最大那行之后会被复用。
// 所以判定必须带上「标记时刻晚于任务创建时刻」，否则复用出 id 的新任务会平白继承一个禁用意图 ——
// 表现为「新建的任务跑完自己就变成已禁用」，而且完全查不出原因。
func TestPendingDisableMarkDoesNotLeakToRecycledTaskID(t *testing.T) {
	withoutGlobalScheduler(t)

	const recycledID uint = 92007

	// 老任务被标记了待禁用，随后连任务一起被删掉，标记留在内存里。
	MarkPendingDisable(recycledID)
	t.Cleanup(func() { ClearPendingDisable(recycledID) })

	// 新任务复用了同一个 id，它的创建时刻晚于那笔标记。
	recycled := &model.Task{
		ID:        recycledID,
		Status:    model.TaskStatusRunning,
		CreatedAt: time.Now().Add(time.Minute),
	}

	if hasPendingDisable(recycled) {
		t.Fatal("复用 id 的新任务不该命中上一个任务留下的待禁用标记")
	}
	if got := ResolveTaskInactiveStatus(recycled); got != model.TaskStatusEnabled {
		t.Fatalf("复用 id 的新任务跑完应结算为启用(%v)，实际 %v。"+
			"命中旧标记的话，用户会看到「新建的任务跑一次就自己禁用了」且查不出原因", model.TaskStatusEnabled, got)
	}
}

// 已禁用 / 已启用这两个明确状态不走待禁用判定，行为与改动前保持一致。
func TestResolveTaskInactiveStatusKeepsExplicitStatus(t *testing.T) {
	withoutGlobalScheduler(t)

	disabled := &model.Task{ID: 92005, Status: model.TaskStatusDisabled, CreatedAt: time.Now().Add(-time.Minute)}
	MarkPendingDisable(disabled.ID)
	if got := ResolveTaskInactiveStatus(disabled); got != model.TaskStatusDisabled {
		t.Fatalf("已禁用的任务应原样返回禁用，实际 %v", got)
	}
	// 标记只读不清，用完手动清掉，避免污染后续用例。
	ClearPendingDisable(disabled.ID)

	enabled := &model.Task{ID: 92006, Status: model.TaskStatusEnabled, CreatedAt: time.Now().Add(-time.Minute)}
	MarkPendingDisable(enabled.ID)
	if got := ResolveTaskInactiveStatus(enabled); got != model.TaskStatusEnabled {
		t.Fatalf("已启用的任务应原样返回启用，实际 %v", got)
	}
	ClearPendingDisable(enabled.ID)
}
