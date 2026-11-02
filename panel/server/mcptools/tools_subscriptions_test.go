package mcptools

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestCreateSubscriptionNormalizesEnumsAndHidesToken(t *testing.T) {
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		return http.StatusCreated, map[string]any{"message": "创建成功", "data": map[string]any{
			"id": 9, "name": "脚本库", "url": "https://example.com/repo.git", "type": "git-repo",
			"auth_type": "token", "has_auth_token": true, "auth_token": "不该出现", "auto_add_task_mode": "enabled",
			"hook_script": "echo ok", "enabled": true,
		}}, nil
	}}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "create_subscription", map[string]any{
		"name": " 脚本库 ", "url": " https://example.com/repo.git ", "type": "Git-Repo", "schedule": "0 */6 * * *",
		"whitelist": "jd_|utils/", "auto_add_task_mode": "ENABLED", "auth_type": "Token", "auth_token": "ghp_secret",
		"hook_script": "echo ok", "full_checkout": false,
	})
	if result.IsError {
		t.Fatalf("create_subscription 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	want := map[string]any{
		"name": "脚本库", "url": "https://example.com/repo.git", "type": "git-repo", "schedule": "0 */6 * * *",
		"whitelist": "jd_|utils/", "auto_add_task_mode": "enabled", "auth_type": "token", "auth_token": "ghp_secret",
		"hook_script": "echo ok", "full_checkout": false,
	}
	if call.method != http.MethodPost || call.path != "/subscriptions" || !reflect.DeepEqual(call.body, want) {
		t.Fatalf("应当以 POST /subscriptions 发送归一后的字段，实际 %s %s %#v", call.method, call.path, call.body)
	}
	if strings.Contains(text, "ghp_secret") || strings.Contains(text, "不该出现") {
		t.Fatalf("输出里不能出现访问令牌: %s", text)
	}
	sub, _ := decodeObject(t, text)["subscription"].(map[string]any)
	if sub["id"] != float64(9) || sub["has_auth_token"] != true || sub["hook_script"] != "echo ok" {
		t.Fatalf("应当返回精简后的订阅: %v", sub)
	}

	for _, args := range []map[string]any{
		{"name": "x", "url": " "},
		{"name": "x", "url": "https://a", "type": "svn"},
		{"name": "x", "url": "https://a", "overwrite_mode": "yes"},
		{"name": "x", "url": "https://a", "auth_type": "password"},
	} {
		if result, text := callTool(t, session, "create_subscription", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 1 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestUpdateSubscriptionSendsOnlyProvidedFields(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"message": "更新成功", "data": map[string]any{"id": 3, "full_checkout": false}})}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "update_subscription", map[string]any{"id": 3, "full_checkout": false, "auth_type": "", "ssh_key_id": 2})
	if result.IsError {
		t.Fatalf("update_subscription 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	want := map[string]any{"full_checkout": false, "auth_type": "", "ssh_key_id": int64(2)}
	if call.method != http.MethodPut || call.path != "/subscriptions/3" || !reflect.DeepEqual(call.body, want) {
		t.Fatalf("只应发送传入的字段（false 与空字符串也算传了），实际 %s %s %#v", call.method, call.path, call.body)
	}

	for _, args := range []map[string]any{{"id": 3}, {"id": 3, "url": " "}, {"id": -1, "name": "x"}} {
		if result, text := callTool(t, session, "update_subscription", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != 1 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestDeleteAndToggleSubscription(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"message": "ok", "data": map[string]any{"id": 4, "enabled": false}})}
	session := connectTestClient(t, fake, true)

	if result, text := callTool(t, session, "delete_subscription", map[string]any{"id": 4}); result.IsError {
		t.Fatalf("delete_subscription 不应失败: %s", text)
	}
	if result, text := callTool(t, session, "set_subscription_enabled", map[string]any{"id": 4, "enabled": false}); result.IsError {
		t.Fatalf("set_subscription_enabled 不应失败: %s", text)
	}
	if result, text := callTool(t, session, "set_subscription_enabled", map[string]any{"id": 4, "enabled": true}); result.IsError {
		t.Fatalf("set_subscription_enabled 不应失败: %s", text)
	}

	calls := fake.recorded()
	want := []struct{ method, path string }{
		{http.MethodDelete, "/subscriptions/4"},
		{http.MethodPut, "/subscriptions/4/disable"},
		{http.MethodPut, "/subscriptions/4/enable"},
	}
	for i, expect := range want {
		if calls[i].method != expect.method || calls[i].path != expect.path || calls[i].body != nil {
			t.Errorf("第 %d 次调用应当是 %s %s（无请求体），实际 %s %s %#v", i+1, expect.method, expect.path, calls[i].method, calls[i].path, calls[i].body)
		}
	}
	if result, _ := callTool(t, session, "delete_subscription", map[string]any{"id": 0}); !result.IsError {
		t.Fatal("id 为 0 应当报错")
	}
}
