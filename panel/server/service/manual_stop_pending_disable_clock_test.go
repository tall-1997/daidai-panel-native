package service

import (
	"testing"
	"time"

	"daidai-panel/model"
)

// markedAtOf 取出某任务待禁用标记里记的那个时刻。
//
// 直接读包内的 map，是为了精确构造「建任务与打标记落在同一个时钟 tick」这种边界：
// 它在真机上确实会发生（time.Now() 的墙钟粒度是百微秒级），但靠 time.Now() 复现不稳定。
func markedAtOf(t *testing.T, taskID uint) time.Time {
	t.Helper()

	value, ok := pendingDisableMarks.Load(taskID)
	if !ok {
		t.Fatalf("任务 %d 的待禁用标记不存在", taskID)
	}
	markedAt, ok := value.(time.Time)
	if !ok {
		t.Fatalf("任务 %d 的待禁用标记应是 time.Time，实际 %T", taskID, value)
	}
	return markedAt
}

// 回归：任务创建时刻与打标记时刻相等时，禁用意图不能被丢掉。
//
// 判据原来是 markedAt.After(task.CreatedAt)，要求标记严格晚于创建。
// 而 time.Now() 的墙钟粒度是百微秒级，「建任务 → MarkPendingDisable」常常落在同一个 tick 内，
// 两个时刻完全相等 —— After 为假，标记像是不存在，用户点的「禁用」被静默吃掉：
// AddJob 会把 cron 条目重新挂回去，本次执行结算也会把任务判回启用。
// 现象是偶发的（本机 100 次隔离单跑里 3 次），排查时几乎无从下手。
func TestPendingDisableMarkSurvivesSameInstantCreation(t *testing.T) {
	withoutGlobalScheduler(t)

	const taskID uint = 92101

	MarkPendingDisable(taskID)
	t.Cleanup(func() { ClearPendingDisable(taskID) })
	markedAt := markedAtOf(t, taskID)

	// 同一个 tick：创建时刻与标记时刻是同一个值。
	sameInstant := &model.Task{ID: taskID, Status: model.TaskStatusRunning, CreatedAt: markedAt}
	if !hasPendingDisable(sameInstant) {
		t.Fatalf("创建时刻与标记时刻相等(%v)时必须认这笔待禁用标记；"+
			"判成不命中的话，用户点的禁用会被静默丢掉，任务跑完又变回启用", markedAt)
	}
	if got := ResolveTaskInactiveStatus(sameInstant); got != model.TaskStatusDisabled {
		t.Fatalf("同一个 tick 内被标记的运行中任务应结算为禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}

	// 真实路径上的任务是从库里读出来的，CreatedAt 没有单调钟读数，
	// 与标记时刻比较时会退化成纯墙钟比较，这一档同样必须命中。
	fromDatabase := &model.Task{ID: taskID, Status: model.TaskStatusRunning, CreatedAt: markedAt.Round(0)}
	if !hasPendingDisable(fromDatabase) {
		t.Fatal("从库里读出的任务（CreatedAt 不带单调钟）与标记同刻时，同样必须认这笔标记")
	}

	// 正常情况：先建任务、后点禁用，标记晚于创建。
	earlier := &model.Task{ID: taskID, Status: model.TaskStatusRunning, CreatedAt: markedAt.Add(-time.Nanosecond)}
	if !hasPendingDisable(earlier) {
		t.Fatal("标记晚于任务创建时刻时必须命中")
	}
}

// 放宽判据之后，防 id 复用这条语义不能跟着丢：
// 标记严格早于任务创建时刻，说明这个 id 是删掉旧任务后被复用的，标记属于上一个任务。
// 漏掉这条的话，新建的同 id 任务会平白继承一个禁用意图，表现为「新建的任务跑一次就自己禁用了」。
func TestPendingDisableMarkStillRejectsTaskCreatedAfterMark(t *testing.T) {
	withoutGlobalScheduler(t)

	const taskID uint = 92102

	MarkPendingDisable(taskID)
	t.Cleanup(func() { ClearPendingDisable(taskID) })
	markedAt := markedAtOf(t, taskID)

	// 只比标记晚 1 纳秒也算复用：这是判据的边界，与上面「相等仍命中」正好相邻。
	recycled := &model.Task{ID: taskID, Status: model.TaskStatusRunning, CreatedAt: markedAt.Add(time.Nanosecond)}
	if hasPendingDisable(recycled) {
		t.Fatal("创建时刻晚于标记的任务必须判成 id 复用，不能继承上一个任务的禁用意图")
	}
	if got := ResolveTaskInactiveStatus(recycled); got != model.TaskStatusEnabled {
		t.Fatalf("复用 id 的新任务跑完应结算为启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}

	// CreatedAt 留零值时一律不命中（无从判断是不是复用），与改动前一致。
	unknownCreatedAt := &model.Task{ID: taskID, Status: model.TaskStatusRunning}
	if hasPendingDisable(unknownCreatedAt) {
		t.Fatal("CreatedAt 为零值时无从判断是否 id 复用，必须不命中")
	}
}
