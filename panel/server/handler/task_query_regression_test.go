package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

func TestTaskListKeepsPinnedDisabledTasksInPinnedArea(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "disabled pinned", Command: "echo disabled-pinned", CronExpression: "0 0 * * *", IsPinned: true},
		{Name: "enabled pinned", Command: "echo enabled-pinned", CronExpression: "0 0 * * *", IsPinned: true},
		{Name: "enabled normal", Command: "echo enabled-normal", CronExpression: "0 0 * * *"},
		{Name: "disabled normal", Command: "echo disabled-normal", CronExpression: "0 0 * * *"},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}
	if err := database.DB.Model(tasks[0]).Update("status", model.TaskStatusDisabled).Error; err != nil {
		t.Fatalf("set disabled status for %q: %v", tasks[0].Name, err)
	}
	if err := database.DB.Model(tasks[1]).Update("status", model.TaskStatusEnabled).Error; err != nil {
		t.Fatalf("set enabled status for %q: %v", tasks[1].Name, err)
	}
	if err := database.DB.Model(tasks[2]).Update("status", model.TaskStatusEnabled).Error; err != nil {
		t.Fatalf("set enabled status for %q: %v", tasks[2].Name, err)
	}
	if err := database.DB.Model(tasks[3]).Update("status", model.TaskStatusDisabled).Error; err != nil {
		t.Fatalf("set disabled status for %q: %v", tasks[3].Name, err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}
	if len(items) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(items))
	}

	gotNames := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		item, ok := items[i].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object at %d, got %#v", i, items[i])
		}
		gotNames = append(gotNames, item["name"].(string))
	}

	wantNames := []string{
		"enabled pinned",
		"disabled pinned",
		"enabled normal",
		"disabled normal",
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("expected order %v, got %v", wantNames, gotNames)
		}
	}
}

// 🔴 这条用例自 v1.9.5 起叫 TestTaskListKeepsStableOrderWhenTaskStatusChangesToRunning，
// 断言的是「任务变成运行中时位置不许变」—— 与现在完全相反。
// v3.2.5 按 issue #118 有意把这个行为翻转了：运行中的任务在默认排序里提到本区最前，
// 因为正在跑的那几条是用户此刻唯一想看的，而它们平时散落在几百条任务里。
// 所以这不是「顺手改了个测试来迁就实现」，是需求本身反向了，用例连名字一起重写。
//
// 顺带钉死边界：排队中（0.5）【不】提前。排队是几秒钟就过去的瞬时态，
// 提上来只会让同一次运行多跳一次 —— 下面 "active third" 是排队中，它必须留在原位。
func TestTaskListMovesRunningTaskToFrontOfItsGroup(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "running-first-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "active first", Command: "echo first", CronExpression: "0 0 * * *", SortOrder: 10},
		{Name: "active second", Command: "echo second", CronExpression: "0 0 * * *", SortOrder: 20},
		{Name: "active third", Command: "echo third", CronExpression: "0 0 * * *", SortOrder: 30},
		{Name: "disabled last", Command: "echo disabled", CronExpression: "0 0 * * *", SortOrder: 40},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}
	if err := database.DB.Model(tasks[0]).Update("status", model.TaskStatusEnabled).Error; err != nil {
		t.Fatalf("set enabled status for %q: %v", tasks[0].Name, err)
	}
	if err := database.DB.Model(tasks[1]).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("set running status for %q: %v", tasks[1].Name, err)
	}
	if err := database.DB.Model(tasks[2]).Update("status", model.TaskStatusQueued).Error; err != nil {
		t.Fatalf("set queued status for %q: %v", tasks[2].Name, err)
	}
	if err := database.DB.Model(tasks[3]).Update("status", model.TaskStatusDisabled).Error; err != nil {
		t.Fatalf("set disabled status for %q: %v", tasks[3].Name, err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}
	if len(items) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(items))
	}

	gotNames := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		item, ok := items[i].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object at %d, got %#v", i, items[i])
		}
		gotNames = append(gotNames, item["name"].(string))
	}

	wantNames := []string{
		// 运行中的那条从第二位提到第一位；
		// 排队中的 "active third" 与空闲的 "active first" 之间的相对次序一个字节都没变。
		"active second",
		"active first",
		"active third",
		"disabled last",
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("expected running-first order %v, got %v", wantNames, gotNames)
		}
	}
}

// 运行中优先只是【临时】提前，跑完必须原样落回自己的 list_order 位置 ——
// 否则用户拖出来的顺序会被每一次运行悄悄改写一次。
func TestTaskListRestoresRunningTaskPositionAfterItFinishes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "running-restore-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 用 list_order 拉开顺序：它是拖拽序，正是这条用例要保护的东西。
	tasks := []*model.Task{
		{Name: "dragged first", Command: "echo one", CronExpression: "0 0 * * *", ListOrder: 10},
		{Name: "dragged second", Command: "echo two", CronExpression: "0 0 * * *", ListOrder: 20},
		{Name: "dragged third", Command: "echo three", CronExpression: "0 0 * * *", ListOrder: 30},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
		if err := database.DB.Model(task).Update("status", model.TaskStatusEnabled).Error; err != nil {
			t.Fatalf("set enabled status for %q: %v", task.Name, err)
		}
	}

	listTaskNames := func(t *testing.T) []string {
		t.Helper()

		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		items, ok := decodeJSONMap(t, rec)["data"].([]interface{})
		if !ok {
			t.Fatalf("expected data array, got %#v", rec.Body.String())
		}
		names := make([]string, 0, len(items))
		for _, raw := range items {
			item, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("expected task object, got %#v", raw)
			}
			names = append(names, item["name"].(string))
		}
		return names
	}

	assertOrder := func(t *testing.T, stage string, want []string) {
		t.Helper()

		got := listTaskNames(t)
		if len(got) < len(want) {
			t.Fatalf("%s: expected at least %d tasks, got %v", stage, len(want), got)
		}
		for i, name := range want {
			if got[i] != name {
				t.Fatalf("%s: expected order %v, got %v", stage, want, got)
			}
		}
	}

	assertOrder(t, "运行前", []string{"dragged first", "dragged second", "dragged third"})

	// 中间那条开始运行 → 提到最前。
	if err := database.DB.Model(tasks[1]).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("set running status: %v", err)
	}
	assertOrder(t, "运行中", []string{"dragged second", "dragged first", "dragged third"})

	// 跑完回到已启用 → 顺序原样退回，拖拽序没被这次运行改写。
	if err := database.DB.Model(tasks[1]).Update("status", model.TaskStatusEnabled).Error; err != nil {
		t.Fatalf("restore enabled status: %v", err)
	}
	assertOrder(t, "运行后", []string{"dragged first", "dragged second", "dragged third"})
}

// 🔴 这条钉死「只改默认序」这个边界：运行中优先只写在 defaultTaskListLess / applyDefaultTaskListOrdering，
// 没写进 sortPreparedTaskListItems 的规则层。用户显式点了「名称 A→Z」，
// 要的就是纯粹按名称排，运行中不该越过他的规则插到第一行 ——
// 它只在「用户给的规则全部打平、回落默认序」时才生效。
func TestTaskListKeepsExplicitSortRulesAboveRunningPriority(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "running-sort-rule-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 运行中的那条名字排最后，一旦运行中优先漏进规则层，它就会冒到第一行。
	tasks := []*model.Task{
		{Name: "alpha idle", Command: "echo alpha", CronExpression: "0 0 * * *"},
		{Name: "beta idle", Command: "echo beta", CronExpression: "0 0 * * *"},
		{Name: "zulu running", Command: "echo zulu", CronExpression: "0 0 * * *"},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}
	for _, task := range tasks[:2] {
		if err := database.DB.Model(task).Update("status", model.TaskStatusEnabled).Error; err != nil {
			t.Fatalf("set enabled status for %q: %v", task.Name, err)
		}
	}
	if err := database.DB.Model(tasks[2]).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("set running status for %q: %v", tasks[2].Name, err)
	}

	sortJSON := `[{"field":"name","direction":"asc"}]`
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items, ok := decodeJSONMap(t, rec)["data"].([]interface{})
	if !ok || len(items) < 3 {
		t.Fatalf("expected 3 sorted tasks, got %s", rec.Body.String())
	}

	gotNames := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		item, ok := items[i].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object at %d, got %#v", i, items[i])
		}
		gotNames = append(gotNames, item["name"].(string))
	}

	wantNames := []string{"alpha idle", "beta idle", "zulu running"}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("显式按名称排序时运行中不该越位，expected %v, got %v", wantNames, gotNames)
		}
	}
}

// 置顶区在运行中优先【之上】：置顶是用户主动设置的展示优先级，
// 普通任务开始运行不该把置顶任务顶下去。运行中优先只在各自的置顶区内部生效。
func TestTaskListKeepsPinnedAreaAboveRunningTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "running-pinned-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "pinned idle", Command: "echo pinned-idle", CronExpression: "0 0 * * *", IsPinned: true, ListOrder: 10},
		{Name: "pinned running", Command: "echo pinned-running", CronExpression: "0 0 * * *", IsPinned: true, ListOrder: 20},
		{Name: "normal idle", Command: "echo normal-idle", CronExpression: "0 0 * * *", ListOrder: 30},
		{Name: "normal running", Command: "echo normal-running", CronExpression: "0 0 * * *", ListOrder: 40},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}
	for _, index := range []int{0, 2} {
		if err := database.DB.Model(tasks[index]).Update("status", model.TaskStatusEnabled).Error; err != nil {
			t.Fatalf("set enabled status for %q: %v", tasks[index].Name, err)
		}
	}
	for _, index := range []int{1, 3} {
		if err := database.DB.Model(tasks[index]).Update("status", model.TaskStatusRunning).Error; err != nil {
			t.Fatalf("set running status for %q: %v", tasks[index].Name, err)
		}
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items, ok := decodeJSONMap(t, rec)["data"].([]interface{})
	if !ok || len(items) < 4 {
		t.Fatalf("expected at least 4 tasks, got %s", rec.Body.String())
	}

	gotNames := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		item, ok := items[i].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object at %d, got %#v", i, items[i])
		}
		gotNames = append(gotNames, item["name"].(string))
	}

	wantNames := []string{
		// 置顶区内部：运行中的提到最前。
		"pinned running",
		// 置顶的空闲任务仍然排在所有非置顶任务之前，包括正在运行的那条。
		"pinned idle",
		// 非置顶区内部：运行中的提到最前。
		"normal running",
		"normal idle",
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("expected pinned-above-running order %v, got %v", wantNames, gotNames)
		}
	}
}

func TestTaskListMapsSubscriptionLabelsToSubscriptionNames(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	subscription := &model.Subscription{
		Name:    "kele",
		Type:    model.SubTypeGitRepo,
		URL:     "https://github.com/Aellyt/kele.git",
		Enabled: true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	task := &model.Task{
		Name:           "subscription task",
		Command:        "task kele/main.js",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	task.SetLabelsFromSlice([]string{"manual", "subscription:" + strconv.FormatUint(uint64(subscription.ID), 10)})
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty data array, got %#v", payload["data"])
	}

	var firstItem map[string]interface{}
	for _, rawItem := range items {
		taskItem, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		if name, ok := taskItem["name"].(string); ok && name == task.Name {
			firstItem = taskItem
			break
		}
	}
	if firstItem == nil {
		t.Fatalf("expected to find task %q in payload, got %#v", task.Name, items)
	}

	displayLabels, ok := firstItem["display_labels"].([]interface{})
	if !ok {
		t.Fatalf("expected display_labels array, got %#v", firstItem["display_labels"])
	}

	got := make([]string, 0, len(displayLabels))
	for _, item := range displayLabels {
		got = append(got, item.(string))
	}

	expected := []string{"manual", "kele"}
	for _, want := range expected {
		found := false
		for _, label := range got {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected display_labels to contain %q, got %v", want, got)
		}
	}
}

func TestTaskListIncludesEditableSettingsForEditBackfill(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	channel := &model.NotifyChannel{
		Name:    "任务成功推送",
		Type:    "telegram",
		Config:  `{"token":"demo","chat_id":"123"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	parentTask := &model.Task{
		Name:           "parent task",
		Command:        "echo parent",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(parentTask).Error; err != nil {
		t.Fatalf("create parent task: %v", err)
	}

	beforeHook := "echo before"
	afterHook := "echo after"

	task := &model.Task{
		Name:                   "notify success task",
		Command:                "echo done",
		CronExpression:         "0 0 * * *",
		Status:                 model.TaskStatusEnabled,
		Timeout:                7200,
		MaxRetries:             3,
		RetryInterval:          180,
		NotifyOnFailure:        false,
		NotifyOnSuccess:        true,
		NotificationChannelID:  &channel.ID,
		DependsOn:              &parentTask.ID,
		TaskBefore:             &beforeHook,
		TaskAfter:              &afterHook,
		AllowMultipleInstances: true,
	}
	task.SetLabelsFromSlice([]string{"manual", "release"})
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := database.DB.Model(task).Updates(map[string]interface{}{
		"notify_on_failure":        false,
		"notify_on_success":        true,
		"notification_channel_id":  channel.ID,
		"depends_on":               parentTask.ID,
		"task_before":              beforeHook,
		"task_after":               afterHook,
		"allow_multiple_instances": true,
		"timeout":                  7200,
		"max_retries":              3,
		"retry_interval":           180,
	}).Error; err != nil {
		t.Fatalf("update task fields: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty data array, got %#v", payload["data"])
	}

	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task object, got %#v", items[0])
	}

	gotValue, exists := firstItem["notify_on_success"]
	if !exists {
		t.Fatalf("expected notify_on_success in task payload, got keys=%v", firstItem)
	}

	gotBool, ok := gotValue.(bool)
	if !ok {
		t.Fatalf("expected notify_on_success to be bool, got %#v", gotValue)
	}
	if !gotBool {
		t.Fatalf("expected notify_on_success=true, got false")
	}

	if gotValue, ok := firstItem["notify_on_failure"].(bool); !ok || gotValue {
		t.Fatalf("expected notify_on_failure=false, got %#v", firstItem["notify_on_failure"])
	}
	if gotValue, ok := firstItem["notification_channel_id"].(float64); !ok || uint(gotValue) != channel.ID {
		t.Fatalf("expected notification_channel_id=%d, got %#v", channel.ID, firstItem["notification_channel_id"])
	}
	if gotValue, ok := firstItem["notification_channel_name"].(string); !ok || gotValue != channel.Name {
		t.Fatalf("expected notification_channel_name=%q, got %#v", channel.Name, firstItem["notification_channel_name"])
	}
	if gotValue, ok := firstItem["allow_multiple_instances"].(bool); !ok || !gotValue {
		t.Fatalf("expected allow_multiple_instances=true, got %#v", firstItem["allow_multiple_instances"])
	}
	if gotValue, ok := firstItem["timeout"].(float64); !ok || gotValue != 7200 {
		t.Fatalf("expected timeout=7200, got %#v", firstItem["timeout"])
	}
	if gotValue, ok := firstItem["max_retries"].(float64); !ok || gotValue != 3 {
		t.Fatalf("expected max_retries=3, got %#v", firstItem["max_retries"])
	}
	if gotValue, ok := firstItem["retry_interval"].(float64); !ok || gotValue != 180 {
		t.Fatalf("expected retry_interval=180, got %#v", firstItem["retry_interval"])
	}
	if gotValue, ok := firstItem["depends_on"].(float64); !ok || uint(gotValue) != parentTask.ID {
		t.Fatalf("expected depends_on=%d, got %#v", parentTask.ID, firstItem["depends_on"])
	}
	if gotValue, ok := firstItem["task_before"].(string); !ok || gotValue != beforeHook {
		t.Fatalf("expected task_before=%q, got %#v", beforeHook, firstItem["task_before"])
	}
	if gotValue, ok := firstItem["task_after"].(string); !ok || gotValue != afterHook {
		t.Fatalf("expected task_after=%q, got %#v", afterHook, firstItem["task_after"])
	}
	labels, ok := firstItem["labels"].([]interface{})
	if !ok || len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %#v", firstItem["labels"])
	}
}

func TestTaskCreatePersistsFalseNotifyOnFailure(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	channel := &model.NotifyChannel{
		Name:    "专属渠道",
		Type:    "telegram",
		Config:  `{"token":"demo","chat_id":"123"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		fmt.Sprintf(`{
			"name":"create notify false",
			"command":"echo hi",
			"cron_expression":"0 0 * * *",
			"notify_on_failure":false,
			"notify_on_success":true,
			"notification_channel_id":%d
		}`, channel.ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	item, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload, got %#v", payload["data"])
	}
	if got, ok := item["notify_on_failure"].(bool); !ok || got {
		t.Fatalf("expected notify_on_failure=false, got %#v", item["notify_on_failure"])
	}
	if got, ok := item["notify_on_success"].(bool); !ok || !got {
		t.Fatalf("expected notify_on_success=true, got %#v", item["notify_on_success"])
	}
	if got, ok := item["notification_channel_id"].(float64); !ok || uint(got) != channel.ID {
		t.Fatalf("expected notification_channel_id=%d, got %#v", channel.ID, item["notification_channel_id"])
	}
}

func TestTaskCreateDefaultsNotifyOnFailureToFalse(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "default-notify-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{
			"name":"create default notify false",
			"command":"echo hi",
			"cron_expression":"0 0 * * *"
		}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	item, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload, got %#v", payload["data"])
	}
	if got, ok := item["notify_on_failure"].(bool); !ok || got {
		t.Fatalf("expected default notify_on_failure=false, got %#v", item["notify_on_failure"])
	}
}

func TestTaskCreateManualTypeAllowsEmptyCron(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{
			"name":"manual task",
			"command":"echo hi",
			"task_type":"manual"
		}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	item, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload, got %#v", payload["data"])
	}
	if got, ok := item["task_type"].(string); !ok || got != model.TaskTypeManual {
		t.Fatalf("expected task_type=%q, got %#v", model.TaskTypeManual, item["task_type"])
	}
	if got, ok := item["cron_expression"].(string); !ok || got != "" {
		t.Fatalf("expected empty cron_expression for manual task, got %#v", item["cron_expression"])
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	listPayload := decodeJSONMap(t, listRec)
	items, ok := listPayload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty task list, got %#v", listPayload["data"])
	}
	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task object, got %#v", items[0])
	}
	if _, exists := firstItem["next_run_at"]; exists {
		t.Fatalf("did not expect next_run_at for manual task, got %#v", firstItem["next_run_at"])
	}
}

func TestTaskCopyKeepsDisabledStatus(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:            "copy source",
		Command:         "echo source",
		CronExpression:  "0 0 * * *",
		Status:          model.TaskStatusEnabled,
		NotifyOnFailure: false,
	}
	randomDelay := 18
	task.RandomDelaySeconds = &randomDelay
	channel := &model.NotifyChannel{
		Name:    "复制渠道",
		Type:    "telegram",
		Config:  `{"token":"demo","chat_id":"123"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}
	task.NotificationChannelID = &channel.ID
	if err := database.DB.Select("*").Create(task).Error; err != nil {
		t.Fatalf("create source task: %v", err)
	}

	rec := performRequest(engine, http.MethodPost, "/api/v1/tasks/"+strconv.FormatUint(uint64(task.ID), 10)+"/copy", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	item, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload, got %#v", payload["data"])
	}
	if got, ok := item["status"].(float64); !ok || got != model.TaskStatusDisabled {
		t.Fatalf("expected copied task status disabled, got %#v", item["status"])
	}
	if got, ok := item["notify_on_failure"].(bool); !ok || got {
		t.Fatalf("expected copied task notify_on_failure=false, got %#v", item["notify_on_failure"])
	}
	if got, ok := item["notification_channel_id"].(float64); !ok || uint(got) != channel.ID {
		t.Fatalf("expected copied task notification_channel_id=%d, got %#v", channel.ID, item["notification_channel_id"])
	}
	if got, ok := item["random_delay_seconds"].(float64); !ok || int(got) != randomDelay {
		t.Fatalf("expected copied task random_delay_seconds=%d, got %#v", randomDelay, item["random_delay_seconds"])
	}
}

func TestTaskCreateAndUpdateRandomDelaySettings(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "delay-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	createRec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"delay task","command":"task demo.py","cron_expression":"0 0 * * *","task_type":"cron","random_delay_seconds":25}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	createPayload := decodeJSONMap(t, createRec)
	created, ok := createPayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected created task payload, got %#v", createPayload["data"])
	}
	if got, ok := created["random_delay_seconds"].(float64); !ok || int(got) != 25 {
		t.Fatalf("expected random_delay_seconds=25, got %#v", created["random_delay_seconds"])
	}
	taskID := uint(created["id"].(float64))

	exportRec := performRequest(engine, http.MethodGet, "/api/v1/tasks/export", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected export 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}
	exportPayload := decodeJSONMap(t, exportRec)
	exportedTasks, ok := exportPayload["data"].([]interface{})
	if !ok || len(exportedTasks) == 0 {
		t.Fatalf("expected exported tasks, got %#v", exportPayload["data"])
	}
	exportedTask, ok := exportedTasks[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected exported task object, got %#v", exportedTasks[0])
	}
	if got, ok := exportedTask["random_delay_seconds"].(float64); !ok || int(got) != 25 {
		t.Fatalf("expected exported random_delay_seconds=25, got %#v", exportedTask["random_delay_seconds"])
	}

	disableRec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/"+strconv.FormatUint(uint64(taskID), 10),
		`{"random_delay_seconds":0}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected disable update 200, got %d: %s", disableRec.Code, disableRec.Body.String())
	}
	disablePayload := decodeJSONMap(t, disableRec)
	disabledTask, ok := disablePayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected disabled task payload, got %#v", disablePayload["data"])
	}
	if got, ok := disabledTask["random_delay_seconds"].(float64); !ok || int(got) != 0 {
		t.Fatalf("expected disabled random_delay_seconds=0, got %#v", disabledTask["random_delay_seconds"])
	}

	inheritRec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/"+strconv.FormatUint(uint64(taskID), 10),
		`{"random_delay_seconds":null}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if inheritRec.Code != http.StatusOK {
		t.Fatalf("expected inherit update 200, got %d: %s", inheritRec.Code, inheritRec.Body.String())
	}
	inheritPayload := decodeJSONMap(t, inheritRec)
	inheritedTask, ok := inheritPayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inherited task payload, got %#v", inheritPayload["data"])
	}
	if value, exists := inheritedTask["random_delay_seconds"]; exists && value != nil {
		t.Fatalf("expected inherited random_delay_seconds=nil, got %#v", value)
	}

	var stored model.Task
	if err := database.DB.First(&stored, taskID).Error; err != nil {
		t.Fatalf("load updated task: %v", err)
	}
	if stored.RandomDelaySeconds != nil {
		t.Fatalf("expected stored random_delay_seconds=nil, got %v", *stored.RandomDelaySeconds)
	}
}

func TestTaskDisableRunningTaskKeepsCurrentExecutionState(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "running-disable-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:           "running disable task",
		Command:        "echo running",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if scheduler := service.GetSchedulerV2(); scheduler != nil {
		if err := scheduler.AddJob(task); err != nil {
			t.Fatalf("add job: %v", err)
		}
		if !scheduler.HasJob(task.ID) {
			t.Fatalf("expected task %d to be registered before disable", task.ID)
		}
	}

	if err := database.DB.Model(task).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatalf("set running status: %v", err)
	}

	rec := performRequest(engine, http.MethodPut, "/api/v1/tasks/"+strconv.FormatUint(uint64(task.ID), 10)+"/disable", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected disable 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	if got, _ := payload["message"].(string); got != "已设置为禁用，当前执行结束后生效" {
		t.Fatalf("unexpected disable message: %#v", payload["message"])
	}

	item, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task payload, got %#v", payload["data"])
	}
	if got, ok := item["status"].(float64); !ok || got != model.TaskStatusRunning {
		t.Fatalf("expected running task to stay running in payload, got %#v", item["status"])
	}

	var stored model.Task
	if err := database.DB.First(&stored, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if stored.Status != model.TaskStatusRunning {
		t.Fatalf("expected stored task to remain running, got %v", stored.Status)
	}

	scheduler := service.GetSchedulerV2()
	if scheduler == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	if scheduler.HasJob(task.ID) {
		t.Fatalf("expected task %d schedule to be removed after disable", task.ID)
	}
	if got := service.ResolveTaskInactiveStatus(&stored); got != model.TaskStatusDisabled {
		t.Fatalf("expected task to resolve to disabled after current run, got %v", got)
	}
}

func TestTaskImportPreservesRandomDelaySettings(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "import-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks/import",
		`{"tasks":[{"name":"imported delay task","command":"task imported.py","python_version":"3.11","cron_expression":"0 0 * * *","task_type":"cron","random_delay_seconds":12}]}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected import 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var task model.Task
	if err := database.DB.Where("name = ?", "imported delay task").First(&task).Error; err != nil {
		t.Fatalf("load imported task: %v", err)
	}
	if task.RandomDelaySeconds == nil || *task.RandomDelaySeconds != 12 {
		t.Fatalf("expected imported random_delay_seconds=12, got %#v", task.RandomDelaySeconds)
	}
	if task.PythonVersion != "3.11" {
		t.Fatalf("expected imported python_version=3.11, got %q", task.PythonVersion)
	}
}

func TestTaskImportPreservesExportedStatusAndRegistersEnabledCron(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "import-status-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks/import",
		`{"tasks":[{"name":"imported enabled task","command":"echo enabled","cron_expression":"0 0 * * *","task_type":"cron","status":1},{"name":"imported disabled task","command":"echo disabled","cron_expression":"0 0 * * *","task_type":"cron","status":0}]}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected import 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var enabledTask model.Task
	if err := database.DB.Where("name = ?", "imported enabled task").First(&enabledTask).Error; err != nil {
		t.Fatalf("load imported enabled task: %v", err)
	}
	if enabledTask.Status != model.TaskStatusEnabled {
		t.Fatalf("expected imported status=1 to stay enabled, got %v", enabledTask.Status)
	}

	var disabledTask model.Task
	if err := database.DB.Where("name = ?", "imported disabled task").First(&disabledTask).Error; err != nil {
		t.Fatalf("load imported disabled task: %v", err)
	}
	if disabledTask.Status != model.TaskStatusDisabled {
		t.Fatalf("expected imported status=0 to stay disabled, got %v", disabledTask.Status)
	}

	scheduler := service.GetSchedulerV2()
	if scheduler == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	if !scheduler.HasJob(enabledTask.ID) {
		t.Fatalf("expected enabled imported cron task %d to be registered", enabledTask.ID)
	}
	if scheduler.HasJob(disabledTask.ID) {
		t.Fatalf("did not expect disabled imported cron task %d to be registered", disabledTask.ID)
	}
}
func TestTaskNotificationChannelsExposeSafeFieldsOnly(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	channel := &model.NotifyChannel{
		Name:    "任务通知",
		Type:    "telegram",
		Config:  `{"token":"secret","chat_id":"123"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks/notification-channels", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty channel list, got %#v", payload["data"])
	}

	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected channel object, got %#v", items[0])
	}
	if got := firstItem["name"]; got != channel.Name {
		t.Fatalf("expected channel name %q, got %#v", channel.Name, got)
	}
	if _, exists := firstItem["config"]; exists {
		t.Fatalf("did not expect config field in safe task channel payload: %#v", firstItem)
	}
}

func TestTaskListAppliesViewFiltersBeforePagination(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "view-filter-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	for i := 1; i <= 25; i++ {
		task := &model.Task{
			Name:           fmt.Sprintf("task-%02d", i),
			Command:        fmt.Sprintf("task scripts/demo-%02d.py", i),
			CronExpression: "0 0 * * *",
			Status:         model.TaskStatusEnabled,
		}
		if i > 20 {
			task.SetLabelsFromSlice([]string{"目标标签"})
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %d: %v", i, err)
		}
	}

	filterJSON := `[{"field":"labels","operator":"equals","value":"目标标签"}]`
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?page=1&page_size=20&filters="+url.QueryEscape(filterJSON), map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	if got, ok := payload["total"].(float64); !ok || int(got) != 5 {
		t.Fatalf("expected filtered total 5, got %#v", payload["total"])
	}

	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 filtered tasks on first page, got %d", len(items))
	}
}

func TestTaskListFiltersLabelsAgainstDisplayLabels(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "display-label-filter", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	subscription := &model.Subscription{
		Name:    "华星电信999答题",
		Type:    model.SubTypeGitRepo,
		URL:     "https://example.com/demo.git",
		Enabled: true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	matchedTask := &model.Task{
		Name:           "matched task",
		Command:        "task telecom/main.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	matchedTask.SetLabelsFromSlice([]string{"subscription:" + strconv.FormatUint(uint64(subscription.ID), 10)})
	if err := database.DB.Create(matchedTask).Error; err != nil {
		t.Fatalf("create matched task: %v", err)
	}

	unmatchedTask := &model.Task{
		Name:           "other task",
		Command:        "task other/main.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	unmatchedTask.SetLabelsFromSlice([]string{"普通标签"})
	if err := database.DB.Create(unmatchedTask).Error; err != nil {
		t.Fatalf("create unmatched task: %v", err)
	}

	filterJSON := `[{"field":"labels","operator":"equals","value":"华星电信999答题"}]`
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?filters="+url.QueryEscape(filterJSON), map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected exactly one matched task, got %#v", payload["data"])
	}

	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task object, got %#v", items[0])
	}
	if got := item["name"]; got != matchedTask.Name {
		t.Fatalf("expected matched task %q, got %#v", matchedTask.Name, got)
	}
}

func TestTaskListAppliesViewSortRules(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "view-sort-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	for _, name := range []string{"task-c", "task-a", "task-b"} {
		task := &model.Task{
			Name:           name,
			Command:        "task demo.py",
			CronExpression: "0 0 * * *",
			Status:         model.TaskStatusEnabled,
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", name, err)
		}
	}

	sortJSON := `[{"field":"name","direction":"asc"}]`
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?page=1&page_size=2&sort_rules="+url.QueryEscape(sortJSON), map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("expected first page with 2 sorted tasks, got %#v", payload["data"])
	}

	firstItem, _ := items[0].(map[string]interface{})
	secondItem, _ := items[1].(map[string]interface{})
	if firstItem["name"] != "task-a" || secondItem["name"] != "task-b" {
		t.Fatalf("expected sorted names [task-a task-b], got [%v %v]", firstItem["name"], secondItem["name"])
	}
}

// 从任务列表响应里挑出指定名字的那条。下面两条同名标签用例都要用，抽出来免得抄两遍。
func findTaskListItem(t *testing.T, payload map[string]interface{}, name string) map[string]interface{} {
	t.Helper()

	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if got, ok := item["name"].(string); ok && got == name {
			return item
		}
	}
	t.Fatalf("expected to find task %q in payload, got %#v", name, items)
	return nil
}

// 把响应里的字符串数组字段读成 []string。这两条用例校验的是【条数】，
// 所以不能像老用例那样只做 contains 判定，必须逐条取出来比。
func taskListStringField(t *testing.T, item map[string]interface{}, field string) []string {
	t.Helper()

	raw, ok := item[field].([]interface{})
	if !ok {
		t.Fatalf("expected %s array, got %#v", field, item[field])
	}
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		value, ok := entry.(string)
		if !ok {
			t.Fatalf("expected %s entries to be strings, got %#v", field, entry)
		}
		values = append(values, value)
	}
	return values
}

// 订阅名与分组名重名时，display_labels 必须留下两条同名项：一条归分组开关、一条归订阅开关。
// 合并成一条的话前端「显示设置」里的两个开关就串台成一个，关掉分组会连订阅标签一起藏掉（issue #109-3）。
func TestTaskListKeepsGroupAndSubscriptionLabelsWhenNamesCollide(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "label-collision-group", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	subscription := &model.Subscription{
		Name:    "娱乐",
		Type:    model.SubTypeGitRepo,
		URL:     "https://example.com/entertainment.git",
		Enabled: true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	task := &model.Task{
		Name:           "group name equals subscription name",
		Command:        "task entertainment/main.js",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	task.SetLabelsFromSlice([]string{"分组:娱乐", "subscription:" + strconv.FormatUint(uint64(subscription.ID), 10)})
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item := findTaskListItem(t, decodeJSONMap(t, rec), task.Name)

	displayLabels := taskListStringField(t, item, "display_labels")
	if len(displayLabels) != 2 || displayLabels[0] != "娱乐" || displayLabels[1] != "娱乐" {
		t.Fatalf("expected display_labels [娱乐 娱乐]（分组一条 + 订阅一条）, got %v", displayLabels)
	}

	// 订阅那份仍然只有一条：前端靠它的出现次数认领「哪几条属于订阅」，多一条就会把分组那条也算成订阅。
	subscriptionLabels := taskListStringField(t, item, "subscription_labels")
	if len(subscriptionLabels) != 1 || subscriptionLabels[0] != "娱乐" {
		t.Fatalf("expected subscription_labels [娱乐], got %v", subscriptionLabels)
	}
}

// 自定义标签与订阅名重名时同样必须留下两条：这两类共用过一个去重集合，
// 合并后表现为「关掉自定义藏不掉它、关掉订阅反而把它藏了」。
func TestTaskListKeepsCustomAndSubscriptionLabelsWhenNamesCollide(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "label-collision-custom", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	subscription := &model.Subscription{
		Name:    "娱乐",
		Type:    model.SubTypeGitRepo,
		URL:     "https://example.com/entertainment.git",
		Enabled: true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	task := &model.Task{
		Name:           "custom label equals subscription name",
		Command:        "task entertainment/main.js",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	task.SetLabelsFromSlice([]string{"娱乐", "subscription:" + strconv.FormatUint(uint64(subscription.ID), 10)})
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item := findTaskListItem(t, decodeJSONMap(t, rec), task.Name)

	displayLabels := taskListStringField(t, item, "display_labels")
	if len(displayLabels) != 2 || displayLabels[0] != "娱乐" || displayLabels[1] != "娱乐" {
		t.Fatalf("expected display_labels [娱乐 娱乐]（自定义一条 + 订阅一条）, got %v", displayLabels)
	}

	subscriptionLabels := taskListStringField(t, item, "subscription_labels")
	if len(subscriptionLabels) != 1 || subscriptionLabels[0] != "娱乐" {
		t.Fatalf("expected subscription_labels [娱乐], got %v", subscriptionLabels)
	}
}

// 读任务列表里的 schedule_hint。handler 只在「有话要说」时才下发这个字段，
// 正常任务上它压根不存在，所以缺省一律当空串处理，不能直接断言类型。
func taskListScheduleHint(t *testing.T, item map[string]interface{}) string {
	t.Helper()

	raw, exists := item["schedule_hint"]
	if !exists || raw == nil {
		return ""
	}
	hint, ok := raw.(string)
	if !ok {
		t.Fatalf("expected schedule_hint to be string, got %#v", raw)
	}
	return hint
}

// 数据库里「已启用 + 有 cron 表达式」不代表任务真的会被触发：
// 只要调度器里没有它的 cron 条目，它就永远不会跑，列表却照样显示下次运行时间，
// 用户只能看到「任务不执行、也没有任何日志」（issue #115）。
// schedule_hint 就是把这种静默失联翻出来给用户看，且不能动 next_run_at 的既有行为。
func TestTaskListExposesScheduleHintForSilentlyUnscheduledTasks(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "schedule-hint-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 三条任务都在调度器起来之后才创建，所以默认谁都没被装载进去；
	// 下面只对 registered 那条显式调用 AddJob，构造出「注册上了」与「没注册上」的对照。
	manualTask := &model.Task{
		Name:     "manual hint task",
		Command:  "echo manual",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	unregisteredTask := &model.Task{
		Name:           "unregistered cron task",
		Command:        "echo lost",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	registeredTask := &model.Task{
		Name:           "registered cron task",
		Command:        "echo ok",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	for _, task := range []*model.Task{manualTask, unregisteredTask, registeredTask} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	scheduler := service.GetSchedulerV2()
	if scheduler == nil {
		t.Fatal("expected scheduler to be initialized")
	}
	if err := scheduler.AddJob(registeredTask); err != nil {
		t.Fatalf("add job for %q: %v", registeredTask.Name, err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)

	manualItem := findTaskListItem(t, payload, manualTask.Name)
	if got := taskListScheduleHint(t, manualItem); got != "该任务类型不会自动触发，只能手动运行" {
		t.Fatalf("expected manual task schedule_hint, got %q", got)
	}
	// next_run_at 的既有行为一个字节都不能变：手动任务从来就不下发它。
	if value, exists := manualItem["next_run_at"]; exists {
		t.Fatalf("did not expect next_run_at for manual task, got %#v", value)
	}

	unregisteredItem := findTaskListItem(t, payload, unregisteredTask.Name)
	if got := taskListScheduleHint(t, unregisteredItem); got != "未注册到调度器，重新保存一次任务即可恢复" {
		t.Fatalf("expected unregistered cron task schedule_hint, got %q", got)
	}
	// 关键对照：它看着完全正常 —— 有下次运行时间，只是永远不会跑。
	if _, exists := unregisteredItem["next_run_at"]; !exists {
		t.Fatalf("expected next_run_at to stay present for unregistered cron task, got %#v", unregisteredItem)
	}

	registeredItem := findTaskListItem(t, payload, registeredTask.Name)
	if got := taskListScheduleHint(t, registeredItem); got != "" {
		t.Fatalf("expected no schedule_hint for registered cron task, got %q", got)
	}
	if _, exists := registeredItem["next_run_at"]; !exists {
		t.Fatalf("expected next_run_at for registered cron task, got %#v", registeredItem)
	}
}

// 两条提示的判据不一样，所以对「调度器没起来」的反应也不一样：
//   - 「该任务类型不会自动触发」纯看任务类型，不问调度器，任何时候都该下发；
//   - 「未注册到调度器」必须有调度器才判得准，没有调度器时每条任务都查不到 cron 条目，
//     下发就是全量误报，比不报更糟，所以一条都不能发。
//
// 另外已禁用的任务整段跳过：它本来就不会自动触发，状态列已经写着「已禁用」，再挂提示只是噪音。
func TestTaskListScheduleHintWithoutSchedulerAndForDisabledTasks(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "no-scheduler-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	manualTask := &model.Task{
		Name:     "manual task without scheduler",
		Command:  "echo manual",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	cronTask := &model.Task{
		Name:           "cron task without scheduler",
		Command:        "echo cron",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	startupTask := &model.Task{
		Name:     "startup task without scheduler",
		Command:  "echo startup",
		TaskType: model.TaskTypeStartup,
		Status:   model.TaskStatusEnabled,
	}
	disabledManualTask := &model.Task{
		Name:     "disabled manual task",
		Command:  "echo disabled",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusDisabled,
	}
	disabledCronTask := &model.Task{
		Name:           "disabled cron task",
		Command:        "echo disabled cron",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusDisabled,
	}
	for _, task := range []*model.Task{manualTask, cronTask, startupTask, disabledManualTask, disabledCronTask} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	if service.GetSchedulerV2() != nil {
		t.Fatal("expected no scheduler for this case")
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)

	manualItem := findTaskListItem(t, payload, manualTask.Name)
	if got := taskListScheduleHint(t, manualItem); got != "该任务类型不会自动触发，只能手动运行" {
		t.Fatalf("expected manual task schedule_hint even without scheduler, got %q", got)
	}

	// 开机运行**会**自动跑，只是按面板启动、不按时间，不能跟手动任务共用「只能手动运行」那句话。
	startupItem := findTaskListItem(t, payload, startupTask.Name)
	if got := taskListScheduleHint(t, startupItem); got != "该任务只在面板启动时自动执行一次，不按定时规则触发" {
		t.Fatalf("expected startup task to get its own schedule_hint, got %q", got)
	}

	for _, name := range []string{cronTask.Name, disabledManualTask.Name, disabledCronTask.Name} {
		item := findTaskListItem(t, payload, name)
		if got := taskListScheduleHint(t, item); got != "" {
			t.Fatalf("expected no schedule_hint for %q, got %q", name, got)
		}
	}
}

// 「运行中点了禁用、等这次跑完再生效」的任务，cron 条目确实已经被摘掉了，
// 但那正是用户要的，不是故障 —— 这时候提示「未注册到调度器，重新保存一次任务即可恢复」，
// 等于在教用户把自己刚关掉的任务重新打开。
func TestTaskListOmitsScheduleHintForTasksPendingDisable(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "pending-disable-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:           "running task pending disable",
		Command:        "echo hi",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusRunning,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 没打标记时，它就是一条「显示正常、实际没注册」的任务，应当提示。
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	item := findTaskListItem(t, decodeJSONMap(t, rec), task.Name)
	if got := taskListScheduleHint(t, item); got != "未注册到调度器，重新保存一次任务即可恢复" {
		t.Fatalf("前置条件不成立：未注册的运行中任务本该提示，实际 %q", got)
	}

	service.MarkPendingDisable(task.ID)
	t.Cleanup(func() { service.ClearPendingDisable(task.ID) })

	rec = performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	item = findTaskListItem(t, decodeJSONMap(t, rec), task.Name)
	if got := taskListScheduleHint(t, item); got != "" {
		t.Fatalf("已经点过禁用的任务不该再提示「重新保存一次即可恢复」，实际 %q", got)
	}
}
