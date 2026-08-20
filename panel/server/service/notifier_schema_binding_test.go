package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"daidai-panel/model"
)

// notifierSourceFile 是被扫描的权威源码。
// 所有 sendXxx 函数都在这一个文件里；如果哪天拆文件了，这里要跟着补路径，
// 否则被拆出去的那部分键会静默逃过绑定检查。
// TestNotifierSourceHoldsEverySenderCalledBySendToChannel 会兜底拦住这种拆分。
const notifierSourceFile = "notifier.go"

// ---- 白名单 ----
//
// 白名单只有一个合法用途：兜住「服务端确实读了这个键，但不是通过 cfg["字面量"] 读的」
// 这类扫描器天然看不见的情况。任何其它理由都不该往这里加键 —— 加进来就等于把
// 这条用例的防线开了个洞。

// notifySchemaOnlyKeys：注册表声明了、但 AST 扫不到的键。
//
// smtp_ssl 是唯一一个：notifier.go 的 smtpImplicitSSLEnabled 是拿一个字符串切片
// 循环 cfg[key]，不是 cfg["smtp_ssl"]，所以 AST 里根本不存在这个字面量。
// 它是给用户看的规范键名（Web 的 SSL 下拉、backup_qinglong 导入时写的也是它），
// 必须留在 schema 里。
var notifySchemaOnlyKeys = map[string]string{
	"smtp_ssl": "由 smtpImplicitSSLEnabled 的别名循环读取，不是 cfg[\"smtp_ssl\"] 字面量",
}

// notifyNotifierOnlyKeys：notifier.go 会读、但故意不给表单入口的键。
//
// 这 4 个是青龙备份导入时的向后兼容别名 —— 老青龙实例用的是这些键名，
// smtpImplicitSSLEnabled 按顺序探测它们，命中哪个用哪个。
// 它们是「只读不写」的历史包袱：新配置一律用 smtp_ssl，所以不该出现在表单里，
// 否则用户会看到 5 个语义重复的 SSL 开关。
//
// 目前这 4 个也是循环读的、AST 同样扫不到，但仍然显式列在这里：
// 万一以后有人把循环展开成 4 个 cfg["..."] 字面量，这份白名单能保证用例不会
// 因为「本来就该被排除的键」而误报红。
var notifyNotifierOnlyKeys = map[string]string{
	"smtp_use_ssl": "青龙导入兼容别名，只读不写，不给表单入口",
	"use_ssl":      "青龙导入兼容别名，只读不写，不给表单入口",
	"enable_ssl":   "青龙导入兼容别名，只读不写，不给表单入口",
	"ssl":          "青龙导入兼容别名，只读不写，不给表单入口",
}

// TestNotifySchemaCoversAllConfigKeysReadByNotifier 是这一整套 schema 里最重要的一条用例。
//
// 它把 model 的渠道字段注册表和 notifier.go 实际读取的 config 键**双向**绑死：
//
//   - notifier 有、schema 没有 -> 服务端读得到，但任何客户端都没有输入框可填。
//     这正是本期要消灭的那个 bug：APP 少了 31 个键，用户根本填不了。
//   - schema 有、notifier 没有 -> 用户辛辛苦苦填了，服务端压根不看。假字段。
//
// 为什么必须靠测试而不能靠人：这份知识在仓库里长期有四份副本，靠人手同步已经失效过 ——
// web/src/views/api-docs/apiData.ts 的 wecom_app 消息类型漏了 mpnews，而 notifier.go
// 和 index.vue 都有，没有任何机制发现。schema 建好之后如果没有这条用例，下一个人往
// notifier.go 里加一句 cfg["retry_count"]，同样的病会原封不动地复发，只是病灶从
// APP 挪到了 notify_channel_registry.go。
//
// 突变验证方式（改动本用例后请手动跑一次确认它还有效）：
// 往 notifier.go 里任意加一句 _ = cfg["__mutation_test__"]，本用例必须变红。
func TestNotifySchemaCoversAllConfigKeysReadByNotifier(t *testing.T) {
	notifierKeys := mustParseNotifierConfigKeys(t)
	if len(notifierKeys) == 0 {
		t.Fatal("没有从 notifier.go 里扫到任何 cfg[\"...\"]，扫描器本身坏了")
	}

	schemaKeys := make(map[string]struct{})
	for _, key := range model.NotifyChannelConfigKeys() {
		schemaKeys[key] = struct{}{}
	}
	if len(schemaKeys) == 0 {
		t.Fatal("渠道字段注册表是空的")
	}

	var missingInSchema []string
	for key := range notifierKeys {
		if _, declared := schemaKeys[key]; declared {
			continue
		}
		if _, allowed := notifyNotifierOnlyKeys[key]; allowed {
			continue
		}
		missingInSchema = append(missingInSchema, key)
	}

	var missingInNotifier []string
	for key := range schemaKeys {
		if _, read := notifierKeys[key]; read {
			continue
		}
		if _, allowed := notifySchemaOnlyKeys[key]; allowed {
			continue
		}
		missingInNotifier = append(missingInNotifier, key)
	}

	sort.Strings(missingInSchema)
	sort.Strings(missingInNotifier)

	if len(missingInSchema) > 0 {
		t.Errorf(
			"notifier.go 读取了这些 config 键，但 model/notify_channel_registry.go 没有声明：%v\n"+
				"后果：服务端认这些键，但 Web 和 APP 都不会渲染输入框，用户没有任何办法填。\n"+
				"修法：去 notify_channel_registry.go 对应渠道补上字段声明。",
			missingInSchema,
		)
	}
	if len(missingInNotifier) > 0 {
		t.Errorf(
			"model/notify_channel_registry.go 声明了这些 config 键，但 notifier.go 从来不读：%v\n"+
				"后果：用户填了也不起作用，是假字段。\n"+
				"修法：要么删掉这个字段声明，要么去 notifier.go 里真的用上它。",
			missingInNotifier,
		)
	}
}

// TestNotifySchemaCoversAllChannelTypesHandledByNotifier 把渠道类型也绑死。
//
// 这是同一类漂移的另一半：以前 handler 的 /notifications/types 是一份硬编码列表，
// 和 notifier.go 的 switch 各写各的。加了渠道漏改 handler，用户在下拉里就看不到它；
// 反过来漏改 notifier.go，用户能建出一个建完就报「未知的通知渠道类型」的渠道。
func TestNotifySchemaCoversAllChannelTypesHandledByNotifier(t *testing.T) {
	notifierTypes := mustParseNotifierChannelTypes(t)
	if len(notifierTypes) == 0 {
		t.Fatal("没有从 sendToChannel 的 switch 里扫到任何渠道类型，扫描器本身坏了")
	}

	schemaTypes := make(map[string]struct{})
	for _, channel := range model.NotifyChannelDefinitions() {
		schemaTypes[channel.Type] = struct{}{}
	}

	var missingInSchema, missingInNotifier []string
	for channelType := range notifierTypes {
		if _, ok := schemaTypes[channelType]; !ok {
			missingInSchema = append(missingInSchema, channelType)
		}
	}
	for channelType := range schemaTypes {
		if _, ok := notifierTypes[channelType]; !ok {
			missingInNotifier = append(missingInNotifier, channelType)
		}
	}
	sort.Strings(missingInSchema)
	sort.Strings(missingInNotifier)

	if len(missingInSchema) > 0 {
		t.Errorf("notifier.go 的 sendToChannel 支持这些渠道，但注册表没声明：%v", missingInSchema)
	}
	if len(missingInNotifier) > 0 {
		t.Errorf("注册表声明了这些渠道，但 notifier.go 的 sendToChannel 不认：%v（建出来会直接报「未知的通知渠道类型」）", missingInNotifier)
	}
}

// TestNotifierSmtpSSLAliasLoopStillExists 防止上面那两份白名单变成永久性的死洞。
//
// 白名单的前提是「别名循环还在」。哪天有人把 smtpImplicitSSLEnabled 重写成显式判断、
// 或者干脆删掉青龙兼容，白名单就该跟着删；不删的话它会一直默默放行本该报错的键。
func TestNotifierSmtpSSLAliasLoopStillExists(t *testing.T) {
	source := mustReadNotifierSource(t)

	for alias := range notifyNotifierOnlyKeys {
		if !strings.Contains(source, strconv.Quote(alias)) {
			t.Errorf(
				"notifier.go 里已经找不到 SSL 兼容别名 %q，但 notifyNotifierOnlyKeys 还在放行它。\n"+
					"请把它从白名单里删掉，否则这条防线上会留一个永久的洞。",
				alias,
			)
		}
	}
	if !strings.Contains(source, "smtpImplicitSSLEnabled") {
		t.Error("notifier.go 里已经没有 smtpImplicitSSLEnabled，notifySchemaOnlyKeys 对 smtp_ssl 的豁免需要重新评估")
	}
}

// TestNotifierSourceHoldsEverySenderCalledBySendToChannel 兜住「有人把 sendXxx 拆到别的文件」。
//
// 上面两条扫描器都只读 notifier.go 这一个文件。一旦某个发送函数被挪走，它读取的
// config 键就会静默逃过绑定检查 —— 防线还在，但已经不覆盖那个渠道了，而且没有任何提示。
func TestNotifierSourceHoldsEverySenderCalledBySendToChannel(t *testing.T) {
	parsed := mustParseNotifierAST(t)

	declared := make(map[string]struct{})
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name != nil {
			declared[fn.Name.Name] = struct{}{}
		}
	}

	var missing []string
	var called int
	ast.Inspect(parsed, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "sendToChannel" {
			return true
		}

		ast.Inspect(fn, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || !strings.HasPrefix(ident.Name, "send") {
				return true
			}
			called++
			if _, exists := declared[ident.Name]; !exists {
				missing = append(missing, ident.Name)
			}
			return true
		})
		return false
	})

	if called == 0 {
		t.Fatal("没有在 sendToChannel 里找到任何 sendXxx 调用，扫描器本身坏了")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf(
			"这些发送函数已经不在 %s 里了：%v\n"+
				"schema 绑定用例只扫描单个文件，被拆出去那部分读取的 config 键会静默逃过检查。\n"+
				"请把 notifierSourceFile 改成文件列表，并让扫描器遍历全部文件。",
			notifierSourceFile, missing,
		)
	}
}

// ---- 扫描器 ----

func mustReadNotifierSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(notifierSourceFile)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", notifierSourceFile, err)
	}
	return string(raw)
}

func mustParseNotifierAST(t *testing.T) *ast.File {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, notifierSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", notifierSourceFile, err)
	}
	return parsed
}

// mustParseNotifierConfigKeys 抽出所有 cfg["..."] 形式的取值。
//
// 用 go/ast 而不是正则：注释里的示例、错误信息字符串里的 cfg["x"] 都不会被误命中，
// 而正则会。代价只是多十几行遍历代码。
func mustParseNotifierConfigKeys(t *testing.T) map[string]struct{} {
	t.Helper()

	keys := make(map[string]struct{})
	ast.Inspect(mustParseNotifierAST(t), func(node ast.Node) bool {
		index, ok := node.(*ast.IndexExpr)
		if !ok {
			return true
		}
		ident, ok := index.X.(*ast.Ident)
		if !ok || ident.Name != "cfg" {
			return true
		}
		literal, ok := index.Index.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		key, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Errorf("无法解析 config 键字面量 %s: %v", literal.Value, err)
			return true
		}
		keys[key] = struct{}{}
		return true
	})

	return keys
}

// mustParseNotifierChannelTypes 抽出 sendToChannel 里 switch ch.Type 的全部 case 值。
func mustParseNotifierChannelTypes(t *testing.T) map[string]struct{} {
	t.Helper()

	types := make(map[string]struct{})
	ast.Inspect(mustParseNotifierAST(t), func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "sendToChannel" {
			return true
		}

		ast.Inspect(fn, func(inner ast.Node) bool {
			clause, ok := inner.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				types[value] = struct{}{}
			}
			return true
		})
		return false
	})

	return types
}
