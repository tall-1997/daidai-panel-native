package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	panelcron "daidai-panel/pkg/cron"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type SchedulerConfig struct {
	WorkerCount  int
	QueueSize    int
	RateInterval time.Duration
}

// ExecutionRequest.TriggerType 的取值，供调度器生产端与执行器闸门共用，避免字面量拼写漂移。
const (
	TriggerTypeCron    = "cron"
	TriggerTypeManual  = "manual"
	TriggerTypeStartup = "startup"
)

type ExecutionRequest struct {
	TaskID             uint
	Task               *model.Task
	TriggerType        string
	RetryIndex         int
	LogID              string
	TaskLogID          uint
	OperationID        string
	ScheduleInstanceID uint
	ScheduledUTC       time.Time
	CommandPlan        *CommandExecutionPlan
}

type ExecutionResult struct {
	Success  bool
	ExitCode int
	Duration float64
	Output   string
	Error    error
}

type SchedulerEventHandler interface {
	OnTaskScheduled(req *ExecutionRequest)
	OnTaskExecuting(req *ExecutionRequest) error
	OnTaskStarted(req *ExecutionRequest)
	OnTaskCompleted(req *ExecutionRequest, result *ExecutionResult)
	OnTaskFailed(req *ExecutionRequest, err error)
}

type SchedulerV2 struct {
	config       SchedulerConfig
	cron         *cron.Cron
	entryMap     map[uint][]cron.EntryID
	stopEntryMap map[uint][]cron.EntryID
	entryLock    sync.RWMutex
	taskQueue    chan *ExecutionRequest
	rateLimiter  <-chan time.Time
	stopCh       chan struct{}
	wg           sync.WaitGroup
	handler      SchedulerEventHandler
	runningTasks map[uint][]int64
	runningLock  sync.RWMutex
	stopOnce     sync.Once
	stopped      atomic.Bool
}

func NewSchedulerV2(config SchedulerConfig, handler SchedulerEventHandler) *SchedulerV2 {
	if config.WorkerCount <= 0 {
		config.WorkerCount = 4
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if config.RateInterval <= 0 {
		config.RateInterval = 200 * time.Millisecond
	}

	s := &SchedulerV2{
		config:       config,
		cron:         cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		entryMap:     make(map[uint][]cron.EntryID),
		stopEntryMap: make(map[uint][]cron.EntryID),
		taskQueue:    make(chan *ExecutionRequest, config.QueueSize),
		rateLimiter:  time.Tick(config.RateInterval),
		stopCh:       make(chan struct{}),
		handler:      handler,
		runningTasks: make(map[uint][]int64),
	}

	return s
}

func (s *SchedulerV2) Start() {
	for i := 0; i < s.config.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	s.cron.Start()
	log.Printf("scheduler v2 started: %d workers, queue size %d", s.config.WorkerCount, s.config.QueueSize)
}

func (s *SchedulerV2) Stop() {
	if s == nil {
		return
	}

	s.stopOnce.Do(func() {
		s.stopped.Store(true)

		if s.cron != nil {
			ctx := s.cron.Stop()
			<-ctx.Done()
		}

		if s.stopCh != nil {
			close(s.stopCh)
		}

		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			log.Println("scheduler v2 stop timed out; continuing shutdown")
		}

		log.Println("scheduler v2 stopped")
	})
}

func (s *SchedulerV2) worker(id int) {
	defer s.wg.Done()

	for {
		if s.stopped.Load() {
			return
		}

		select {
		case <-s.stopCh:
			return
		case req := <-s.taskQueue:
			if s.stopped.Load() {
				return
			}
			select {
			case <-s.stopCh:
				return
			case <-s.rateLimiter:
			}
			if s.stopped.Load() {
				return
			}
			s.executeTask(req)
		}
	}
}

func (s *SchedulerV2) executeTask(req *ExecutionRequest) {
	if !s.prepareConcurrency(req) {
		log.Printf("task %d: concurrency limit reached, skipping", req.TaskID)
		if req.ScheduleInstanceID != 0 {
			_ = markScheduleInstanceSkipped(req.ScheduleInstanceID, "policy_skip_running")
		}
		return
	}

	goid := getGoroutineID()
	s.addRunningTask(req.TaskID, goid)
	defer s.removeRunningTask(req.TaskID, goid)

	if s.handler != nil {
		s.handler.OnTaskScheduled(req)
	}

	if s.handler == nil {
		return
	}
	err := s.handler.OnTaskExecuting(req)
	if err != nil {
		if s.handler != nil {
			s.handler.OnTaskFailed(req, err)
		}
		return
	}

	if s.handler != nil {
		s.handler.OnTaskStarted(req)
	}
}

func (s *SchedulerV2) prepareConcurrency(req *ExecutionRequest) bool {
	if req == nil || req.Task == nil {
		return false
	}
	policy := req.Task.EffectiveSchedulePolicy()
	if req.TriggerType != TriggerTypeCron || policy == model.SchedulePolicyParallel {
		return true
	}
	if policy == model.SchedulePolicyQueue {
		for s.isTaskRunning(req.TaskID) {
			if s.stopped.Load() {
				return false
			}
			select {
			case <-s.stopCh:
				return false
			case <-time.After(500 * time.Millisecond):
			}
		}
		return true
	}
	return !s.isTaskRunning(req.TaskID)
}

func (s *SchedulerV2) isTaskRunning(taskID uint) bool {
	s.runningLock.RLock()
	goids, exists := s.runningTasks[taskID]
	running := exists && len(goids) > 0
	s.runningLock.RUnlock()
	if running {
		return true
	}
	var task model.Task
	if err := database.DB.Select("status").First(&task, taskID).Error; err != nil {
		return false
	}
	return task.Status == model.TaskStatusRunning
}

func (s *SchedulerV2) addRunningTask(taskID uint, goid int64) {
	s.runningLock.Lock()
	defer s.runningLock.Unlock()

	if s.runningTasks[taskID] == nil {
		s.runningTasks[taskID] = []int64{}
	}
	s.runningTasks[taskID] = append(s.runningTasks[taskID], goid)
}

func (s *SchedulerV2) removeRunningTask(taskID uint, goid int64) {
	s.runningLock.Lock()
	defer s.runningLock.Unlock()

	goids := s.runningTasks[taskID]
	for i, id := range goids {
		if id == goid {
			s.runningTasks[taskID] = append(goids[:i], goids[i+1:]...)
			break
		}
	}

	if len(s.runningTasks[taskID]) == 0 {
		delete(s.runningTasks, taskID)
	}
}

func (s *SchedulerV2) Enqueue(req *ExecutionRequest) error {
	if s == nil || s.stopped.Load() {
		return fmt.Errorf("scheduler stopped")
	}

	select {
	case s.taskQueue <- req:
		return nil
	default:
		return fmt.Errorf("task queue is full")
	}
}

func (s *SchedulerV2) EnqueueDelayed(delay time.Duration, reqFunc func() *ExecutionRequest) {
	go func() {
		time.Sleep(delay)
		req := reqFunc()
		if req != nil {
			s.Enqueue(req)
		}
	}()
}

func (s *SchedulerV2) AddJob(task *model.Task) error {
	s.entryLock.Lock()
	defer s.entryLock.Unlock()

	if oldIDs, exists := s.entryMap[task.ID]; exists {
		for _, oldID := range oldIDs {
			if oldID != 0 {
				s.cron.Remove(oldID)
			}
		}
		delete(s.entryMap, task.ID)
	}

	if task.Status != model.TaskStatusEnabled {
		return nil
	}
	if !task.UsesCronSchedule() {
		s.entryMap[task.ID] = []cron.EntryID{}
		return nil
	}

	expressions := panelcron.SplitExpressions(task.CronExpression)
	if len(expressions) == 0 {
		return fmt.Errorf("invalid cron expression")
	}

	taskID := task.ID
	entryIDs := make([]cron.EntryID, 0, len(expressions))
	for _, expression := range expressions {
		expression := expression
		schedule, err := panelcron.ParseSchedule(expression)
		if err != nil {
			for _, entryID := range entryIDs {
				s.cron.Remove(entryID)
			}
			return fmt.Errorf("invalid cron expression: %w", err)
		}

		entryID := s.cron.Schedule(schedule, cron.FuncJob(func() {
			if _, err := s.enqueueScheduleInstance(taskID, expression, time.Now()); err != nil {
				log.Printf("task %d enqueue failed: %v", taskID, err)
				return
			}
		}))
		entryIDs = append(entryIDs, entryID)
	}

	s.entryMap[task.ID] = entryIDs

	if oldStopIDs, exists := s.stopEntryMap[task.ID]; exists {
		for _, id := range oldStopIDs {
			if id != 0 {
				s.cron.Remove(id)
			}
		}
		delete(s.stopEntryMap, task.ID)
	}
	if task.StopSchedule != "" {
		stopExprs := panelcron.SplitExpressions(task.StopSchedule)
		stopIDs := make([]cron.EntryID, 0, len(stopExprs))
		for _, expr := range stopExprs {
			schedule, err := panelcron.ParseSchedule(expr)
			if err != nil {
				continue
			}
			stopID := s.cron.Schedule(schedule, cron.FuncJob(func() {
				s.stopTaskBySchedule(taskID)
			}))
			stopIDs = append(stopIDs, stopID)
		}
		if len(stopIDs) > 0 {
			s.stopEntryMap[task.ID] = stopIDs
		}
	}

	return nil
}

func scheduleExpressionHash(expression string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(expression)))
	return hex.EncodeToString(sum[:])
}

func (s *SchedulerV2) enqueueScheduleInstance(taskID uint, expression string, scheduledAt time.Time) (*model.ScheduleInstance, error) {
	if s == nil {
		return nil, fmt.Errorf("scheduler stopped")
	}
	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return nil, err
	}
	if task.Status != model.TaskStatusEnabled && task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusRunning {
		return nil, fmt.Errorf("task disabled")
	}

	scheduledUTC := scheduledAt.UTC().Truncate(time.Second)
	instance := &model.ScheduleInstance{
		TaskID:         taskID,
		ScheduledUTC:   scheduledUTC,
		ExpressionHash: scheduleExpressionHash(expression),
		Expression:     strings.TrimSpace(expression),
		State:          model.ScheduleInstanceStatePending,
		Policy:         task.EffectiveSchedulePolicy(),
		CreatedAt:      time.Now().UTC(),
	}
	if err := database.DB.Where("task_id = ? AND scheduled_utc = ? AND expression_hash = ?", instance.TaskID, instance.ScheduledUTC, instance.ExpressionHash).
		FirstOrCreate(instance).Error; err != nil {
		return nil, err
	}

	claimed, err := claimScheduleInstance(instance.ID)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return instance, nil
	}
	if !task.AllowMultipleInstances && task.EffectiveSchedulePolicy() == model.SchedulePolicySkip && s.isTaskRunning(taskID) {
		_ = markScheduleInstanceSkipped(claimed.ID, "policy_skip_running")
		return claimed, nil
	}

	operationID, err := NewTaskRunOperation(taskID)
	if err != nil {
		_ = markScheduleInstanceUnknown(claimed.ID, "operation_create_failed")
		return nil, err
	}
	if err := bindScheduleInstanceOperation(claimed.ID, operationID); err != nil {
		return nil, err
	}
	req := &ExecutionRequest{
		TaskID:             taskID,
		Task:               &task,
		TriggerType:        TriggerTypeCron,
		RetryIndex:         0,
		OperationID:        operationID,
		ScheduleInstanceID: claimed.ID,
		ScheduledUTC:       scheduledUTC,
	}
	if err := s.Enqueue(req); err != nil {
		_ = markScheduleInstanceUnknown(claimed.ID, "enqueue_failed")
		return nil, err
	}
	if task.Status != model.TaskStatusRunning {
		database.DB.Model(&model.Task{}).Where("id = ? AND status != ?", taskID, model.TaskStatusRunning).Update("status", model.TaskStatusQueued)
	}
	return claimed, nil
}

func claimScheduleInstance(instanceID uint) (*model.ScheduleInstance, error) {
	var claimed model.ScheduleInstance
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&model.ScheduleInstance{}).
			Where("id = ? AND state = ?", instanceID, model.ScheduleInstanceStatePending).
			Updates(map[string]interface{}{
				"state":      model.ScheduleInstanceStateLaunching,
				"claimed_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return tx.First(&claimed, instanceID).Error
	})
	if err != nil || claimed.ID == 0 {
		return nil, err
	}
	return &claimed, nil
}

func bindScheduleInstanceOperation(instanceID uint, operationID string) error {
	return database.DB.Model(&model.ScheduleInstance{}).
		Where("id = ? AND state = ?", instanceID, model.ScheduleInstanceStateLaunching).
		Updates(map[string]interface{}{"operation_id": strings.TrimSpace(operationID)}).Error
}

func markScheduleInstanceLaunched(instanceID uint) error {
	if instanceID == 0 {
		return nil
	}
	now := time.Now().UTC()
	return database.DB.Model(&model.ScheduleInstance{}).
		Where("id = ? AND state = ?", instanceID, model.ScheduleInstanceStateLaunching).
		Updates(map[string]interface{}{
			"state":      model.ScheduleInstanceStateLaunched,
			"started_at": now,
		}).Error
}

func markScheduleInstanceFinished(instanceID uint) error {
	if instanceID == 0 {
		return nil
	}
	now := time.Now().UTC()
	return database.DB.Model(&model.ScheduleInstance{}).
		Where("id = ?", instanceID).
		Updates(map[string]interface{}{"state": model.ScheduleInstanceStateLaunched, "ended_at": now}).Error
}

func markScheduleInstanceSkipped(instanceID uint, reason string) error {
	if instanceID == 0 {
		return nil
	}
	now := time.Now().UTC()
	var instance model.ScheduleInstance
	if err := database.DB.Select("task_id", "operation_id").First(&instance, instanceID).Error; err == nil && instance.OperationID != "" {
		_ = DefaultOperationStore().Cancel(instance.OperationID, reason, CurrentTaskLogCursor(instance.TaskID))
		clearTaskOperation(instance.TaskID, instance.OperationID)
	}
	return database.DB.Model(&model.ScheduleInstance{}).
		Where("id = ?", instanceID).
		Updates(map[string]interface{}{
			"state":    model.ScheduleInstanceStateSkipped,
			"reason":   strings.TrimSpace(reason),
			"ended_at": now,
		}).Error
}

func markScheduleInstanceUnknown(instanceID uint, reason string) error {
	if instanceID == 0 {
		return nil
	}
	now := time.Now().UTC()
	var instance model.ScheduleInstance
	if err := database.DB.Select("task_id", "operation_id").First(&instance, instanceID).Error; err == nil && instance.OperationID != "" {
		_ = DefaultOperationStore().Unknown(instance.OperationID, reason, CurrentTaskLogCursor(instance.TaskID))
		clearTaskOperation(instance.TaskID, instance.OperationID)
	}
	return database.DB.Model(&model.ScheduleInstance{}).
		Where("id = ?", instanceID).
		Updates(map[string]interface{}{
			"state":    model.ScheduleInstanceStateResultUnknown,
			"reason":   strings.TrimSpace(reason),
			"ended_at": now,
		}).Error
}

func RecoverLaunchingScheduleInstances() int64 {
	if database.DB == nil {
		return 0
	}
	var instances []model.ScheduleInstance
	if err := database.DB.Where("state = ?", model.ScheduleInstanceStateLaunching).Find(&instances).Error; err != nil {
		return 0
	}
	var running []model.ScheduleInstance
	if err := database.DB.Where("state = ? AND ended_at IS NULL", model.ScheduleInstanceStateLaunched).Find(&running).Error; err == nil {
		instances = append(instances, running...)
	}
	var count int64
	for i := range instances {
		if err := markScheduleInstanceUnknown(instances[i].ID, "result_unknown_after_restart"); err == nil {
			count++
		}
	}
	return count
}

func (s *SchedulerV2) EnqueueRecentMissedSchedules(window time.Duration) int {
	if s == nil || window <= 0 {
		return 0
	}
	now := time.Now()
	from := now.Add(-window)
	var tasks []model.Task
	database.DB.Where("status = ? AND task_type = ?", model.TaskStatusEnabled, model.TaskTypeCron).Find(&tasks)
	compensated := 0
	for i := range tasks {
		task := tasks[i]
		for _, expression := range panelcron.SplitExpressions(task.CronExpression) {
			schedule, err := panelcron.ParseSchedule(expression)
			if err != nil {
				continue
			}
			misses := make([]time.Time, 0, 4)
			cursor := from.Add(-time.Second)
			for {
				next := schedule.Next(cursor)
				if next.IsZero() || next.After(now) {
					break
				}
				if !next.Before(from) {
					misses = append(misses, next)
				}
				cursor = next
				if len(misses) > 512 {
					break
				}
			}
			if len(misses) == 0 {
				continue
			}
			for _, missed := range misses[:len(misses)-1] {
				_ = createMissedScheduleRecord(task.ID, expression, missed, task.EffectiveSchedulePolicy(), "older_miss_recorded")
			}
			if _, err := s.enqueueScheduleInstance(task.ID, expression, misses[len(misses)-1]); err == nil {
				compensated++
			}
		}
	}
	return compensated
}

func createMissedScheduleRecord(taskID uint, expression string, scheduledAt time.Time, policy, reason string) error {
	instance := &model.ScheduleInstance{
		TaskID:         taskID,
		ScheduledUTC:   scheduledAt.UTC().Truncate(time.Second),
		ExpressionHash: scheduleExpressionHash(expression),
		Expression:     strings.TrimSpace(expression),
		State:          model.ScheduleInstanceStateSkipped,
		Policy:         model.NormalizeSchedulePolicy(policy),
		Reason:         strings.TrimSpace(reason),
		CreatedAt:      time.Now().UTC(),
	}
	return database.DB.Where("task_id = ? AND scheduled_utc = ? AND expression_hash = ?", instance.TaskID, instance.ScheduledUTC, instance.ExpressionHash).
		FirstOrCreate(instance).Error
}

func (s *SchedulerV2) stopTaskBySchedule(taskID uint) {
	executor := GetTaskExecutor()
	if executor != nil {
		executor.StopTask(taskID)
	}

	var task model.Task
	if database.DB.First(&task, taskID).Error != nil {
		return
	}
	if task.PID != nil && *task.PID > 0 {
		// 定时停止按 PID 兜底时也要先打停止标记，避免被结算成普通脚本失败。
		markManualStop(taskID)
		KillProcessByPid(*task.PID)
	}
	if task.Status == model.TaskStatusRunning {
		inactiveStatus := ResolveTaskInactiveStatus(&task)
		database.DB.Model(&task).Updates(map[string]interface{}{
			"status":          inactiveStatus,
			"last_run_status": model.RunAborted,
			"pid":             nil,
			"log_path":        nil,
		})
		stopLogStatus := model.LogStatusAborted
		var runningLog model.TaskLog
		if err := database.DB.Where("task_id = ? AND status = ?", taskID, model.LogStatusRunning).
			Order("started_at DESC").First(&runningLog).Error; err == nil {
			now := time.Now()
			duration := now.Sub(runningLog.StartedAt).Seconds()
			if duration < 0 {
				duration = 0
			}
			// 定时停止也先把运行中日志收口；如果执行器随后完成，会按 Aborted 同一口径再次写入，不会冲突。
			database.DB.Model(&runningLog).Updates(map[string]interface{}{
				"status":   stopLogStatus,
				"ended_at": now,
				"duration": duration,
			})
			// 如果执行器没有机会回写，任务详情也能立刻看到“已终止”和本次耗时。
			database.DB.Model(&task).Updates(map[string]interface{}{
				"last_run_status":   model.RunAborted,
				"last_running_time": duration,
			})
		}
		log.Printf("task %d stopped by scheduled stop rule", taskID)
	}
}

func (s *SchedulerV2) UpdateJob(task *model.Task) error {
	return s.AddJob(task)
}

func (s *SchedulerV2) RemoveJob(taskID uint) {
	s.entryLock.Lock()
	defer s.entryLock.Unlock()

	if entryIDs, exists := s.entryMap[taskID]; exists {
		for _, entryID := range entryIDs {
			if entryID != 0 {
				s.cron.Remove(entryID)
			}
		}
		delete(s.entryMap, taskID)
	}
	if stopIDs, exists := s.stopEntryMap[taskID]; exists {
		for _, id := range stopIDs {
			if id != 0 {
				s.cron.Remove(id)
			}
		}
		delete(s.stopEntryMap, taskID)
	}
}

func (s *SchedulerV2) HasJob(taskID uint) bool {
	if s == nil {
		return false
	}

	s.entryLock.RLock()
	defer s.entryLock.RUnlock()

	_, exists := s.entryMap[taskID]
	return exists
}

func (s *SchedulerV2) RunNow(taskID uint) error {
	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return err
	}

	req := &ExecutionRequest{
		TaskID:      taskID,
		Task:        &task,
		TriggerType: TriggerTypeManual,
		RetryIndex:  0,
	}

	if err := s.Enqueue(req); err != nil {
		return err
	}

	database.DB.Model(&model.Task{}).Where("id = ? AND status != ?", taskID, model.TaskStatusRunning).Update("status", model.TaskStatusQueued)
	return nil
}

func (s *SchedulerV2) GetQueueLength() int {
	return len(s.taskQueue)
}

func (s *SchedulerV2) GetRunningCount() int {
	s.runningLock.RLock()
	defer s.runningLock.RUnlock()

	count := 0
	for _, goids := range s.runningTasks {
		count += len(goids)
	}
	return count
}

func (s *SchedulerV2) GetHandler() SchedulerEventHandler {
	return s.handler
}

func (s *SchedulerV2) EnqueueStartupTasks() int {
	if s == nil {
		return 0
	}

	today := time.Now().Format("2006-01-02")
	var tasks []model.Task
	database.DB.
		Where("status = ? AND task_type = ?", model.TaskStatusEnabled, model.TaskTypeStartup).
		// 开机运行任务按“自动触发日期”限流：同一天只自动入队一次，手动运行走 RunNow，不受这里影响。
		Where("last_startup_auto_run_date IS NULL OR last_startup_auto_run_date <> ?", today).
		Order("sort_order ASC, created_at ASC, id ASC").
		Find(&tasks)

	count := 0
	for i := range tasks {
		task := tasks[i]
		req := &ExecutionRequest{
			TaskID:      task.ID,
			Task:        &task,
			TriggerType: TriggerTypeStartup,
			RetryIndex:  0,
		}
		if err := s.Enqueue(req); err != nil {
			log.Printf("startup task %d enqueue failed: %v", task.ID, err)
			continue
		}

		updates := map[string]interface{}{
			"last_startup_auto_run_date": today,
		}
		if task.Status != model.TaskStatusRunning {
			updates["status"] = model.TaskStatusQueued
		}
		if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			log.Printf("startup task %d mark auto run date failed: %v", task.ID, err)
		}
		count++
	}

	return count
}

func (s *SchedulerV2) ReloadAllJobs() {
	s.entryLock.Lock()
	for taskID, entryIDs := range s.entryMap {
		for _, entryID := range entryIDs {
			if entryID != 0 {
				s.cron.Remove(entryID)
			}
		}
		delete(s.entryMap, taskID)
	}
	s.entryLock.Unlock()

	var tasks []model.Task
	database.DB.Where("status = ?", model.TaskStatusEnabled).Find(&tasks)

	for i := range tasks {
		if err := s.AddJob(&tasks[i]); err != nil {
			log.Printf("reload job failed for task %d: %v", tasks[i].ID, err)
		}
	}

	log.Printf("scheduler reloaded: %d jobs", len(tasks))
}

func getGoroutineID() int64 {
	return time.Now().UnixNano()
}
