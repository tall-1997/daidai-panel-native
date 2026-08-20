package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

var globalScheduler *SchedulerV2
var globalExecutor *TaskExecutor

type schedulerRuntime struct {
	mu      sync.Mutex
	started bool
}

var schedulerLifecycle schedulerRuntime

const maxConcurrentTasksConfigKey = "max_concurrent_tasks"
const defaultSchedulerWorkerCount = 4

func resolveSchedulerWorkerCount() int {
	workerCount := model.GetRegisteredConfigInt(maxConcurrentTasksConfigKey)
	if workerCount < 1 {
		return defaultSchedulerWorkerCount
	}
	return workerCount
}

func ApplySchedulerWorkerCount() {
	scheduler := globalScheduler
	if scheduler == nil {
		return
	}
	previous, applied := scheduler.SetWorkerCount(resolveSchedulerWorkerCount())
	if previous != applied {
		log.Printf("scheduler v2 concurrency limit updated: %d -> %d worker(s)", previous, applied)
	}
}

func StartSchedulerV2(ctx context.Context) error {
	_ = ctx
	schedulerLifecycle.mu.Lock()
	defer schedulerLifecycle.mu.Unlock()

	if schedulerLifecycle.started {
		return nil
	}

	globalExecutor = NewTaskExecutor()
	if count := RecoverAbandonedActiveTasks("面板上次异常退出，运行中的任务已标记为中断"); count > 0 {
		log.Printf("recovered %d abandoned active task(s)", count)
	}
	if count := RecoverLaunchingScheduleInstances(); count > 0 {
		log.Printf("marked %d launching schedule instance(s) as result_unknown", count)
	}

	workerCount := resolveSchedulerWorkerCount()

	cfg := SchedulerConfig{
		WorkerCount:  workerCount,
		QueueSize:    1000,
		RateInterval: 200 * time.Millisecond,
	}

	globalScheduler = NewSchedulerV2(cfg, globalExecutor)
	globalScheduler.Start()

	var tasks []model.Task
	database.DB.Where("status = ?", model.TaskStatusEnabled).Find(&tasks)

	for _, task := range tasks {
		if err := globalScheduler.AddJob(&task); err != nil {
			globalScheduler.Stop()
			globalScheduler = nil
			globalExecutor = nil
			return fmt.Errorf("failed to add task %d: %w", task.ID, err)
		}
	}
	if missed := globalScheduler.EnqueueRecentMissedSchedules(15 * time.Minute); missed > 0 {
		log.Printf("scheduler v2 compensated %d recent missed schedule(s)", missed)
	}

	startupCount := globalScheduler.EnqueueStartupTasks()
	log.Printf("scheduler v2 initialized with %d tasks", len(tasks))
	if startupCount > 0 {
		log.Printf("scheduler v2 enqueued %d startup task(s)", startupCount)
	}

	schedulerLifecycle.started = true
	return nil
}

func StopSchedulerV2(ctx context.Context) error {
	_ = ctx
	schedulerLifecycle.mu.Lock()
	defer schedulerLifecycle.mu.Unlock()

	if !schedulerLifecycle.started {
		return nil
	}

	if globalScheduler != nil {
		globalScheduler.SignalStop()
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

	if globalScheduler != nil {
		if ok := globalScheduler.WaitWorkers(5 * time.Second); !ok {
			log.Println("timed out waiting for scheduler workers to finish")
		}
	}

	if count := MarkActiveTasksInterrupted("面板正在关闭或重启，任务已被中断"); count > 0 {
		log.Printf("marked %d active task(s) as interrupted during shutdown", count)
	}

	globalScheduler = nil
	globalExecutor = nil
	schedulerLifecycle.started = false
	return nil
}

func InitSchedulerV2() {
	if err := StartSchedulerV2(context.Background()); err != nil {
		log.Printf("scheduler v2 start failed: %v", err)
	}
}

func ShutdownSchedulerV2() {
	if err := StopSchedulerV2(context.Background()); err != nil {
		log.Printf("scheduler v2 stop failed: %v", err)
	}
}

func GetSchedulerV2() *SchedulerV2 {
	return globalScheduler
}

func GetTaskExecutor() *TaskExecutor {
	return globalExecutor
}
