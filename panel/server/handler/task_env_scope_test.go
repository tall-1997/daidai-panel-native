package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// taskEnvScopeOf 从任务响应里取出 env_scope / env_groups / env_names，两个列表必须是数组（不是 null）。
func taskEnvScopeOf(t *testing.T, task map[string]interface{}) (string, []string, []string) {
	t.Helper()

	scope, _ := task["env_scope"].(string)
	toStrings := func(key string) []string {
		raw, ok := task[key].([]interface{})
		if !ok {
			t.Fatalf("%s 必须是数组，实际 %#v", key, task[key])
		}
		values := make([]string, 0, len(raw))
		for _, item := range raw {
			text, _ := item.(string)
			values = append(values, text)
		}
		return values
	}
	return scope, toStrings("env_groups"), toStrings("env_names")
}

func assertTaskEnvScope(t *testing.T, task map[string]interface{}, wantScope, wantGroups, wantNames string) {
	t.Helper()

	scope, groups, names := taskEnvScopeOf(t, task)
	if scope != wantScope || strings.Join(groups, ",") != wantGroups || strings.Join(names, ",") != wantNames {
		t.Fatalf("环境变量范围不对：期望 %s / [%s] / [%s]，实际 %s / %v / %v", wantScope, wantGroups, wantNames, scope, groups, names)
	}
}

func loadTaskRowForEnvScope(t *testing.T, id uint) model.Task {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("load task %d: %v", id, err)
	}
	return task
}

// #168：创建、局部更新、非法入参、改回 all、不带三项保持原值、复制。
func TestTaskEnvScopeCreateUpdateAndCopy(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-scope-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	// 不带三项：默认全部，两个列表是空数组
	defaultRec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks",
		`{"name":"默认范围","command":"task demo.py","cron_expression":"0 0 * * *"}`, headers, "")
	if defaultRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", defaultRec.Code, defaultRec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, defaultRec)), "all", "", "")

	// 指定：分组去空去重；变量名兼容逗号字符串
	createRec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks",
		`{"name":"指定范围","command":"task demo.py","cron_expression":"0 0 * * *","env_scope":"selected","env_groups":["京东"," 京东 ","","日常"],"env_names":"JD_COOKIE, WX_TOKEN,JD_COOKIE"}`, headers, "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	created := responseTaskData(t, decodeJSONMap(t, createRec))
	assertTaskEnvScope(t, created, "selected", "京东,日常", "JD_COOKIE,WX_TOKEN")
	taskID := uint(created["id"].(float64))
	if row := loadTaskRowForEnvScope(t, taskID); row.EnvScope != "selected" || row.EnvGroups != "京东,日常" || row.EnvNames != "JD_COOKIE,WX_TOKEN" {
		t.Fatalf("库里的三列不对：%q / %q / %q", row.EnvScope, row.EnvGroups, row.EnvNames)
	}

	// 非法 scope、非法变量名：400，什么都不建
	for _, body := range []string{
		`{"name":"坏范围","command":"task demo.py","cron_expression":"0 0 * * *","env_scope":"some"}`,
		`{"name":"坏变量名","command":"task demo.py","cron_expression":"0 0 * * *","env_scope":"selected","env_names":["OK_NAME","1BAD-NAME"]}`,
		`{"name":"坏分组类型","command":"task demo.py","cron_expression":"0 0 * * *","env_scope":"selected","env_groups":[1]}`,
	} {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks", body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	badNameRec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks",
		`{"name":"坏变量名","command":"task demo.py","cron_expression":"0 0 * * *","env_scope":"selected","env_names":["1BAD-NAME"]}`, headers, "")
	if errText, _ := decodeJSONMap(t, badNameRec)["error"].(string); !strings.Contains(errText, "1BAD-NAME") {
		t.Fatalf("非法变量名的报错要点名是哪个，实际 %q", errText)
	}
	var badCount int64
	database.DB.Model(&model.Task{}).Where("name LIKE ?", "坏%").Count(&badCount)
	if badCount != 0 {
		t.Fatalf("非法入参不应建出任务，实际建了 %d 个", badCount)
	}

	updatePath := fmt.Sprintf("/api/v1/tasks/%d", taskID)

	// 不带三项（旧网页缓存、旧版 APP、开放 API）：保持原值
	rec := performJSONRequest(engine, http.MethodPut, updatePath, `{"name":"指定范围-改名"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, rec)), "selected", "京东,日常", "JD_COOKIE,WX_TOKEN")

	// 局部更新：只带 env_names，scope 与分组沿用库里的值
	rec = performJSONRequest(engine, http.MethodPut, updatePath, `{"env_names":["ONLY_ME"]}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, rec)), "selected", "京东,日常", "ONLY_ME")

	// 非法 scope / 变量名 / 类型：400，库里保持原样
	for _, body := range []string{`{"env_scope":"bogus"}`, `{"env_scope":null}`, `{"env_names":["bad name"]}`, `{"env_groups":{"a":1}}`} {
		rec = performJSONRequest(engine, http.MethodPut, updatePath, body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	if row := loadTaskRowForEnvScope(t, taskID); row.EnvScope != "selected" || row.EnvGroups != "京东,日常" || row.EnvNames != "ONLY_ME" {
		t.Fatalf("非法更新不应改动库里的值，实际 %q / %q / %q", row.EnvScope, row.EnvGroups, row.EnvNames)
	}

	// 复制：范围原样跟到副本上
	copyRec := performRequest(engine, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%d/copy", taskID), headers)
	if copyRec.Code != http.StatusCreated {
		t.Fatalf("expected copy 201, got %d: %s", copyRec.Code, copyRec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, copyRec)), "selected", "京东,日常", "ONLY_ME")

	// 改回 all：两列一并清空
	rec = performJSONRequest(engine, http.MethodPut, updatePath, `{"env_scope":"all"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, rec)), "all", "", "")
	if row := loadTaskRowForEnvScope(t, taskID); row.EnvScope != "all" || row.EnvGroups != "" || row.EnvNames != "" {
		t.Fatalf("改回 all 后两列应清空，实际 %q / %q / %q", row.EnvScope, row.EnvGroups, row.EnvNames)
	}

	// 再切回指定、两个列表都空：合法（= 一个用户变量都不注入）
	rec = performJSONRequest(engine, http.MethodPut, updatePath, `{"env_scope":"selected","env_groups":[],"env_names":[]}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertTaskEnvScope(t, responseTaskData(t, decodeJSONMap(t, rec)), "selected", "", "")
}

// #168：导出带上三项（数组形态），导入原样读回；老导出文件没有这三个键 → 全部；导入时非法的那条记错误、其余照导。
func TestTaskEnvScopeExportImportRoundTrip(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-scope-export", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	task := &model.Task{
		Name:           "导出任务",
		Command:        "task demo.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
		EnvScope:       model.TaskEnvScopeSelected,
		EnvGroups:      "京东",
		EnvNames:       "JD_COOKIE,WX_TOKEN",
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	exportRec := performRequest(engine, http.MethodGet, "/api/v1/tasks/export", headers)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected export 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}
	exported, _ := decodeJSONMap(t, exportRec)["data"].([]interface{})
	if len(exported) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(exported))
	}
	exportedTask, _ := exported[0].(map[string]interface{})
	assertTaskEnvScope(t, exportedTask, "selected", "京东", "JD_COOKIE,WX_TOKEN")

	// 把导出的那条改个名再导回去，外加一条老格式（没有三个键）和一条非法的
	exportedTask["name"] = "导入任务"
	legacy := map[string]interface{}{"name": "老格式任务", "command": "task old.py", "cron_expression": "0 0 * * *"}
	invalid := map[string]interface{}{"name": "非法任务", "command": "task bad.py", "cron_expression": "0 0 * * *", "env_scope": "selected", "env_names": []string{"9X"}}
	body, err := json.Marshal(map[string]interface{}{"tasks": []interface{}{exportedTask, legacy, invalid}})
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}
	importRec := performJSONRequest(engine, http.MethodPost, "/api/v1/tasks/import", string(body), headers, "")
	if importRec.Code != http.StatusCreated {
		t.Fatalf("expected import 201, got %d: %s", importRec.Code, importRec.Body.String())
	}
	importErrors, _ := decodeJSONMap(t, importRec)["errors"].([]interface{})
	if len(importErrors) != 1 || !strings.Contains(fmt.Sprint(importErrors[0]), "9X") {
		t.Fatalf("非法那条应记进 errors 并点名变量名，实际 %v", importErrors)
	}

	var imported model.Task
	if err := database.DB.Where("name = ?", "导入任务").First(&imported).Error; err != nil {
		t.Fatalf("load imported task: %v", err)
	}
	if imported.EnvScope != "selected" || imported.EnvGroups != "京东" || imported.EnvNames != "JD_COOKIE,WX_TOKEN" {
		t.Fatalf("导入后环境变量范围不对：%q / %q / %q", imported.EnvScope, imported.EnvGroups, imported.EnvNames)
	}
	var legacyTask model.Task
	if err := database.DB.Where("name = ?", "老格式任务").First(&legacyTask).Error; err != nil {
		t.Fatalf("load legacy imported task: %v", err)
	}
	if legacyTask.EnvScope != "all" || legacyTask.EnvGroups != "" || legacyTask.EnvNames != "" {
		t.Fatalf("老格式导入应为全部，实际 %q / %q / %q", legacyTask.EnvScope, legacyTask.EnvGroups, legacyTask.EnvNames)
	}
	var invalidCount int64
	database.DB.Model(&model.Task{}).Where("name = ?", "非法任务").Count(&invalidCount)
	if invalidCount != 0 {
		t.Fatal("非法的那条不应导入")
	}
}

// buildScriptTokenForTest 走真实的 BuildNotifyHelperEnv 签脚本令牌：notifyOnly=true 就是任务「指定」范围时注入的那枚。
func buildScriptTokenForTest(t *testing.T, notifyOnly bool) string {
	t.Helper()

	scriptsDir := config.C.Data.ScriptsDir
	env, _, err := service.BuildNotifyHelperEnv(scriptsDir, scriptsDir, config.C.Server.Port, nil, time.Hour, notifyOnly)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}
	if env["DAIDAI_TOKEN"] == "" {
		t.Fatal("expected DAIDAI_TOKEN")
	}
	return env["DAIDAI_TOKEN"]
}

// #168：notify-only 脚本令牌只能调发通知接口（/api/v1 与旧 /api 两份），其余一律 403；普通脚本令牌不受影响。
func TestNotifyOnlyScriptTokenOnlyReachesNotificationSend(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newMCPTestEngine(t)
	notifyOnly := buildScriptTokenForTest(t, true)
	headers := map[string]string{"Authorization": "Bearer " + notifyOnly}

	// 发通知：不被这条 403 拦（没有渠道时可以因为别的原因失败，那不是这里要管的）
	for _, path := range []string{"/api/v1/notifications/send", "/api/notifications/send"} {
		rec := performJSONRequest(engine, http.MethodPost, path, `{"title":"t","content":"c"}`, headers, "")
		if rec.Code == http.StatusForbidden || strings.Contains(rec.Body.String(), middleware.NotifyOnlyTokenForbiddenMessage) {
			t.Fatalf("notify-only 令牌调 %s 不应被拦，实际 %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/envs", ""},
		{http.MethodGet, "/api/envs", ""},
		{http.MethodPost, "/api/v1/tasks", `{"name":"x","command":"task x.py","cron_expression":"0 0 * * *"}`},
		{http.MethodPut, "/api/v1/tasks/1/run", ""},
		// 发通知路由的其它方法也不放行
		{http.MethodGet, "/api/v1/notifications", ""},
	}
	for _, tc := range cases {
		rec := performJSONRequest(engine, tc.method, tc.path, tc.body, headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("notify-only 令牌调 %s %s 应回 403，实际 %d: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if errText, _ := decodeJSONMap(t, rec)["error"].(string); errText != middleware.NotifyOnlyTokenForbiddenMessage {
			t.Fatalf("%s %s 的 403 文案不对：%q", tc.method, tc.path, errText)
		}
	}

	// 普通脚本令牌（任务是「全部」时注入的那枚）照旧能调 /envs
	normal := buildScriptTokenForTest(t, false)
	rec := performRequest(engine, http.MethodGet, "/api/v1/envs", map[string]string{"Authorization": "Bearer " + normal})
	if rec.Code != http.StatusOK {
		t.Fatalf("普通脚本令牌调 /envs 应照旧 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if claims, err := middleware.ParseToken(normal); err != nil || claims.NotifyOnly {
		t.Fatalf("普通脚本令牌不应带 notify_only：claims=%#v err=%v", claims, err)
	}
}

// #168：MCP 的 Bearer 鉴权直接拒绝 notify-only 令牌（连工具清单都列不出来）。
func TestMCPRejectsNotifyOnlyScriptToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	notifyOnly := buildScriptTokenForTest(t, true)

	rec := postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpInitializeBody, map[string]string{"Authorization": "Bearer " + notifyOnly})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("MCP 应拒绝 notify-only 令牌（403），实际 %d: %s", rec.Code, rec.Body.String())
	}
	if errText, _ := decodeJSONMap(t, rec)["error"].(string); errText != middleware.NotifyOnlyTokenForbiddenMessage {
		t.Fatalf("MCP 拒绝文案不对：%q", errText)
	}
}
