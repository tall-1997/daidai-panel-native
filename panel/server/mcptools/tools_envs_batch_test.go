package mcptools

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestBatchEnvActionRoutesEachAction(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"message": "已处理 2 个环境变量"})}
	session := connectTestClient(t, fake, true)

	for _, args := range []map[string]any{
		{"ids": []int{1}, "action": "rename"},
		{"ids": []int{}, "action": "enable"},
		{"ids": []int{0}, "action": "enable"},
	} {
		if result, text := callTool(t, session, "batch_env_action", args); !result.IsError {
			t.Errorf("非法参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}

	want := []struct{ action, method, path string }{
		{"Enable", http.MethodPut, "/envs/batch/enable"},
		{"disable", http.MethodPut, "/envs/batch/disable"},
		{"delete", http.MethodDelete, "/envs/batch"},
	}
	for i, expect := range want {
		result, text := callTool(t, session, "batch_env_action", map[string]any{"ids": []int{1, 2}, "action": expect.action})
		if result.IsError {
			t.Fatalf("%s 不应失败: %s", expect.action, text)
		}
		call := fake.recorded()[i]
		if call.method != expect.method || call.path != expect.path || !reflect.DeepEqual(call.body, map[string]any{"ids": []int64{1, 2}}) {
			t.Errorf("%s 应当以 %s %s 转发 ids，实际 %s %s %#v", expect.action, expect.method, expect.path, call.method, call.path, call.body)
		}
	}
}

func TestExportEnvsMasksSensitiveValues(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"data": []any{
		map[string]any{"name": "JD_COOKIE", "value": "pt_key=abcdef;pt_pin=zz;", "remarks": "账号1", "group": "京东", "groups": []any{"京东"}, "enabled": true},
		map[string]any{"name": "PLAIN_SETTING", "value": "hello", "remarks": "", "group": "", "groups": []any{}, "enabled": true},
		map[string]any{"name": "OFF_SETTING", "value": "off", "remarks": "", "group": "", "groups": []any{}, "enabled": false},
	}})}
	session := connectTestClient(t, fake, false)

	result, text := callTool(t, session, "export_envs", map[string]any{"ids": []int{3, 1}, "enabled_only": true})
	if result.IsError {
		t.Fatalf("export_envs 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	if call.method != http.MethodGet || call.path != "/envs/export-all" || call.query.Get("ids") != "3,1" {
		t.Fatalf("应当以 ids 查询 GET /envs/export-all，实际 %s %s %v", call.method, call.path, call.query)
	}
	if strings.Contains(text, "abcdef") {
		t.Fatalf("导出同样不能泄露敏感变量的明文: %s", text)
	}
	out := decodeObject(t, text)
	envs, _ := out["envs"].([]any)
	if out["total"] != float64(2) || len(envs) != 2 || out["note"] == nil {
		t.Fatalf("enabled_only 应当滤掉禁用变量，并注明有值被遮蔽，实际 %s", text)
	}
	cookie, plain := envs[0].(map[string]any), envs[1].(map[string]any)
	if cookie["value"] != "pt_******zz;" || cookie["value_masked"] != true || cookie["remarks"] != "账号1" || cookie["groups"] != nil {
		t.Errorf("敏感变量应当遮蔽并保留导入需要的字段: %v", cookie)
	}
	if plain["value"] != "hello" || plain["value_masked"] != nil {
		t.Errorf("普通变量应当原样导出: %v", plain)
	}

	if result, _ := callTool(t, session, "export_envs", map[string]any{"ids": []int{0}}); !result.IsError {
		t.Fatal("非法 ID 应当报错")
	}
}

func TestImportEnvsValidatesBeforeCalling(t *testing.T) {
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		return http.StatusCreated, map[string]any{"message": "成功导入 2 个环境变量", "errors": []any{}}, nil
	}}
	session := connectTestClient(t, fake, true)

	for _, args := range []map[string]any{
		{"envs": []map[string]any{}},
		{"envs": []map[string]any{{"name": "OK", "value": "1"}}, "mode": "overwrite"},
		// replace 模式下面板先清空再逐条导入：名称全不合法时原有变量已经删光了，所以必须在发请求前拦下。
		{"envs": []map[string]any{{"name": "1BAD", "value": "1"}}, "mode": "replace"},
		{"envs": []map[string]any{{"name": "a-b", "value": "1"}}},
		// 把 export_envs 的遮蔽值原样导回去，会用星号覆盖真实凭据。
		{"envs": []map[string]any{{"name": "JD_COOKIE", "value": "pt_******zz;"}}},
		{"envs": []map[string]any{{"name": "BARK_KEY", "value": "********"}}},
	} {
		if result, text := callTool(t, session, "import_envs", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}

	result, text := callTool(t, session, "import_envs", map[string]any{"envs": []map[string]any{
		{"name": " PLAIN ", "value": "1", "group": "g"},
		{"name": "API_TOKEN", "value": "tok-1234567890", "remarks": "r", "enabled": false},
		// 非敏感变量的值恰好是星号不受影响：它从来不会被遮蔽。
		{"name": "STARS", "value": "********"},
	}})
	if result.IsError {
		t.Fatalf("import_envs 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	want := map[string]any{"mode": "merge", "envs": []map[string]any{
		{"name": "PLAIN", "value": "1", "remarks": "", "group": "g"},
		{"name": "API_TOKEN", "value": "tok-1234567890", "remarks": "r", "enabled": false},
		{"name": "STARS", "value": "********", "remarks": ""},
	}}
	if call.method != http.MethodPost || call.path != "/envs/import" || !reflect.DeepEqual(call.body, want) {
		t.Fatalf("应当以 POST /envs/import 转发（mode 默认 merge），实际 %s %s %#v", call.method, call.path, call.body)
	}
	if out := decodeObject(t, text); out["mode"] != "merge" || out["errors"] != nil {
		t.Fatalf("没有逐条错误时不应带 errors，实际 %s", text)
	}
}

func TestImportEnvsReportsPerItemErrors(t *testing.T) {
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		return http.StatusCreated, map[string]any{"message": "成功导入 1 个环境变量", "errors": []any{"item 2: UNIQUE constraint failed"}}, nil
	}}
	session := connectTestClient(t, fake, true)
	result, text := callTool(t, session, "import_envs", map[string]any{"mode": "replace", "envs": []map[string]any{
		{"name": "A", "value": "1"}, {"name": "B", "value": "2"},
	}})
	if result.IsError || !strings.Contains(text, "UNIQUE constraint failed") {
		t.Fatalf("部分成功时应当把逐条错误带给调用方，实际 isError=%v: %s", result.IsError, text)
	}
	if body, _ := fake.recorded()[0].body.(map[string]any); body["mode"] != "replace" {
		t.Fatalf("mode 应当原样转发，实际 %#v", fake.recorded()[0].body)
	}
}

// #161：merge_value 原样转发；没给备注的条目不带 remarks 键（面板「有键才写备注」），非法模式仍报错。
func TestImportEnvsAcceptsMergeValueMode(t *testing.T) {
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		return http.StatusCreated, map[string]any{"message": "成功导入 2 个环境变量（新建 1 个，更新 1 个）", "errors": []any{}}, nil
	}}
	session := connectTestClient(t, fake, true)

	if result, text := callTool(t, session, "import_envs", map[string]any{"mode": "merge-value", "envs": []map[string]any{{"name": "A", "value": "1"}}}); !result.IsError {
		t.Fatalf("非法模式应当报错，实际 %s", text)
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("模式非法时不应调用面板接口")
	}

	result, text := callTool(t, session, "import_envs", map[string]any{"mode": " Merge_Value ", "envs": []map[string]any{
		{"name": "A", "value": "1"},
		{"name": "B", "value": "2", "remarks": "r"},
	}})
	if result.IsError {
		t.Fatalf("merge_value 不应失败: %s", text)
	}
	want := map[string]any{"mode": "merge_value", "envs": []map[string]any{
		{"name": "A", "value": "1"},
		{"name": "B", "value": "2", "remarks": "r"},
	}}
	if call := fake.recorded()[0]; call.path != "/envs/import" || !reflect.DeepEqual(call.body, want) {
		t.Fatalf("merge_value 转发内容不对，实际 %s %#v", call.path, call.body)
	}
	if out := decodeObject(t, text); out["mode"] != "merge_value" {
		t.Fatalf("返回应带 mode=merge_value，实际 %s", text)
	}
}

func TestLooksLikeMaskedEnvValueMatchesMaskOutput(t *testing.T) {
	for _, value := range []string{"abcdefghijkl", "short", "", "中文字符组成的一段很长的值啊", "pt_key=abc;pt_pin=x;"} {
		if masked := MaskEnvValue(value); !looksLikeMaskedEnvValue(masked) {
			t.Errorf("MaskEnvValue(%q) = %q 应当被识别为遮蔽值", value, masked)
		}
	}
	for _, value := range []string{"pt_key=abc;", "abc*****xyz", "abcd******xyz", "tok-1234567890"} {
		if looksLikeMaskedEnvValue(value) {
			t.Errorf("%q 不是遮蔽值，不应被拦", value)
		}
	}
}
