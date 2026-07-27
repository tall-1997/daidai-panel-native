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

func TestTaskListKeepsStableOrderWhenTaskStatusChangesToRunning(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "stable-order-operator", "operator")
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
		"active first",
		"active second",
		"active third",
		"disabled last",
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("expected stable active order %v, got %v", wantNames, gotNames)
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
