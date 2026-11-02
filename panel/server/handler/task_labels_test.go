package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/router"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
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

// taskLabelEntry 对应 GET /tasks/labels 的一项（#157 契约 L1）。
type taskLabelEntry struct {
	Name  string
	Count int
}

// fetchTaskLabels 请求 GET path（/api/v1/tasks/labels 或旧前缀），并校验响应是裸数组、每一项恰好 name 与 count 两个字段。
func fetchTaskLabels(t *testing.T, engine *gin.Engine, path, token string) []taskLabelEntry {
	t.Helper()

	rec := performRequest(engine, http.MethodGet, path, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d: %s", path, rec.Code, rec.Body.String())
	}

	// 契约是裸数组：外面包一层 {data: ...} 的话，网页的标签候选会按「没有候选」静默处理，功能就看不见了。
	var rawItems []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &rawItems); err != nil {
		t.Fatalf("expected %s to return a bare JSON array, got %s (%v)", path, rec.Body.String(), err)
	}
	// 一个标签都没有时必须是 []，不能是 null。
	if rawItems == nil {
		t.Fatalf("expected [] rather than null, got %s", rec.Body.String())
	}

	labels := make([]taskLabelEntry, 0, len(rawItems))
	for _, item := range rawItems {
		if len(item) != 2 {
			t.Fatalf("expected each label item to carry exactly name and count, got %#v", item)
		}
		name, nameOK := item["name"].(string)
		count, countOK := item["count"].(float64)
		if !nameOK || !countOK {
			t.Fatalf("expected {name: string, count: number}, got %#v", item)
		}
		labels = append(labels, taskLabelEntry{Name: name, Count: int(count)})
	}
	return labels
}

// #157 契约 L1：GET /tasks/labels 列出全部自定义标签与任务数。
// 逐条 trim、跳过空串；内部前缀（分组: / subscription:）在 trim 之后判断，前缀不在开头的保留；
// 同一任务里重复的只算一次；去重区分大小写；按 name 字节序升序；一个都没有时是 []。
func TestTaskLabelsListsDistinctCustomLabelsSortedByName(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "label-list-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 空库：响应体原样就是 []（不是 null，也不是 {data: []}）。
	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks/labels", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected 200 with [] before any task exists, got %d: %s", rec.Code, rec.Body.String())
	}

	mustCreateLabeledTask(t, "普通标签加内部标签", "京东", "分组:日常", "subscription:3")
	mustCreateLabeledTask(t, "前后空格", " 京东 ", "联通")
	mustCreateLabeledTask(t, "同一任务里重复", "京东", "京东")
	mustCreateLabeledTask(t, "大写", "Prod")
	mustCreateLabeledTask(t, "小写", "prod")
	// 带前导 / 尾随空格的内部标签：trim 之后才判断前缀，一样要排除。
	mustCreateLabeledTask(t, "带空格的内部标签", " 分组:x", " subscription:9 ")
	// 含「分组:」但不是以它开头：是普通标签。
	mustCreateLabeledTask(t, "前缀不在开头", "my分组:beta")
	mustCreateLabeledTask(t, "没有标签")
	mustCreateLabeledTask(t, "空标签", "", "a")

	got := fetchTaskLabels(t, engine, "/api/v1/tasks/labels", token)
	want := []taskLabelEntry{
		{Name: "Prod", Count: 1},
		{Name: "a", Count: 1},
		{Name: "my分组:beta", Count: 1},
		{Name: "prod", Count: 1},
		{Name: "京东", Count: 3},
		{Name: "联通", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected labels %v, got %v", want, got)
	}
}

// 权限矩阵与路由共存：用 router.Setup 装出与线上一致的整套路由（/api/v1 与旧前缀 /api 都挂了 TaskHandler）。
// viewer 与带 tasks 权限的应用令牌能看；只有别的模块权限的应用 403；不带令牌 401。
// 静态段 labels 与同层的 /:id/... 共存：labels 不会被当成 :id，/:id/... 与 /groups 也照常可用。
func TestTaskLabelsAccessMatrixAndRouteCoexistence(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := gin.New()
	router.Setup(engine)

	viewer := testutil.MustCreateUser(t, "label-matrix-viewer", "viewer")
	viewerToken := testutil.MustCreateAccessToken(t, viewer.Username, viewer.Role)
	tasksAppToken := testutil.MustCreateAppToken(t, "label-matrix-tasks", "tasks")
	allScopeAppToken := testutil.MustCreateAppToken(t, "label-matrix-all", "*")
	envsAppToken := testutil.MustCreateAppToken(t, "label-matrix-envs", "envs")

	task := mustCreateLabeledTask(t, "路由共存", "共存标签", "分组:共存")
	want := []taskLabelEntry{{Name: "共存标签", Count: 1}}

	for _, tc := range []struct {
		name  string
		path  string
		token string
	}{
		{name: "viewer", path: "/api/v1/tasks/labels", token: viewerToken},
		{name: "tasks app", path: "/api/v1/tasks/labels", token: tasksAppToken},
		{name: "all-scope app", path: "/api/v1/tasks/labels", token: allScopeAppToken},
		{name: "legacy prefix", path: "/api/tasks/labels", token: viewerToken},
	} {
		if got := fetchTaskLabels(t, engine, tc.path, tc.token); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: expected labels %v, got %v", tc.name, want, got)
		}
	}

	denied := performRequest(engine, http.MethodGet, "/api/v1/tasks/labels", map[string]string{
		"Authorization": "Bearer " + envsAppToken,
	})
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "应用无权访问此资源") {
		t.Fatalf("expected 403 for an app token without tasks scope, got %d: %s", denied.Code, denied.Body.String())
	}
	if anonymous := performRequest(engine, http.MethodGet, "/api/v1/tasks/labels", nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d: %s", anonymous.Code, anonymous.Body.String())
	}

	for _, path := range []string{
		fmt.Sprintf("/api/v1/tasks/%d/stats", task.ID),
		fmt.Sprintf("/api/v1/tasks/%d/log-files", task.ID),
		"/api/v1/tasks/groups",
	} {
		rec := performRequest(engine, http.MethodGet, path, map[string]string{
			"Authorization": "Bearer " + viewerToken,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s to stay reachable next to /tasks/labels, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// 查库失败时回 500「加载任务标签失败」，不能回一个看起来正常的 []（前端会把它当成「还没有标签」）。
func TestTaskLabelsReturns500WhenQueryFails(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "label-error-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 把 tasks 表删掉，让查询必然失败（相当于库文件损坏、表丢失这类故障）。
	if err := database.DB.Exec("DROP TABLE tasks").Error; err != nil {
		t.Fatalf("drop tasks table: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks/labels", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the query fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONMap(t, rec); payload["error"] != "加载任务标签失败" {
		t.Fatalf("expected error 加载任务标签失败, got %#v", payload)
	}
}

// #157：MCP 只读工具 list_task_labels 经真实路由回放到 GET /tasks/labels。
// 这个接口返回裸数组，工具层通用的 call 只会解对象，这条用例钉住「工具单独解码、输出里带着标签数组」，
// 以及权限沿用接口本身：应用缺 tasks 权限时，工具与直接调接口一样报错。
func TestTaskLabelsMCPToolReachesRealRoute(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	tasksApp := testutil.MustCreateOpenApp(t, "mcp-labels-tasks", "tasks")
	envsApp := testutil.MustCreateOpenApp(t, "mcp-labels-envs", "envs")

	mustCreateLabeledTask(t, "签到", "京东", "分组:日常", "subscription:1")
	mustCreateLabeledTask(t, "巡检", "京东", "监控")

	session := connectMCPSession(t, engine, mcpBasicAuth(tasksApp.AppKey, tasksApp.AppSecret))
	if names := mcpToolNames(t, session); !names["list_task_labels"] {
		t.Fatalf("只开启 MCP（只读）时就应当能看到 list_task_labels，实际 %v", names)
	}

	result, text := mcpCallText(t, session, "list_task_labels", map[string]any{})
	if result.IsError {
		t.Fatalf("list_task_labels 不应失败: %s", text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("工具输出不是合法 JSON 对象: %v\n%s", err, text)
	}
	want := map[string]any{
		"total": float64(2),
		"labels": []any{
			map[string]any{"name": "京东", "count": float64(2)},
			map[string]any{"name": "监控", "count": float64(1)},
		},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("工具应当原样给出接口的标签数组与总数，期望 %v，实际 %s", want, text)
	}

	denied := connectMCPSession(t, engine, mcpBasicAuth(envsApp.AppKey, envsApp.AppSecret))
	result, text = mcpCallText(t, denied, "list_task_labels", map[string]any{})
	if !result.IsError || !strings.Contains(text, "应用无权访问此资源") {
		t.Fatalf("应用缺 tasks 权限时工具应当报错，实际 isError=%v: %s", result.IsError, text)
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
