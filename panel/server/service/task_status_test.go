package service

import (
	"testing"

	"daidai-panel/model"

	"github.com/robfig/cron/v3"
)

func TestResolveTaskInactiveStatus(t *testing.T) {
	previousScheduler := globalScheduler
	t.Cleanup(func() {
		globalScheduler = previousScheduler
	})

	t.Run("disabled task stays disabled", func(t *testing.T) {
		globalScheduler = nil
		task := &model.Task{ID: 1, Status: model.TaskStatusDisabled}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
			t.Fatalf("expected disabled status, got %v", got)
		}
	})

	t.Run("enabled task stays enabled", func(t *testing.T) {
		globalScheduler = nil
		task := &model.Task{ID: 2, Status: model.TaskStatusEnabled}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
			t.Fatalf("expected enabled status, got %v", got)
		}
	})

	t.Run("running task without scheduled job falls back to disabled", func(t *testing.T) {
		globalScheduler = &SchedulerV2{
			entryMap: make(map[uint][]cron.EntryID),
		}
		task := &model.Task{ID: 3, Status: model.TaskStatusRunning}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
			t.Fatalf("expected disabled status, got %v", got)
		}
	})

	t.Run("running task with scheduled job falls back to enabled", func(t *testing.T) {
		globalScheduler = &SchedulerV2{
			entryMap: map[uint][]cron.EntryID{4: {1}},
		}
		task := &model.Task{ID: 4, Status: model.TaskStatusRunning}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
			t.Fatalf("expected enabled status, got %v", got)
		}
	})

	t.Run("running enabled manual task falls back to enabled when registered", func(t *testing.T) {
		globalScheduler = &SchedulerV2{
			entryMap: map[uint][]cron.EntryID{5: {}},
		}
		task := &model.Task{ID: 5, Status: model.TaskStatusRunning, TaskType: model.TaskTypeManual}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
			t.Fatalf("expected enabled status, got %v", got)
		}
	})

	t.Run("running disabled manual task falls back to disabled when unregistered", func(t *testing.T) {
		globalScheduler = &SchedulerV2{
			entryMap: make(map[uint][]cron.EntryID),
		}
		task := &model.Task{ID: 6, Status: model.TaskStatusRunning, TaskType: model.TaskTypeManual}
		if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
			t.Fatalf("expected disabled status, got %v", got)
		}
	})
}
