package handler_test

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestBatchAddLabelsAppendsDedupsAndKeepsInternalLabels(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// task1：已有普通标签 + 一个内部分组标签 + 一个订阅内部标签。
	task1 := &model.Task{Name: "t1", Command: "echo t1", CronExpression: "0 0 * * *"}
	task1.SetLabelsFromSlice([]string{"旧标签", "分组:工作", "subscription:1"})
	// task2：已有一个与待追加重复的标签，验证去重。
	task2 := &model.Task{Name: "t2", Command: "echo t2", CronExpression: "0 0 * * *"}
	task2.SetLabelsFromSlice([]string{"测试"})
	for _, task := range []*model.Task{task1, task2} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	// 请求体：含重复输入「测试」、含内部前缀输入（应被忽略）、含空白输入。
	body := fmt.Sprintf(
		`{"task_ids":[%d,%d],"labels":["测试","测试"," 重要 ","","分组:注入","subscription:99"]}`,
		task1.ID, task2.ID,
	)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/add-labels", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	got, ok := payload["success_count"].(float64)
	if !ok {
		t.Fatalf("expected success_count in response, got %#v", payload)
	}
	if got != 2 {
		t.Fatalf("expected success_count 2, got %v", got)
	}

	// task1：保留原有全部标签（含内部标签），追加「测试」「重要」，忽略内部前缀输入。
	var reloaded1 model.Task
	if err := database.DB.First(&reloaded1, task1.ID).Error; err != nil {
		t.Fatalf("reload task1: %v", err)
	}
	assertLabelSet(t, reloaded1.GetLabels(), []string{"旧标签", "分组:工作", "subscription:1", "测试", "重要"})

	// task2：原有「测试」不重复，追加「重要」。
	var reloaded2 model.Task
	if err := database.DB.First(&reloaded2, task2.ID).Error; err != nil {
		t.Fatalf("reload task2: %v", err)
	}
	assertLabelSet(t, reloaded2.GetLabels(), []string{"测试", "重要"})
}

func TestBatchAddLabelsRejectsWhenNoValidLabels(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "t", Command: "echo t", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 全部是内部前缀/空白，无有效标签 → 400。
	body := fmt.Sprintf(`{"task_ids":[%d],"labels":["","分组:x","subscription:1"]}`, task.ID)
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/tasks/batch/add-labels", body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func assertLabelSet(t *testing.T, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("label set mismatch: got %v, want %v", got, want)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("label set mismatch: got %v, want %v", got, want)
		}
	}
}
