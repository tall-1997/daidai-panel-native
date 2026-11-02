package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// 契约 C1 同样覆盖新建 / 编辑 / 复制三个响应：前端拿响应就地刷新这一行，
// 缺了 enabled 会按 status 回退 —— 运行中的禁用任务编辑保存后开关位被算成「开」，要等下一次轮询才纠正。
func TestTaskMutateResponsesCarryEnabledSwitch(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "mutate-enabled-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	createRec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks",
		`{"name":"开关位-新建","command":"task demo.py","cron_expression":"0 0 * * *","task_type":"cron"}`, headers, "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	created := responseTaskData(t, decodeJSONMap(t, createRec))
	if !taskListEnabled(t, created) {
		t.Fatal("新建任务默认启用，创建响应里 enabled 应为 true")
	}
	createdID := uint(created["id"].(float64))

	updateRec := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", createdID),
		`{"name":"开关位-新建-改名"}`, headers, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	if !taskListEnabled(t, responseTaskData(t, decodeJSONMap(t, updateRec))) {
		t.Fatal("编辑启用中的空闲任务，响应里 enabled 应为 true")
	}

	copyRec := performRequest(engine, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%d/copy", createdID), headers)
	if copyRec.Code != http.StatusCreated {
		t.Fatalf("expected copy 201, got %d: %s", copyRec.Code, copyRec.Body.String())
	}
	copied := responseTaskData(t, decodeJSONMap(t, copyRec))
	if got, _ := copied["status"].(float64); got != model.TaskStatusDisabled {
		t.Fatalf("前置条件不成立：复制出来的任务应为禁用，实际 status=%v", copied["status"])
	}
	if taskListEnabled(t, copied) {
		t.Fatal("复制出来的任务是禁用的，响应里 enabled 应为 false")
	}
}

// 运行中的任务编辑保存后，响应里的 enabled 按开关位给：
// 被手动运行的禁用任务是 false（不是按 status=2 回退出来的 true），运行中的启用任务是 true。
func TestUpdateRunningTaskRespondsWithItsSwitchState(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "update-running-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	global := service.GetSchedulerV2()
	if global == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	idle := newIdleScheduler(t)

	disabledRunning := mustCreateTaskWithStatus(t, "手动运行中的禁用任务-编辑响应", model.TaskTypeCron, "0 0 * * *", model.TaskStatusDisabled)
	if err := idle.RunNow(disabledRunning.ID); err != nil {
		t.Fatalf("run disabled task: %v", err)
	}
	setTaskStatusInDB(t, disabledRunning, model.TaskStatusRunning)

	enabledRunning := mustCreateTaskWithStatus(t, "运行中的启用任务-编辑响应", model.TaskTypeCron, "0 0 * * *", model.TaskStatusEnabled)
	if err := global.AddJob(enabledRunning); err != nil {
		t.Fatalf("add job: %v", err)
	}
	setTaskStatusInDB(t, enabledRunning, model.TaskStatusRunning)

	cases := []struct {
		task    *model.Task
		enabled bool
	}{
		{task: disabledRunning, enabled: false},
		{task: enabledRunning, enabled: true},
	}
	for _, testCase := range cases {
		rec := performJSONRequest(engine, http.MethodPut, taskActionPath(testCase.task, ""),
			`{"name":"`+testCase.task.Name+`-改名"}`, headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected update %q 200, got %d: %s", testCase.task.Name, rec.Code, rec.Body.String())
		}
		data := responseTaskData(t, decodeJSONMap(t, rec))
		if got, _ := data["status"].(float64); got != model.TaskStatusRunning {
			t.Fatalf("前置条件不成立：%q 编辑后应仍是运行中，响应里 status=%v", testCase.task.Name, data["status"])
		}
		if got := taskListEnabled(t, data); got != testCase.enabled {
			t.Fatalf("%q 编辑保存后响应里 enabled 期望 %v，实际 %v", testCase.task.Name, testCase.enabled, got)
		}
	}
}
