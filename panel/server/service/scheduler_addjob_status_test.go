package service

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// newStatusGateScheduler 造一个「不启动 cron、也不起 worker」的调度器。
// 这组用例只关心「AddJob 到底有没有把 cron 条目挂上去」，
// 断言全部落在注册状态和数据库行上，绝不靠 sleep 等真实触发。
func newStatusGateScheduler(t *testing.T) *SchedulerV2 {
	t.Helper()

	scheduler := NewSchedulerV2(SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    10,
		RateInterval: time.Hour,
	}, nil)
	// 即使没有 Start()，也收口一次，避免用例之间互相污染。
	t.Cleanup(scheduler.Stop)
	return scheduler
}

// mustCreateScheduledTask 建一条真实任务行，主要是为了拿到自增 ID：
// entryMap 用任务 ID 做键，如果所有任务都是 ID 0，后一条会把前一条的登记覆盖掉，用例就测了个寂寞。
func mustCreateScheduledTask(t *testing.T, name, taskType string, status float64, cronExpression string) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:           name,
		Command:        "echo hi",
		TaskType:       taskType,
		CronExpression: cronExpression,
		Status:         status,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("创建任务 %q 失败: %v", name, err)
	}
	return task
}

// describeEntryCount 把 ScheduledEntryCount 的三种取值翻译成人话，用于断言失败信息。
func describeEntryCount(count int) string {
	switch {
	case count < 0:
		return "压根不在调度器里（entryMap 无此任务）"
	case count == 0:
		return "在 entryMap 里但一个 cron 触发条目都没有（空登记）"
	default:
		return "已正常注册 cron 触发条目"
	}
}

// issue #115 回归：AddJob 的早退条件只能认「已禁用」，绝不能写回 `status != 已启用`。
//
// 排队中(0.5) 与 运行中(2) 都只是启用态任务的瞬时状态。而 AddJob 会先无条件摘掉旧 entry
// 再决定要不要重挂，一旦在这两个瞬间早退，这个任务从此就再也不会被 cron 触发，
// 面板上却依旧显示「已启用 + 有下次运行时间」——用户完全无从察觉，
// 任务日志抽屉里更是一条记录都没有（因为这次执行压根没被交给执行器）。
// 编辑任务保存、订阅同步、备份恢复的 ReloadAllJobs 都可能正好撞上这两个瞬间。
func TestSchedulerV2AddJobOnlySkipsDisabledTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)

	cases := []struct {
		name           string
		taskType       string
		status         float64
		cronExpression string
		wantCount      int
		reason         string
	}{
		{
			name:           "排队中的定时任务仍要注册",
			taskType:       model.TaskTypeCron,
			status:         model.TaskStatusQueued,
			cronExpression: "0 5 * * *",
			wantCount:      1,
			reason:         "排队中只是「已入队、还没轮到执行」的瞬间；此刻早退会把 cron 条目永久摘掉，任务从此静默失联",
		},
		{
			name:           "运行中的定时任务仍要注册",
			taskType:       model.TaskTypeCron,
			status:         model.TaskStatusRunning,
			cronExpression: "0 5 * * *",
			wantCount:      1,
			reason:         "运行中是启用态任务的瞬时状态，不是关闭；此刻早退等于「跑一次就把自己关掉」",
		},
		{
			name:           "多条定时规则要逐条注册",
			taskType:       model.TaskTypeCron,
			status:         model.TaskStatusRunning,
			cronExpression: "0 5 * * *\n0 6 * * *",
			wantCount:      2,
			reason:         "运行中的任务重新注册时，多条定时规则一条都不能少",
		},
		{
			name:           "已禁用的定时任务不注册",
			taskType:       model.TaskTypeCron,
			status:         model.TaskStatusDisabled,
			cronExpression: "0 5 * * *",
			wantCount:      -1,
			reason:         "已禁用是用户的明确意图，必须真的从调度器里摘掉，否则关掉的任务还会到点自己跑",
		},
		{
			name:      "启用的手动任务走空登记",
			taskType:  model.TaskTypeManual,
			status:    model.TaskStatusEnabled,
			wantCount: 0,
			reason:    "手动任务不该有 cron 条目，但必须在 entryMap 里留个空登记，否则 HasJob 为假会让运行结束时被判成已禁用",
		},
		{
			name:      "启用的开机任务走空登记",
			taskType:  model.TaskTypeStartup,
			status:    model.TaskStatusEnabled,
			wantCount: 0,
			reason:    "开机任务同上：没有 cron 条目，但状态复原依赖 entryMap 里的这条空登记",
		},
		{
			name:      "运行中的手动任务也要空登记",
			taskType:  model.TaskTypeManual,
			status:    model.TaskStatusRunning,
			wantCount: 0,
			reason:    "手动任务跑起来之后被重新注册（例如编辑保存），仍要保留空登记，否则跑完会被静默判成已禁用",
		},
		{
			name:      "已禁用的手动任务不注册",
			taskType:  model.TaskTypeManual,
			status:    model.TaskStatusDisabled,
			wantCount: -1,
			reason:    "禁用的手动任务不该在调度器里留任何登记",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			task := mustCreateScheduledTask(t, testCase.name, testCase.taskType, testCase.status, testCase.cronExpression)

			if err := scheduler.AddJob(task); err != nil {
				t.Fatalf("AddJob 返回错误: %v（状态 %v 的任务注册不应报错）", err, testCase.status)
			}

			got := scheduler.ScheduledEntryCount(task.ID)
			if got != testCase.wantCount {
				t.Fatalf("任务状态 %v / 类型 %s：期望 ScheduledEntryCount = %d（%s），实际 = %d（%s）。%s",
					testCase.status, task.GetTaskType(),
					testCase.wantCount, describeEntryCount(testCase.wantCount),
					got, describeEntryCount(got),
					testCase.reason)
			}

			// HasJob 与 ScheduledEntryCount 的口径必须一致：只有 -1 才代表「不在调度器里」。
			wantHasJob := testCase.wantCount >= 0
			if gotHasJob := scheduler.HasJob(task.ID); gotHasJob != wantHasJob {
				t.Fatalf("任务状态 %v：期望 HasJob = %v，实际 = %v。HasJob 与 entryMap 登记必须同口径，"+
					"否则运行结束时 ResolveTaskInactiveStatus 会把任务判错状态", testCase.status, wantHasJob, gotHasJob)
			}
		})
	}
}

// issue #115 复合回归（这条最重要）：编辑一个正在运行的任务，不能把它静默关掉。
//
// 缺陷链路是：编辑保存 -> UpdateJob 摘掉 entry 后早退 -> HasJob 变 false ->
// 本次执行跑完时 ResolveTaskInactiveStatus 看到「运行中 + 没有 job」，把任务判成【已禁用】。
// 用户什么都没做，只是保存了一下任务，任务就被悄悄关了；下次到点自然什么都不会发生，
// 日志抽屉也永远停在「该任务还没有日志记录」。
// 改动前这条用例必然失败（拿到 Disabled），改动后应通过。
func TestSchedulerV2UpdateJobKeepsRunningTaskScheduledAndEnabled(t *testing.T) {
	testutil.SetupTestEnv(t)

	// ResolveTaskInactiveStatus 内部走 GetSchedulerV2() 读全局调度器，这里必须把它换成用例自己的实例。
	previousScheduler := globalScheduler.Load()
	scheduler := newStatusGateScheduler(t)
	globalScheduler.Store(scheduler)
	t.Cleanup(func() {
		globalScheduler.Store(previousScheduler)
	})

	cases := []struct {
		name           string
		taskType       string
		cronExpression string
		wantCount      int
	}{
		{
			name:           "编辑运行中的定时任务",
			taskType:       model.TaskTypeCron,
			cronExpression: "0 5 * * *",
			wantCount:      1,
		},
		{
			name:      "编辑运行中的手动任务",
			taskType:  model.TaskTypeManual,
			wantCount: 0,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// 第一步：任务本来是启用态，已经正常注册在调度器里。
			task := mustCreateScheduledTask(t, testCase.name, testCase.taskType, model.TaskStatusEnabled, testCase.cronExpression)
			if err := scheduler.AddJob(task); err != nil {
				t.Fatalf("初始注册失败: %v", err)
			}
			if got := scheduler.ScheduledEntryCount(task.ID); got != testCase.wantCount {
				t.Fatalf("前置条件不成立：启用态任务期望登记 %d 条触发条目，实际 %d 条", testCase.wantCount, got)
			}

			// 第二步：任务被触发，进入运行中。
			task.Status = model.TaskStatusRunning
			if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).
				Update("status", model.TaskStatusRunning).Error; err != nil {
				t.Fatalf("把任务置为运行中失败: %v", err)
			}

			// 第三步：用户此刻点了保存。handler 会重新读出真实状态（运行中）再调 UpdateJob。
			if err := scheduler.UpdateJob(task); err != nil {
				t.Fatalf("编辑保存时 UpdateJob 报错: %v", err)
			}

			// 第四步：cron 登记必须原样还在。
			if got := scheduler.ScheduledEntryCount(task.ID); got != testCase.wantCount {
				t.Fatalf("编辑一个运行中的任务后，期望仍有 %d 条触发条目（%s），实际 %d 条（%s）。"+
					"entry 一旦在运行中被摘掉，这个任务就再也不会被 cron 触发了",
					testCase.wantCount, describeEntryCount(testCase.wantCount), got, describeEntryCount(got))
			}
			if !scheduler.HasJob(task.ID) {
				t.Fatal("编辑一个运行中的任务后 HasJob 变成了 false，" +
					"这会让本次执行结束时 ResolveTaskInactiveStatus 把任务判成已禁用")
			}

			// 第五步：本次执行跑完时的状态复原，必须回到「已启用」而不是「已禁用」。
			if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusEnabled {
				t.Fatalf("运行中的任务被编辑保存后，期望 ResolveTaskInactiveStatus 返回已启用(%v)，实际返回 %v。"+
					"返回已禁用意味着用户只是保存了一下任务，任务就被静默关掉了——这正是 issue #115 里"+
					"「调试运行正常、加了定时就不执行、日志抽屉还是空的」的根因，绝不能回归",
					float64(model.TaskStatusEnabled), got)
			}
		})
	}
}

// 放宽 AddJob 早退条件之后必须补上的护栏：
// 「运行中点了禁用、等这次跑完再生效」的任务，status 还停在运行中，但用户的意图已经是禁用，
// 这期间任何一次 AddJob（编辑保存、订阅同步、备份恢复的全量重载）都不能把 cron 条目挂回去。
// 漏掉这条的话，任务会带着活着的条目落成「已禁用」，之后一直按点被触发执行 —— 比原缺陷更糟。
func TestSchedulerV2AddJobSkipsTasksPendingDisable(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)

	task := mustCreateScheduledTask(t, "运行中被禁用的任务", model.TaskTypeCron, model.TaskStatusRunning, "0 5 * * *")

	// 先按正常情况注册一次，确认前置条件成立。
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}
	if got := scheduler.ScheduledEntryCount(task.ID); got != 1 {
		t.Fatalf("前置条件不成立：运行中的启用任务应有 1 条触发条目，实际 %d", got)
	}

	// 用户点了「禁用」：面板摘掉条目、记下意图，status 仍停在运行中。
	scheduler.RemoveJob(task.ID)
	MarkPendingDisable(task.ID)
	t.Cleanup(func() { ClearPendingDisable(task.ID) })

	// 此刻再被 AddJob 一次（编辑保存 / 订阅同步 / 全量重载）。
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("待禁用任务的 AddJob 不应报错: %v", err)
	}
	if got := scheduler.ScheduledEntryCount(task.ID); got >= 0 {
		t.Fatalf("已经点过禁用的任务不能被重新挂回 cron，期望 ScheduledEntryCount = -1（%s），实际 = %d（%s）。"+
			"挂回去的话它会带着活着的触发条目落成「已禁用」，之后一直按点执行",
			describeEntryCount(-1), got, describeEntryCount(got))
	}

	// 结算时必须落成禁用。
	if got := ResolveTaskInactiveStatus(task); got != model.TaskStatusDisabled {
		t.Fatalf("点过禁用的任务跑完应结算为已禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}

	// 用户重新启用后，意图撤销，注册要恢复正常。
	ClearPendingDisable(task.ID)
	if err := scheduler.AddJob(task); err != nil {
		t.Fatalf("撤销禁用意图后重新注册失败: %v", err)
	}
	if got := scheduler.ScheduledEntryCount(task.ID); got != 1 {
		t.Fatalf("撤销禁用意图后应能重新注册，期望 1 条触发条目，实际 %d", got)
	}
}

// issue #115 回归：ReloadAllJobs 的查询口径必须是 `status <> 已禁用`，
// 不能写成 `status = 已启用`——否则备份恢复 / 全量重载恰好撞上有任务在排队或在跑时，
// 那条任务会连 entry 一起丢掉，且面板上看不出任何异常。
func TestSchedulerV2ReloadAllJobsKeepsQueuedAndRunningTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	scheduler := newStatusGateScheduler(t)

	enabled := mustCreateScheduledTask(t, "启用的定时任务", model.TaskTypeCron, model.TaskStatusEnabled, "0 5 * * *")
	queued := mustCreateScheduledTask(t, "排队中的定时任务", model.TaskTypeCron, model.TaskStatusQueued, "0 5 * * *")
	running := mustCreateScheduledTask(t, "运行中的定时任务", model.TaskTypeCron, model.TaskStatusRunning, "0 5 * * *")
	disabled := mustCreateScheduledTask(t, "已禁用的定时任务", model.TaskTypeCron, model.TaskStatusDisabled, "0 5 * * *")

	scheduler.ReloadAllJobs()

	cases := []struct {
		task      *model.Task
		wantCount int
		reason    string
	}{
		{task: enabled, wantCount: 1, reason: "启用态任务是重载的基本盘"},
		{task: queued, wantCount: 1, reason: "排队中只是瞬时状态，重载时漏掉它，这个任务从此不会再被触发"},
		{task: running, wantCount: 1, reason: "运行中只是瞬时状态，重载时漏掉它，跑完还会被判成已禁用"},
		{task: disabled, wantCount: -1, reason: "已禁用是用户的明确意图，重载后不该被重新挂上去"},
	}

	for _, testCase := range cases {
		got := scheduler.ScheduledEntryCount(testCase.task.ID)
		if got != testCase.wantCount {
			t.Fatalf("ReloadAllJobs 之后，任务 %q（状态 %v）期望 ScheduledEntryCount = %d（%s），实际 = %d（%s）。%s",
				testCase.task.Name, testCase.task.Status,
				testCase.wantCount, describeEntryCount(testCase.wantCount),
				got, describeEntryCount(got),
				testCase.reason)
		}
	}
}

// issue #115 回归：被多实例闸门拦下的触发必须留下用户看得见的原因，
// 而且必须记在任务行上、不能建 task_logs 行。
//
// 这是整个调度链路上唯一一处「返回正常、却既不执行也不留任何痕迹」的分支，
// 用户看到的就是「到点了却什么都没发生」+ 日志里的「该任务还没有日志记录」。
// 但它也绝不能建日志行：跳过意味着压根没执行，建成日志会混进仪表板的「已终止」与耗时统计，
// 而且跳过记录的时间比它所阻塞的那次执行更晚，会顶掉「最近一次日志」，
// 用户点开日志看到的就成了「本次触发被跳过」，而不是真正的执行输出。
func TestRecordSkippedTriggerWritesTaskFieldsNotLogRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	task := mustCreateScheduledTask(t, "长跑的定时任务", model.TaskTypeCron, model.TaskStatusRunning, "* * * * *")

	// 还原真实现场：上一次执行还在跑，任务日志里有一条「运行中」的记录。
	runningStatus := model.LogStatusRunning
	runningStartedAt := time.Now().Add(-2 * time.Minute)
	if err := database.DB.Create(&model.TaskLog{
		TaskID:    task.ID,
		Content:   "=== 上一次执行仍在进行中 ===",
		Status:    &runningStatus,
		StartedAt: runningStartedAt,
	}).Error; err != nil {
		t.Fatalf("创建运行中日志失败: %v", err)
	}

	countLogs := func() int64 {
		t.Helper()

		var total int64
		if err := database.DB.Model(&model.TaskLog{}).Where("task_id = ?", task.ID).Count(&total).Error; err != nil {
			t.Fatalf("统计任务日志失败: %v", err)
		}
		return total
	}
	reload := func() model.Task {
		t.Helper()

		var latest model.Task
		if err := database.DB.First(&latest, task.ID).Error; err != nil {
			t.Fatalf("重新读取任务失败: %v", err)
		}
		return latest
	}

	before := countLogs()
	req := &ExecutionRequest{
		TaskID:      task.ID,
		Task:        task,
		TriggerType: TriggerTypeCron,
	}

	recordSkippedTrigger(req, SkipReasonSingleInstance)

	updated := reload()
	if updated.LastSkipReason != SkipReasonSingleInstance {
		t.Fatalf("期望把跳过原因写到任务上，实际 last_skip_reason = %q。"+
			"不写的话用户只能看到「什么都没发生」，无从判断任务为什么没执行", updated.LastSkipReason)
	}
	if updated.LastSkipAt == nil {
		t.Fatal("last_skip_at 为空，界面上就没法说清是「什么时候」被跳过的")
	}

	if after := countLogs(); after != before {
		t.Fatalf("跳过不能建 task_logs 行：期望日志条数仍是 %d，实际变成 %d。"+
			"建行会混进「已终止」与耗时统计，还会因为时间更晚顶掉「最近一次日志」", before, after)
	}

	// 同一个阻塞期内反复被拦：写的是同一行，天然幂等，不需要任何去重逻辑。
	recordSkippedTrigger(req, SkipReasonSingleInstance)
	recordSkippedTrigger(req, SkipReasonSingleInstance)
	if after := countLogs(); after != before {
		t.Fatalf("反复被拦仍然不该产生任何日志行，期望 %d 条，实际 %d 条", before, after)
	}

	// 关键回归：被跳过之后，「最近一次日志」必须仍然是那条真正在跑的记录。
	var latestLog model.TaskLog
	if err := database.DB.Where("task_id = ?", task.ID).Order("started_at DESC").First(&latestLog).Error; err != nil {
		t.Fatalf("查询最近一次日志失败: %v", err)
	}
	if latestLog.Status == nil || *latestLog.Status != model.LogStatusRunning {
		t.Fatalf("被跳过之后「最近一次日志」应当仍是正在运行的那条，实际拿到的状态是 %v。"+
			"被顶掉的话，用户点开日志看到的是「跳过说明」而不是真正的执行输出", latestLog.Status)
	}
}
