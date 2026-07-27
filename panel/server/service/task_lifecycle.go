package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/pathutil"

	"gorm.io/gorm"
)

type taskStatusResolver func(*model.Task) float64

func MarkActiveTasksInterrupted(reason string) int {
	return markActiveTasksInterrupted(reason, ResolveTaskInactiveStatus)
}

func RecoverAbandonedActiveTasks(reason string) int {
	return markActiveTasksInterrupted(reason, ResolveTaskRecoveredStatus)
}

func ResolveTaskRecoveredStatus(task *model.Task) float64 {
	if task == nil {
		return model.TaskStatusEnabled
	}
	if task.Status == model.TaskStatusDisabled {
		return model.TaskStatusDisabled
	}
	if task.Status == model.TaskStatusEnabled {
		return model.TaskStatusEnabled
	}

	if scheduler := GetSchedulerV2(); scheduler != nil && scheduler.HasJob(task.ID) {
		return model.TaskStatusEnabled
	}

	switch task.GetTaskType() {
	case model.TaskTypeCron, model.TaskTypeManual, model.TaskTypeStartup:
		return model.TaskStatusEnabled
	default:
		return model.TaskStatusEnabled
	}
}

func markActiveTasksInterrupted(reason string, resolve taskStatusResolver) int {
	if strings.TrimSpace(reason) == "" {
		reason = "任务已被面板中断"
	}
	if resolve == nil {
		resolve = ResolveTaskInactiveStatus
	}

	var tasks []model.Task
	if err := database.DB.
		Where("status IN ?", []float64{model.TaskStatusQueued, model.TaskStatusRunning}).
		Find(&tasks).Error; err != nil {
		log.Printf("query active tasks for interrupt cleanup failed: %v", err)
		return 0
	}

	now := time.Now()
	failedRunStatus := model.RunFailed
	interruptedCount := 0
	for i := range tasks {
		task := &tasks[i]
		duration := interruptedTaskDuration(task, now)
		targetStatus := resolve(task)

		if err := database.DB.Model(task).Updates(map[string]interface{}{
			"status":            targetStatus,
			"last_run_status":   failedRunStatus,
			"last_running_time": duration,
			"pid":               gorm.Expr("NULL"),
			"log_path":          gorm.Expr("NULL"),
		}).Error; err != nil {
			log.Printf("mark task %d interrupted failed: %v", task.ID, err)
			continue
		}

		markTaskLogInterrupted(task, reason, now, duration)
		interruptedCount++
	}

	return interruptedCount
}

func interruptedTaskDuration(task *model.Task, now time.Time) float64 {
	if task == nil || task.LastRunAt == nil {
		return 0
	}
	duration := now.Sub(*task.LastRunAt).Seconds()
	if duration < 0 {
		return 0
	}
	return duration
}

func markTaskLogInterrupted(task *model.Task, reason string, now time.Time, duration float64) {
	if task == nil {
		return
	}

	failedLogStatus := model.LogStatusFailed
	message := fmt.Sprintf("=== 任务中断 [%s] ===\n%s\n", now.Format("2006-01-02 15:04:05"), reason)

	var runningLog model.TaskLog
	if err := database.DB.
		Where("task_id = ? AND status = ?", task.ID, model.LogStatusRunning).
		Order("started_at DESC").
		First(&runningLog).Error; err == nil {
		updates := map[string]interface{}{
			"status":   failedLogStatus,
			"ended_at": now,
			"duration": duration,
		}
		if strings.TrimSpace(runningLog.Content) == "" {
			updates["content"] = message
		} else {
			updates["content"] = strings.TrimRight(runningLog.Content, "\n") + "\n" + message
		}
		if err := database.DB.Model(&runningLog).Updates(updates).Error; err != nil {
			log.Printf("mark task log %d interrupted failed: %v", runningLog.ID, err)
		}
		if runningLog.LogPath != nil {
			appendTaskInterruptLogFile(*runningLog.LogPath, message)
		}
		return
	}

	startedAt := now
	if task.LastRunAt != nil {
		startedAt = *task.LastRunAt
	}
	taskLog := &model.TaskLog{
		TaskID:    task.ID,
		Content:   message,
		Status:    &failedLogStatus,
		Duration:  &duration,
		StartedAt: startedAt,
		EndedAt:   &now,
	}
	if err := database.DB.Create(taskLog).Error; err != nil {
		log.Printf("create interrupted task log for task %d failed: %v", task.ID, err)
	}
}

func appendTaskInterruptLogFile(relLogPath string, content string) {
	relLogPath = strings.TrimSpace(relLogPath)
	if relLogPath == "" || strings.TrimSpace(config.C.Data.LogDir) == "" {
		return
	}

	fullPath := filepath.Join(config.C.Data.LogDir, relLogPath)
	absPath, err := pathutil.ResolveWithinBase(config.C.Data.LogDir, fullPath, false)
	if err != nil {
		log.Printf("resolve interrupted task log path failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		log.Printf("create interrupted task log dir failed: %v", err)
		return
	}
	file, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("open interrupted task log file failed: %v", err)
		return
	}
	defer file.Close()
	if _, err := file.WriteString("\n" + content); err != nil {
		log.Printf("write interrupted task log file failed: %v", err)
	}
}
