package mcptools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// taskEnvScopeKeys 是 #168 任务级「可用环境变量」的三个字段。
var taskEnvScopeKeys = []string{"env_scope", "env_groups", "env_names"}

// #168：list_tasks 的精简输出每一项都带 env_scope（只带这一个，分组与变量名要看 get_task）。
func TestListTasksCarriesEnvScope(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{
		"data": []any{
			map[string]any{"id": 1, "name": "指定", "status": 1, "enabled": true, "labels": []any{},
				"env_scope": "selected", "env_groups": []any{"京东"}, "env_names": []any{"JD_COOKIE"}},
			map[string]any{"id": 2, "name": "全部", "status": 1, "enabled": true, "labels": []any{},
				"env_scope": "all", "env_groups": []any{}, "env_names": []any{}},
		},
		"total": 2, "page": 1, "page_size": 20,
	})}
	session := connectTestClient(t, fake, false)

	result, text := callTool(t, session, "list_tasks", map[string]any{})
	if result.IsError {
		t.Fatalf("list_tasks 不应失败: %s", text)
	}
	items, _ := decodeObject(t, text)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("应当返回 2 条任务，实际 %v", items)
	}
	for i, want := range []string{"selected", "all"} {
		item := items[i].(map[string]any)
		if item["env_scope"] != want {
			t.Errorf("第 %d 条任务的 env_scope 应为 %q，实际 %v", i+1, want, item["env_scope"])
		}
		for _, key := range []string{"env_groups", "env_names"} {
			if _, leaked := item[key]; leaked {
				t.Errorf("列表要精简，不应带 %s: %v", key, item)
			}
		}
	}
}

// #168：create_task / update_task 不开放可用环境变量三项（与「重要」标记同一先例，AI 不能改这类保护性设置）：
// 工具的入参 schema 里没有这三个键；调用方硬塞进来也不会进 PUT /tasks/:id 的请求体。
func TestUpdateTaskNeverSendsEnvScope(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{
		"message": "task updated",
		"data":    map[string]any{"id": 7, "name": "签到", "status": 1, "enabled": true, "labels": []any{}},
	})}
	session := connectTestClient(t, fake, true)

	tools := listedTools(t, session)
	for _, name := range []string{"create_task", "update_task"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("开启写入时应当有 %s", name)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s input schema: %v", name, err)
		}
		for _, key := range taskEnvScopeKeys {
			if strings.Contains(string(schema), `"`+key+`"`) {
				t.Errorf("%s 的入参不应开放 %s: %s", name, key, schema)
			}
		}
	}
	if !strings.Contains(tools["update_task"].Description, "不改可用环境变量范围") {
		t.Errorf("update_task 的描述应当说明不改可用环境变量范围，实际 %q", tools["update_task"].Description)
	}

	// 硬塞三项：SDK 按 schema 拒掉也好、忽略未知键照常调用也好，请求体里都不能有它们。
	// 这里不用 callTool：入参校验失败时 SDK 可能直接返回协议错误，callTool 会把它当成用例失败。
	_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "update_task", Arguments: map[string]any{
		"id": 7, "name": "签到", "env_scope": "all", "env_groups": []string{}, "env_names": []string{},
	}})
	// 正常调用一次，保证下面的断言至少检查到一条真实请求
	if result, text := callTool(t, session, "update_task", map[string]any{"id": 7, "name": "签到"}); result.IsError {
		t.Fatalf("update_task 不应失败: %s", text)
	}
	calls := fake.recorded()
	if len(calls) == 0 {
		t.Fatal("应当至少调用一次 PUT /tasks/7")
	}
	for _, call := range calls {
		if call.method != http.MethodPut || call.path != "/tasks/7" {
			t.Fatalf("只应调用 PUT /tasks/7，实际 %s %s", call.method, call.path)
		}
		body, _ := call.body.(map[string]any)
		for _, key := range taskEnvScopeKeys {
			if _, ok := body[key]; ok {
				t.Fatalf("update_task 的请求体不应带 %s，实际 %#v", key, call.body)
			}
		}
	}
}
