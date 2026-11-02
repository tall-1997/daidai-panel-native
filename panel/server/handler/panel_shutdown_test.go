package handler

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 这一组锁的是面板关停（server/main.go 的 shutdownPanel）里落在 handler 包的几条契约。

// newShutdownWiredServer 起一个与 main 同样接线的 HTTP 服务：BaseContext 返回可取消的根 ctx，
// RegisterOnShutdown 注册取消。http.Server.Shutdown 本身不取消请求 ctx，只有这样接线 SSE 才会在关停时立刻收流。
func newShutdownWiredServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	requestsCtx, cancelRequests := context.WithCancel(context.Background())
	server := httptest.NewUnstartedServer(handler)
	server.Config.BaseContext = func(net.Listener) context.Context { return requestsCtx }
	server.Config.RegisterOnShutdown(cancelRequests)
	server.Start()
	t.Cleanup(func() {
		cancelRequests()
		server.Close()
	})
	return server
}

func newShutdownStreamEngine() *gin.Engine {
	engine := gin.New()
	// 实时日志流的 TinyLog 分支要从 ctx 里取 claims（心跳复查会话撤销用）；鉴权不是这组用例要测的，直接塞一份。
	withClaims := func(c *gin.Context) {
		c.Set("claims", &middleware.Claims{Username: "shutdown-sse-tester", Role: "admin", TokenType: "access"})
		c.Next()
	}
	engine.GET("/logs/:id/stream", withClaims, NewLogHandler().Stream)
	engine.GET("/deps/:id/log-stream", NewDepsHandler().LogStream)
	engine.GET("/subscriptions/:id/pull-stream", NewSubscriptionHandler().PullStream)
	return engine
}

func mustCreateShutdownStreamTask(t *testing.T, name string, status float64) *model.Task {
	t.Helper()
	task := &model.Task{Name: name, Command: "echo " + name, CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	if err := database.DB.Model(task).Update("status", status).Error; err != nil {
		t.Fatalf("set status for %q: %v", name, err)
	}
	return task
}

// 面板关停时三条 SSE（实时日志、依赖安装输出、订阅拉取输出）必须先发结束事件再收流。
// 取消 BaseContext 后 handler 走的是 ctx.Done 分支、干净地结束响应（chunked 结束块，curl 退出码 0）；
// 网页的日志弹窗、日志管理、依赖页只认 done 事件与 onError，干净 EOF 一个回调都不触发，会静默卡在「运行中」。
// 原来被 SIGKILL 时连接是异常断开、会走 onError，所以不发 done 是新设计才会引入的回退。
//
// 突变验证：删掉任一 handler 里 ctx.Done 分支的那句结束事件，对应那行变红；
// 删掉实时日志轮询分支（c.Stream）开头的 ctx 判断，「轮询分支」那行要等满 60 秒，Shutdown 超时、被 Close 断开，同样变红。
func TestShutdownEndsSSEStreamsWithDoneEvent(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T) string // 准备好状态，返回流地址
		ready    func(line string) bool    // 读到这一行说明 handler 已经进入要测的分支
		wantData string
	}{
		{
			name: "实时日志：任务在跑、有 TinyLog",
			setup: func(t *testing.T) string {
				task := mustCreateShutdownStreamTask(t, "shutdown-sse-live", model.TaskStatusRunning)
				logID := fmt.Sprintf("%d_shutdown-sse", task.ID)
				tl, err := service.GetTinyLogManager().Create(logID)
				if err != nil {
					t.Fatalf("create tiny log: %v", err)
				}
				// TinyLogManager 是进程级单例，用完必须摘掉，免得留给同包的其它用例。
				t.Cleanup(func() {
					tl.Close()
					service.GetTinyLogManager().Remove(logID)
				})
				if _, err := tl.Write([]byte("tick-before-shutdown\n")); err != nil {
					t.Fatalf("write tiny log: %v", err)
				}
				return fmt.Sprintf("/logs/%d/stream", task.ID)
			},
			ready:    func(line string) bool { return line == "data: tick-before-shutdown" },
			wantData: "data: reconnect",
		},
		{
			name: "实时日志：排队中、等 TinyLog 出现的短轮询",
			setup: func(t *testing.T) string {
				task := mustCreateShutdownStreamTask(t, "shutdown-sse-queued", model.TaskStatusQueued)
				return fmt.Sprintf("/logs/%d/stream", task.ID)
			},
			ready:    func(line string) bool { return line == ": open" },
			wantData: "data: reconnect",
		},
		{
			name: "实时日志：运行中却没有 TinyLog 的轮询分支（conc / 不输出实时日志）",
			setup: func(t *testing.T) string {
				task := mustCreateShutdownStreamTask(t, "shutdown-sse-polling", model.TaskStatusRunning)
				return fmt.Sprintf("/logs/%d/stream", task.ID)
			},
			ready:    func(line string) bool { return line == ": open" },
			wantData: "data: reconnect",
		},
		{
			name: "依赖安装输出",
			setup: func(t *testing.T) string {
				dep := &model.Dependency{Type: model.DepTypePython, Name: "shutdown-sse-dep", Status: model.DepStatusInstalling, Log: "deps-history-line"}
				if err := database.DB.Create(dep).Error; err != nil {
					t.Fatalf("create dependency: %v", err)
				}
				getOrCreateBroadcaster(dep.ID)
				t.Cleanup(func() { removeBroadcaster(dep.ID) })
				return fmt.Sprintf("/deps/%d/log-stream", dep.ID)
			},
			ready: func(line string) bool { return line == "data: deps-history-line" },
			// 依赖、订阅两条也发 reconnect，不能发 timeout：APP 的依赖页、订阅页（v1.0.2 起）把 reconnect 以外的 done
			// 都当成完成，会显示绿色「安装完成 / 拉取完成」。
			wantData: "data: reconnect",
		},
		{
			name: "订阅拉取输出",
			setup: func(t *testing.T) string {
				const subID = 987801
				broadcaster := getOrCreateSubBroadcaster(subID)
				broadcaster.broadcast("sub-history-line")
				t.Cleanup(func() { removeSubBroadcaster(subID) })
				return fmt.Sprintf("/subscriptions/%d/pull-stream", subID)
			},
			ready:    func(line string) bool { return line == "data: sub-history-line" },
			wantData: "data: reconnect",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			path := tc.setup(t)
			server := newShutdownWiredServer(t, newShutdownStreamEngine())

			resp, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			// Cleanup 后进先出：先于 server.Close 断开客户端，Close 不会被没收尾的连接卡住。
			t.Cleanup(func() { resp.Body.Close() })
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("open stream: got %d", resp.StatusCode)
			}
			lines := rvReadSSE(resp.Body)
			if found, ended := rvNextMatch(lines, 5*time.Second, tc.ready); !found {
				t.Fatalf("stream never reached the branch under test (ended=%v)", ended)
			}

			shutdownStarted := time.Now()
			shutdownDone := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				shutdownDone <- server.Config.Shutdown(ctx)
			}()

			// 收集关停之后的每一行，直到服务端收流（EOF）。
			var tail []string
			deadline := time.After(2 * time.Second)
		collect:
			for {
				select {
				case line, ok := <-lines:
					if !ok {
						break collect
					}
					if line != "" {
						tail = append(tail, line)
					}
				case <-deadline:
					t.Fatalf("stream did not end within 2s after Shutdown, got lines %q", tail)
				}
			}
			if elapsed := time.Since(shutdownStarted); elapsed > time.Second {
				t.Errorf("stream should end right after Shutdown cancels the base context, took %s", elapsed)
			}
			if len(tail) < 2 || tail[len(tail)-2] != "event: done" || tail[len(tail)-1] != tc.wantData {
				t.Fatalf("stream must end with \"event: done\" + %q so the web UI leaves the running state, got tail %q", tc.wantData, tail)
			}

			select {
			case err := <-shutdownDone:
				if err != nil {
					t.Fatalf("Shutdown should finish cleanly once SSE handlers return, got %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Shutdown did not return")
			}
			if elapsed := time.Since(shutdownStarted); elapsed > 2*time.Second {
				t.Errorf("Shutdown should return quickly once SSE handlers return, took %s", elapsed)
			}
		})
	}
}

// 「重启面板」「停止面板服务」接上 SetPanelExitRequester 之后，退出码交给主程序（main 走完整关停再退出），
// 不再直接 os.Exit。退出码保持原样：重启 = 1（Docker 重启循环 / systemd on-failure / Magisk 守护按它拉起），停止 = 0。
func TestRestartAndStopRouteThroughPanelExitRequester(t *testing.T) {
	engine, token, _, _ := newSystemStopPanelTestEnv(t, true, "2")

	requested := make(chan int, 4)
	SetPanelExitRequester(func(code int) { requested <- code })
	// 传 nil 不能把已经接好的出口换掉（否则重启又会退回直接 os.Exit）。
	SetPanelExitRequester(nil)

	if rec := postSystemAction(t, engine, token, "/api/v1/system/restart"); rec.Code != http.StatusOK {
		t.Fatalf("restart should return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if code := awaitProcessExitCode(t, requested); code != 1 {
		t.Fatalf("restart must request exit code 1, got %d", code)
	}

	if rec := postSystemAction(t, engine, token, "/api/v1/system/stop"); rec.Code != http.StatusOK {
		t.Fatalf("stop should return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if code := awaitProcessExitCode(t, requested); code != 0 {
		t.Fatalf("stop must request exit code 0, got %d", code)
	}
}

// 二进制 / Magisk 在线升级里面板自己退出让位的那两处，必须走 panelProcessExit（面板进程里会被接到 main 的关停），
// 不能写回 os.Exit：那样运行中的任务、软链、数据库都没人收尾，不输出的任务还会变成孤儿。
// 升级流程本身要下载、解压、起 helper，单测跑不到那一步，这里按 AST 读源码核对（注释里提到 os.Exit 不算）。
func TestPanelSelfUpdateExitsThroughPanelExitRequester(t *testing.T) {
	for _, file := range []string{"system_update_binary.go", "system_update_magisk.go"} {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		usesRequester := false
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "os" && fun.Sel.Name == "Exit" {
					t.Errorf("%s:%d calls os.Exit directly; panel self-exit must go through panelProcessExit", file, fset.Position(call.Pos()).Line)
				}
			case *ast.Ident:
				if fun.Name == "panelProcessExit" {
					usesRequester = true
				}
			}
			return true
		})
		if !usesRequester {
			t.Errorf("%s no longer calls panelProcessExit; the panel would not exit after scheduling the update helper", file)
		}
	}
}

// 旧 docker.sock 一键更新的辅助脚本：docker rm -f 等于直接 SIGKILL，面板来不及收尾。
// 先 docker stop -t 10（面板收到 SIGTERM，8 秒内自行收完尾退出），stop 失败也放行，rm -f 照样兜底。
func TestPanelUpdateHelperStopsContainerBeforeRemoving(t *testing.T) {
	plan := &panelUpdatePlan{
		ContainerName: "daidai-panel",
		RunArgs:       []string{"run", "-d", "--name", "daidai-panel", "linzixuanzz/daidai-panel:latest"},
	}
	script := buildPanelUpdateHelperScript(plan)

	stopIdx := strings.Index(script, "docker stop -t 10 'daidai-panel' >/dev/null 2>&1 || true")
	rmIdx := strings.Index(script, "docker rm -f 'daidai-panel' >/dev/null 2>&1 || true")
	runIdx := strings.Index(script, "docker 'run'")
	if stopIdx < 0 {
		t.Fatalf("helper script must stop the container gracefully before removing it, got:\n%s", script)
	}
	if rmIdx < 0 || stopIdx > rmIdx {
		t.Fatalf("docker stop must come before docker rm -f, got:\n%s", script)
	}
	if runIdx < 0 || rmIdx > runIdx {
		t.Fatalf("the new container must still be started after the old one is removed, got:\n%s", script)
	}
}
