package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// 这几个启动钩子是否被接上、先后顺序对不对，单测直接调 service 函数是看不出来的：
// 把 appboot.go / main.go 里的调用删掉或调换顺序，其它用例照样全绿。这里按 AST 读源码核对
// （不做子串匹配，注释里提到函数名不算数）。

// collectCallNamesInFunc 按源码顺序列出 file 中顶层函数 funcName 里的所有调用，形如 service.Foo、verifyInstalledDeps。
func collectCallNamesInFunc(t *testing.T, file, funcName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var body *ast.BlockStmt
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == funcName {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatalf("function %s not found in %s", funcName, file)
	}

	type call struct {
		pos  token.Pos
		name string
	}
	var calls []call
	ast.Inspect(body, func(node ast.Node) bool {
		expr, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := expr.Fun.(type) {
		case *ast.Ident:
			calls = append(calls, call{pos: expr.Pos(), name: fun.Name})
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok {
				calls = append(calls, call{pos: expr.Pos(), name: pkg.Name + "." + fun.Sel.Name})
			}
		}
		return true
	})
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].pos < calls[j].pos })
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.name)
	}
	return names
}

// assertStartupCallOrder 要求 want 里每个调用恰好出现一次，并且按给定顺序排列。
func assertStartupCallOrder(t *testing.T, where string, calls []string, want ...string) {
	t.Helper()
	previous := -1
	for _, name := range want {
		index, count := -1, 0
		for i, got := range calls {
			if got == name {
				count++
				index = i
			}
		}
		if count != 1 {
			t.Fatalf("%s: expected %s to be called exactly once, found %d times (calls: %v)", where, name, count, calls)
		}
		if index <= previous {
			t.Fatalf("%s: expected call order %v, but %s comes too early (calls: %v)", where, want, name, calls)
		}
		previous = index
	}
}

// appboot：模块版 Python 迁移必须接在 single 策略之后、合并重复依赖之前。
// 删掉它，R4 的迁移就在生产里静默失效；挪到合并之后，迁移造成的同名重复要到下次开机才合并，
// 这一次开机的启动校验会看到同一个包两行记录。
func TestAppbootWiresMagiskPythonMigrationBeforeDuplicateMerge(t *testing.T) {
	// 本机版（Android）：appboot 把初始化体拆到 initWithConfig（InitWithConfig 只是薄包装），
	// 接线检查跟着指向真正执行启动步骤的函数。
	calls := collectCallNamesInFunc(t, "../appboot/appboot.go", "initWithConfig")
	// 本机版（Android）：single 策略与重复依赖合并走可注入的包级函数变量（测试要模拟失败），
	// 因此这里按真实调用名断言顺序，并额外核对变量初始化确实指向对应 service 函数。
	assertStartupCallOrder(t, "appboot.InitWithConfig", calls,
		"applyPythonRuntimePolicy",
		"service.ApplyMagiskPythonRuntimeMigrationOnStartup",
		"mergePythonDependencies",
	)
	source, err := os.ReadFile("../appboot/appboot.go")
	if err != nil {
		t.Fatalf("read appboot source: %v", err)
	}
	for _, want := range []string{
		"applyPythonRuntimePolicy     = service.ApplySinglePythonRuntimePolicyOnStartup",
		"mergePythonDependencies      = service.MergeDuplicatePythonDependencies",
	} {
		if !strings.Contains(string(source), want) {
			t.Errorf("appboot 启动变量未指向预期 service 函数：%q", want)
		}
	}
}

// main：Playwright 浏览器目录（#142）要在 config / 数据库就绪之后、启动校验排队重装依赖之前写进进程环境。
// 删掉它，系统命令行与依赖安装子进程会把浏览器下到容器可写层，重建即丢；挪到启动校验之后，
// 第一批重装的 pip 子进程就拿不到这个目录。
func TestMainWiresPlaywrightBrowsersPathBeforeDependencyVerification(t *testing.T) {
	calls := collectCallNamesInFunc(t, "../main.go", "main")
	assertStartupCallOrder(t, "main", calls,
		"appboot.InitWithConfig",
		"service.ApplyPlaywrightBrowsersPathProcessEnv",
		"verifyInstalledDeps",
	)
}

// main：Node ABI 重建检查接在启动校验之后。删掉它，换 Node 大版本后加载失败的原生扩展就不会自动修复。
func TestMainWiresNodeABIRebuildAfterDependencyVerification(t *testing.T) {
	calls := collectCallNamesInFunc(t, "../main.go", "main")
	assertStartupCallOrder(t, "main", calls,
		"appboot.InitWithConfig",
		"verifyInstalledDeps",
		"service.RebuildNodeDependenciesIfABIChanged",
	)
}

// main：Python 跨 C 库修复（#150）接在 Node ABI 重建之后。删掉它，从 Alpine 换到 Debian 镜像后，
// 数据卷里按 musl 编译的原生扩展会一直加载不了，面板上的重装按钮也修不好（pip 对已安装的包直接跳过）。
func TestMainWiresPythonLibcRepairAfterNodeABIRebuild(t *testing.T) {
	calls := collectCallNamesInFunc(t, "../main.go", "main")
	assertStartupCallOrder(t, "main", calls,
		"appboot.InitWithConfig",
		"verifyInstalledDeps",
		"service.RebuildNodeDependenciesIfABIChanged",
		"service.RepairPythonPackagesForLibcChange",
	)
}

// main：两遍整棵脚本目录的遍历（隔离污染目录并清悬空软链、清通知脚本副本）放进同一个 go 语句在后台跑，不挡监听（#158），
// 软链那遍在前；根目录两份通知辅助脚本照旧同步准备（开机任务一跑就要用）；都排在调度器起来之前。
// 把任意一遍挪回同步路径都不行：慢盘上时间几乎全花在第一次读目录上，留在同步路径上的那遍会变成冷缓存上的第一遍，照样几秒。
func TestMainRunsScriptTreeWalksInBackground(t *testing.T) {
	calls := collectCallNamesInFunc(t, "../main.go", "main")
	assertStartupCallOrder(t, "main", calls,
		"service.EnsureBuiltinNotifyHelpers",
		"service.QuarantineUnexpectedScriptEntriesOnStartup",
		"service.CleanupManagedHelperCopiesUnderRoot",
		"service.InitSchedulerV2",
	)

	// 再单独看 main 里每个 go 语句内部的调用：两遍遍历要在同一个 go 语句里，根目录 helper 不能在任何 go 语句里。
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "../main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	inGo := map[string]bool{}
	walksInSameGo := false
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "main" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			goStmt, ok := node.(*ast.GoStmt)
			if !ok {
				return true
			}
			names := map[string]bool{}
			ast.Inspect(goStmt.Call, func(inner ast.Node) bool {
				if call, ok := inner.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok {
							names[pkg.Name+"."+sel.Sel.Name] = true
							inGo[pkg.Name+"."+sel.Sel.Name] = true
						}
					}
				}
				return true
			})
			if names["service.QuarantineUnexpectedScriptEntriesOnStartup"] && names["service.CleanupManagedHelperCopiesUnderRoot"] {
				walksInSameGo = true
			}
			return false
		})
	}
	if !walksInSameGo {
		t.Fatalf("两遍整树遍历要放进同一个 go 语句（同一个后台协程，软链那遍先跑），实际 go 语句里的调用：%v", inGo)
	}
	if inGo["service.EnsureBuiltinNotifyHelpers"] {
		t.Fatal("根目录两份通知辅助脚本只读写两个文件，要同步准备好：开机任务一跑就要用")
	}
}

// ddp：每条命令都要走 bootstrap，不能再整棵遍历脚本目录（#158，慢盘上每条命令都要多等几秒）。
// 会跑脚本的 ddp python、ddp task run 运行前各自清工作目录里的副本，整棵清理只留给面板启动时的后台协程。
func TestDdpBootstrapDoesNotWalkScriptTree(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "../cmd/ddp/runtime.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse cmd/ddp/runtime.go: %v", err)
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "service" {
				switch sel.Sel.Name {
				case "CleanupManagedHelperCopiesUnderRoot", "QuarantineUnexpectedScriptEntriesOnStartup":
					t.Fatalf("ddp 不应再整棵遍历脚本目录：%s 调用了 service.%s", fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
		}
		return true
	})
}
