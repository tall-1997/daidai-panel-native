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
	// DelayResolved 标记本次请求已经完成过随机延迟判定。
	// 延迟等待通过“重新入队”实现，这个标记用于防止重新入队后再次延迟。
	DelayResolved bool

	// taskLog / tinyLog 由 OnTaskExecuting 准备、交给 RunTask 使用，只在包内流转。
	taskLog       *model.TaskLog
	tinyLog       *TinyLog
	executionDone chan struct{}
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
	// ResolveExecutionDelay 返回本次执行在占用并发槽位之前需要等待的时长（0 表示无需等待）。
	ResolveExecutionDelay(req *ExecutionRequest) time.Duration
	// OnTaskExecuting 只做执行前准备（依赖检查、解析命令、建立日志记录），不得阻塞到任务结束。
	OnTaskExecuting(req *ExecutionRequest) error
	OnTaskStarted(req *ExecutionRequest)
	// RunTask 同步执行任务直到结束（含全部重试），worker 以此实现真正的并发上限。
	RunTask(req *ExecutionRequest)
	OnTaskCompleted(req *ExecutionRequest, result *ExecutionResult)
	OnTaskFailed(req *ExecutionRequest, err error)
}

// workerResizeSignalBuffer 是「并发数被调小」唤醒信号的缓冲长度。
// 信号只是让空闲 worker 回到循环顶部重新判断一次，多发少发都不影响正确性，
// 缓冲满了直接丢弃即可，绝不能阻塞调用方。
const workerResizeSignalBuffer = 128

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

	// worker 数量可以在运行期调整，用于让「定时任务最大并发数」保存后立刻生效。
	// desiredWorkers / liveWorkers 用普通 int 加一把小锁保护，刻意不用 atomic：
	// 「超编就退休」是典型的 check-then-act，用 atomic 会让两个超编 worker 同时
	// 判定自己该退出，最终退得比该退的多。这里的操作频率极低，锁开销可以忽略。
	workerLock         sync.Mutex
	desiredWorkers     int
	liveWorkers        int
	workerSeq          int
	workersInitialized bool
	// resizeCh 负责把「并发数被调小」的消息推给空闲 worker——
	// 它们本来一直阻塞在 taskQueue 上，不叫醒就发现不了自己已经超编。
	resizeCh chan struct{}
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
		resizeCh:     make(chan struct{}, workerResizeSignalBuffer),
	}

	return s
}

func (s *SchedulerV2) Start() {
	s.workerLock.Lock()
	// 只在从未设置过时才用构造参数播种，这样「Start 之前就调过 SetWorkerCount」不会被覆盖。
	if !s.workersInitialized {
		s.workersInitialized = true
		s.desiredWorkers = s.config.WorkerCount
	}
	desired := s.desiredWorkers
	s.spawnWorkersLocked()
	s.workerLock.Unlock()

	s.cron.Start()
	log.Printf("scheduler v2 started: %d workers, queue size %d", desired, s.config.QueueSize)
}

// SetWorkerCount 在运行期调整并发上限（即 worker 数量），返回调整前后的目标值。
// 调大立刻补齐 worker；调小只是把多余的 worker 标记为超编，它们会在两次任务之间自行退出，
// 绝不会打断正在执行的任务。n < 1 会被钳到 1。
func (s *SchedulerV2) SetWorkerCount(n int) (previous int, applied int) {
	if s == nil {
		return 0, 0
	}
	if n < 1 {
		n = 1
	}

	s.workerLock.Lock()
	previous = s.desiredWorkers
	s.workersInitialized = true
	s.desiredWorkers = n
	s.spawnWorkersLocked()
	surplus := s.liveWorkers - s.desiredWorkers
	s.workerLock.Unlock()

	// 空闲 worker 阻塞在 taskQueue 上，不叫醒它们就得等到下一个任务进来才会发现自己该退休；
	// 队列长期空闲时，调小并发数会一直不生效。非阻塞发送：channel 满了也无所谓，
	// worker 回到循环顶部本来就会重新判断一次。
	for i := 0; i < surplus; i++ {
		select {
		case s.resizeCh <- struct{}{}:
		default:
		}
	}

	return previous, n
}

// GetWorkerCount 返回当前存活的 worker 数量，也就是真实生效的并发上限。
// 调小并发数之后，它会随着多余 worker 陆续收工逐步降到目标值。
func (s *SchedulerV2) GetWorkerCount() int {
	if s == nil {
		return 0
	}

	s.workerLock.Lock()
	defer s.workerLock.Unlock()
	return s.liveWorkers
}

// spawnWorkersLocked 把存活 worker 补到 desiredWorkers，必须在持有 workerLock 时调用。
func (s *SchedulerV2) spawnWorkersLocked() {
	if s.stopped.Load() {
		// 已经在关停流程里就不要再补 worker，否则可能和 WaitWorkers 里的 wg.Wait 撞车。
		return
	}

	for s.liveWorkers < s.desiredWorkers {
		s.liveWorkers++
		id := s.workerSeq
		s.workerSeq++
		s.wg.Add(1)
		go s.worker(id)
	}
}

// retireWorkerIfSurplus 在同一把锁内完成「判断自己是否超编 + 注销自己」。
// 这两步必须原子：先判断后减计数的话，多个超编 worker 会同时判定该退出，退得比该退的多。
func (s *SchedulerV2) retireWorkerIfSurplus() bool {
	s.workerLock.Lock()
	defer s.workerLock.Unlock()

	if s.liveWorkers <= s.desiredWorkers {
		return false
	}
	s.liveWorkers--
	return true
}

// releaseWorkerSlot 用于「退休」之外的退出路径（调度器关停、goroutine 异常退出）。
func (s *SchedulerV2) releaseWorkerSlot() {
	s.workerLock.Lock()
	defer s.workerLock.Unlock()

	if s.liveWorkers > 0 {
		s.liveWorkers--
	}
}

// Stop 保留为“不再接新活 + 等待 worker 收工”的组合，方便一次性关停。
// 关机流程需要在两步之间插入“中断执行中的进程”，请改用 SignalStop + WaitWorkers。
func (s *SchedulerV2) Stop() {
	if s == nil {
		return
	}

	s.SignalStop()
	if !s.WaitWorkers(5 * time.Second) {
		log.Println("scheduler v2 stop timed out; continuing shutdown")
	}
	log.Println("scheduler v2 stopped")
}

// SignalStop 停掉 cron、关闭 stopCh 并拒绝新的入队，但不等待正在执行的任务。
func (s *SchedulerV2) SignalStop() {
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
	})
}

// WaitWorkers 等待所有 worker 退出，返回是否在超时前完成。timeout <= 0 表示一直等。
func (s *SchedulerV2) WaitWorkers(timeout time.Duration) bool {
	if s == nil {
		return true
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	if timeout <= 0 {
		<-done
		return true
	}

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *SchedulerV2) worker(id int) {
	// 退休路径会在锁内自己把计数减掉，其余任何退出路径（含 panic 逃逸）都由这里兜底归还名额，
	// 保证 liveWorkers 始终等于真实存活的 worker 数。
	retired := false
	defer func() {
		if !retired {
			s.releaseWorkerSlot()
		}
		s.wg.Done()
	}()

	for {
		if s.stopped.Load() {
			return
		}
		// 退休判断只发生在「取下一个任务之前」，因此调小并发数不会打断正在执行的任务。
		if s.retireWorkerIfSurplus() {
			retired = true
			log.Printf("scheduler v2 worker %d retired: concurrency limit was lowered", id)
			return
		}

		select {
		case <-s.stopCh:
			return
		case <-s.resizeCh:
			// 并发数刚被调小：回到循环顶部重新判断自己是否超编。
			continue
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
			// 随机延迟必须在占用并发槽位之前完成，否则 max_concurrent_tasks 会被空等吃满。
			if s.deferForExecutionDelay(req) {
				continue
			}
			s.executeTask(req)
		}
	}
}

// deferForExecutionDelay 处理执行前的随机延迟：
// 需要等待时把请求交给 EnqueueDelayed 稍后重新入队，worker 立刻返回去取下一个请求，不占用并发槽位。
// 返回 true 表示本次请求已被推迟，调用方不应继续执行它。
func (s *SchedulerV2) deferForExecutionDelay(req *ExecutionRequest) bool {
	if req == nil || req.DelayResolved || s.handler == nil {
		return false
	}

	// 无论是否真的需要等待，都只判定一次，避免重新入队后反复延迟。
	req.DelayResolved = true

	delay := s.handler.ResolveExecutionDelay(req)
	if delay <= 0 {
		return false
	}

	log.Printf("task %d: delaying execution by %s before taking a concurrency slot", req.TaskID, delay)
	s.EnqueueDelayed(delay, func() *ExecutionRequest { return req })
	return true
}

// executeTask 阻塞到任务真正结束（含全部重试）才返回，worker 因此成为真实的并发闸门。
func (s *SchedulerV2) executeTask(req *ExecutionRequest) {
	if req == nil || req.Task == nil {
		return
	}

	goid, ok := s.acquireRunningSlot(req)
	if !ok {
		// 最大并发数现在靠 worker 阻塞实现，只会让请求排队、不会丢弃；
		// 走到这里的唯一原因是「不允许多实例」。日志必须说清是哪条规则，
		// 否则用户会误以为是并发数把任务吃掉了。
		log.Printf("task %d: previous run still in progress and multiple instances are disabled, skipping this trigger", req.TaskID)
		return
	}
	defer s.removeRunningTask(req.TaskID, goid)

	// worker 阻塞化之后，它不再只是“派发线程”，而是一个并发名额。
	// 准备阶段（OnTaskScheduled / OnTaskExecuting / OnTaskStarted，内含多次 DB 调用与命令解析）
	// 一旦 panic 就会打穿 worker goroutine，那个名额将永久消失且无法恢复；
	// max_concurrent_tasks=1 时等于整个调度器直接停摆，只能重启面板。
	// runTask 内部已有自己的 recover，这里兜的是它之外的所有阶段。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("task %d: scheduler stage panicked, worker recovered: %v", req.TaskID, r)
			if s.handler != nil {
				s.handler.OnTaskFailed(req, fmt.Errorf("任务调度阶段异常: %v", r))
			}
		}
	}()

	if s.handler == nil {
		return
	}

	s.handler.OnTaskScheduled(req)

	if err := s.handler.OnTaskExecuting(req); err != nil {
		s.handler.OnTaskFailed(req, err)
		return
	}

	s.handler.OnTaskStarted(req)
	s.handler.RunTask(req)
}

// acquireRunningSlot 在同一把写锁内完成“多实例检查 + 登记运行中”。
// 两步必须原子，否则两个 worker 可能同时通过检查、把不允许多实例的任务跑成两份。
// 返回的标识用于任务结束时精确移除自己的登记。
func (s *SchedulerV2) isTaskRunning(taskID uint) bool {
	s.runningLock.RLock()
	defer s.runningLock.RUnlock()
	return len(s.runningTasks[taskID]) > 0
}

func (s *SchedulerV2) acquireRunningSlot(req *ExecutionRequest) (int64, bool) {
	s.runningLock.Lock()
	defer s.runningLock.Unlock()

	if !req.Task.AllowMultipleInstances && len(s.runningTasks[req.TaskID]) > 0 {
		return 0, false
	}

	goid := getGoroutineID()
	s.runningTasks[req.TaskID] = append(s.runningTasks[req.TaskID], goid)
	return goid, true
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

// EnqueueDelayed 等待 delay 后重新入队；等待期间调度器被关停则直接放弃。
func (s *SchedulerV2) EnqueueDelayed(delay time.Duration, reqFunc func() *ExecutionRequest) {
	if s == nil || reqFunc == nil {
		return
	}

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-s.stopCh:
			return
		case <-timer.C:
		}

		req := reqFunc()
		if req == nil {
			return
		}
		if err := s.Enqueue(req); err != nil {
			log.Printf("task %d delayed enqueue failed: %v", req.TaskID, err)
			// 生产端在首次入队成功时已经把任务置为「排队中」。
			// 延迟到期后重新入队失败意味着这次触发不会再被执行，
			// 必须把状态放回去，否则任务会一直停在 queued 假象上。
			releaseQueuedTaskStatus(req)
		}
	}()
}

// releaseQueuedTaskStatus 把「不会再被执行的排队请求」对应的任务状态从 queued 放回空闲状态。
// 只更新仍然停在 queued 的行，避免覆盖任务已经被其它触发跑起来后的真实状态。
func releaseQueuedTaskStatus(req *ExecutionRequest) {
	if req == nil || req.TaskID == 0 || database.DB == nil {
		return
	}

	if err := database.DB.Model(&model.Task{}).
		Where("id = ? AND status = ?", req.TaskID, model.TaskStatusQueued).
		Update("status", ResolveTaskInactiveStatus(req.Task)).Error; err != nil {
		log.Printf("task %d reset queued status failed: %v", req.TaskID, err)
	}
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
