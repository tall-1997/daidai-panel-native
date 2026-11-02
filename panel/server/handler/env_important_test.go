package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// importantByName 读库里每个变量名对应的 important（测试数据里变量名不重复）。
func importantByName(t *testing.T) map[string]bool {
	t.Helper()

	var envs []model.EnvVar
	if err := database.DB.Order("id ASC").Find(&envs).Error; err != nil {
		t.Fatalf("list envs: %v", err)
	}
	out := make(map[string]bool, len(envs))
	for _, env := range envs {
		out[env.Name] = env.Important
	}
	return out
}

// APP #16 契约：ToDict 永远带 important（新 APP 靠「有没有这个键」判断面板支不支持，不能 omitempty）；
// POST 可直接标重要；PUT 是指针字段，不传不动、false 是合法修改、非布尔 400 且什么都不写。
func TestEnvImportantFlagCreateUpdateAndList(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-important-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs", `{"name":"CFG","value":"1","important":true}`, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	created, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if created["important"] != true {
		t.Fatalf("POST 带 important:true 应当落库并回显，实际 %s", rec.Body.String())
	}
	cfgID := uint(created["id"].(float64))
	if !reloadEnvVar(t, cfgID).Important {
		t.Fatalf("POST 带 important:true 应当落库")
	}

	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs", `{"name":"ACCOUNT","value":"2"}`, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// important 不是布尔：与其它字段的绑定错误同口径，整个请求 400，一条都不建。
	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs", `{"name":"BAD_FLAG","value":"3","important":"yes"}`, headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST 带非布尔 important 应当 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if _, exists := importantByName(t)["BAD_FLAG"]; exists {
		t.Fatalf("POST 400 时不应建出任何变量")
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/envs?all=1", headers)
	items, _ := decodeJSONMap(t, listRec)["data"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected 2 envs, got %s", listRec.Body.String())
	}
	for _, raw := range items {
		item := raw.(map[string]interface{})
		value, present := item["important"]
		if !present {
			t.Fatalf("列表项必须永远带 important 键（false 也带），实际 %v", item)
		}
		if want := item["name"] == "CFG"; value != want {
			t.Fatalf("%v 的 important 应为 %v，实际 %v", item["name"], want, value)
		}
	}

	// 网页编辑弹窗没拨开关、APP 的编辑请求体都不带 important：整体回写不能把标记冲掉。
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", cfgID),
		`{"name":"CFG","value":"changed","remarks":"r","group":"","groups":[]}`, headers, "")
	if rec.Code != http.StatusOK || !reloadEnvVar(t, cfgID).Important {
		t.Fatalf("不带 important 的 PUT 不能动标记，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", cfgID), `{"important":false}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected unmark 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if data, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{}); data["important"] != false || reloadEnvVar(t, cfgID).Important {
		t.Fatalf("PUT important:false 应当取消标记，实际 %s", rec.Body.String())
	}

	// 非布尔整单 400：同一个请求里合法的字段也不能写进去。
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", cfgID), `{"value":"must-not-write","important":"yes"}`, headers, "")
	// 本机版（Android）：变量值落库即密封（Value 为空、密文在 sealed），这里按解封后的明文比对。
	storedAfterReject := reloadEnvVar(t, cfgID)
	if opened := service.OpenEnvVarValue(context.Background(), &storedAfterReject); rec.Code != http.StatusBadRequest || storedAfterReject.Important || opened != "changed" {
		t.Fatalf("非布尔的 important 应当 400 且不动库，实际 %d body=%s important=%v value=%q", rec.Code, rec.Body.String(), storedAfterReject.Important, service.OpenEnvVarValue(context.Background(), &storedAfterReject))
	}

	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", cfgID), `{"important":true}`, headers, "")
	if rec.Code != http.StatusOK || !reloadEnvVar(t, cfgID).Important {
		t.Fatalf("PUT important:true 应当标为重要，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", cfgID), `{"important":true}`, headers, "")
	if got, _ := decodeJSONMap(t, rec)["message"].(string); rec.Code != http.StatusOK || got != "未检测到字段变更" {
		t.Fatalf("值没变不算变更，实际 %d %q", rec.Code, got)
	}
}

// APP #16 契约：export-all 永远带 important；merge 导入只升不降（文件里 false / 没这个键都不动，
// 否则旧导出文件一导入就静默取消标记）；replace 清空后按文件原样恢复。
func TestEnvImportantFlagExportAndImport(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-important-import", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	mustCreateEnvVars(t,
		&model.EnvVar{Name: "KEEP_TRUE_ON_FALSE", Value: "1", Enabled: true, Position: 1000, Important: true},
		&model.EnvVar{Name: "KEEP_TRUE_ON_MISSING", Value: "2", Enabled: true, Position: 2000, Important: true},
		&model.EnvVar{Name: "UPGRADE", Value: "3", Enabled: true, Position: 3000},
		&model.EnvVar{Name: "STAY_FALSE", Value: "4", Enabled: true, Position: 4000},
	)

	rec := performRequest(engine, http.MethodGet, "/api/v1/envs/export-all", headers)
	exported, _ := decodeJSONMap(t, rec)["data"].([]interface{})
	if len(exported) != 4 {
		t.Fatalf("expected 4 exported envs, got %s", rec.Body.String())
	}
	for _, raw := range exported {
		item := raw.(map[string]interface{})
		value, present := item["important"]
		if !present {
			t.Fatalf("export-all 每一条都要带 important（false 也带），实际 %v", item)
		}
		if want := strings.HasPrefix(fmt.Sprint(item["name"]), "KEEP_TRUE"); value != want {
			t.Fatalf("export-all 里 %v 的 important 应为 %v，实际 %v", item["name"], want, value)
		}
	}

	importBody := func(mode string, envs ...map[string]interface{}) string {
		body, _ := json.Marshal(map[string]interface{}{"mode": mode, "envs": envs})
		return string(body)
	}
	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import", importBody("merge",
		map[string]interface{}{"name": "KEEP_TRUE_ON_FALSE", "value": "1b", "important": false},
		map[string]interface{}{"name": "KEEP_TRUE_ON_MISSING", "value": "2b"},
		map[string]interface{}{"name": "UPGRADE", "value": "3b", "important": true},
		map[string]interface{}{"name": "STAY_FALSE", "value": "4b", "important": "true"},
		map[string]interface{}{"name": "NEW_TRUE", "value": "5", "important": true},
		map[string]interface{}{"name": "NEW_FALSE", "value": "6", "important": false},
	), headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected merge import 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	want := map[string]bool{
		"KEEP_TRUE_ON_FALSE": true, "KEEP_TRUE_ON_MISSING": true, "UPGRADE": true,
		"STAY_FALSE": false, "NEW_TRUE": true, "NEW_FALSE": false,
	}
	if got := importantByName(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("merge 应当只升不降、新行按文件写（字符串 \"true\" 不算），期望 %v，实际 %v", want, got)
	}

	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import", importBody("replace",
		map[string]interface{}{"name": "FROM_FILE_TRUE", "value": "x", "important": true},
		map[string]interface{}{"name": "UPGRADE", "value": "y", "important": false},
	), headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected replace import 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	want = map[string]bool{"FROM_FILE_TRUE": true, "UPGRADE": false}
	if got := importantByName(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("replace 应当清空全部（含重要变量）后按文件原样恢复，期望 %v，实际 %v", want, got)
	}
}

// DD8：POST /envs/import 新建的行要保住文件里的禁用状态（enabled:false，以及青龙格式的 status:1）。
// EnvVar.Enabled 带 default:true，以前新建行一律落成启用，「导出 → 替换导入」会把禁用的变量静默重新启用。
// merge 命中走 map 更新，本来就不受影响（TestImportMergeMatchesOnNameAndRemarks 管那条路）。
func TestEnvImportNewRowsKeepDisabledState(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-import-disabled", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	enabledByName := func() map[string]bool {
		var envs []model.EnvVar
		if err := database.DB.Order("id ASC").Find(&envs).Error; err != nil {
			t.Fatalf("list envs: %v", err)
		}
		out := make(map[string]bool, len(envs))
		for _, env := range envs {
			out[env.Name] = env.Enabled
		}
		return out
	}

	// replace：先清空，导进来的全是新行。
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import",
		`{"mode":"replace","envs":[`+
			`{"name":"OFF_BY_ENABLED","value":"1","enabled":false},`+
			`{"name":"OFF_BY_QL_STATUS","value":"2","status":1},`+
			`{"name":"ON_BY_ENABLED","value":"3","enabled":true},`+
			`{"name":"ON_BY_QL_STATUS","value":"4","status":0},`+
			`{"name":"ON_BY_DEFAULT","value":"5"}]}`, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected replace import 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if problems, _ := decodeJSONMap(t, rec)["errors"].([]interface{}); len(problems) != 0 {
		t.Fatalf("禁用状态补写不应报错，实际 %s", rec.Body.String())
	}
	want := map[string]bool{
		"OFF_BY_ENABLED": false, "OFF_BY_QL_STATUS": false,
		"ON_BY_ENABLED": true, "ON_BY_QL_STATUS": true, "ON_BY_DEFAULT": true,
	}
	if got := enabledByName(); !reflect.DeepEqual(got, want) {
		t.Fatalf("replace 导入应当保住文件里的启用 / 禁用状态，期望 %v，实际 %v", want, got)
	}

	// merge 没命中：同样是新行，同样要保住禁用状态。
	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import",
		`{"mode":"merge","envs":[`+
			`{"name":"MERGE_NEW_OFF","value":"6","enabled":false},`+
			`{"name":"MERGE_NEW_QL_OFF","value":"7","status":1}]}`, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected merge import 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	want["MERGE_NEW_OFF"] = false
	want["MERGE_NEW_QL_OFF"] = false
	if got := enabledByName(); !reflect.DeepEqual(got, want) {
		t.Fatalf("merge 没命中时新建的行应当保住禁用状态，期望 %v，实际 %v", want, got)
	}
}

// 轻量档（D1）：服务端不拦删除，脚本、开放 API、MCP、老 APP 照常能删重要变量。
func TestEnvDeleteIgnoresImportantFlag(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-important-delete", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	single := &model.EnvVar{Name: "SINGLE", Value: "1", Enabled: true, Position: 1000, Important: true}
	batchImportant := &model.EnvVar{Name: "BATCH_IMPORTANT", Value: "2", Enabled: true, Position: 2000, Important: true}
	batchNormal := &model.EnvVar{Name: "BATCH_NORMAL", Value: "3", Enabled: true, Position: 3000}
	mustCreateEnvVars(t, single, batchImportant, batchNormal)

	rec := performRequest(engine, http.MethodDelete, fmt.Sprintf("/api/v1/envs/%d", single.ID), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, exists := importantByName(t)["SINGLE"]; exists {
		t.Fatalf("单删重要变量应当真的删掉")
	}
	rec = performJSONRequest(engine, http.MethodDelete, "/api/v1/envs/batch",
		fmt.Sprintf(`{"ids":[%d,%d]}`, batchImportant.ID, batchNormal.ID), headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "已删除 2 个环境变量") {
		t.Fatalf("批量删除不跳过重要变量（跳过由客户端做），实际 %d body=%s", rec.Code, rec.Body.String())
	}
	var left int64
	database.DB.Model(&model.EnvVar{}).Count(&left)
	if left != 0 {
		t.Fatalf("expected all envs deleted, %d left", left)
	}
}

// MCP 经真实路由：list_envs / export_envs 输出带 important，import_envs 接受 important（merge 同样只升不降）。
func TestMCPEnvToolsCarryImportantFlagEndToEnd(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, true)
	admin := testutil.MustCreateUser(t, "mcp-env-important", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	mustCreateEnvVars(t,
		&model.EnvVar{Name: "CFG_IMPORTANT", Value: "1", Enabled: true, Position: 1000, Important: true},
		&model.EnvVar{Name: "PLAIN", Value: "2", Enabled: true, Position: 2000},
	)

	session := connectMCPSession(t, engine, "Bearer "+token)
	for _, tool := range []string{"list_envs", "export_envs"} {
		result, text := mcpCallText(t, session, tool, map[string]any{})
		if result.IsError || !strings.Contains(text, `"important":true`) || !strings.Contains(text, `"important":false`) {
			t.Fatalf("%s 的输出应当带每条变量的 important，实际: %s", tool, text)
		}
	}

	result, text := mcpCallText(t, session, "import_envs", map[string]any{"envs": []map[string]any{
		{"name": "PLAIN", "value": "2b", "important": true},
		{"name": "CFG_IMPORTANT", "value": "1b", "important": false},
	}})
	if result.IsError {
		t.Fatalf("import_envs 不应失败: %s", text)
	}
	if got := importantByName(t); !got["PLAIN"] || !got["CFG_IMPORTANT"] {
		t.Fatalf("import_envs merge 应当把 PLAIN 标为重要、不取消 CFG_IMPORTANT，实际 %v", got)
	}
}
