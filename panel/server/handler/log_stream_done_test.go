package handler

import (
	"testing"

	"daidai-panel/model"
)

func TestStreamDoneEventRunningReconnects(t *testing.T) {
	// 运行中一律 reconnect，与「服务端有没有等过」无关：
	// 前端重连后会进入 tl != nil 的流式分支，那才是实时日志的正路。
	for _, waited := range []bool{false, true} {
		if got := streamDoneEvent(model.TaskStatusRunning, waited); got != "reconnect" {
			t.Fatalf("运行中任务应返回 reconnect（waited=%v），得到 %q", waited, got)
		}
	}
}

func TestStreamDoneEventNonRunningFinishes(t *testing.T) {
	cases := []struct {
		name   string
		status float64
	}{
		{"已禁用", model.TaskStatusDisabled},
		{"排队中", model.TaskStatusQueued},
		{"已启用空闲", model.TaskStatusEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := streamDoneEvent(tc.status, false); got != "finished" {
				t.Fatalf("非运行状态(%v)且未等待时应返回 finished，得到 %q", tc.status, got)
			}
		})
	}
}

// 这条锁的是 issue #109-1 那次优化里最容易被「顺手统一掉」的一处契约：
// 服务端只要在短轮询里真的等过，就必须回 finished-late，前端据此丢弃并行预取的那份
// latest-log 快照。合成一个 finished 会让「点运行 → 任务一两百毫秒内跑完」这条路径
// 把上一次运行的日志渲染成本次结果，而前端那道 TTL 挡不住（它比等待时长还长）。
func TestStreamDoneEventNonRunningAfterWaitingIsLate(t *testing.T) {
	for _, status := range []float64{model.TaskStatusDisabled, model.TaskStatusQueued, model.TaskStatusEnabled} {
		if got := streamDoneEvent(status, true); got != "finished-late" {
			t.Fatalf("等待过之后(status=%v)应返回 finished-late，得到 %q", status, got)
		}
	}
}
