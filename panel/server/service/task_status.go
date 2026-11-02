package service

import "daidai-panel/model"

// pendingDisableInEffect 判断「待禁用」标记对这条任务此刻是否作数。
//
// 标记只在排队中(0.5) / 运行中(2) 这两个瞬时态里才有意义：这时 status 说明不了用户的开关意图，只能看标记。
// 任务一旦落回明确的启用(1) / 禁用(0)，就以 status 本身为准、不再看标记——
// 标记只活在内存里，个别路径不会清它（例如 releaseQueuedTaskStatus 那种「算了未必写回」的），
// 残留的标记要是对明确状态也作数，一条已经被写回启用的任务会在下一次 AddJob 时被静默跳过注册。
func pendingDisableInEffect(task *model.Task) bool {
	if task == nil {
		return false
	}
	if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusRunning {
		return false
	}
	return hasPendingDisable(task)
}

func ResolveTaskInactiveStatus(task *model.Task) float64 {
	if task == nil {
		return model.TaskStatusEnabled
	}

	if task.Status == model.TaskStatusDisabled {
		return model.TaskStatusDisabled
	}
	if task.Status == model.TaskStatusEnabled {
		return model.TaskStatusEnabled
	}

	if task.Status == model.TaskStatusQueued {
		// 排队中同样认「待禁用」标记（issue #133）：禁用中的任务被手动运行时，RunNow 会在入队前打这笔标记，
		// 排队期间被停止 / 放弃执行，也必须落回禁用，不能像原来那样一律回到启用。
		// 刻意不接下面 !HasJob 的兜底：排队中原来一律回到启用，这里只补「有明确意图」这一种；
		// 启用任务万一恰好没注册上（例如定时规则解析失败），套兜底会被静默结算成禁用。
		if pendingDisableInEffect(task) {
			return model.TaskStatusDisabled
		}
		return model.TaskStatusEnabled
	}

	if task.Status == model.TaskStatusRunning {
		// 「运行中被禁用」的意图以显式标记为准，一直有效到用户重新启用（见 hasPendingDisable 的注释）。
		// 禁用中的任务被手动运行时，RunNow 打的也是这笔标记。
		if pendingDisableInEffect(task) {
			return model.TaskStatusDisabled
		}
		// 兜底：标记只活在内存里，面板中途重启就没了；这时仍按「调度器里已经没有这条任务」反推。
		// 注意它不是可靠判据——任务被重新注册过就会失效，所以只能放在标记之后当兜底。
		scheduler := GetSchedulerV2()
		if scheduler != nil && !scheduler.HasJob(task.ID) {
			return model.TaskStatusDisabled
		}
	}

	return model.TaskStatusEnabled
}

// ResolveTaskEnabledSwitch 回答「这条任务的启用开关此刻是开还是关」，与运行态无关（issue #133，契约 C1 的 enabled）。
//
// status 一个字段同时装着两件事：启用开关（0 / 1）和运行态（排队 0.5 / 运行 2）。
// 禁用中的任务被手动运行时，status 同样会走到 0.5 / 2，和启用任务一模一样，
// 前端按 status 猜开关，就会把「运行中的禁用任务」显示成已启用、菜单第一项变成「禁用」。
//
// 刻意不复用 ResolveTaskInactiveStatus：那是「本次执行结束后该落成什么」的结算函数，
// 被好几条结算路径共用，而且对排队中不接 !HasJob 兜底（理由见上），口径不该为了展示去改。
func ResolveTaskEnabledSwitch(task *model.Task) bool {
	if task == nil {
		return false
	}
	switch task.Status {
	case model.TaskStatusDisabled:
		return false
	case model.TaskStatusQueued, model.TaskStatusRunning:
		if pendingDisableInEffect(task) {
			return false
		}
		// 标记只活在当前进程里：`ddp task run` 在另一个进程里打的标记这里看不见，面板重启后标记也没了。
		// 这时按「调度器里没有这条任务」反推（禁用任务本来就不在调度器里），与 ResolveTaskInactiveStatus 同一个兜底。
		// 调度器为 nil（测试、只读场景）时无从判断，按开处理，不凭空报「关」。
		if scheduler := GetSchedulerV2(); scheduler != nil && !scheduler.HasJob(task.ID) {
			return false
		}
		return true
	default:
		// 启用(1)，以及库里万一出现的其它取值：与前端的回退口径（status !== 0 即开）保持一致。
		return true
	}
}
