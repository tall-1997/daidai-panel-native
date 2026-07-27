package service

import (
	"sync"

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
