package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// 面板关停的接线（server/main.go 的 main 与 shutdownPanel）同样是单测直接调 service 函数看不出来的：
// 把 BaseContext 删掉、把关停步骤换个顺序、把 SetPanelExitRequester 挪到 Serve 之后，其它用例照样全绿。
// 这里与 startup_wiring_test.go 一样按 AST 读源码核对（注释里提到函数名不算数）。

// parseMainFunc 返回 ../main.go 里顶层函数 funcName 的函数体。
func parseMainFunc(t *testing.T, funcName string) *ast.BlockStmt {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "../main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse ../main.go: %v", err)
	}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == funcName {
			return fn.Body
		}
	}
	t.Fatalf("function %s not found in ../main.go", funcName)
	return nil
}

// callName 把 pkg.Fn / ident.Method / fn 形式的调用写成字符串，认不出的返回空串。
func callName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			return x.Name + "." + fun.Sel.Name
		}
	}
	return ""
}

func TestMainShutdownOrder(t *testing.T) {
	// ---- shutdownPanel：步骤顺序 ----
	shutdownCalls := collectCallNamesInFunc(t, "../main.go", "shutdownPanel")
	// 总兜底最先装；停调度杀任务 → 等结算 → 两个调度器 → 关库。关库必须是最后一步：之后任何读写都会报 database is closed。
	assertStartupCallOrder(t, "shutdownPanel", shutdownCalls,
		"time.AfterFunc",
		"service.HaltSchedulerV2",
		"service.ShutdownSchedulerV2",
		"service.ShutdownBackupScheduler",
		"service.ShutdownSubscriptionScheduler",
		"database.Close",
	)
	// HTTP 关停必须与「停调度杀任务」同时开始：写在 HaltSchedulerV2 之前、并且放在 go 协程里。
	// 串行写在后面的话，HaltSchedulerV2 卡住（例如 cron 回调拿不到数据库连接）时 SSE 也断不开。
	assertStartupCallOrder(t, "shutdownPanel", shutdownCalls, "server.Shutdown", "service.HaltSchedulerV2")
	shutdownBody := parseMainFunc(t, "shutdownPanel")
	var httpGoroutine *ast.GoStmt
	var haltPos token.Pos
	ast.Inspect(shutdownBody, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.GoStmt:
			ast.Inspect(n, func(inner ast.Node) bool {
				if call, ok := inner.(*ast.CallExpr); ok && callName(call) == "server.Shutdown" {
					httpGoroutine = n
				}
				return true
			})
		case *ast.CallExpr:
			if callName(n) == "service.HaltSchedulerV2" {
				haltPos = n.Pos()
			}
		}
		return true
	})
	if httpGoroutine == nil {
		t.Fatal("shutdownPanel must run server.Shutdown in its own goroutine so HTTP shutdown starts together with HaltSchedulerV2")
	}
	// 起完 HTTP 关停协程之后要立刻停调度杀任务，中间不能先等任何东西（例如先 <-httpDone 等 HTTP 关完）。
	ast.Inspect(shutdownBody, func(node ast.Node) bool {
		if recv, ok := node.(*ast.UnaryExpr); ok && recv.Op == token.ARROW &&
			recv.Pos() > httpGoroutine.End() && recv.Pos() < haltPos {
			t.Fatalf("shutdownPanel waits on a channel before HaltSchedulerV2; HTTP shutdown and task termination must start together")
		}
		return true
	})

	// ---- main：接线 ----
	mainCalls := collectCallNamesInFunc(t, "../main.go", "main")
	// 面板自请求退出的出口要早于任何可能触发退出的协程（自动更新检查、HTTP 服务）接上，之后再接有数据竞争。
	assertStartupCallOrder(t, "main", mainCalls, "handler.SetPanelExitRequester", "handler.StartPanelAutoUpdateWatcher")
	assertStartupCallOrder(t, "main", mainCalls, "handler.SetPanelExitRequester", "server.Serve", "shutdownPanel")
	// 关停时取消请求 ctx 靠的是 RegisterOnShutdown(cancel)：少了它 SSE 不会收流，Shutdown 要等满超时。
	assertStartupCallOrder(t, "main", mainCalls, "server.RegisterOnShutdown", "server.Serve")
	// 启动时清理上次留下的任务临时文件，必须在调度器起来之前（之后命中前缀的就可能是这一次的任务）。
	assertStartupCallOrder(t, "main", mainCalls, "service.CleanupLeakedTaskTempEntriesOnStartup", "service.InitSchedulerV2")
	// 调度器一起来就可能有开机任务在跑：停止信号必须在那之前接管，否则这段时间里收到 SIGTERM 会按默认方式直接退出，没人收尾。
	assertStartupCallOrder(t, "main", mainCalls, "signal.Notify", "service.InitSchedulerV2")

	mainBody := parseMainFunc(t, "main")

	// http.Server 必须设 BaseContext：请求 ctx 的根默认是 Background，取消不了。
	hasBaseContext := false
	ast.Inspect(mainBody, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Server" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "http" {
			return true
		}
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "BaseContext" {
					hasBaseContext = true
				}
			}
		}
		return true
	})
	if !hasBaseContext {
		t.Fatal("main must build http.Server with a cancellable BaseContext; otherwise SSE handlers never see the shutdown")
	}

	// 关停顺序只由 shutdownPanel 一处决定：main 里不能再 defer 这些收尾函数（defer 的逆序曾经让 HTTP 先关、任务后杀），
	// 也不能绕过 shutdownPanel 直接调它们。
	forbidden := map[string]bool{
		"service.HaltSchedulerV2":               true,
		"service.ShutdownSchedulerV2":           true,
		"service.ShutdownSubscriptionScheduler": true,
		"service.ShutdownBackupScheduler":       true,
		"service.StopResourceWatcher":           true,
		"service.StopLogCleanupWorker":          true,
		"handler.StopPanelAutoUpdateWatcher":    true,
		"database.Close":                        true,
	}
	for _, name := range mainCalls {
		if forbidden[name] {
			t.Fatalf("main must not call %s directly; shutdown steps belong to shutdownPanel", name)
		}
	}
	ast.Inspect(mainBody, func(node ast.Node) bool {
		deferStmt, ok := node.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if name := callName(deferStmt.Call); forbidden[name] || name == "cleanupPIDFile" {
			t.Fatalf("main must not defer %s; shutdownPanel decides the shutdown order", name)
		}
		return true
	})
}
