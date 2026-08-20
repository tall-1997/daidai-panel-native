package service

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

var errFakePrepare = errors.New("prepare failed")

// fakeSchedulerHandler 是 SchedulerEventHandler 的轻量实现：
// 只记录调度器的调用序列与并发峰值，不落库、不执行真实脚本，用于验证并发闸门本身。
type fakeSchedulerHandler struct {
	mu sync.Mutex

	delay        time.Duration
	prepareErr   error
	preparePanic any
	runDuration  time.Duration
	runFunc      func(req *ExecutionRequest)

	running     int
	peakRunning int
	runs        []fakeRunRecord
	failed      []uint
}

type fakeRunRecord struct {
	TaskID uint
	Start  time.Time
	End    time.Time
}

func newFakeSchedulerHandler(runDuration time.Duration) *fakeSchedulerHandler {
	return &fakeSchedulerHandler{runDuration: runDuration}
}

func (h *fakeSchedulerHandler) OnTaskScheduled(req *ExecutionRequest) {}

func (h *fakeSchedulerHandler) ResolveExecutionDelay(req *ExecutionRequest) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.delay
}

func (h *fakeSchedulerHandler) OnTaskExecuting(req *ExecutionRequest) error {
	h.mu.Lock()
	panicValue := h.preparePanic
	prepareErr := h.prepareErr
	h.mu.Unlock()

	if panicValue != nil {
		panic(panicValue)
	}
	return prepareErr
}

func (h *fakeSchedulerHandler) setPreparePanic(value any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.preparePanic = value
}

// waitForFailed 等待至少 want 个任务被结算为失败。
func (h *fakeSchedulerHandler) waitForFailed(t *testing.T, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.failedCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d failed task(s), got %d", want, h.failedCount())
}

func (h *fakeSchedulerHandler) OnTaskStarted(req *ExecutionRequest) {}

func (h *fakeSchedulerHandler) RunTask(req *ExecutionRequest) {
	h.mu.Lock()
	h.running++
	if h.running > h.peakRunning {
		h.peakRunning = h.running
	}
	index := len(h.runs)
	h.runs = append(h.runs, fakeRunRecord{TaskID: req.TaskID, Start: time.Now()})
	runFunc := h.runFunc
	runDuration := h.runDuration
	h.mu.Unlock()

	switch {
	case runFunc != nil:
		runFunc(req)
	case runDuration > 0:
		time.Sleep(runDuration)
	}

	h.mu.Lock()
	h.running--
	h.runs[index].End = time.Now()
	h.mu.Unlock()
}

func (h *fakeSchedulerHandler) OnTaskCompleted(req *ExecutionRequest, result *ExecutionResult) {}

func (h *fakeSchedulerHandler) OnTaskFailed(req *ExecutionRequest, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failed = append(h.failed, req.TaskID)
}

func (h *fakeSchedulerHandler) snapshot() []fakeRunRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeRunRecord(nil), h.runs...)
}

func (h *fakeSchedulerHandler) peak() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.peakRunning
}

func (h *fakeSchedulerHandler) startedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.runs)
}

func (h *fakeSchedulerHandler) failedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.failed)
}

// waitForStarts 等待至少 want 个任务进入 RunTask。
func (h *fakeSchedulerHandler) waitForStarts(t *testing.T, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.startedCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d task start(s), got %d", want, h.startedCount())
}

// waitForFinishes 等待至少 want 个任务执行完毕。
func (h *fakeSchedulerHandler) waitForFinishes(t *testing.T, want int, timeout time.Duration) []fakeRunRecord {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		runs := h.snapshot()
		finished := 0
		for _, run := range runs {
			if !run.End.IsZero() {
				finished++
			}
		}
		if finished >= want {
			return runs
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d task(s) to finish, got %#v", want, h.snapshot())
	return nil
}

func newTestScheduler(t *testing.T, workerCount int, handler SchedulerEventHandler) *SchedulerV2 {
	t.Helper()

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  workerCount,
		QueueSize:    32,
		RateInterval: time.Millisecond,
	}, handler)
	t.Cleanup(scheduler.Stop)
	return scheduler
}

// waitForWorkerCount 轮询等待存活 worker 数收敛到 want。
// worker 只在两次任务之间退休，因此调小并发数是最终一致的，不能用固定 sleep 断言。
func waitForWorkerCount(t *testing.T, scheduler *SchedulerV2, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if scheduler.GetWorkerCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for worker count %d, got %d", want, scheduler.GetWorkerCount())
}

func newTestRequest(taskID uint, allowMultiple bool) *ExecutionRequest {
	task := &model.Task{
		ID:                     taskID,
		Name:                   "fake task",
		Command:                "echo hi",
		Status:                 model.TaskStatusEnabled,
		AllowMultipleInstances: allowMultiple,
	}
	return &ExecutionRequest{
		TaskID:      taskID,
		Task:        task,
		TriggerType: TriggerTypeCron,
	}
}

// 准备阶段 panic 不得吃掉并发名额。
//
// worker 阻塞化之后，每个 worker 就是一个并发名额。若 OnTaskExecuting 之类的准备阶段
// panic 打穿 worker goroutine，这个名额会永久消失且无法恢复——并发数为 1 时
// 整个调度器直接停摆，只能重启面板。这条用例用唯一的 worker 验证 panic 后它还活着。
func TestSchedulerV2RecoversFromPreparePanicAndKeepsWorkerAlive(t *testing.T) {
	handler := newFakeSchedulerHandler(10 * time.Millisecond)
	handler.setPreparePanic("boom")

	scheduler := newTestScheduler(t, 1, handler)
	scheduler.Start()

	if err := scheduler.Enqueue(newTestRequest(1, false)); err != nil {
		t.Fatalf("enqueue panicking request: %v", err)
	}
	// panic 必须被兜住并按失败结算，而不是让 goroutine 裸崩。
	handler.waitForFailed(t, 1, 10*time.Second)

	// 名额必须归还，否则后续任务永远拿不到槽位。
	deadline := time.Now().Add(2 * time.Second)
	for scheduler.GetRunningCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := scheduler.GetRunningCount(); got != 0 {
		t.Fatalf("expected concurrency slot to be released after panic, running count = %d", got)
	}

	// 关掉 panic 再投一个正常请求：唯一的 worker 若已死，这里会超时变红。
	handler.setPreparePanic(nil)
	if err := scheduler.Enqueue(newTestRequest(2, false)); err != nil {
		t.Fatalf("enqueue follow-up request: %v", err)
	}
	runs := handler.waitForFinishes(t, 1, 10*time.Second)
	if len(runs) != 1 || runs[0].TaskID != 2 {
		t.Fatalf("expected follow-up task 2 to run after panic, got %#v", runs)
	}
}

// A1：并发数为 1 时任务必须严格串行——后一个任务的开始时间不早于前一个任务的结束时间。
func TestSchedulerV2SingleWorkerRunsTasksSerially(t *testing.T) {
	handler := newFakeSchedulerHandler(60 * time.Millisecond)
	scheduler := newTestScheduler(t, 1, handler)
	scheduler.Start()

	for _, taskID := range []uint{1, 2, 3} {
		if err := scheduler.Enqueue(newTestRequest(taskID, false)); err != nil {
			t.Fatalf("enqueue task %d: %v", taskID, err)
		}
	}

	runs := handler.waitForFinishes(t, 3, 10*time.Second)
	if len(runs) != 3 {
		t.Fatalf("expected exactly 3 runs, got %d", len(runs))
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Start.Before(runs[i-1].End) {
			t.Fatalf("run %d started at %s before run %d ended at %s: worker did not block until completion",
				i, runs[i].Start, i-1, runs[i-1].End)
		}
	}
	if got := handler.peak(); got != 1 {
		t.Fatalf("expected peak concurrency 1, got %d", got)
	}
}

// A2：并发数为 2 时，任意时刻同时执行的任务不超过 2 个。
func TestSchedulerV2LimitsConcurrentExecutions(t *testing.T) {
	handler := newFakeSchedulerHandler(120 * time.Millisecond)
	scheduler := newTestScheduler(t, 2, handler)
	scheduler.Start()

	for _, taskID := range []uint{1, 2, 3, 4} {
		if err := scheduler.Enqueue(newTestRequest(taskID, false)); err != nil {
			t.Fatalf("enqueue task %d: %v", taskID, err)
		}
	}

	runs := handler.waitForFinishes(t, 4, 10*time.Second)
	if len(runs) != 4 {
		t.Fatalf("expected exactly 4 runs, got %d", len(runs))
	}
	if got := handler.peak(); got > 2 {
		t.Fatalf("expected peak concurrency <= 2, got %d", got)
	}
	if got := handler.peak(); got < 2 {
		t.Fatalf("expected both workers to be used (peak 2), got %d", got)
	}
}

// A3：不允许多实例的任务在执行期间被再次触发时，第二次必须被拒绝，且不影响第一次执行。
func TestSchedulerV2RejectsSecondInstanceWhileRunning(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	release := make(chan struct{})
	handler.runFunc = func(req *ExecutionRequest) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}

	scheduler := newTestScheduler(t, 2, handler)
	scheduler.Start()

	req := newTestRequest(7, false)
	if err := scheduler.Enqueue(req); err != nil {
		t.Fatalf("enqueue first instance: %v", err)
	}
	handler.waitForStarts(t, 1, 5*time.Second)

	second := newTestRequest(7, false)
	if err := scheduler.Enqueue(second); err != nil {
		t.Fatalf("enqueue second instance: %v", err)
	}

	// 给第二个请求足够时间被 worker 取走并判定；它应当被并发闸门直接拒绝。
	time.Sleep(200 * time.Millisecond)
	if got := handler.startedCount(); got != 1 {
		t.Fatalf("expected the second instance to be rejected, got %d run(s)", got)
	}
	if got := scheduler.GetRunningCount(); got != 1 {
		t.Fatalf("expected running count 1 while the first instance is executing, got %d", got)
	}

	close(release)
	runs := handler.waitForFinishes(t, 1, 5*time.Second)
	if len(runs) != 1 {
		t.Fatalf("expected the first instance to finish exactly once, got %d run(s)", len(runs))
	}
}

// A4：允许多实例的任务不应被并发闸门之外的逻辑误拦。
func TestSchedulerV2AllowsMultipleInstancesWhenEnabled(t *testing.T) {
	handler := newFakeSchedulerHandler(150 * time.Millisecond)
	scheduler := newTestScheduler(t, 2, handler)
	scheduler.Start()

	for i := 0; i < 2; i++ {
		if err := scheduler.Enqueue(newTestRequest(9, true)); err != nil {
			t.Fatalf("enqueue instance %d: %v", i, err)
		}
	}

	runs := handler.waitForFinishes(t, 2, 10*time.Second)
	if len(runs) != 2 {
		t.Fatalf("expected 2 parallel instances, got %d", len(runs))
	}
	if got := handler.peak(); got != 2 {
		t.Fatalf("expected both instances to run in parallel (peak 2), got %d", got)
	}
}

// A6：并发数为 1 时，开机任务按 sort_order 升序串行执行。
func TestSchedulerV2RunsStartupTasksInSortOrder(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 故意按乱序创建，验证执行顺序取决于 sort_order 而不是创建顺序。
	for _, spec := range []struct {
		name      string
		sortOrder int
	}{
		{"third", 30},
		{"first", 10},
		{"second", 20},
	} {
		task := &model.Task{
			Name:      spec.name,
			Command:   "echo boot",
			TaskType:  model.TaskTypeStartup,
			Status:    model.TaskStatusEnabled,
			SortOrder: spec.sortOrder,
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create startup task %s: %v", spec.name, err)
		}
	}

	handler := newFakeSchedulerHandler(20 * time.Millisecond)
	scheduler := newTestScheduler(t, 1, handler)

	if count := scheduler.EnqueueStartupTasks(); count != 3 {
		t.Fatalf("expected 3 startup tasks enqueued, got %d", count)
	}
	scheduler.Start()

	runs := handler.waitForFinishes(t, 3, 10*time.Second)
	if len(runs) != 3 {
		t.Fatalf("expected 3 startup runs, got %d", len(runs))
	}

	var names []string
	for _, run := range runs {
		var task model.Task
		if err := database.DB.First(&task, run.TaskID).Error; err != nil {
			t.Fatalf("reload task %d: %v", run.TaskID, err)
		}
		names = append(names, task.Name)
	}
	want := []string{"first", "second", "third"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("expected startup execution order %v, got %v", want, names)
		}
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Start.Before(runs[i-1].End) {
			t.Fatalf("startup tasks overlapped: run %d started before run %d ended", i, i-1)
		}
	}
}

// A7：任务执行期间 GetRunningCount 返回 1，执行结束后回落到 0。
func TestSchedulerV2RunningCountReflectsExecution(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	var scheduler *SchedulerV2
	observed := make(chan int, 1)
	release := make(chan struct{})
	handler.runFunc = func(req *ExecutionRequest) {
		select {
		case observed <- scheduler.GetRunningCount():
		default:
		}
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}

	scheduler = newTestScheduler(t, 1, handler)
	scheduler.Start()

	if got := scheduler.GetRunningCount(); got != 0 {
		t.Fatalf("expected idle running count 0, got %d", got)
	}
	if err := scheduler.Enqueue(newTestRequest(5, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	select {
	case got := <-observed:
		if got != 1 {
			t.Fatalf("expected running count 1 during execution, got %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the task to start")
	}

	close(release)
	handler.waitForFinishes(t, 1, 5*time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if scheduler.GetRunningCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected running count to drop back to 0, got %d", scheduler.GetRunningCount())
}

// 随机延迟必须在占用并发槽位之前完成：延迟期间 worker 不应被占用。
func TestSchedulerV2ExecutionDelayDoesNotHoldWorkerSlot(t *testing.T) {
	handler := newFakeSchedulerHandler(10 * time.Millisecond)
	handler.delay = 150 * time.Millisecond

	scheduler := newTestScheduler(t, 1, handler)
	scheduler.Start()

	if err := scheduler.Enqueue(newTestRequest(3, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	// 延迟期间既不应开始执行，也不应占用并发槽位。
	time.Sleep(60 * time.Millisecond)
	if got := handler.startedCount(); got != 0 {
		t.Fatalf("expected the task to still be waiting for its delay, got %d run(s)", got)
	}
	if got := scheduler.GetRunningCount(); got != 0 {
		t.Fatalf("expected no concurrency slot to be held during the delay, got %d", got)
	}

	runs := handler.waitForFinishes(t, 1, 10*time.Second)
	if len(runs) != 1 {
		t.Fatalf("expected the delayed task to run exactly once, got %d", len(runs))
	}
}

// 延迟到期后重新入队失败（队列满）时，任务不能继续停在「排队中」——它这次已经不会被执行了。
func TestSchedulerV2DelayedEnqueueFailureClearsQueuedStatus(t *testing.T) {
	testutil.SetupTestEnv(t)

	task := &model.Task{
		Name:     "delayed requeue task",
		Command:  "echo hi",
		TaskType: model.TaskTypeCron,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	handler := newFakeSchedulerHandler(0)
	// 队列只放得下 1 个请求，且不启动 worker，保证延迟到期时队列一定是满的。
	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    1,
		RateInterval: time.Hour,
	}, handler)
	t.Cleanup(scheduler.Stop)

	if err := scheduler.Enqueue(newTestRequest(999, false)); err != nil {
		t.Fatalf("fill the queue: %v", err)
	}

	req := newTestRequest(task.ID, false)
	req.Task = task
	// 模拟生产端入队成功后写下的「排队中」状态。
	if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("status", model.TaskStatusQueued).Error; err != nil {
		t.Fatalf("mark task queued: %v", err)
	}

	scheduler.EnqueueDelayed(time.Millisecond, func() *ExecutionRequest { return req })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var reloaded model.Task
		if err := database.DB.First(&reloaded, task.ID).Error; err != nil {
			t.Fatalf("reload task: %v", err)
		}
		if reloaded.Status != model.TaskStatusQueued {
			if reloaded.Status != model.TaskStatusEnabled {
				t.Fatalf("expected the task to fall back to enabled, got %v", reloaded.Status)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected a failed delayed enqueue to clear the queued status")
}

// 准备阶段失败时必须走 OnTaskFailed，并把并发槽位还回去，不能因为阻塞化把槽位漏掉。
func TestSchedulerV2ReleasesSlotWhenPreparationFails(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	handler.prepareErr = errFakePrepare

	scheduler := newTestScheduler(t, 1, handler)
	scheduler.Start()

	if err := scheduler.Enqueue(newTestRequest(4, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if handler.failedCount() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := handler.failedCount(); got != 1 {
		t.Fatalf("expected exactly one preparation failure, got %d", got)
	}
	if got := handler.startedCount(); got != 0 {
		t.Fatalf("expected no execution after a failed preparation, got %d", got)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if scheduler.GetRunningCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected the concurrency slot to be released, running count is %d", scheduler.GetRunningCount())
}

// R6：并发数调大必须立刻生效，不需要重启面板。
func TestSchedulerV2SetWorkerCountScalesUpImmediately(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	release := make(chan struct{})
	handler.runFunc = func(req *ExecutionRequest) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}

	scheduler := newTestScheduler(t, 1, handler)
	scheduler.Start()

	for _, taskID := range []uint{1, 2, 3} {
		if err := scheduler.Enqueue(newTestRequest(taskID, false)); err != nil {
			t.Fatalf("enqueue task %d: %v", taskID, err)
		}
	}

	// 并发数为 1：三个任务里只有一个能进入执行，另外两个必须在队列里等着。
	handler.waitForStarts(t, 1, 5*time.Second)
	time.Sleep(100 * time.Millisecond)
	if got := handler.startedCount(); got != 1 {
		t.Fatalf("expected exactly 1 task to run while the worker count is 1, got %d", got)
	}

	previous, applied := scheduler.SetWorkerCount(3)
	if previous != 1 || applied != 3 {
		t.Fatalf("expected the worker count to go 1 -> 3, got %d -> %d", previous, applied)
	}

	// 排队中的两个任务必须被新补的 worker 立刻接走。
	handler.waitForStarts(t, 3, 5*time.Second)
	if got := handler.peak(); got != 3 {
		t.Fatalf("expected peak concurrency 3 after scaling up, got %d", got)
	}

	close(release)
	handler.waitForFinishes(t, 3, 5*time.Second)
}

// R6：并发数调小必须立刻生效——收缩之后的执行峰值不得超过新的上限。
func TestSchedulerV2SetWorkerCountScalesDownAndCapsConcurrency(t *testing.T) {
	handler := newFakeSchedulerHandler(40 * time.Millisecond)
	scheduler := newTestScheduler(t, 3, handler)
	scheduler.Start()

	if _, applied := scheduler.SetWorkerCount(1); applied != 1 {
		t.Fatalf("expected the worker count to be lowered to 1, got %d", applied)
	}
	waitForWorkerCount(t, scheduler, 1, 5*time.Second)

	for _, taskID := range []uint{1, 2, 3, 4} {
		if err := scheduler.Enqueue(newTestRequest(taskID, false)); err != nil {
			t.Fatalf("enqueue task %d: %v", taskID, err)
		}
	}

	runs := handler.waitForFinishes(t, 4, 10*time.Second)
	if len(runs) != 4 {
		t.Fatalf("expected exactly 4 runs, got %d", len(runs))
	}
	if got := handler.peak(); got != 1 {
		t.Fatalf("expected peak concurrency 1 after lowering the worker count, got %d", got)
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Start.Before(runs[i-1].End) {
			t.Fatalf("run %d started at %s before run %d ended at %s: the lowered limit did not hold",
				i, runs[i].Start, i-1, runs[i-1].End)
		}
	}
}

// R6：队列空闲时调小也必须真的生效。
//
// 空闲 worker 一直阻塞在 taskQueue 上，如果没有唤醒信号，它永远发现不了自己已经超编——
// 用户改小并发数后要等到下一个任务进来才部分生效，队列长期空闲时则完全不生效。
//
// 【为什么必须先把 worker 逼进 select，不要把这条用例简化回「Start 完立刻 SetWorkerCount」】
// worker 的退休判断在循环顶部、select 之前：
//
//	for { if stopped {...}; if retireWorkerIfSurplus() { return }; select { ...taskQueue... } }
//
// 停在循环顶部的 worker 压根不需要任何唤醒信号，下一次判断就会自己退休；
// 只有已经阻塞在 select 上的 worker 才必须靠 resizeCh 叫醒。
// 而 Start() 刚返回时，4 个 worker goroutine 很可能还没跑到 select——
// liveWorkers 是在 spawnWorkersLocked 里 spawn 之前就自增的，
// 所以 GetWorkerCount() == 4 只代表名额已登记，不代表 goroutine 已经开始跑。
// 于是「Start 完立刻 SetWorkerCount(1)」走的是循环顶部那条路，
// 把 resizeCh 唤醒分支整个删掉用例照样会绿，等于什么都没测到。
//
// 因此这里先让 4 个 worker 各跑一个任务（进入过 RunTask 就证明它们已越过循环顶部），
// 跑完后重新阻塞回 select，再调小并发数：此时唤醒路径是用例唯一可能通过的方式。
func TestSchedulerV2SetWorkerCountShrinksWhileQueueIsIdle(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	release := make(chan struct{})
	// runFunc 必须在 Start() 之前设置：worker 起来之后 RunTask 会持 h.mu 读这个字段，
	// 测试中途裸写就是一次无同步的并发读写（-race 下直接报 data race）。
	// Start() 之前还没有任何 worker goroutine，goroutine 创建本身就构成 happens-before。
	handler.runFunc = func(req *ExecutionRequest) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}

	scheduler := newTestScheduler(t, 4, handler)
	scheduler.Start()

	if got := scheduler.GetWorkerCount(); got != 4 {
		t.Fatalf("expected 4 workers after start, got %d", got)
	}

	// 预热：4 个任务同时卡在 RunTask 里 —— 一个 worker 一次只能跑一个任务，
	// 所以「4 个 RunTask 同时在执行」等价于「4 个 worker 全部离开了循环顶部」。
	for _, taskID := range []uint{21, 22, 23, 24} {
		if err := scheduler.Enqueue(newTestRequest(taskID, false)); err != nil {
			t.Fatalf("enqueue warm-up task %d: %v", taskID, err)
		}
	}
	handler.waitForStarts(t, 4, 5*time.Second)
	if got := handler.peak(); got != 4 {
		t.Fatalf("expected all 4 workers to be inside RunTask before resizing, peak concurrency = %d", got)
	}

	// 放行预热任务，让 4 个 worker 跑完回到 select 上阻塞。
	close(release)
	handler.waitForFinishes(t, 4, 5*time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for scheduler.GetRunningCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := scheduler.GetRunningCount(); got != 0 {
		t.Fatalf("expected every warm-up task to be settled before resizing, running count = %d", got)
	}

	// 全用例唯一一处 sleep，且它是「等待稳定」而不是「代替断言」：
	// GetRunningCount() 归零只说明 executeTask 的 defer 已经跑完、马上要返回，
	// worker 还要再走「循环顶部判断 -> 进入 select」这几步才算真正空闲阻塞。
	// 「goroutine 已经停在 select 上」没有任何可观测状态能断言（这恰恰是本用例要构造的前提），
	// 只能给一段远超实际需要的稳定期；真正的断言仍然是下面 waitForWorkerCount 的轮询。
	time.Sleep(100 * time.Millisecond)

	// 此刻队列已空、4 个 worker 全部阻塞在 select 上：不叫醒就永远发现不了自己超编。
	scheduler.SetWorkerCount(1)

	// 之后不再投任何任务，多余的 worker 只能靠唤醒信号回到循环顶部自行退休。
	waitForWorkerCount(t, scheduler, 1, 5*time.Second)
	if got := handler.startedCount(); got != 4 {
		t.Fatalf("expected exactly the 4 warm-up tasks to have run, got %d", got)
	}
}

// R6：调小并发数只能在两次任务之间收 worker，绝不能打断正在执行的任务。
func TestSchedulerV2SetWorkerCountDoesNotInterruptRunningTask(t *testing.T) {
	handler := newFakeSchedulerHandler(0)
	release := make(chan struct{})
	handler.runFunc = func(req *ExecutionRequest) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}

	scheduler := newTestScheduler(t, 3, handler)
	scheduler.Start()

	if err := scheduler.Enqueue(newTestRequest(11, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	handler.waitForStarts(t, 1, 5*time.Second)

	scheduler.SetWorkerCount(1)
	// 两个空闲 worker 退休后，正在执行任务的那个必须留下来把任务跑完。
	waitForWorkerCount(t, scheduler, 1, 5*time.Second)
	if got := scheduler.GetRunningCount(); got != 1 {
		t.Fatalf("expected the in-flight task to survive the resize, running count = %d", got)
	}

	close(release)
	runs := handler.waitForFinishes(t, 1, 5*time.Second)
	if len(runs) != 1 || runs[0].TaskID != 11 || runs[0].End.IsZero() {
		t.Fatalf("expected task 11 to finish normally after the resize, got %#v", runs)
	}
}

// R6：并发数被填成 0 或负数时钳到 1，调度器仍然要能正常执行任务。
func TestSchedulerV2SetWorkerCountClampsToAtLeastOne(t *testing.T) {
	handler := newFakeSchedulerHandler(10 * time.Millisecond)
	scheduler := newTestScheduler(t, 2, handler)
	scheduler.Start()

	if _, applied := scheduler.SetWorkerCount(0); applied != 1 {
		t.Fatalf("expected 0 to be clamped to 1, got %d", applied)
	}
	if _, applied := scheduler.SetWorkerCount(-5); applied != 1 {
		t.Fatalf("expected a negative worker count to be clamped to 1, got %d", applied)
	}

	waitForWorkerCount(t, scheduler, 1, 5*time.Second)
	if got := scheduler.GetWorkerCount(); got < 1 {
		t.Fatalf("expected at least one worker to survive the clamp, got %d", got)
	}

	if err := scheduler.Enqueue(newTestRequest(12, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	runs := handler.waitForFinishes(t, 1, 5*time.Second)
	if len(runs) != 1 || runs[0].TaskID != 12 {
		t.Fatalf("expected the scheduler to keep working after the clamp, got %#v", runs)
	}
}

// A9：存在运行中任务时关机，必须先中断执行、再等待 worker，整体耗时远小于原来的 5s 超时。
func TestShutdownSchedulerV2InterruptsRunningTasksBeforeWaiting(t *testing.T) {
	testutil.SetupTestEnv(t)

	oldScheduler := globalScheduler
	oldExecutor := globalExecutor
	t.Cleanup(func() {
		globalScheduler = oldScheduler
		globalExecutor = oldExecutor
	})

	executor := NewTaskExecutor()
	// 占位条目：不含真实进程，仅用于观察 StopAllRunningTasks 是否已经执行过。
	executor.processLock.Lock()
	executor.runningProcesses[1] = map[int]*os.Process{}
	executor.processLock.Unlock()

	processesCleared := func() bool {
		executor.processLock.Lock()
		defer executor.processLock.Unlock()
		return len(executor.runningProcesses) == 0
	}

	handler := newFakeSchedulerHandler(0)
	var interrupted bool
	var interruptedMu sync.Mutex
	handler.runFunc = func(req *ExecutionRequest) {
		// 模拟真实执行：子进程被 StopAllRunningTasks 杀掉后，runTask 才会返回。
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if processesCleared() {
				interruptedMu.Lock()
				interrupted = true
				interruptedMu.Unlock()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    8,
		RateInterval: time.Millisecond,
	}, handler)
	globalScheduler = scheduler
	globalExecutor = executor
	scheduler.Start()

	if err := scheduler.Enqueue(newTestRequest(1, false)); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	handler.waitForStarts(t, 1, 5*time.Second)

	start := time.Now()
	ShutdownSchedulerV2()
	elapsed := time.Since(start)

	interruptedMu.Lock()
	got := interrupted
	interruptedMu.Unlock()
	if !got {
		t.Fatal("expected running task to be interrupted before waiting for workers")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected shutdown to finish well below the 5s worker timeout, took %s", elapsed)
	}
	if globalScheduler != nil || globalExecutor != nil {
		t.Fatal("expected shutdown to clear scheduler and executor globals")
	}
}
