package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 从任务列表响应里按返回顺序取出任务名。拖拽与列排序的用例校验的都是【顺序】，
// 所以不能像老用例那样只做 contains 判定，必须逐条按下标比。
func taskListNamesInOrder(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}

	names := make([]string, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object, got %#v", raw)
		}
		name, _ := item["name"].(string)
		names = append(names, name)
	}
	return names
}

// 取任务列表响应里每条的 status。按状态列排序的用例要断言的是【状态值序列本身单调】，
// 只比任务名的话，「标着升序、读出来数值不递增」这个现象在用例里根本看不出来。
func taskListStatusesInOrder(t *testing.T, rec *httptest.ResponseRecorder) []float64 {
	t.Helper()

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}

	statuses := make([]float64, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object, got %#v", raw)
		}
		status, ok := item["status"].(float64)
		if !ok {
			t.Fatalf("expected numeric status, got %#v", item["status"])
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// 取任务列表响应里每条的 next_run_at。没有这个键（禁用 / 非 cron 任务）时放一个 nil，
// 用来断言「算不出下次运行的任务恒排最后」。
func taskListNextRunTimesInOrder(t *testing.T, rec *httptest.ResponseRecorder) []*time.Time {
	t.Helper()

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %#v", payload["data"])
	}

	times := make([]*time.Time, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected task object, got %#v", raw)
		}
		text, ok := item["next_run_at"].(string)
		if !ok || text == "" {
			times = append(times, nil)
			continue
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatalf("parse next_run_at %q: %v", text, err)
		}
		times = append(times, &parsed)
	}
	return times
}

// 拖拽只允许写 list_order。同桶拖拽后整桶按 (idx+1)*10 重编号，且列表首位换成被拖的那条。
func TestTaskSortWithinBucketRenumbersListOrder(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := make([]*model.Task, 0, 3)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		task := &model.Task{
			Name:           name,
			Command:        "task demo.py",
			CronExpression: "0 0 * * *",
			Status:         model.TaskStatusEnabled,
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", name, err)
		}
		tasks = append(tasks, task)
	}

	// 默认序里同桶按 created_at DESC / id DESC，所以初始展示是 gamma、beta、alpha。
	// 把 alpha 拖到 gamma 前面，期望变成 alpha、gamma、beta。
	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d,"target_id":%d,"position":"before"}`, tasks[0].ID, tasks[2].ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wantListOrder := map[uint]int{
		tasks[0].ID: 10,
		tasks[2].ID: 20,
		tasks[1].ID: 30,
	}
	for id, want := range wantListOrder {
		var current model.Task
		if err := database.DB.First(&current, id).Error; err != nil {
			t.Fatalf("reload task %d: %v", id, err)
		}
		if current.ListOrder != want {
			t.Fatalf("任务 %q 的 list_order 应为 %d，实际 %d", current.Name, want, current.ListOrder)
		}
		// 拖拽绝不能碰 sort_order —— 它是开机任务的串行执行顺序契约。
		if current.SortOrder != 0 {
			t.Fatalf("拖拽不应改动 sort_order，任务 %q 变成了 %d", current.Name, current.SortOrder)
		}
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	gotNames := taskListNamesInOrder(t, listRec)
	wantNames := []string{"alpha", "gamma", "beta"}
	for i, want := range wantNames {
		if i >= len(gotNames) || gotNames[i] != want {
			t.Fatalf("expected order %v, got %v", wantNames, gotNames)
		}
	}
}

// 省略 target_id = 移到本桶末尾。
func TestTaskSortWithoutTargetMovesToBucketEnd(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-tail-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := make([]*model.Task, 0, 3)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		task := &model.Task{
			Name:           name,
			Command:        "task demo.py",
			CronExpression: "0 0 * * *",
			Status:         model.TaskStatusEnabled,
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", name, err)
		}
		tasks = append(tasks, task)
	}

	// 初始展示 gamma、beta、alpha；把 gamma 不带目标地拖走，应落到末尾变成 beta、alpha、gamma。
	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d}`, tasks[2].ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	gotNames := taskListNamesInOrder(t, listRec)
	wantNames := []string{"beta", "alpha", "gamma"}
	for i, want := range wantNames {
		if i >= len(gotNames) || gotNames[i] != want {
			t.Fatalf("expected order %v, got %v", wantNames, gotNames)
		}
	}

	var moved model.Task
	if err := database.DB.First(&moved, tasks[2].ID).Error; err != nil {
		t.Fatalf("reload moved task: %v", err)
	}
	if moved.ListOrder != 30 {
		t.Fatalf("末尾那条的 list_order 应为 30，实际 %d", moved.ListOrder)
	}
}

// 跨页拖拽：兄弟列表是从数据库取整桶而不是当前页，所以把第 3 页的任务拖到第 1 页也能算对位置。
func TestTaskSortWorksAcrossPages(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-paging-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := make([]*model.Task, 0, 5)
	for _, name := range []string{"t1", "t2", "t3", "t4", "t5"} {
		task := &model.Task{
			Name:           name,
			Command:        "task demo.py",
			CronExpression: "0 0 * * *",
			Status:         model.TaskStatusEnabled,
		}
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", name, err)
		}
		tasks = append(tasks, task)
	}

	// 初始展示 t5、t4、t3、t2、t1；page_size=2 时 t1 在第 3 页。把它拖到第 1 页的 t5 前面。
	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d,"target_id":%d,"position":"before"}`, tasks[0].ID, tasks[4].ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/tasks?page=1&page_size=2", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	gotNames := taskListNamesInOrder(t, listRec)
	wantNames := []string{"t1", "t5"}
	if len(gotNames) != 2 || gotNames[0] != wantNames[0] || gotNames[1] != wantNames[1] {
		t.Fatalf("expected first page %v, got %v", wantNames, gotNames)
	}
}

// 跨置顶区、跨状态分组一律 400，且一个字都不许写库。
func TestTaskSortRejectsCrossBucketDrag(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-bucket-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	pinned := &model.Task{Name: "pinned", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, IsPinned: true}
	enabled := &model.Task{Name: "enabled", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	disabled := &model.Task{Name: "disabled", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled}
	for _, task := range []*model.Task{pinned, enabled, disabled} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		name     string
		sourceID uint
		targetID uint
	}{
		{name: "普通任务拖进置顶区", sourceID: enabled.ID, targetID: pinned.ID},
		{name: "禁用任务拖进启用区", sourceID: disabled.ID, targetID: enabled.ID},
	}
	for _, tc := range cases {
		rec := performJSONRequest(
			engine,
			http.MethodPut,
			"/api/v1/tasks/sort",
			fmt.Sprintf(`{"source_id":%d,"target_id":%d}`, tc.sourceID, tc.targetID),
			map[string]string{"Authorization": "Bearer " + accessToken},
			"",
		)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "分别排序") {
			t.Fatalf("%s：expected cross-bucket hint, got %s", tc.name, rec.Body.String())
		}
	}

	// 被拒绝的请求不能留下半截写入：三条任务的 list_order 都必须还是补列后的 0。
	for _, id := range []uint{pinned.ID, enabled.ID, disabled.ID} {
		var current model.Task
		if err := database.DB.First(&current, id).Error; err != nil {
			t.Fatalf("reload task %d: %v", id, err)
		}
		if current.ListOrder != 0 {
			t.Fatalf("跨桶拖拽被拒绝后不该写库，任务 %q 的 list_order 变成了 %d", current.Name, current.ListOrder)
		}
	}
}

// 拖到自己身上（前端抖一下就会发出来）必须是纯幂等，不能顺手把整桶重编号一遍。
func TestTaskSortSameSourceAndTargetKeepsListOrder(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-noop-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	first := &model.Task{Name: "first", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, ListOrder: 5}
	second := &model.Task{Name: "second", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, ListOrder: 7}
	for _, task := range []*model.Task{first, second} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d,"target_id":%d}`, first.ID, first.ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wantListOrder := map[uint]int{first.ID: 5, second.ID: 7}
	for id, want := range wantListOrder {
		var current model.Task
		if err := database.DB.First(&current, id).Error; err != nil {
			t.Fatalf("reload task %d: %v", id, err)
		}
		if current.ListOrder != want {
			t.Fatalf("拖到自己身上不该重编号，任务 %q 的 list_order 从 %d 变成了 %d", current.Name, want, current.ListOrder)
		}
	}
}

// 排序接口是 operator 权限，viewer 调用必须 403。
func TestTaskSortRejectsViewerRole(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	viewer := testutil.MustCreateUser(t, "task-sort-viewer", "viewer")
	viewerToken := testutil.MustCreateAccessToken(t, viewer.Username, viewer.Role)

	task := &model.Task{Name: "alpha", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d}`, task.ID),
		map[string]string{"Authorization": "Bearer " + viewerToken},
		"",
	)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d: %s", rec.Code, rec.Body.String())
	}
}

// 拖过之后再不带 sort_rules 查列表，置顶区与禁用分组仍然优先于拖拽顺序。
// list_order 只在同一个桶内部生效，不能把置顶任务顶下去、也不能把禁用任务顶上来。
func TestTaskListKeepsPinnedAndStatusGroupingAfterDrag(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-sort-grouping-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	pinned := &model.Task{Name: "pinned", Command: "task p.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, IsPinned: true}
	alpha := &model.Task{Name: "alpha", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	beta := &model.Task{Name: "beta", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	disabled := &model.Task{Name: "disabled", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled}
	for _, task := range []*model.Task{pinned, alpha, beta, disabled} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	// 普通启用区初始展示是 beta、alpha，把 alpha 拖到 beta 前面。
	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/sort",
		fmt.Sprintf(`{"source_id":%d,"target_id":%d}`, alpha.ID, beta.ID),
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 禁用任务不在被拖的那个桶里，list_order 必须保持存量的 0。
	var currentDisabled model.Task
	if err := database.DB.First(&currentDisabled, disabled.ID).Error; err != nil {
		t.Fatalf("reload disabled task: %v", err)
	}
	if currentDisabled.ListOrder != 0 {
		t.Fatalf("重编号只该动本桶，禁用任务的 list_order 变成了 %d", currentDisabled.ListOrder)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/tasks", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	gotNames := taskListNamesInOrder(t, listRec)
	wantNames := []string{"pinned", "alpha", "beta", "disabled"}
	for i, want := range wantNames {
		if i >= len(gotNames) || gotNames[i] != want {
			t.Fatalf("expected order %v, got %v", wantNames, gotNames)
		}
	}
}

// 「最后运行」列排序：升降序都生效，从未运行过的任务恒排最后、不跟随方向翻转。
func TestTaskListSortsByLastRunAtWithNeverRunLast(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-last-run-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	tasks := []*model.Task{
		{Name: "ran-older", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &older},
		{Name: "ran-newer", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &newer},
		{Name: "never-run", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		direction string
		want      []string
	}{
		{direction: "asc", want: []string{"ran-older", "ran-newer", "never-run"}},
		{direction: "desc", want: []string{"ran-newer", "ran-older", "never-run"}},
	}
	for _, tc := range cases {
		sortJSON := fmt.Sprintf(`[{"field":"last_run_at","direction":"%s"}]`, tc.direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", tc.direction, rec.Code, rec.Body.String())
		}
		gotNames := taskListNamesInOrder(t, rec)
		if len(gotNames) != len(tc.want) {
			t.Fatalf("%s：expected %d tasks, got %v", tc.direction, len(tc.want), gotNames)
		}
		for i, want := range tc.want {
			if gotNames[i] != want {
				t.Fatalf("%s：expected order %v, got %v", tc.direction, tc.want, gotNames)
			}
		}
	}
}

// 「下次运行」列排序：升序时时间由早到晚，且禁用任务与手动任务（都算不出下次运行）恒排最后。
// 刻意不写死两条 cron 任务的先后：next_run_at 是响应期现算的，
// 写死具体名字会在「两条 cron 恰好命中同一个时间点」的时刻翻车，
// 所以断言改成「有值的都在前面且非递减、没值的都在后面」——这正是契约本身。
func TestTaskListSortsByNextRunAtWithNoScheduleLast(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-next-run-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "cron-minutely", Command: "task a.py", CronExpression: "* * * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
		{Name: "cron-daily", Command: "task b.py", CronExpression: "0 3 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
		{Name: "disabled-cron", Command: "task c.py", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusDisabled},
		{Name: "manual-task", Command: "task d.py", CronExpression: "", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	noSchedule := map[string]bool{"disabled-cron": true, "manual-task": true}

	for _, direction := range []string{"asc", "desc"} {
		sortJSON := fmt.Sprintf(`[{"field":"next_run_at","direction":"%s"}]`, direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", direction, rec.Code, rec.Body.String())
		}

		gotNames := taskListNamesInOrder(t, rec)
		gotTimes := taskListNextRunTimesInOrder(t, rec)
		if len(gotNames) != 4 || len(gotTimes) != 4 {
			t.Fatalf("%s：expected 4 tasks, got %v", direction, gotNames)
		}

		// 前两条必须是算得出下次运行的 cron 任务。
		for i := 0; i < 2; i++ {
			if gotTimes[i] == nil {
				t.Fatalf("%s：前两条应当有 next_run_at，实际顺序 %v", direction, gotNames)
			}
			if noSchedule[gotNames[i]] {
				t.Fatalf("%s：算不出下次运行的任务不该排在前面，实际顺序 %v", direction, gotNames)
			}
		}
		// 后两条必须是禁用任务与手动任务，且 asc / desc 都一样 —— 空值不跟随方向翻转。
		for i := 2; i < 4; i++ {
			if gotTimes[i] != nil {
				t.Fatalf("%s：后两条不该有 next_run_at，实际顺序 %v", direction, gotNames)
			}
			if !noSchedule[gotNames[i]] {
				t.Fatalf("%s：期望禁用任务与手动任务排在最后，实际顺序 %v", direction, gotNames)
			}
		}

		if direction == "asc" && gotTimes[1].Before(*gotTimes[0]) {
			t.Fatalf("asc：next_run_at 应当由早到晚，got %v / %v", gotTimes[0], gotTimes[1])
		}
		if direction == "desc" && gotTimes[1].After(*gotTimes[0]) {
			t.Fatalf("desc：next_run_at 应当由晚到早，got %v / %v", gotTimes[0], gotTimes[1])
		}
	}
}

// 按「最后运行」列排序时，禁用任务必须整体沉到所有启用任务之后（issue #117）。
// 这条用例在修复前必然失败：sortPreparedTaskListItems 的比较器当时只按 sort_rules 排，
// 状态分区埋在 defaultTaskListLess 里、只有所有规则都打平才轮得到，
// 于是禁用任务只要有 last_run_at 就会和启用任务完全混排 ——
// desc 方向下「刚被禁用、恰好留着最新 last_run_at」的那条直接冒到第一行，正是用户报的现象。
func TestTaskListKeepsDisabledTasksLastWhenSortingByLastRunAt(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-disabled-last-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	newest := time.Now().Add(-10 * time.Minute)
	newer := time.Now().Add(-1 * time.Hour)
	older := time.Now().Add(-2 * time.Hour)
	tasks := []*model.Task{
		// 禁用 + 全场最新的 last_run_at：修复前 desc 方向它会排在第一行。
		{Name: "disabled-newest", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, LastRunAt: &newest},
		{Name: "enabled-newer", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &newer},
		{Name: "enabled-older", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &older},
		// 启用但从未运行过：修复前 asc 方向它排在最后，禁用任务会挤在它前面。
		{Name: "enabled-never", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
		{Name: "disabled-never", Command: "task e.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	enabledNames := map[string]bool{"enabled-newer": true, "enabled-older": true, "enabled-never": true}

	// 升降序都要断言：分区是骨架，不跟随 direction 翻转。
	for _, direction := range []string{"asc", "desc"} {
		sortJSON := fmt.Sprintf(`[{"field":"last_run_at","direction":"%s"}]`, direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", direction, rec.Code, rec.Body.String())
		}

		gotNames := taskListNamesInOrder(t, rec)
		if len(gotNames) != 5 {
			t.Fatalf("%s：expected 5 tasks, got %v", direction, gotNames)
		}
		for i := 0; i < 3; i++ {
			if !enabledNames[gotNames[i]] {
				t.Fatalf("%s：前三条应当全是启用任务，实际顺序 %v", direction, gotNames)
			}
		}
		for i := 3; i < 5; i++ {
			if enabledNames[gotNames[i]] {
				t.Fatalf("%s：禁用任务必须沉到所有启用任务之后，实际顺序 %v", direction, gotNames)
			}
		}
	}
}

// 置顶优先级高于状态分区：置顶任务哪怕 last_run_at 是全场最旧的、哪怕它本身已被禁用，
// 按列排序时仍然留在最前面的置顶区。口径必须和默认排序完全一致
// （见 task_query_regression_test.go 的 TestTaskListKeepsPinnedDisabledTasksInPinnedArea），
// 否则会出现「默认排序时置顶的禁用任务在最上、点一下最后运行它掉到最下」的自相矛盾。
func TestTaskListKeepsPinnedTasksFirstWhenSortingByLastRunAt(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-pinned-first-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	ancient := time.Now().Add(-72 * time.Hour)
	stale := time.Now().Add(-48 * time.Hour)
	fresh := time.Now().Add(-5 * time.Minute)
	tasks := []*model.Task{
		{Name: "pinned-enabled", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, IsPinned: true, LastRunAt: &stale},
		{Name: "pinned-disabled", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, IsPinned: true, LastRunAt: &ancient},
		{Name: "normal-enabled", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &fresh},
		{Name: "normal-disabled", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, LastRunAt: &fresh},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	// 四个分区（置顶启用 / 置顶禁用 / 普通启用 / 普通禁用）各只有一条，
	// 所以区内排序规则不参与，asc / desc 的期望顺序完全相同。
	wantNames := []string{"pinned-enabled", "pinned-disabled", "normal-enabled", "normal-disabled"}
	for _, direction := range []string{"asc", "desc"} {
		sortJSON := fmt.Sprintf(`[{"field":"last_run_at","direction":"%s"}]`, direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", direction, rec.Code, rec.Body.String())
		}

		gotNames := taskListNamesInOrder(t, rec)
		if len(gotNames) != len(wantNames) {
			t.Fatalf("%s：expected %d tasks, got %v", direction, len(wantNames), gotNames)
		}
		for i, want := range wantNames {
			if gotNames[i] != want {
				t.Fatalf("%s：expected order %v, got %v", direction, wantNames, gotNames)
			}
		}
	}
}

// 分区不能把原有排序语义吃掉：启用任务组【内部】仍按规则升降序排，
// 而「从未运行过」的空值仍然沉到本组末尾且不跟随方向翻转。
// 禁用组同理：组内有值的在前、无值的在后。
func TestTaskListSortsWithinStatusGroupWhenSortingByLastRunAt(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-group-inner-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	newest := time.Now().Add(-10 * time.Minute)
	newer := time.Now().Add(-1 * time.Hour)
	older := time.Now().Add(-2 * time.Hour)
	tasks := []*model.Task{
		{Name: "enabled-newer", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &newer},
		{Name: "enabled-older", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled, LastRunAt: &older},
		{Name: "enabled-never", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
		{Name: "disabled-newest", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, LastRunAt: &newest},
		{Name: "disabled-never", Command: "task e.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		direction string
		want      []string
	}{
		// 启用组内按时间由早到晚，空值垫底；禁用组整体在后，组内同样是有值在前、空值垫底。
		{direction: "asc", want: []string{"enabled-older", "enabled-newer", "enabled-never", "disabled-newest", "disabled-never"}},
		// 翻成倒序只翻转「组内有值的那几条」，空值与分区都纹丝不动。
		{direction: "desc", want: []string{"enabled-newer", "enabled-older", "enabled-never", "disabled-newest", "disabled-never"}},
	}
	for _, tc := range cases {
		sortJSON := fmt.Sprintf(`[{"field":"last_run_at","direction":"%s"}]`, tc.direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", tc.direction, rec.Code, rec.Body.String())
		}

		gotNames := taskListNamesInOrder(t, rec)
		if len(gotNames) != len(tc.want) {
			t.Fatalf("%s：expected %d tasks, got %v", tc.direction, len(tc.want), gotNames)
		}
		for i, want := range tc.want {
			if gotNames[i] != want {
				t.Fatalf("%s：expected order %v, got %v", tc.direction, tc.want, gotNames)
			}
		}
	}
}

// 「状态」列排序必须是纯粹按状态值排：升序 0(禁用) → 0.5(排队) → 1(空闲) → 2(运行)，降序反过来。
//
// 状态分区（taskSortGroup）会把 {0.5,1,2} 归一组、{0} 归另一组，
// 无条件跑在 sort_rules 之前的话，「状态 + 升序」读出来是 0.5 → 1 → 2 → 0：
// 一个标着「升序」的排序，状态值序列并不递增；改成降序又是 2 → 1 → 0.5 → 0，
// 禁用任务照样垫底 —— 对禁用任务而言 asc/desc 这个开关完全失效。
// 存量视图会静默变样，用户还查不出原因，所以这里断言的是【数值序列】而不只是任务名。
func TestTaskListSortsByStatusWithoutStatusGrouping(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-status-sort-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "st-idle", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
		{Name: "st-disabled", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled},
		{Name: "st-running", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusRunning},
		{Name: "st-queued", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusQueued},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		direction string
		want      []float64
	}{
		{direction: "asc", want: []float64{0, 0.5, 1, 2}},
		{direction: "desc", want: []float64{2, 1, 0.5, 0}},
	}
	for _, tc := range cases {
		sortJSON := fmt.Sprintf(`[{"field":"status","direction":"%s"}]`, tc.direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", tc.direction, rec.Code, rec.Body.String())
		}

		gotStatuses := taskListStatusesInOrder(t, rec)
		if len(gotStatuses) != len(tc.want) {
			t.Fatalf("%s：expected %d tasks, got %v", tc.direction, len(tc.want), gotStatuses)
		}
		for i, want := range tc.want {
			if gotStatuses[i] != want {
				t.Fatalf("%s：expected status order %v, got %v（任务顺序 %v）",
					tc.direction, tc.want, gotStatuses, taskListNamesInOrder(t, rec))
			}
		}
	}
}

// 豁免的判据是「**第一条**规则是 status」，不是「任意一条规则里有 status」。
// status 当次级 tie-break 时（先按定时规则、每天时刻相同再按状态），
// 先分区、再在区内 tie-break 才是对的：这时禁用任务仍然要整体沉到最后。
// 三条任务的 cron 完全相同，所以第一条规则一定打平、必然落到 status 这条 ——
// 判据要是写成「任意一条含 status」，这里就会变成全局按状态升序、禁用任务冒到第一行。
func TestTaskListKeepsStatusGroupingWhenStatusIsSecondaryRule(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-status-secondary-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "alpha-disabled", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled},
		{Name: "beta-idle", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
		{Name: "gamma-running", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusRunning},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	sortJSON := `[{"field":"cron_expression","direction":"asc"},{"field":"status","direction":"asc"}]`
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 启用区（空闲 → 运行，按次级规则升序）在前，禁用任务整体在后。
	wantNames := []string{"beta-idle", "gamma-running", "alpha-disabled"}
	gotNames := taskListNamesInOrder(t, rec)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("expected %d tasks, got %v", len(wantNames), gotNames)
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Fatalf("status 作为次级规则时状态分区仍须生效，expected %v, got %v", wantNames, gotNames)
		}
	}
}

// 豁免的只是状态分区，置顶分区照旧：按状态排序时置顶任务仍然全部排在最前面。
// 同时这条也钉住了「豁免之后 asc/desc 对禁用任务真的生效了」——
// 置顶区内部 asc 是 禁用(0) → 运行(2)，desc 是 运行(2) → 禁用(0)，两者必须不同。
func TestTaskListKeepsPinnedTasksFirstWhenSortingByStatus(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-status-pinned-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "pinned-disabled", Command: "task a.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled, IsPinned: true},
		{Name: "pinned-running", Command: "task b.py", CronExpression: "0 0 * * *", Status: model.TaskStatusRunning, IsPinned: true},
		{Name: "normal-disabled", Command: "task c.py", CronExpression: "0 0 * * *", Status: model.TaskStatusDisabled},
		{Name: "normal-idle", Command: "task d.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		direction string
		want      []string
	}{
		{direction: "asc", want: []string{"pinned-disabled", "pinned-running", "normal-disabled", "normal-idle"}},
		{direction: "desc", want: []string{"pinned-running", "pinned-disabled", "normal-idle", "normal-disabled"}},
	}
	for _, tc := range cases {
		sortJSON := fmt.Sprintf(`[{"field":"status","direction":"%s"}]`, tc.direction)
		rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?sort_rules="+url.QueryEscape(sortJSON), map[string]string{
			"Authorization": "Bearer " + accessToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：expected status 200, got %d: %s", tc.direction, rec.Code, rec.Body.String())
		}

		gotNames := taskListNamesInOrder(t, rec)
		if len(gotNames) != len(tc.want) {
			t.Fatalf("%s：expected %d tasks, got %v", tc.direction, len(tc.want), gotNames)
		}
		for i, want := range tc.want {
			if gotNames[i] != want {
				t.Fatalf("%s：expected order %v, got %v", tc.direction, tc.want, gotNames)
			}
		}
	}
}

// 「定时规则」（cron_expression）排序按一天之内最早一次执行的时刻排（issue #151），不再按表达式字符串排。
// 按字符串排时 `0 0 15 * * *` 会排在 `0 0 8,20 * * *` 前面（'1' < '8'），
// 同是 09:00 的 5 段 `0 9 * * *` 与 6 段 `0 0 9 * * *` 也被拆到两头 —— 这两处都是本用例要钉住的。
//
// 时刻只取决于规则、与「现在」无关，所以可以写死完整顺序（不像 next_run_at 那条只能断言非递减）。
// 同时钉住排序的通用契约：置顶区在最前、禁用分区在最后（禁用任务照样按时刻排）；
// 手动任务没有时刻，沉到启用区末尾且不随 asc/desc 翻转；时刻相同交给默认序（list_order），也不随方向翻转。
func TestTaskListSortsByCronTimeOfDay(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-cron-time-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	tasks := []*model.Task{
		{Name: "pinned-2000", Command: "task p.py", CronExpression: "0 0 20 * * *", Status: model.TaskStatusEnabled, IsPinned: true},
		{Name: "at-1500", Command: "task a.py", CronExpression: "0 0 15 * * *", Status: model.TaskStatusEnabled},
		{Name: "at-0800-2000", Command: "task b.py", CronExpression: "0 0 8,20 * * *", Status: model.TaskStatusEnabled},
		// 两条 09:00 用 list_order 定好先后：时刻相同返回 0、回落默认序，所以升序降序里都是 6 段在前。
		{Name: "at-0900-5seg", Command: "task c.py", CronExpression: "0 9 * * *", Status: model.TaskStatusEnabled, ListOrder: 20},
		{Name: "at-0900-6seg", Command: "task d.py", CronExpression: "0 0 9 * * *", Status: model.TaskStatusEnabled, ListOrder: 10},
		// 每 5 分钟：0 点整也会跑，时刻算 00:00。
		{Name: "every-5min", Command: "task e.py", CronExpression: "0 */5 * * * *", Status: model.TaskStatusEnabled},
		{Name: "manual", Command: "task f.py", CronExpression: "", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled},
		{Name: "disabled-0600", Command: "task g.py", CronExpression: "0 0 6 * * *", Status: model.TaskStatusDisabled},
		{Name: "disabled-2200", Command: "task h.py", CronExpression: "0 0 22 * * *", Status: model.TaskStatusDisabled},
	}
	for _, task := range tasks {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %q: %v", task.Name, err)
		}
	}

	cases := []struct {
		direction string
		want      []string
	}{
		{direction: "asc", want: []string{
			"pinned-2000",
			"every-5min", "at-0800-2000", "at-0900-6seg", "at-0900-5seg", "at-1500", "manual",
			"disabled-0600", "disabled-2200",
		}},
		{direction: "desc", want: []string{
			"pinned-2000",
			"at-1500", "at-0900-6seg", "at-0900-5seg", "at-0800-2000", "every-5min", "manual",
			"disabled-2200", "disabled-0600",
		}},
	}
	for _, tc := range cases {
		sortJSON := fmt.Sprintf(`[{"field":"cron_expression","direction":"%s"}]`, tc.direction)

		// 按页取、再拼起来：排序是全表排好再切片，第 2 页必须紧接第 1 页，不能每页各排各的。
		gotNames := make([]string, 0, len(tc.want))
		for page := 1; page <= 3; page++ {
			rec := performRequest(engine, http.MethodGet,
				fmt.Sprintf("/api/v1/tasks?page=%d&page_size=4&sort_rules=%s", page, url.QueryEscape(sortJSON)),
				map[string]string{"Authorization": "Bearer " + accessToken})
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 第 %d 页：expected status 200, got %d: %s", tc.direction, page, rec.Code, rec.Body.String())
			}
			gotNames = append(gotNames, taskListNamesInOrder(t, rec)...)
		}

		if strings.Join(gotNames, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s：expected order %v, got %v", tc.direction, tc.want, gotNames)
		}
	}
}
