package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/mcptools"
	"daidai-panel/model"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	gormlogger "gorm.io/gorm/logger"
)

// mcpOptions 是 ddp mcp 的参数。命令行参数优先，其次是环境变量（方便写进 MCP 客户端配置的 env 段）。
type mcpOptions struct {
	url       string
	appKey    string
	appSecret string
	help      bool
}

func parseMCPArgs(args []string, getenv func(string) string) (mcpOptions, error) {
	opts := mcpOptions{
		url:       strings.TrimSpace(getenv("DDP_MCP_URL")),
		appKey:    strings.TrimSpace(getenv("DDP_MCP_APP_KEY")),
		appSecret: strings.TrimSpace(getenv("DDP_MCP_APP_SECRET")),
	}

	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "-h", "--help", "help":
			opts.help = true
			continue
		case "--url", "--app-key", "--app-secret":
		default:
			return opts, fmt.Errorf("未知参数: %s（用法见 ddp mcp --help）", args[i])
		}
		if !hasValue {
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 需要参数", name)
			}
			value = args[i+1]
			i++
		}
		value = strings.TrimSpace(value)
		switch name {
		case "--url":
			opts.url = value
		case "--app-key":
			opts.appKey = value
		case "--app-secret":
			opts.appSecret = value
		}
	}

	if opts.url != "" {
		parsed, err := url.Parse(opts.url)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return opts, fmt.Errorf("面板地址无效: %s（示例: http://127.0.0.1:5701）", opts.url)
		}
	}
	return opts, nil
}

// runMCP 以 stdio 方式运行 MCP 服务（issue #128），给 Claude Desktop 这类「拉起子进程」的客户端用。
//
// 工具经 HTTP 调本机正在运行的面板（mcptools.RemoteDispatcher），用开放 API 应用凭据鉴权；
// 这个进程只读数据库里的两个开关，绝不直连数据库写 —— 否则会绕过 scope、审计，还会和面板进程争写锁。
func runMCP(rt *cliRuntime, args []string) error {
	opts, err := parseMCPArgs(args, os.Getenv)
	if err != nil {
		return err
	}
	if opts.help {
		printMCPHelp(os.Stdout)
		return nil
	}

	// stdio 模式下 stdout 只能出现 MCP 协议消息，混进一行别的东西客户端就会解析失败，而且极难排查。
	// 先把真正的 stdout 留给传输层，再把 os.Stdout 指到 stderr：之后 bootstrap 里新建的
	// GORM 日志器（database.Init 用的是 log.New(os.Stdout, ...)）和任何误写的 fmt.Print 都会落到 stderr。
	protocolOut := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = protocolOut }()
	log.SetOutput(os.Stderr)

	if err := rt.bootstrap(); err != nil {
		return err
	}
	// 数据库若在切换之前就初始化过（测试里就是这样），它的日志器还指着真正的 stdout，这里再兜一次。
	redirectDatabaseLoggerToStderr()

	if !model.GetRegisteredConfigBool(model.MCPEnabledConfigKey) {
		return errors.New("MCP 服务未开启：请先在面板「系统设置 → MCP 服务」中启用")
	}
	if opts.appKey == "" || opts.appSecret == "" {
		return errors.New("缺少应用凭据：请先在面板「Open API」创建应用，再用 --app-key / --app-secret（或环境变量 DDP_MCP_APP_KEY / DDP_MCP_APP_SECRET）传入")
	}

	baseURL := opts.url
	if baseURL == "" {
		baseURL = rt.backendBaseURL()
	}
	allowMutations := model.GetRegisteredConfigBool(model.MCPAllowMutationsConfigKey)

	mcptools.ServerVersion = handler.Version
	dispatcher := &mcpSwitchGuard{inner: mcptools.NewRemoteDispatcher(baseURL, opts.appKey, opts.appSecret, nil)}
	server := mcptools.BuildServer(dispatcher, allowMutations)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "[ddp mcp] 已就绪（stdio）：面板 %s，写入与执行工具%s\n", baseURL, boolLabel(allowMutations, "已开启", "未开启"))
	err = server.Run(ctx, &mcp.IOTransport{Reader: os.Stdin, Writer: nopWriteCloser{protocolOut}})
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		// 客户端关掉 stdin（正常退出）或收到终止信号，都是正常结束。
		return nil
	}
	return err
}

// mcpSwitchGuard 让面板上的两个开关对已经在跑的 ddp mcp 也立即生效。
// 工具清单在启动时按 mcp_allow_mutations 定下来，但每次调用前都重读数据库里的开关（只读）：
// 管理员关掉 MCP 或关掉写入后，已连着的 AI 客户端再调就会拿到明确的错误，而不是继续写。
type mcpSwitchGuard struct {
	inner mcptools.Dispatcher
}

func (g *mcpSwitchGuard) Do(ctx context.Context, method, path string, query url.Values, body any) (int, []byte, error) {
	if !model.GetRegisteredConfigBool(model.MCPEnabledConfigKey) {
		return 0, nil, errors.New("MCP 服务已在面板关闭（系统设置 → MCP 服务）")
	}
	if method != http.MethodGet && !model.GetRegisteredConfigBool(model.MCPAllowMutationsConfigKey) {
		return 0, nil, errors.New("写入与执行类工具已在面板关闭（系统设置 → MCP 服务）；重新启动 ddp mcp 可刷新工具列表")
	}
	return g.inner.Do(ctx, method, path, query, body)
}

func redirectDatabaseLoggerToStderr() {
	if database.DB == nil {
		return
	}
	database.DB.Logger = gormlogger.New(log.New(os.Stderr, "\r\n", log.LstdFlags), gormlogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
}

// nopWriteCloser 让传输层关闭连接时不去关真正的 stdout。
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func printMCPHelp(w io.Writer) {
	fmt.Fprint(w, `ddp mcp - 以 stdio 方式运行呆呆面板的 MCP 服务，供 Claude Desktop 等 AI 客户端调用

用法:
  ddp mcp --app-key <app_key> --app-secret <app_secret> [--url http://127.0.0.1:5701]

参数（也可以用环境变量）:
  --url         面板后端地址，默认 http://127.0.0.1:<面板后端端口>    DDP_MCP_URL
  --app-key     Open API 应用的 app_key                               DDP_MCP_APP_KEY
  --app-secret  Open API 应用的 app_secret                            DDP_MCP_APP_SECRET

说明:
  1. 先在面板「系统设置 → MCP 服务」开启 MCP；写入与执行类工具还要再开「允许写入与执行工具」。
  2. 能访问哪些模块由应用的权限范围决定。工具都经面板开放接口执行，所以面板必须在运行。
  3. stdout 只输出 MCP 协议消息，提示与错误一律写到 stderr。
  4. Docker 部署：docker exec -i <容器名> ddp mcp --app-key <app_key> --app-secret <app_secret>
  5. 手机（Magisk 模块版）上客户端进不去容器，请改用 HTTP 方式：POST http://<面板地址>/api/v1/mcp
`)
}
