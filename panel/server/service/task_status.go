package service

import "daidai-panel/model"

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

	if task.Status == model.TaskStatusRunning {
		scheduler := GetSchedulerV2()
		if scheduler != nil && !scheduler.HasJob(task.ID) {
			return model.TaskStatusDisabled
		}
	}

	return model.TaskStatusEnabled
}
