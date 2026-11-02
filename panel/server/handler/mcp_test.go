package handler_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/router"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 这组用例都走 router.Setup 装出的完整路由：MCP 的工具调用会在进程内回放到真实的 /api/v1 接口，
// 只有整套路由都在，才能验证 scope、审计、CORS 这些「白拿」的能力真的生效了。

const (
	mcpInitializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw-test","version":"1.0"}}}`
	mcpToolsListBody  = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
)

func newMCPTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	engine := gin.New()
	router.Setup(engine)
	return engine
}

func setMCPSwitches(t *testing.T, enabled, allowMutations bool) {
	t.Helper()
	if err := model.SetConfig(model.MCPEnabledConfigKey, strconv.FormatBool(enabled)); err != nil {
		t.Fatalf("set mcp_enabled: %v", err)
	}
	if err := model.SetConfig(model.MCPAllowMutationsConfigKey, strconv.FormatBool(allowMutations)); err != nil {
		t.Fatalf("set mcp_allow_mutations: %v", err)
	}
}

func mcpBasicAuth(appKey, appSecret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(appKey+":"+appSecret))
}

func postMCPRaw(engine http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

type mcpAuthTransport struct {
	authorization string
}

func (a mcpAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", a.authorization)
	return http.DefaultTransport.RoundTrip(clone)
}

// connectMCPSession 用官方 SDK 的客户端连真实监听的面板，走的是完整的 Streamable HTTP 协商流程。
func connectMCPSession(t *testing.T, engine http.Handler, authorization string) *mcp.ClientSession {
	t.Helper()
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	transport := &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/api/v1/mcp",
		HTTPClient:           &http.Client{Transport: mcpAuthTransport{authorization: authorization}, Timeout: 30 * time.Second},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "handler-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func mcpToolNames(t *testing.T, session *mcp.ClientSession) map[string]bool {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	return names
}

func mcpCallText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return result, strings.Join(parts, "\n")
}

func createMCPTestTask(t *testing.T, name string) *model.Task {
	t.Helper()
	task := &model.Task{Name: name, Command: "echo hi", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestMCPEndpointIsForbiddenUntilEnabled(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	app := testutil.MustCreateOpenApp(t, "mcp-off", "tasks")

	rec := postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpToolsListBody, map[string]string{
		"Authorization": mcpBasicAuth(app.AppKey, app.AppSecret),
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("默认关闭时应当 403，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "MCP 服务未开启") || strings.Contains(body, "list_tasks") {
		t.Fatalf("关闭时应当提示去设置里开启，且不能透出工具: %s", body)
	}
}

func TestMCPEndpointRejectsMissingOrInvalidCredentials(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, true)

	app := testutil.MustCreateOpenApp(t, "mcp-live", "tasks")
	disabled := testutil.MustCreateOpenApp(t, "mcp-disabled", "tasks")
	disabledToken := testutil.MustCreateAccessToken(t, "app:"+disabled.AppKey, "app:tasks")
	if err := database.DB.Model(disabled).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable app: %v", err)
	}
	refreshToken := testutil.MustCreateRefreshToken(t, "someone", "admin")

	cases := []struct {
		name          string
		authorization string
	}{
		{"no credentials", ""},
		{"garbage bearer", "Bearer not-a-jwt"},
		{"refresh token", "Bearer " + refreshToken},
		{"wrong secret", mcpBasicAuth(app.AppKey, "wrong-secret")},
		{"unknown app", mcpBasicAuth("no-such-app", app.AppSecret)},
		{"disabled app basic", mcpBasicAuth(disabled.AppKey, disabled.AppSecret)},
		{"disabled app bearer", "Bearer " + disabledToken},
	}
	for _, tc := range cases {
		headers := map[string]string{}
		if tc.authorization != "" {
			headers["Authorization"] = tc.authorization
		}
		rec := postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpToolsListBody, headers)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: 应当 401，实际 %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "list_tasks") {
			t.Errorf("%s: 鉴权失败的响应不能泄露工具清单", tc.name)
		}
	}
}

func TestMCPBasicCredentialsServeReadOnlyToolsThroughOpenAPI(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	app := testutil.MustCreateOpenApp(t, "mcp-reader", "tasks")
	task := createMCPTestTask(t, "mcp-demo-task")

	session := connectMCPSession(t, engine, mcpBasicAuth(app.AppKey, app.AppSecret))
	if info := session.InitializeResult(); info == nil || info.ServerInfo == nil || info.ServerInfo.Name != "daidai-panel" {
		t.Fatalf("serverInfo 不对: %+v", info)
	}

	names := mcpToolNames(t, session)
	if !names["list_tasks"] || names["run_task"] || names["save_script"] {
		t.Fatalf("关闭写入时只能看到只读工具，实际 %v", names)
	}

	result, text := mcpCallText(t, session, "list_tasks", map[string]any{})
	if result.IsError || !strings.Contains(text, "mcp-demo-task") {
		t.Fatalf("list_tasks 应当经开放接口拿到任务，实际 isError=%v: %s", result.IsError, text)
	}

	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_task", Arguments: map[string]any{"id": task.ID}}); err == nil {
		t.Fatal("关闭写入时直接调用 run_task 必须失败")
	}

	// 回放经过了 OpenAPIAccess：调用审计记在这个应用名下，IP 是 MCP 客户端的真实地址。
	var logs []model.ApiCallLog
	if err := database.DB.Where("app_id = ?", app.ID).Find(&logs).Error; err != nil {
		t.Fatalf("load api call logs: %v", err)
	}
	audited := false
	for _, entry := range logs {
		if entry.Endpoint == "/api/v1/tasks" && entry.Method == http.MethodGet && entry.Status == http.StatusOK {
			audited = true
			if entry.IP != "127.0.0.1" {
				t.Errorf("审计 IP 应当是 MCP 客户端地址 127.0.0.1，实际 %q", entry.IP)
			}
		}
		if strings.Contains(entry.Endpoint, "/run") {
			t.Errorf("被拦下的写工具不应触达接口，实际审计里有 %s", entry.Endpoint)
		}
	}
	if !audited {
		t.Fatalf("工具调用应当以应用身份记进开放 API 调用日志，实际 %+v", logs)
	}
}

func TestMCPToolReportsMissingAppScope(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	app := testutil.MustCreateOpenApp(t, "mcp-tasks-only", "tasks")

	session := connectMCPSession(t, engine, mcpBasicAuth(app.AppKey, app.AppSecret))
	result, text := mcpCallText(t, session, "list_envs", map[string]any{})
	if !result.IsError || !strings.Contains(text, "应用无权访问此资源") {
		t.Fatalf("应用缺 envs 权限时工具应当报错，实际 isError=%v: %s", result.IsError, text)
	}
}

func TestMCPAllowMutationsExposesWriteToolsToAdminToken(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, true)
	admin := testutil.MustCreateUser(t, "mcp-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	task := createMCPTestTask(t, "mcp-toggle-task")

	session := connectMCPSession(t, engine, "Bearer "+token)
	names := mcpToolNames(t, session)
	for _, name := range []string{"run_task", "stop_task", "set_task_enabled", "batch_task_action", "create_env", "update_env", "delete_env", "save_script", "run_script", "pull_subscription"} {
		if !names[name] {
			t.Errorf("开启写入后应当能看到 %s", name)
		}
	}

	result, text := mcpCallText(t, session, "set_task_enabled", map[string]any{"id": task.ID, "enabled": false})
	if result.IsError {
		t.Fatalf("set_task_enabled 不应失败: %s", text)
	}
	var reloaded model.Task
	if err := database.DB.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if reloaded.Status != model.TaskStatusDisabled {
		t.Fatalf("任务应当被禁用，实际 status=%v", reloaded.Status)
	}
}

// #139 新增的工具经真实路由回放：参数名与请求体必须和 handler 对得上，假 Dispatcher 验证不了这一点。
func TestMCPIssue139ToolsReachRealRoutes(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, true)
	admin := testutil.MustCreateUser(t, "mcp-139-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	session := connectMCPSession(t, engine, "Bearer "+token)

	decode := func(text string) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("工具输出不是合法 JSON: %v\n%s", err, text)
		}
		return out
	}

	// 任务：新建后按脚本改名同步命令。
	result, text := mcpCallText(t, session, "create_task", map[string]any{
		"name": "mcp-139-task", "command": "task demo/a.py", "cron_expression": "0 9 * * *", "labels": []string{"分组:巡检"},
	})
	if result.IsError {
		t.Fatalf("create_task 不应失败: %s", text)
	}
	var created model.Task
	if err := database.DB.Where("name = ?", "mcp-139-task").First(&created).Error; err != nil {
		t.Fatalf("任务应当已落库: %v", err)
	}
	if created.Labels != "分组:巡检" || created.CronExpression != "0 9 * * *" {
		t.Fatalf("新建任务的字段不对: labels=%q cron=%q", created.Labels, created.CronExpression)
	}
	result, text = mcpCallText(t, session, "update_task", map[string]any{"id": created.ID, "command": "task demo/b.py", "timeout": 30})
	if result.IsError {
		t.Fatalf("update_task 不应失败: %s", text)
	}
	var updated model.Task
	if err := database.DB.First(&updated, created.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.Command != "task demo/b.py" || updated.Timeout != 30 || updated.Name != "mcp-139-task" {
		t.Fatalf("修改应当只动传入的字段: command=%q timeout=%d name=%q", updated.Command, updated.Timeout, updated.Name)
	}

	// 脚本：改名、隔离目录被拒、删除。
	scriptsDir := config.C.Data.ScriptsDir
	for rel, content := range map[string]string{
		"demo/a.py":                          "print('a')\n",
		"demo/__pycache__/a.cpython-312.pyc": "cache",
	} {
		full := filepath.Join(scriptsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	result, text = mcpCallText(t, session, "rename_script", map[string]any{"path": "demo/a.py", "new_name": "b.py"})
	if result.IsError || decode(text)["new_path"] != "demo/b.py" {
		t.Fatalf("rename_script 应当成功并返回新路径，实际 %s", text)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "demo", "b.py")); err != nil {
		t.Fatalf("改名后的文件应当存在: %v", err)
	}
	result, text = mcpCallText(t, session, "delete_script", map[string]any{"path": "demo/__pycache__", "type": "directory"})
	if !result.IsError || !strings.Contains(text, "该路径不可访问") {
		t.Fatalf("__pycache__ 属于隔离目录，脚本接口应当拒绝，实际 isError=%v: %s", result.IsError, text)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "demo", "__pycache__")); err != nil {
		t.Fatalf("被拒绝的删除不应动到隔离目录: %v", err)
	}
	result, text = mcpCallText(t, session, "delete_script", map[string]any{"path": "demo/b.py"})
	if result.IsError {
		t.Fatalf("delete_script 不应失败: %s", text)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "demo", "b.py")); !os.IsNotExist(err) {
		t.Fatalf("文件应当已被删除，实际 err=%v", err)
	}

	// read_script 分段读取真实文件，拼回原文。
	var builder strings.Builder
	for i := 0; builder.Len() < 120*1024; i++ {
		builder.WriteString("console.log(\"呆呆面板 " + strconv.Itoa(i) + "\");\n")
	}
	original := builder.String()
	if err := os.WriteFile(filepath.Join(scriptsDir, "demo", "big.js"), []byte(original), 0o644); err != nil {
		t.Fatalf("write big.js: %v", err)
	}
	var joined strings.Builder
	offset, segments := 0, 0
	for {
		result, text = mcpCallText(t, session, "read_script", map[string]any{"path": "demo/big.js", "offset": offset})
		if result.IsError {
			t.Fatalf("read_script 不应失败: %s", text)
		}
		out := decode(text)
		chunk, _ := out["content"].(string)
		joined.WriteString(chunk)
		segments++
		if out["truncated"] != true {
			break
		}
		next, _ := out["next_offset"].(float64)
		offset = int(next)
		if segments > 10 {
			t.Fatal("分段读取没有收敛")
		}
	}
	if joined.String() != original || segments < 3 {
		t.Fatalf("分段拼接应当等于原文（%d 段），实际长度 %d / %d", segments, joined.Len(), len(original))
	}
}

func TestMCPListEnvsMasksSensitiveValuesEndToEnd(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	admin := testutil.MustCreateUser(t, "mcp-env-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	for _, env := range []model.EnvVar{
		{Name: "JD_COOKIE", Value: "pt_key=abcdef;pt_pin=zz;", Enabled: true},
		{Name: "PLAIN_SETTING", Value: "hello-world-123", Enabled: true},
	} {
		env := env
		if err := database.DB.Create(&env).Error; err != nil {
			t.Fatalf("create env: %v", err)
		}
	}

	session := connectMCPSession(t, engine, "Bearer "+token)
	result, text := mcpCallText(t, session, "list_envs", map[string]any{})
	if result.IsError {
		t.Fatalf("list_envs 不应失败: %s", text)
	}
	if strings.Contains(text, "abcdef") || !strings.Contains(text, "pt_******zz;") || !strings.Contains(text, "hello-world-123") {
		t.Fatalf("敏感变量应当遮蔽、普通变量照常显示，实际: %s", text)
	}

	// 关键词在 MCP 本地筛（经真实接口的 all=1 取全量）：敏感变量的值不参与匹配，total 也看不出来；
	// 普通变量照常按值匹配、不区分大小写。
	result, text = mcpCallText(t, session, "list_envs", map[string]any{"keyword": "abcdef"})
	if result.IsError || strings.Contains(text, "JD_COOKIE") || !strings.Contains(text, `"total":0`) {
		t.Fatalf("按敏感变量的值搜索不能命中，也不能从 total 看出来，实际: %s", text)
	}
	result, text = mcpCallText(t, session, "list_envs", map[string]any{"keyword": "HELLO"})
	if result.IsError || !strings.Contains(text, "hello-world-123") || !strings.Contains(text, `"total":1`) {
		t.Fatalf("普通变量应当能按值（不区分大小写）搜到，实际: %s", text)
	}
}

// 两个开关每次请求现读：设置页改完，下一个请求立即生效、不用重启。
// 顺带覆盖开放 API 应用令牌走 Bearer 的正常路径（其余用例的 Bearer 都是登录令牌）。
func TestMCPSwitchesTakeEffectPerRequestWithAppBearerToken(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	token := testutil.MustCreateAppToken(t, "mcp-bearer", "tasks")
	headers := map[string]string{"Authorization": "Bearer " + token}
	listTools := func() *httptest.ResponseRecorder {
		return postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpToolsListBody, headers)
	}

	setMCPSwitches(t, true, true)
	if rec := listTools(); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"run_task"`) {
		t.Fatalf("开启写入时应用令牌应当能看到写工具，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	setMCPSwitches(t, true, false)
	if rec := listTools(); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"name":"run_task"`) ||
		!strings.Contains(rec.Body.String(), `"name":"list_tasks"`) {
		t.Fatalf("关掉写入后，下一个请求就不该再看到写工具，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	setMCPSwitches(t, false, false)
	if rec := listTools(); rec.Code != http.StatusForbidden {
		t.Fatalf("关掉 MCP 后，下一个请求就应当 403，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMCPRejectsCrossSiteOrigin(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	app := testutil.MustCreateOpenApp(t, "mcp-origin", "tasks")
	auth := mcpBasicAuth(app.AppKey, app.AppSecret)

	evil := postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpInitializeBody, map[string]string{
		"Authorization": auth,
		"Origin":        "https://evil.example.com",
	})
	if evil.Code != http.StatusForbidden {
		t.Fatalf("跨站 Origin 应当被 403（防 DNS 重绑定），实际 %d body=%s", evil.Code, evil.Body.String())
	}

	// testutil 的 CORS 放行名单里有 https://allowed.example.com。
	allowed := postMCPRaw(engine, http.MethodPost, "/api/v1/mcp", mcpInitializeBody, map[string]string{
		"Authorization": auth,
		"Origin":        "https://allowed.example.com",
	})
	if allowed.Code != http.StatusOK {
		t.Fatalf("放行名单内的 Origin 应当正常处理，实际 %d body=%s", allowed.Code, allowed.Body.String())
	}
}

// Docker 镜像里 nginx 从 127.0.0.1 反代到后端并透传公网 Host。SDK 默认的 localhost 防护会把这种请求 403，
// 这里在真实的回环监听上带一个非 localhost 的 Host，确认面板没有被它误伤。
func TestMCPServesInitializeBehindReverseProxyHost(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	app := testutil.MustCreateOpenApp(t, "mcp-proxy", "tasks")

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/mcp", strings.NewReader(mcpInitializeBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "panel.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", mcpBasicAuth(app.AppKey, app.AppSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post initialize: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("经反代 Host 的 initialize 应当成功，实际 %d body=%s", resp.StatusCode, body)
	}

	var payload struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("响应应当是单个 JSON 对象（JSONResponse 模式）: %v body=%s", err, body)
	}
	if payload.Result.ServerInfo.Name != "daidai-panel" || payload.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("老版本握手应当按客户端请求的版本协商成功，实际 %+v", payload.Result)
	}
}

func TestMCPLegacyPrefixAndMethodNotAllowed(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newMCPTestEngine(t)
	setMCPSwitches(t, true, false)
	app := testutil.MustCreateOpenApp(t, "mcp-legacy", "tasks")
	headers := map[string]string{"Authorization": mcpBasicAuth(app.AppKey, app.AppSecret)}

	if rec := postMCPRaw(engine, http.MethodPost, "/api/mcp", mcpInitializeBody, headers); rec.Code != http.StatusOK {
		t.Fatalf("旧前缀 /api/mcp 也应当可用，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postMCPRaw(engine, http.MethodGet, "/api/v1/mcp", "", headers); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("无状态模式下 GET 应当 405，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}
