package service

import (
	"sync"
	"time"

	"daidai-panel/model"
)

// manualStopMarks 记录被主动停止过的任务 ID。
//
// 手动停止、定时停止或孤儿 PID 兜底停止，必须在杀进程之前打标记，
// 这样任务完成结算块运行时标记已可见，可把本次运行结算为 Aborted。
// key: taskID(uint) -> struct{}{}
var manualStopMarks sync.Map

// markManualStop 标记某任务本次运行被主动停止。
//
// 必须在杀进程之前调用，保证完成块运行时标记可见；重复标记安全（幂等）。
func markManualStop(taskID uint) {
	manualStopMarks.Store(taskID, struct{}{})
}

// MarkManualStop 是 markManualStop 的导出包装，供 handler 等其他包跨包调用。
func MarkManualStop(taskID uint) {
	markManualStop(taskID)
}

// consumeManualStop 读取并清除某任务的手动停止标记（读即清，LoadAndDelete 语义）。
//
// 返回 true 表示本次运行是被主动停止的。读即清保证幂等、不残留：
// 自然完成（未打标记）的任务消费时返回 false，行为完全不变。
func consumeManualStop(taskID uint) bool {
	_, ok := manualStopMarks.LoadAndDelete(taskID)
	return ok
}

// pendingDisableMarks 记录「在运行中被禁用、等这一次跑完才真正生效」的任务 ID。
//
// 禁用一个正在运行的任务时，面板刻意不把 status 直接写成已禁用（否则列表会立刻显示成禁用、
// 停止按钮也没了，而进程其实还在跑），只是先把 cron entry 摘掉，等本次执行结算时再落成禁用。
// 这个「等一下再生效」的意图原来是靠 ResolveTaskInactiveStatus 里的 !HasJob 反推的，
// 但 HasJob 表达的是「调度器里有没有这条任务」，不等于「用户想禁用它」——
// 中途只要有任何一次 AddJob（比如用户顺手编辑保存了一下）把 entry 加回来，这个意图就丢了。
// 所以显式记一笔，语义就不再依赖别的状态。
// key: taskID(uint) -> 打标记的时刻(time.Time)
//
// 存时刻而不是 struct{}{}，是为了防「任务 id 被复用」：标记只活在内存里，
// 而 SQLite 的自增 id 在删掉最大那行之后是会被下一条记录重新用上的。
// 万一某个任务被标记后连着被删掉，光看 id 的话，新建的同 id 任务会平白继承一个禁用意图。
// 加上「标记时刻不早于任务的创建时刻」这条判据，复用出来的新任务就一定不会命中：
// 顺序一定是「先打标记 → 再删任务 → 再建新任务」，新任务的创建时刻只会更晚。
var pendingDisableMarks sync.Map

// MarkPendingDisable 标记某任务「本次执行结束后落成禁用」。重复标记安全（幂等）。
func MarkPendingDisable(taskID uint) {
	pendingDisableMarks.Store(taskID, time.Now())
}

// ClearPendingDisable 撤销待生效的禁用意图，用户重新启用任务时调用。
// 这是这个标记唯一的清除入口。
func ClearPendingDisable(taskID uint) {
	pendingDisableMarks.Delete(taskID)
}

// hasPendingDisable 只读不清。
//
// 刻意不做成手动停止那样的「读即清」：ResolveTaskInactiveStatus 会被好几处调用，
// 其中 releaseQueuedTaskStatus 只是先算一个目标状态、最后未必真的写回去
// （它的 UPDATE 带 `status = 排队中` 条件）。读即清的话，那一次白算就会把用户的禁用意图吃掉，
// 等这次执行真正结束时标记已经没了，任务又变回启用。
// 这个标记表达的是「用户已经要求禁用」，本来也应该一直有效到用户重新启用为止。
func hasPendingDisable(task *model.Task) bool {
	if task == nil || task.ID == 0 {
		return false
	}
	value, ok := pendingDisableMarks.Load(task.ID)
	if !ok {
		return false
	}
	markedAt, ok := value.(time.Time)
	if !ok {
		return false
	}
	// 标记严格早于任务的创建时刻，说明这个 id 是复用来的，标记属于上一个任务，不能算数。
	//
	// 判据刻意是「不早于」而不是「晚于」：time.Now() 的墙钟粒度在 Windows 上是百微秒级，
	// 「建任务 → 打标记」之间不足一个 tick 时，两个时刻会完全相等。要求严格晚于的话，
	// 相等这一档会被判成 id 复用，用户的禁用意图被静默丢掉（本机实测偶发 3/100）。
	// 相等算命中并不会削弱防复用：id 复用的新任务是「先有标记、再删任务、再新建」才出现的，
	// 它的创建时刻必然晚于那笔标记，只有整套删建都挤进同一个 tick 才可能误继承。
	return !task.CreatedAt.IsZero() && !markedAt.Before(task.CreatedAt)
}

// HasPendingDisable 是 hasPendingDisable 的导出包装，供 handler 跨包调用。
// 任务列表用它避免对「刚被禁用、还在跑完最后一次」的任务误报「未注册到调度器」——
// 那种情况下条目确实被摘掉了，但那正是用户要的，不是故障。
func HasPendingDisable(task *model.Task) bool {
	return hasPendingDisable(task)
}

// applyManualStopOverride 在任务完成结算时应用主动停止结算规则。
//
// 它消费一次停止标记（读即清）：
//   - 命中标记：强制写入 Aborted，调用方据此发送终止通知、跳过成功/失败通知；
//   - 未命中标记：原样返回传入状态，自然成功/失败仍按原逻辑处理。
func applyManualStopOverride(taskID uint, runStatus, logStatus int) (finalRun int, finalLog int, aborted bool) {
	if !consumeManualStop(taskID) {
		return runStatus, logStatus, false
	}
	return model.RunAborted, model.LogStatusAborted, true
}
