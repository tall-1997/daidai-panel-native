package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 批量设置通知（issue #149，PUT /tasks/batch/notify）的接口契约：
// 三个开关用指针，只改传了的那几项、false 也要能写进去；all=true 改全部任务；
// 没传开关 / 没选任务回 400，一个都没命中回 404，viewer 回 403。

// batchNotifyTaskState 把三个开关拼成一个可直接比较的值，断言失败时一眼能看出是哪一项不对。
func batchNotifyTaskState(t *testing.T, id uint) [3]bool {
	t.Helper()
	task := reloadTaskRow(t, id)
	return [3]bool{task.NotifyOnFailure, task.NotifyOnSuccess, task.NotifyOnAbort}
}

func TestBatchSetNotifyUpdatesOnlyProvidedSwitchesOfSelectedTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// selected1 的成功 / 终止通知本来是开的，只传失败开关时这两项必须原样保留。
	selected1 := &model.Task{Name: "selected1", Command: "task a.py", CronExpression: "0 0 * * *", NotifyOnSuccess: true, NotifyOnAbort: true}
	selected2 := &model.Task{Name: "selected2", Command: "task b.py", CronExpression: "0 0 * * *"}
	untouched := &model.Task{Name: "untouched", Command: "task c.py", CronExpression: "0 0 * * *"}
	for _, task := range []*model.Task{selected1, selected2, untouched} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	body := fmt.Sprintf(`{"task_ids":[%d,%d],"notify_on_failure":true}`, selected1.ID, selected2.ID)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)
	if payload["success_count"] != float64(2) || payload["message"] != "已更新 2 个任务的通知设置" {
		t.Fatalf("回包应当与 batch/add-labels 同形（message + success_count），got %#v", payload)
	}

	if got := batchNotifyTaskState(t, selected1.ID); got != [3]bool{true, true, true} {
		t.Fatalf("selected1 只该打开失败通知、成功与终止保持原样，got %v", got)
	}
	if got := batchNotifyTaskState(t, selected2.ID); got != [3]bool{true, false, false} {
		t.Fatalf("selected2 只该打开失败通知，got %v", got)
	}
	if got := batchNotifyTaskState(t, untouched.ID); got != [3]bool{false, false, false} {
		t.Fatalf("没选中的任务不该被修改，got %v", got)
	}
}

// 传 false 是「批量关闭」，必须真的写进库：用 bool 而不是 *bool 接的话，false 会被当成「没传」静默跳过。
func TestBatchSetNotifyWritesFalse(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-false-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	target := &model.Task{Name: "target", Command: "task a.py", CronExpression: "0 0 * * *", NotifyOnFailure: true, NotifyOnSuccess: true, NotifyOnAbort: true}
	other := &model.Task{Name: "other", Command: "task b.py", CronExpression: "0 0 * * *", NotifyOnFailure: true, NotifyOnSuccess: true, NotifyOnAbort: true}
	for _, task := range []*model.Task{target, other} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	body := fmt.Sprintf(`{"task_ids":[%d],"notify_on_failure":false,"notify_on_abort":false}`, target.ID)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := batchNotifyTaskState(t, target.ID); got != [3]bool{false, true, false} {
		t.Fatalf("失败与终止通知应当被关掉、成功通知没传要保持开启，got %v", got)
	}
	if got := batchNotifyTaskState(t, other.ID); got != [3]bool{true, true, true} {
		t.Fatalf("没选中的任务不该被修改，got %v", got)
	}
}

// all=true 改全部任务并忽略 task_ids：网页任务列表只能勾当前页，「全部任务」靠的就是这个开关。
func TestBatchSetNotifyAllUpdatesEveryTaskAndIgnoresTaskIDs(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-all-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "all-1", Command: "task a.py", CronExpression: "0 0 * * *"},
		{Name: "all-2", Command: "task b.py", CronExpression: "", TaskType: model.TaskTypeManual},
		// 已经开着的任务也计入 success_count：RowsAffected 是命中的行数，不是「值真的变了」的行数。
		{Name: "all-3", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, NotifyOnSuccess: true},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	// task_ids 只写了一个，all=true 时必须被忽略。
	body := fmt.Sprintf(`{"all":true,"task_ids":[%d],"notify_on_success":true}`, tasks[0].ID)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONMap(t, rec); payload["success_count"] != float64(len(tasks)) {
		t.Fatalf("all=true 时 success_count 应当等于任务总数 %d，got %#v", len(tasks), payload)
	}

	for _, task := range tasks {
		if got := batchNotifyTaskState(t, task.ID); got != [3]bool{false, true, false} {
			t.Fatalf("任务 %q 应当只打开成功通知，got %v", task.Name, got)
		}
	}
}

func TestBatchSetNotifyRejectsMissingSwitchesOrTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-reject-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "t", Command: "task a.py", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	cases := []struct {
		name      string
		body      string
		wantError string
	}{
		// 三个开关一个都没传：什么都不改，不能回 200 假装成功。
		{name: "没传开关", body: fmt.Sprintf(`{"task_ids":[%d]}`, task.ID), wantError: "请至少设置一项通知开关"},
		{name: "all 为 true 但没传开关", body: `{"all":true}`, wantError: "请至少设置一项通知开关"},
		// all 不为 true 时 task_ids 必须非空，否则等于没选范围。
		{name: "没选任务", body: `{"notify_on_failure":true}`, wantError: "请先选择任务"},
		{name: "all 为 false 且 task_ids 为空", body: `{"all":false,"task_ids":[],"notify_on_failure":true}`, wantError: "请先选择任务"},
	}
	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", tc.body, map[string]string{
			"Authorization": "Bearer " + accessToken,
		}, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：expected status 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		if payload := decodeJSONMap(t, rec); payload["error"] != tc.wantError {
			t.Fatalf("%s：expected error %q, got %#v", tc.name, tc.wantError, payload)
		}
	}

	if got := batchNotifyTaskState(t, task.ID); got != [3]bool{false, false, false} {
		t.Fatalf("被拒绝的请求不该改动任何任务，got %v", got)
	}
}

// 一个都没命中时回 404：批量接口用 200 表达「全军覆没」，APP 会提示「已设置」而实际什么都没变。
func TestBatchSetNotifyReturns404WhenNoTaskMatched(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-404-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "t", Command: "task a.py", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	body := fmt.Sprintf(`{"task_ids":[%d,%d],"notify_on_failure":true}`, task.ID+100, task.ID+200)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONMap(t, rec); payload["error"] != "没有找到要修改的任务" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if got := batchNotifyTaskState(t, task.ID); got != [3]bool{false, false, false} {
		t.Fatalf("不在 task_ids 里的任务不该被修改，got %v", got)
	}
}

func TestBatchSetNotifyRejectsViewerRole(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "batch-notify-viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "t", Command: "task a.py", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/notify", `{"all":true,"notify_on_failure":true}`, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := batchNotifyTaskState(t, task.ID); got != [3]bool{false, false, false} {
		t.Fatalf("viewer 的请求不该改动任何任务，got %v", got)
	}
}
