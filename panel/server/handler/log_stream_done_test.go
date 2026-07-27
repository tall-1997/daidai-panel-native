package handler

import (
	"testing"

	"daidai-panel/model"
)

func TestStreamDoneEventForStatusRunningReconnects(t *testing.T) {
	if got := streamDoneEventForStatus(model.TaskStatusRunning); got != "reconnect" {
		t.Fatalf("运行中任务应返回 reconnect，得到 %q", got)
	}
}

func TestStreamDoneEventForStatusNonRunningFinishes(t *testing.T) {
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
			if got := streamDoneEventForStatus(tc.status); got != "finished" {
				t.Fatalf("非运行状态(%v)应返回 finished，得到 %q", tc.status, got)
			}
		})
	}
}
