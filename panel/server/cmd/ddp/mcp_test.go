package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseMCPArgsPrefersFlagsOverEnv(t *testing.T) {
	env := map[string]string{
		"DDP_MCP_URL":        "http://10.0.0.2:5701",
		"DDP_MCP_APP_KEY":    "env-key",
		"DDP_MCP_APP_SECRET": "env-secret",
	}
	opts, err := parseMCPArgs([]string{"--app-key", "flag-key", "--url=http://127.0.0.1:9999"}, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.url != "http://127.0.0.1:9999" || opts.appKey != "flag-key" || opts.appSecret != "env-secret" {
		t.Fatalf("命令行参数应当覆盖环境变量、没给的沿用环境变量，实际 %+v", opts)
	}

	opts, err = parseMCPArgs([]string{"--help"}, func(string) string { return "" })
	if err != nil || !opts.help {
		t.Fatalf("--help 应当被识别，实际 %+v err=%v", opts, err)
	}
}

func TestParseMCPArgsRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"--nope"},
		{"--app-key"},
		{"--url", "ftp://127.0.0.1"},
		{"--url", "127.0.0.1:5701"},
	} {
		if _, err := parseMCPArgs(args, func(string) string { return "" }); err == nil {
			t.Errorf("参数 %v 应当报错", args)
		}
	}
}

func clearMCPEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DDP_MCP_URL", "DDP_MCP_APP_KEY", "DDP_MCP_APP_SECRET"} {
		t.Setenv(key, "")
	}
}

func TestRunMCPRefusesWhenDisabled(t *testing.T) {
	testutil.SetupTestEnv(t)
	clearMCPEnv(t)

	err := runMCP(&cliRuntime{cfg: config.C}, []string{"--app-key", "k", "--app-secret", "s"})
	if err == nil || !strings.Contains(err.Error(), "MCP 服务未开启") {
		t.Fatalf("面板没开 MCP 时 ddp mcp 应当直接报错退出，实际 %v", err)
	}
}

func TestRunMCPRequiresAppCredentials(t *testing.T) {
	testutil.SetupTestEnv(t)
	clearMCPEnv(t)
	if err := model.SetConfig(model.MCPEnabledConfigKey, "true"); err != nil {
		t.Fatalf("enable mcp: %v", err)
	}

	err := runMCP(&cliRuntime{cfg: config.C}, nil)
	if err == nil || !strings.Contains(err.Error(), "缺少应用凭据") {
		t.Fatalf("没给应用凭据时应当提示去建应用，实际 %v", err)
	}
}

type recordingDispatcher struct {
	calls []string
}

func (r *recordingDispatcher) Do(_ context.Context, method, path string, _ url.Values, _ any) (int, []byte, error) {
	r.calls = append(r.calls, method+" "+path)
	return http.StatusOK, []byte(`{}`), nil
}

// ddp mcp 的工具清单在启动时定下；面板上的两个开关要靠 mcpSwitchGuard 在每次调用前重读，
// 才能对已经连着的进程立即生效：关掉 MCP 或关掉写入后再调，拿到的是错误，而不是继续写。
func TestMCPSwitchGuardRechecksSwitchesBeforeEveryCall(t *testing.T) {
	testutil.SetupTestEnv(t)
	setSwitches := func(enabled, allowMutations bool) {
		t.Helper()
		if err := model.SetConfig(model.MCPEnabledConfigKey, strconv.FormatBool(enabled)); err != nil {
			t.Fatalf("set mcp_enabled: %v", err)
		}
		if err := model.SetConfig(model.MCPAllowMutationsConfigKey, strconv.FormatBool(allowMutations)); err != nil {
			t.Fatalf("set mcp_allow_mutations: %v", err)
		}
	}
	inner := &recordingDispatcher{}
	guard := &mcpSwitchGuard{inner: inner}
	ctx := context.Background()

	setSwitches(true, true)
	if _, _, err := guard.Do(ctx, http.MethodPost, "/scripts/run", nil, map[string]any{"path": "a.py"}); err != nil {
		t.Fatalf("两个开关都开时写入应当放行: %v", err)
	}

	setSwitches(true, false)
	if _, _, err := guard.Do(ctx, http.MethodPut, "/tasks/1/run", nil, nil); err == nil || !strings.Contains(err.Error(), "写入与执行类工具已在面板关闭") {
		t.Fatalf("关掉写入后，已在跑的进程再调写入应当报错，实际 %v", err)
	}
	if _, _, err := guard.Do(ctx, http.MethodGet, "/tasks", nil, nil); err != nil {
		t.Fatalf("关掉写入不影响查询: %v", err)
	}

	setSwitches(false, false)
	if _, _, err := guard.Do(ctx, http.MethodGet, "/tasks", nil, nil); err == nil || !strings.Contains(err.Error(), "MCP 服务已在面板关闭") {
		t.Fatalf("关掉 MCP 后查询也应当报错，实际 %v", err)
	}

	if want := []string{"POST /scripts/run", "GET /tasks"}; !reflect.DeepEqual(inner.calls, want) {
		t.Fatalf("被开关拦下的调用不应到达面板，实际 %v", inner.calls)
	}
}

const mcpStdioHelperEnv = "DDP_MCP_STDIO_HELPER"

// TestMCPStdioHelperProcess 不是真正的用例：它只在 TestMCPStdioEndToEnd 拉起的子进程里运行，
// 在真实的 stdin / stdout 上跑 ddp mcp。直接执行 go test 时它会跳过。
func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv(mcpStdioHelperEnv) != "1" {
		t.Skip("只作为 TestMCPStdioEndToEnd 拉起的子进程运行")
	}

	// 测试环境初始化会建库、迁移、写默认配置，而 GORM 的日志器在 database.Init 时绑定 os.Stdout。
	// 这段先把 os.Stdout 指到 stderr，免得机器慢时一行「SLOW SQL」混进协议流；进 runMCP 之前再换回来。
	protocolOut := os.Stdout
	os.Stdout = os.Stderr
	root := testutil.SetupTestEnv(t)
	os.Stdout = protocolOut

	code := 0
	if err := model.SetConfig(model.MCPEnabledConfigKey, "true"); err != nil {
		fmt.Fprintln(os.Stderr, "enable mcp:", err)
		code = 2
	} else if err := runMCP(&cliRuntime{cfg: config.C}, nil); err != nil {
		// 凭据与面板地址都走环境变量（DDP_MCP_*），顺带覆盖这条读取路径。
		fmt.Fprintln(os.Stderr, "runMCP:", err)
		code = 1
	}

	// os.Exit 之后 t.Cleanup 不会执行，这里手动关库、删临时目录。
	if sqlDB, err := database.DB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	_ = os.RemoveAll(filepath.Dir(root))
	os.Exit(code)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestMCPStdioEndToEnd 真正拉起一个 ddp mcp 子进程，经它的 stdin / stdout 走完
// initialize → tools/list → 一次只读调用。stdout 上但凡混进一行非协议输出，这里的客户端就会解析失败。
func TestMCPStdioEndToEnd(t *testing.T) {
	var exchanges atomic.Int32
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/open-api/token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["app_key"] != "stdio-app" || body["app_secret"] != "stdio-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"凭证无效"}`))
				return
			}
			exchanges.Add(1)
			_, _ = w.Write([]byte(`{"data":{"access_token":"stdio-token","token_type":"Bearer","expires_in":86400}}`))
		case "/api/v1/tasks":
			if r.Header.Get("Authorization") != "Bearer stdio-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"令牌无效或已过期"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"stdio-demo-task","status":1,"labels":[]}],"total":1,"page":1,"page_size":20}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"route not found"}`))
		}
	}))
	defer panel.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPStdioHelperProcess$")
	cmd.Env = append(os.Environ(),
		mcpStdioHelperEnv+"=1",
		"DDP_MCP_URL="+panel.URL,
		"DDP_MCP_APP_KEY=stdio-app",
		"DDP_MCP_APP_SECRET=stdio-secret",
	)
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "ddp-stdio-e2e", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("连接 ddp mcp 子进程失败: %v\nstderr:\n%s", err, stderr.String())
	}

	if info := session.InitializeResult(); info == nil || info.ServerInfo == nil || info.ServerInfo.Name != "daidai-panel" {
		t.Fatalf("serverInfo 不对: %+v\nstderr:\n%s", info, stderr.String())
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list 失败: %v\nstderr:\n%s", err, stderr.String())
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if !names["list_tasks"] || names["run_task"] {
		t.Fatalf("默认只开放只读工具，实际 %v", names)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_tasks", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("tools/call 失败: %v\nstderr:\n%s", err, stderr.String())
	}
	var text string
	for _, content := range result.Content {
		if item, ok := content.(*mcp.TextContent); ok {
			text += item.Text
		}
	}
	if result.IsError || !strings.Contains(text, "stdio-demo-task") {
		t.Fatalf("list_tasks 应当经假面板拿到任务，实际 isError=%v: %s\nstderr:\n%s", result.IsError, text, stderr.String())
	}

	// 关闭会话 = 关掉子进程的 stdin，子进程应当自己正常退出（退出码 0）。
	if err := session.Close(); err != nil {
		t.Fatalf("关闭会话失败（子进程应在 stdin 关闭后正常退出）: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "[ddp mcp] 已就绪") {
		t.Fatalf("就绪提示应当写在 stderr，实际 stderr:\n%s", stderr.String())
	}
	if exchanges.Load() != 1 {
		t.Fatalf("应当用应用凭据换一次令牌，实际 %d 次", exchanges.Load())
	}
}
