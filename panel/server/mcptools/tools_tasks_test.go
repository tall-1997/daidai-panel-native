package mcptools

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestCreateTaskSendsProvidedFieldsAndDecoratesResult(t *testing.T) {
	fake := &fakeDispatcher{respond: func(call recordedCall) (int, any, error) {
		return http.StatusCreated, map[string]any{
			"message": "创建成功",
			"data": map[string]any{"id": 5, "name": "签到", "command": "task a.py", "status": 1, "enabled": true,
				"labels": []any{"分组:日常"}, "last_run_status": nil, "cron_expression": "0 9 * * *"},
		}, nil
	}}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "create_task", map[string]any{
		"name": " 签到 ", "command": "task a.py", "task_type": "CRON", "cron_expression": "0 9 * * *",
		"timeout": 30, "labels": []string{"分组:日常", " "}, "notify_on_failure": false,
	})
	if result.IsError {
		t.Fatalf("create_task 不应失败: %s", text)
	}
	calls := fake.recorded()
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != "/tasks" {
		t.Fatalf("应当调用一次 POST /tasks，实际 %+v", calls)
	}
	want := map[string]any{
		"name": "签到", "command": "task a.py", "task_type": "cron", "cron_expression": "0 9 * * *",
		"timeout": 30, "labels": []string{"分组:日常"}, "notify_on_failure": false,
	}
	if !reflect.DeepEqual(calls[0].body, want) {
		t.Fatalf("请求体只应包含传入的字段（并做归一），实际 %#v", calls[0].body)
	}

	task, _ := decodeObject(t, text)["task"].(map[string]any)
	if task["id"] != float64(5) || task["group"] != "日常" || task["status_text"] != "空闲" || task["enabled"] != true {
		t.Fatalf("返回的任务应当带上派生字段: %v", task)
	}
}

func TestCreateTaskValidatesBeforeCalling(t *testing.T) {
	fake := &fakeDispatcher{}
	session := connectTestClient(t, fake, true)
	for _, args := range []map[string]any{
		{"name": "x", "command": "  "},
		{"name": "x", "command": "task a.py", "task_type": "weekly"},
		{"name": "x", "command": "task a.py", "timeout": -1},
	} {
		if result, text := callTool(t, session, "create_task", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestUpdateTaskSendsOnlyProvidedFields(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{
		"message": "task updated",
		"data":    map[string]any{"id": 7, "name": "签到", "command": "task b.py", "status": 0, "enabled": false, "labels": []any{}},
	})}
	session := connectTestClient(t, fake, true)

	// 脚本改名后同步命令（#139 的头号场景），顺带清空标签。
	result, text := callTool(t, session, "update_task", map[string]any{"id": 7, "command": " task b.py ", "labels": []string{}})
	if result.IsError {
		t.Fatalf("update_task 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	if call.method != http.MethodPut || call.path != "/tasks/7" {
		t.Fatalf("应当调用 PUT /tasks/7，实际 %s %s", call.method, call.path)
	}
	if !reflect.DeepEqual(call.body, map[string]any{"command": "task b.py", "labels": []string{}}) {
		t.Fatalf("只应发送传入的字段，空标签数组要原样发出以便清空，实际 %#v", call.body)
	}
	task, _ := decodeObject(t, text)["task"].(map[string]any)
	if task["command"] != "task b.py" || task["status_text"] != "禁用" {
		t.Fatalf("应当返回修改后的任务: %v", task)
	}

	for _, args := range []map[string]any{{"id": 7}, {"id": 7, "name": " "}, {"id": 0, "name": "x"}} {
		if result, text := callTool(t, session, "update_task", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 1 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestUpdateTaskSurfacesPanelValidation(t *testing.T) {
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		return http.StatusBadRequest, map[string]any{"error": "第 1 条定时规则无效: bad"}, nil
	}}
	session := connectTestClient(t, fake, true)
	result, text := callTool(t, session, "update_task", map[string]any{"id": 3, "cron_expression": "bad"})
	if !result.IsError || !strings.Contains(text, "定时规则无效") {
		t.Fatalf("面板的校验错误应当原样带给调用方，实际 isError=%v: %s", result.IsError, text)
	}
}
