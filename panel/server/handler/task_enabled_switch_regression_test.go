package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// newIdleScheduler 造一个不启动 worker 的调度器，只用来走真实的 RunNow（打待禁用标记 + 把库里改成排队中）。
// 请求停在它自己的队列里、永远不会被执行，用例不会真的去跑命令；
// 负责 AddJob 登记的全局调度器照旧由 InitSchedulerV2 提供。
func newIdleScheduler(t *testing.T) *service.SchedulerV2 {
	t.Helper()

	scheduler := service.NewSchedulerV2(service.SchedulerConfig{
		WorkerCount:  1,
		QueueSize:    16,
		RateInterval: time.Hour,
	}, nil)
	t.Cleanup(scheduler.Stop)
	return scheduler
}

func mustCreateTaskWithStatus(t *testing.T, name, taskType, cronExpression string, status float64) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:           name,
		Command:        "echo switch",
		TaskType:       taskType,
		CronExpression: cronExpression,
		Status:         status,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	// 待禁用标记只活在内存里、跨用例存活，用完一律清掉，免得污染同一进程里的后续用例。
	t.Cleanup(func() { service.ClearPendingDisable(task.ID) })
	return task
}

func setTaskStatusInDB(t *testing.T, task *model.Task, status float64) {
	t.Helper()

	if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", status).Error; err != nil {
		t.Fatalf("set status %v for task %q: %v", status, task.Name, err)
	}
}

func reloadTaskRow(t *testing.T, id uint) model.Task {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("reload task %d: %v", id, err)
	}
	return task
}

// taskListEnabled 取任务项上的 enabled（契约 C1）：必须是布尔值，前端靠 typeof === 'boolean' 区分新旧后端。
func taskListEnabled(t *testing.T, item map[string]interface{}) bool {
	t.Helper()

	enabled, ok := item["enabled"].(bool)
	if !ok {
		t.Fatalf("expected enabled to be a boolean, got %#v", item["enabled"])
	}
	return enabled
}

func listAllTasksPayload(t *testing.T, engine *gin.Engine, token, extraQuery string) map[string]interface{} {
	t.Helper()

	path := "/api/v1/tasks?all=1"
	if extraQuery != "" {
		path += "&" + extraQuery
	}
	rec := performRequest(engine, http.MethodGet, path, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected task list 200, got %d: %s", rec.Code, rec.Body.String())
	}
	return decodeJSONMap(t, rec)
}

func taskActionPath(task *model.Task, action string) string {
	if action == "" {
		return fmt.Sprintf("/api/v1/tasks/%d", task.ID)
	}
	return fmt.Sprintf("/api/v1/tasks/%d/%s", task.ID, action)
}

func responseTaskData(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload in data, got %#v", payload["data"])
	}
	return data
}

// issue #133：任务列表的 enabled 是启用开关位，与运行态无关。
// 禁用任务被手动运行时 status 同样会走到 0.5 / 2，前端光看 status 会把它当成已启用，
// 运行期间下拉第一项就显示成「禁用」—— 这正是 issue 里报的 BUG。
func TestTaskListEnabledSwitchIsIndependentOfRunState(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "enabled-switch-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	global := service.GetSchedulerV2()
	if global == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	idle := newIdleScheduler(t)
	register := func(task *model.Task) {
		t.Helper()
		if err := global.AddJob(task); err != nil {
			t.Fatalf("add job for %q: %v", task.Name, err)
		}
	}

	disabledIdle := mustCreateTaskWithStatus(t, "禁用-空闲", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)

	enabledIdle := mustCreateTaskWithStatus(t, "启用-空闲", model.TaskTypeCron, "0 0 * * *", model.TaskStatusEnabled)
	register(enabledIdle)

	enabledRunning := mustCreateTaskWithStatus(t, "启用-运行中", model.TaskTypeCron, "0 0 * * *", model.TaskStatusEnabled)
	register(enabledRunning)
	setTaskStatusInDB(t, enabledRunning, model.TaskStatusRunning)

	enabledQueued := mustCreateTaskWithStatus(t, "启用-排队中", model.TaskTypeManual, "", model.TaskStatusEnabled)
	register(enabledQueued)
	setTaskStatusInDB(t, enabledQueued, model.TaskStatusQueued)

	// 禁用任务走真实的 RunNow：打待禁用标记、库里改成排队中。
	disabledQueued := mustCreateTaskWithStatus(t, "禁用-手动运行排队中", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)
	if err := idle.RunNow(disabledQueued.ID); err != nil {
		t.Fatalf("run disabled task: %v", err)
	}

	disabledRunning := mustCreateTaskWithStatus(t, "禁用-手动运行中", model.TaskTypeManual, "", model.TaskStatusDisabled)
	if err := idle.RunNow(disabledRunning.ID); err != nil {
		t.Fatalf("run disabled manual task: %v", err)
	}
	setTaskStatusInDB(t, disabledRunning, model.TaskStatusRunning)

	// 标记只活在当前进程：`ddp task run` 在另一个进程里跑的禁用任务，面板这边只能靠「调度器里没有它」认出来。
	disabledRunningElsewhere := mustCreateTaskWithStatus(t, "禁用-别的进程在跑", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)
	setTaskStatusInDB(t, disabledRunningElsewhere, model.TaskStatusRunning)

	// 运行中点了禁用、等这次跑完再生效：status 还是运行中，开关位已经是关，禁用接口的响应也要如实下发。
	pendingDisable := mustCreateTaskWithStatus(t, "运行中被禁用", model.TaskTypeCron, "0 0 * * *", model.TaskStatusEnabled)
	register(pendingDisable)
	setTaskStatusInDB(t, pendingDisable, model.TaskStatusRunning)
	disableRec := performRequest(engine, http.MethodPut, taskActionPath(pendingDisable, "disable"), headers)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected disable 200, got %d: %s", disableRec.Code, disableRec.Body.String())
	}
	disableData := responseTaskData(t, decodeJSONMap(t, disableRec))
	if got, _ := disableData["status"].(float64); got != model.TaskStatusRunning {
		t.Fatalf("前置条件不成立：运行中被禁用的任务应保持运行中，实际 status=%v", disableData["status"])
	}
	if taskListEnabled(t, disableData) {
		t.Fatal("禁用接口的响应里 enabled 应为 false（开关已关，只是这次还在跑）")
	}

	cases := []struct {
		name    string
		status  float64
		enabled bool
	}{
		{name: disabledIdle.Name, status: model.TaskStatusDisabled, enabled: false},
		{name: enabledIdle.Name, status: model.TaskStatusEnabled, enabled: true},
		{name: enabledRunning.Name, status: model.TaskStatusRunning, enabled: true},
		{name: enabledQueued.Name, status: model.TaskStatusQueued, enabled: true},
		{name: disabledQueued.Name, status: model.TaskStatusQueued, enabled: false},
		{name: disabledRunning.Name, status: model.TaskStatusRunning, enabled: false},
		{name: disabledRunningElsewhere.Name, status: model.TaskStatusRunning, enabled: false},
		{name: pendingDisable.Name, status: model.TaskStatusRunning, enabled: false},
	}

	// 默认路径（SQL 分页）与视图路径（内存筛选 / 排序）都要带上 enabled。
	for _, query := range []string{"", "sort_rules=" + url.QueryEscape(`[{"field":"name","direction":"asc"}]`)} {
		payload := listAllTasksPayload(t, engine, token, query)
		for _, testCase := range cases {
			item := findTaskListItem(t, payload, testCase.name)
			if got, _ := item["status"].(float64); got != testCase.status {
				t.Fatalf("前置条件不成立：%q 期望 status=%v，实际 %v（query=%q）", testCase.name, testCase.status, item["status"], query)
			}
			if got := taskListEnabled(t, item); got != testCase.enabled {
				t.Fatalf("%q（status=%v）期望 enabled=%v，实际 %v（query=%q）", testCase.name, testCase.status, testCase.enabled, got, query)
			}
		}
	}

	// 启用接口的响应同样下发 enabled。
	enableRec := performRequest(engine, http.MethodPut, taskActionPath(disabledIdle, "enable"), headers)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("expected enable 200, got %d: %s", enableRec.Code, enableRec.Body.String())
	}
	enableData := responseTaskData(t, decodeJSONMap(t, enableRec))
	if got, _ := enableData["status"].(float64); got != model.TaskStatusEnabled || !taskListEnabled(t, enableData) {
		t.Fatalf("expected enabled idle task in enable response, got status=%v enabled=%v", enableData["status"], enableData["enabled"])
	}
}

// issue #133 复合回归：禁用任务手动运行期间编辑保存一次，不能把它静默重新启用。
// 缺陷链路：编辑保存 -> task_mutate 重载出 status=2 -> UpdateJob -> AddJob 原来只跳过「已禁用 / 待禁用」，
// 于是把这条禁用任务重新注册进调度器 -> 跑完 HasJob 为真、被结算成已启用，cron 条目也是活的。
func TestDisabledTaskManualRunEditedWhileRunningStaysDisabled(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "disabled-run-edit-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	global := service.GetSchedulerV2()
	if global == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	idle := newIdleScheduler(t)

	task := mustCreateTaskWithStatus(t, "手动运行中的禁用任务", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)
	if err := idle.RunNow(task.ID); err != nil {
		t.Fatalf("run disabled task: %v", err)
	}
	if stored := reloadTaskRow(t, task.ID); stored.Status != model.TaskStatusQueued {
		t.Fatalf("前置条件不成立：RunNow 之后应处于排队中，实际 %v", stored.Status)
	}
	// 执行器接手：status 被改写成运行中。
	setTaskStatusInDB(t, task, model.TaskStatusRunning)

	rec := performJSONRequest(engine, http.MethodPut, taskActionPath(task, ""), `{"name":"手动运行中的禁用任务-改名"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := global.ScheduledEntryCount(task.ID); got != -1 {
		t.Fatalf("禁用任务在手动运行期间被编辑保存后不能被注册进调度器，期望 ScheduledEntryCount=-1，实际 %d", got)
	}

	stored := reloadTaskRow(t, task.ID)
	if got := service.ResolveTaskInactiveStatus(&stored); got != model.TaskStatusDisabled {
		t.Fatalf("这次跑完应结算为禁用(%v)，实际 %v —— 任务被静默重新启用了", model.TaskStatusDisabled, got)
	}

	item := findTaskListItem(t, listAllTasksPayload(t, engine, token, ""), "手动运行中的禁用任务-改名")
	if taskListEnabled(t, item) {
		t.Fatal("手动运行中的禁用任务在列表里 enabled 应为 false")
	}
}

// issue #133：排队中的禁用任务被停止，也必须回到禁用；启用任务排队中被停止照旧回到启用。
func TestStopQueuedTaskRestoresItsSwitchState(t *testing.T) {
	testutil.SetupTestEnv(t)
	// 停止接口在没有执行器 / 调度器时也要能兜底改状态，这里刻意不起全局调度器。
	service.ShutdownSchedulerV2()

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "stop-queued-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	idle := newIdleScheduler(t)

	disabledTask := mustCreateTaskWithStatus(t, "排队中被停止的禁用任务", model.TaskTypeManual, "", model.TaskStatusDisabled)
	enabledTask := mustCreateTaskWithStatus(t, "排队中被停止的启用任务", model.TaskTypeManual, "", model.TaskStatusEnabled)
	for _, task := range []*model.Task{disabledTask, enabledTask} {
		if err := idle.RunNow(task.ID); err != nil {
			t.Fatalf("run %q: %v", task.Name, err)
		}
		if stored := reloadTaskRow(t, task.ID); stored.Status != model.TaskStatusQueued {
			t.Fatalf("前置条件不成立：%q 应处于排队中，实际 %v", task.Name, stored.Status)
		}
		rec := performRequest(engine, http.MethodPut, taskActionPath(task, "stop"), headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected stop %q 200, got %d: %s", task.Name, rec.Code, rec.Body.String())
		}
	}

	if got := reloadTaskRow(t, disabledTask.ID).Status; got != model.TaskStatusDisabled {
		t.Fatalf("排队中被停止的禁用任务应回到禁用(%v)，实际 %v", model.TaskStatusDisabled, got)
	}
	if got := reloadTaskRow(t, enabledTask.ID).Status; got != model.TaskStatusEnabled {
		t.Fatalf("排队中被停止的启用任务应回到启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}
}

// 菜单如实给出「启用」之后，用户可以在禁用任务运行期间把它启用。
// 这时不能把 status 直接写成启用（列表会立刻显示成空闲、停止按钮消失，可进程还在跑），
// 只撤掉禁用意图并重新注册调度，跑完自然落回启用。
func TestEnableDisabledTaskDuringManualRunKeepsRunState(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "enable-running-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	global := service.GetSchedulerV2()
	if global == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	idle := newIdleScheduler(t)

	task := mustCreateTaskWithStatus(t, "运行中被启用的禁用任务", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)
	if err := idle.RunNow(task.ID); err != nil {
		t.Fatalf("run disabled task: %v", err)
	}
	setTaskStatusInDB(t, task, model.TaskStatusRunning)

	rec := performRequest(engine, http.MethodPut, taskActionPath(task, "enable"), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected enable 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := responseTaskData(t, decodeJSONMap(t, rec))
	if got, _ := data["status"].(float64); got != model.TaskStatusRunning {
		t.Fatalf("运行中的任务被启用后应仍显示运行中，响应里 status=%v", data["status"])
	}
	if !taskListEnabled(t, data) {
		t.Fatal("启用后响应里的 enabled 应为 true")
	}

	stored := reloadTaskRow(t, task.ID)
	if stored.Status != model.TaskStatusRunning {
		t.Fatalf("运行中的任务被启用后库里 status 不能被改写，期望运行中(%v)，实际 %v", model.TaskStatusRunning, stored.Status)
	}
	if service.HasPendingDisable(&stored) {
		t.Fatal("启用必须撤掉 RunNow 打的待禁用标记")
	}
	if got := global.ScheduledEntryCount(task.ID); got != 1 {
		t.Fatalf("启用后应重新注册 1 条定时触发，实际 ScheduledEntryCount=%d", got)
	}
	if got := service.ResolveTaskInactiveStatus(&stored); got != model.TaskStatusEnabled {
		t.Fatalf("这次跑完应结算为启用(%v)，实际 %v", model.TaskStatusEnabled, got)
	}
}
