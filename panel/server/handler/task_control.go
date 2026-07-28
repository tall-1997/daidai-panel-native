package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	panelcron "daidai-panel/pkg/cron"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func validateAndEnableTask(task *model.Task) error {
	if task == nil {
		return nil
	}

	if task.UsesCronSchedule() {
		task.CronExpression = panelcron.NormalizeExpressions(task.CronExpression)
		if err := panelcron.ValidateExpressions(task.CronExpression); err != nil {
			return err
		}
	}

	task.Status = model.TaskStatusEnabled
	if err := database.DB.Save(task).Error; err != nil {
		return err
	}
	recordTaskControlOperation("task.enable", task.ID, model.OperationStateSuccess, 0, "")

	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		if err := scheduler.AddJob(task); err != nil {
			return err
		}
	}

	return nil
}

func disableTaskAndRemoveSchedule(task *model.Task) string {
	if task == nil {
		return "已禁用"
	}

	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		scheduler.RemoveJob(task.ID)
	}

	if task.Status == model.TaskStatusRunning {
		return "已设置为禁用，当前执行结束后生效"
	}

	task.Status = model.TaskStatusDisabled
	database.DB.Save(task)
	recordTaskControlOperation("task.disable", task.ID, model.OperationStateSuccess, 0, "")
	return "已禁用"
}

func recordTaskControlOperation(kind string, taskID uint, state string, exitCode int, errorCode string) string {
	operation, err := service.DefaultOperationStore().Create(service.OperationCreateOptions{
		ID:       fmt.Sprintf("%s_%d_%d", strings.ReplaceAll(kind, ".", "_"), taskID, time.Now().UnixNano()),
		Kind:     model.OperationKindTask,
		Phase:    kind,
		Progress: 0,
	})
	if err != nil {
		return ""
	}
	_ = service.DefaultOperationStore().Start(operation.ID, kind)
	switch state {
	case model.OperationStateSuccess:
		_ = service.DefaultOperationStore().Finish(operation.ID, exitCode, 0)
	case model.OperationStateFailed:
		code := exitCode
		_ = service.DefaultOperationStore().Fail(operation.ID, &code, errorCode, 0)
	default:
		_ = service.DefaultOperationStore().Unknown(operation.ID, errorCode, 0)
	}
	return operation.ID
}

func (h *TaskHandler) Run(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		response.NotFound(c, "任务不存在")
		return
	}

	if task.Status == model.TaskStatusRunning {
		response.BadRequest(c, "任务正在运行中")
		return
	}

	operationID := recordTaskControlOperation("task.run.request", uint(taskID), model.OperationStateSuccess, 0, "")
	if err := service.GetSchedulerV2().RunNow(uint(taskID)); err != nil {
		recordTaskControlOperation("task.run.enqueue", uint(taskID), model.OperationStateFailed, 1, "enqueue_failed")
		response.Error(c, http.StatusServiceUnavailable, "任务入队失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"message": "任务已启动", "operation_id": operationID})
}

func (h *TaskHandler) Stop(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		response.NotFound(c, "任务不存在")
		return
	}

	stopped := false
	if executor := service.GetTaskExecutor(); executor != nil {
		stopped = executor.StopTask(uint(taskID))
	}
	if !stopped {
		if scheduler := service.GetScheduler(); scheduler != nil {
			stopped = scheduler.StopRunningTask(uint(taskID))
		}
	}

	if task.PID != nil && *task.PID > 0 {
		// 兜底杀孤儿 PID 前也打"手动停止"标记，覆盖进程未被内存追踪的场景。
		service.MarkManualStop(uint(taskID))
		service.KillProcessByPid(*task.PID)
	}
	recordTaskControlOperation("task.stop", uint(taskID), model.OperationStateUnknown, model.RunAborted, "aborted")

	inactiveStatus := service.ResolveTaskInactiveStatus(&task)
	abortRunStatus := model.RunAborted
	database.DB.Model(&task).Updates(map[string]interface{}{
		"status":          inactiveStatus,
		"last_run_status": abortRunStatus,
		"pid":             gorm.Expr("NULL"),
		"log_path":        gorm.Expr("NULL"),
	})

	var runningLog model.TaskLog
	if err := database.DB.Where("task_id = ? AND status = ?", taskID, model.LogStatusRunning).
		Order("started_at DESC").First(&runningLog).Error; err == nil {
		now := time.Now()
		stopLogStatus := model.LogStatusAborted
		duration := now.Sub(runningLog.StartedAt).Seconds()
		if duration < 0 {
			duration = 0
		}
		// 主动停止立即标记为 Aborted；如果执行器随后完成，会按同一口径再次写入，不会冲突。
		database.DB.Model(&runningLog).Updates(map[string]interface{}{
			"status":   stopLogStatus,
			"ended_at": now,
			"duration": duration,
		})
		database.DB.Model(&task).Updates(map[string]interface{}{
			"last_run_status":   abortRunStatus,
			"last_running_time": duration,
		})
	}

	response.Success(c, gin.H{"message": "任务已停止"})
}

func (h *TaskHandler) Enable(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		response.NotFound(c, "任务不存在")
		return
	}

	if err := validateAndEnableTask(&task); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "已启用", "data": task.ToDict()})
}

func (h *TaskHandler) Disable(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		response.NotFound(c, "任务不存在")
		return
	}

	message := disableTaskAndRemoveSchedule(&task)
	response.Success(c, gin.H{"message": message, "data": task.ToDict()})
}
