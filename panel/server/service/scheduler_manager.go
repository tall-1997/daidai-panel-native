package service

import (
	"log"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

var globalScheduler *SchedulerV2
var globalExecutor *TaskExecutor

func InitSchedulerV2() {
	globalExecutor = NewTaskExecutor()
	if count := RecoverAbandonedActiveTasks("面板上次异常退出，运行中的任务已标记为中断"); count > 0 {
		log.Printf("recovered %d abandoned active task(s)", count)
	}

	workerCount := model.GetRegisteredConfigInt("max_concurrent_tasks")
	if workerCount < 1 {
		workerCount = 4
	}

	cfg := SchedulerConfig{
		WorkerCount:  workerCount,
		QueueSize:    100,
		RateInterval: 200 * time.Millisecond,
	}

	globalScheduler = NewSchedulerV2(cfg, globalExecutor)
	globalScheduler.Start()

	var tasks []model.Task
	database.DB.Where("status = ?", model.TaskStatusEnabled).Find(&tasks)

	for _, task := range tasks {
		if err := globalScheduler.AddJob(&task); err != nil {
			log.Printf("failed to add task %d: %v", task.ID, err)
		}
	}

	startupCount := globalScheduler.EnqueueStartupTasks()
	log.Printf("scheduler v2 initialized with %d tasks", len(tasks))
	if startupCount > 0 {
		log.Printf("scheduler v2 enqueued %d startup task(s)", startupCount)
	}
}

func ShutdownSchedulerV2() {
	if globalScheduler != nil {
		globalScheduler.Stop()
	}

	if globalExecutor != nil {
		killed := globalExecutor.StopAllRunningTasks()
		if killed > 0 {
			log.Printf("interrupted %d running task process(es) during panel shutdown", killed)
		}
		if ok := globalExecutor.Wait(5 * time.Second); !ok {
			log.Println("timed out waiting for running task cleanup")
		}
	}

	if count := MarkActiveTasksInterrupted("面板正在关闭或重启，任务已被中断"); count > 0 {
		log.Printf("marked %d active task(s) as interrupted during shutdown", count)
	}

	if globalScheduler != nil {
		globalScheduler = nil
	}
	globalExecutor = nil
}

func GetSchedulerV2() *SchedulerV2 {
	return globalScheduler
}

func GetTaskExecutor() *TaskExecutor {
	return globalExecutor
}
