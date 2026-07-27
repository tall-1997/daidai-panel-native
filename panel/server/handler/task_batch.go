package handler

import (
	"fmt"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *TaskHandler) Batch(c *gin.Context) {
	var req struct {
		IDs    []uint `json:"ids" binding:"required"`
		Action string `json:"action" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	scheduler := service.GetSchedulerV2()
	count := 0

	for _, id := range req.IDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}

		switch req.Action {
		case "enable":
			if err := validateAndEnableTask(&task); err != nil {
				continue
			}
		case "disable":
			disableTaskAndRemoveSchedule(&task)
		case "delete":
			if scheduler != nil {
				scheduler.RemoveJob(id)
			}
			database.DB.Where("task_id = ?", id).Delete(&model.TaskLog{})
			database.DB.Delete(&task)
		case "run":
			if task.Status != model.TaskStatusRunning {
				if err := scheduler.RunNow(id); err == nil {
					count++
				}
				continue
			}
		case "stop":
			if task.Status == model.TaskStatusRunning {
				if executor := service.GetTaskExecutor(); executor != nil {
					executor.StopTask(id)
				}
				if task.PID != nil && *task.PID > 0 {
					// StopTask 已打标记，这里再显式标记一次覆盖按 PID 兜底场景（幂等）。
					service.MarkManualStop(id)
					service.KillProcessByPid(*task.PID)
				}
				stopLogStatus := model.LogStatusAborted
				var runningLog model.TaskLog
				if err := database.DB.Where("task_id = ? AND status = ?", id, model.LogStatusRunning).
					Order("started_at DESC").First(&runningLog).Error; err == nil {
					now := time.Now()
					duration := now.Sub(runningLog.StartedAt).Seconds()
					if duration < 0 {
						duration = 0
					}
					// 批量停止也统一标记为 Aborted，避免进入成功/失败统计。
					database.DB.Model(&runningLog).Updates(map[string]interface{}{
						"status":   stopLogStatus,
						"ended_at": now,
						"duration": duration,
					})
					database.DB.Model(&task).Updates(map[string]interface{}{
						"last_run_status":   model.RunAborted,
						"last_running_time": duration,
					})
				}
				inactiveStatus := service.ResolveTaskInactiveStatus(&task)
				database.DB.Model(&task).Updates(map[string]interface{}{
					"status":          inactiveStatus,
					"last_run_status": model.RunAborted,
					"pid":             gorm.Expr("NULL"),
					"log_path":        gorm.Expr("NULL"),
				})
			} else {
				continue
			}
		case "pin":
			database.DB.Model(&task).Update("is_pinned", true)
		case "unpin":
			database.DB.Model(&task).Update("is_pinned", false)
		}
		count++
	}

	response.Success(c, gin.H{"message": fmt.Sprintf("批量%s: %d 个任务", req.Action, count), "count": count})
}

func (h *TaskHandler) BatchEnable(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		if err := validateAndEnableTask(&task); err != nil {
			continue
		}
		count++
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已启用 %d 个任务", count), "success_count": count})
}

func (h *TaskHandler) BatchDisable(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		disableTaskAndRemoveSchedule(&task)
		count++
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已禁用 %d 个任务", count), "success_count": count})
}

func (h *TaskHandler) BatchDelete(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	scheduler := service.GetSchedulerV2()
	count := 0
	for _, id := range req.TaskIDs {
		if scheduler != nil {
			scheduler.RemoveJob(id)
		}
		database.DB.Where("task_id = ?", id).Delete(&model.TaskLog{})
		database.DB.Where("id = ?", id).Delete(&model.Task{})
		count++
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已删除 %d 个任务", count), "count": count})
}

func (h *TaskHandler) BatchRun(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	if len(req.TaskIDs) > 10 {
		response.BadRequest(c, "批量运行最多 10 个任务")
		return
	}

	scheduler := service.GetSchedulerV2()
	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		if task.Status != model.TaskStatusRunning {
			if scheduler != nil && scheduler.RunNow(id) == nil {
				count++
			}
		}
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已启动 %d 个任务", count), "count": count})
}
