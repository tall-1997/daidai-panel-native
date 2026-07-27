package handler

import (
	"daidai-panel/model"
	"daidai-panel/service"
)

type TaskHandler struct {
	schedulerEffects bool
}

var addTaskToScheduler = func(task *model.Task) {
	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		_ = scheduler.AddJob(task)
	}
}

var updateTaskInScheduler = func(task *model.Task) {
	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		scheduler.UpdateJob(task)
	}
}

var removeTaskFromScheduler = func(taskID uint) {
	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		scheduler.RemoveJob(taskID)
	}
}

func NewTaskHandler() *TaskHandler {
	return &TaskHandler{schedulerEffects: true}
}

func NewManagementTaskHandler() *TaskHandler {
	return &TaskHandler{}
}
