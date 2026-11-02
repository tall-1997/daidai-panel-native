package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// taskGroupEntry 对应 GET /tasks/groups 的一项（契约 C2）。
type taskGroupEntry struct {
	Name  string
	Count int
}

// mustCreateLabeledTask 建一条带标签的启用任务。分组就是 labels 里的 `分组:<名>`，App 的分组管理写的就是它。
func mustCreateLabeledTask(t *testing.T, name string, labels ...string) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:           name,
		Command:        "echo group",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	task.SetLabelsFromSlice(labels)
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	return task
}

// listTaskNames 请求任务列表并按响应顺序取出任务名。
func listTaskNames(t *testing.T, engine *gin.Engine, token, rawQuery string) []string {
	t.Helper()

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks?"+rawQuery, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for task list, got %d: %s", rec.Code, rec.Body.String())
	}

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

// fetchTaskGroups 请求 GET /tasks/groups，并校验响应是裸数组、每一项只有 name 与 count 两个字段。
func fetchTaskGroups(t *testing.T, engine *gin.Engine, token string) []taskGroupEntry {
	t.Helper()

	rec := performRequest(engine, http.MethodGet, "/api/v1/tasks/groups", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /tasks/groups, got %d: %s", rec.Code, rec.Body.String())
	}

	// 契约是裸数组：外面包一层 {data: ...} 的话，网页、App、演示站都会按数组解析失败。
	var rawItems []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &rawItems); err != nil {
		t.Fatalf("expected /tasks/groups to return a bare JSON array, got %s (%v)", rec.Body.String(), err)
	}
	// 一个分组都没有时必须是 []：null 会让前端直接 .map 报错。
	if rawItems == nil {
		t.Fatalf("expected [] rather than null, got %s", rec.Body.String())
	}

	groups := make([]taskGroupEntry, 0, len(rawItems))
	for _, item := range rawItems {
		if len(item) != 2 {
			t.Fatalf("expected each group item to carry exactly name and count, got %#v", item)
		}
		name, nameOK := item["name"].(string)
		count, countOK := item["count"].(float64)
		if !nameOK || !countOK {
			t.Fatalf("expected {name: string, count: number}, got %#v", item)
		}
		groups = append(groups, taskGroupEntry{Name: name, Count: int(count)})
	}
	return groups
}

// #130：视图筛选的 group 字段只认 `分组:` 标签，绝不能误中同名的自定义标签或订阅名。
// 用「labels 等于 X」近似分组就会踩这个坑：display_labels 里分组名、自定义标签、订阅名是混装的。
// 取法与列表展示的分组 chip 逐字一致：trim 后只认第一个非空的 `分组:`。
func TestTaskListGroupFilterMatchesOnlyGroupLabels(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "group-filter-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	subscription := &model.Subscription{
		Name:    "娱乐",
		Type:    model.SubTypeGitRepo,
		URL:     "https://example.com/fun.git",
		Enabled: true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	mustCreateLabeledTask(t, "分组正好是娱乐", "分组:娱乐")
	mustCreateLabeledTask(t, "分组标签带空格", " 分组: 娱乐 ", "自定义")
	mustCreateLabeledTask(t, "同名自定义标签", "娱乐")
	mustCreateLabeledTask(t, "同名订阅", "subscription:"+strconv.FormatUint(uint64(subscription.ID), 10))
	mustCreateLabeledTask(t, "分组名更长", "分组:娱乐环境")
	mustCreateLabeledTask(t, "第二个分组标签才是娱乐", "分组:工作", "分组:娱乐")
	mustCreateLabeledTask(t, "没有分组")

	cases := []struct {
		operator string
		want     []string
	}{
		{operator: "equals", want: []string{"分组正好是娱乐", "分组标签带空格"}},
		{operator: "not_equals", want: []string{"同名自定义标签", "同名订阅", "分组名更长", "第二个分组标签才是娱乐", "没有分组"}},
		{operator: "contains", want: []string{"分组正好是娱乐", "分组标签带空格", "分组名更长"}},
		{operator: "not_contains", want: []string{"同名自定义标签", "同名订阅", "第二个分组标签才是娱乐", "没有分组"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.operator, func(t *testing.T) {
			filterJSON := fmt.Sprintf(`[{"field":"group","operator":%q,"value":"娱乐"}]`, testCase.operator)
			got := listTaskNames(t, engine, token, "all=1&filters="+url.QueryEscape(filterJSON))
			sort.Strings(got)
			want := append([]string(nil), testCase.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("group %s 娱乐: expected %v, got %v", testCase.operator, want, got)
			}
		})
	}
}

// group 也能当视图的排序字段；没有分组的按空串参与比较。
func TestTaskListSortsByGroup(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "group-sort-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	mustCreateLabeledTask(t, "beta 组任务", "分组:beta")
	mustCreateLabeledTask(t, "没有分组的任务")
	mustCreateLabeledTask(t, "alpha 组任务", "分组:alpha")

	asc := listTaskNames(t, engine, token, "all=1&sort_rules="+url.QueryEscape(`[{"field":"group","direction":"asc"}]`))
	if want := []string{"没有分组的任务", "alpha 组任务", "beta 组任务"}; !reflect.DeepEqual(asc, want) {
		t.Fatalf("expected ascending group order %v, got %v", want, asc)
	}

	desc := listTaskNames(t, engine, token, "all=1&sort_rules="+url.QueryEscape(`[{"field":"group","direction":"desc"}]`))
	if want := []string{"beta 组任务", "alpha 组任务", "没有分组的任务"}; !reflect.DeepEqual(desc, want) {
		t.Fatalf("expected descending group order %v, got %v", want, desc)
	}
}

// #130：GET /tasks/groups 返回全部分组（契约 C2），口径与列表的分组名逐字一致：
// trim、只认第一个非空的 `分组:`、同名去重计数、按 name 升序。
func TestTaskGroupsReturnsDistinctFirstGroupLabelsSortedByName(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "group-list-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	if got := fetchTaskGroups(t, engine, token); len(got) != 0 {
		t.Fatalf("expected no group before any task exists, got %v", got)
	}

	mustCreateLabeledTask(t, "生产-1", "分组:生产")
	mustCreateLabeledTask(t, "生产-2", " 分组: 生产 ", "自定义")
	// 一条任务挂了多个分组标签时只认第一个，与列表里那枚分组 chip 一致。
	mustCreateLabeledTask(t, "多个分组标签", "分组:测试", "分组:生产")
	// 空分组名跳过，接着认下一个。
	mustCreateLabeledTask(t, "空分组在前", "分组:", "分组:alpha")
	// 同名的自定义标签不是分组。
	mustCreateLabeledTask(t, "只有同名自定义标签", "生产")
	// 含 `分组:` 子串但不是以它开头：SQL 粗筛会捞到，Go 侧必须排除。
	mustCreateLabeledTask(t, "前缀不在开头", "my分组:beta")
	mustCreateLabeledTask(t, "没有标签")
	mustCreateLabeledTask(t, "beta 组", "分组:beta")

	got := fetchTaskGroups(t, engine, token)
	want := []taskGroupEntry{
		{Name: "alpha", Count: 1},
		{Name: "beta", Count: 1},
		{Name: "测试", Count: 1},
		{Name: "生产", Count: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected groups %v, got %v", want, got)
	}
}

// 静态段 /tasks/groups 与同层的 /:id/... 共存：路由注册不能 panic（newProtectedRouter 能建出来即证明），
// /groups 必须真的落到 ListGroups，/:id/... 也不能受影响。
// 复用组上的 OpenAPIAccess("tasks")：viewer 与带 tasks scope 的应用令牌都能访问。
func TestTaskGroupsRouteCoexistsWithTaskIDRoutes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	viewer := testutil.MustCreateUser(t, "group-route-viewer", "viewer")
	viewerToken := testutil.MustCreateAccessToken(t, viewer.Username, viewer.Role)
	appToken := testutil.MustCreateAppToken(t, "group-route-app", "tasks")

	task := mustCreateLabeledTask(t, "路由共存", "分组:共存")

	for label, token := range map[string]string{"viewer": viewerToken, "tasks app": appToken} {
		got := fetchTaskGroups(t, engine, token)
		if want := []taskGroupEntry{{Name: "共存", Count: 1}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: expected groups %v, got %v", label, want, got)
		}
	}

	statsRec := performRequest(engine, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/stats", task.ID), map[string]string{
		"Authorization": "Bearer " + viewerToken,
	})
	if statsRec.Code != http.StatusOK {
		t.Fatalf("expected /tasks/:id/stats to stay reachable next to /tasks/groups, got %d: %s", statsRec.Code, statsRec.Body.String())
	}
}
