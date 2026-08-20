package service

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 拉取前指令与拉取后钩子共用 RunInlineScript，它固定用 bash 跑临时 .sh。
// Windows 上 `bash` 会解析到 WSL 的 C:\Windows\system32\bash.exe，跑 shellEnvBootstrap
// 会直接报 `__dd_key: invalid indirect expansion` —— 既有的「拉取后钩子」在本机同样如此，
// 属于宿主 shell 语义差异而非代码问题（生产环境是 Linux/Docker，CI 在 Linux 上跑全量）。
// 与 handler/script_runtime_test.go 里那批用例同口径直接跳过。
func requireBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows 下的 bash 不提供等价的 POSIX shell 语义，该用例只在 Linux 有意义")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
}

func TestRunSubscriptionPreScriptEmitsOutput(t *testing.T) {
	requireBash(t)
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		ID:        1,
		Name:      "demo",
		Type:      model.SubTypeGitRepo,
		URL:       "https://example.com/owner/repo.git",
		SaveDir:   "owner_repo",
		PreScript: `echo "pre-ok $SUB_NAME"`,
	}

	var lines []string
	AuthorizeSubscriptionPreScriptForTest(sub)
	err := runSubscriptionPreScriptIfConfigured(sub, func(line string) {
		lines = append(lines, line)
	})
	joined := strings.Join(lines, "\n")
	if err != nil {
		t.Fatalf("expected pre script to succeed, got %v\nlog:\n%s", err, joined)
	}
	if !strings.Contains(joined, "[执行拉取前指令]") || !strings.Contains(joined, "[拉取前指令完成]") {
		t.Fatalf("expected begin/end markers in log, got:\n%s", joined)
	}
	// 每行输出要带 [pre] 前缀，才能在一次拉取日志里和 [hook] 区分开
	if !strings.Contains(joined, "[pre] pre-ok demo") {
		t.Fatalf("expected prefixed script output, got:\n%s", joined)
	}
}

// 非 0 退出必须冒泡成错误 —— 调用方靠它中断整次拉取。
func TestRunSubscriptionPreScriptReturnsErrorOnFailure(t *testing.T) {
	requireBash(t)
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		ID:        2,
		Name:      "demo",
		Type:      model.SubTypeGitRepo,
		URL:       "https://example.com/owner/repo.git",
		SaveDir:   "owner_repo",
		PreScript: "exit 3",
	}

	AuthorizeSubscriptionPreScriptForTest(sub)
	err := runSubscriptionPreScriptIfConfigured(sub, func(string) {})
	if err == nil {
		t.Fatal("expected error when pre script exits non-zero")
	}
	if !strings.Contains(err.Error(), "拉取前指令") {
		t.Fatalf("error should name the pre script stage, got %v", err)
	}
}

// 没配前置指令的存量订阅：完全不执行、不报错、不产生日志。
func TestRunSubscriptionPreScriptSkippedWhenEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{ID: 3, Name: "demo", Type: model.SubTypeGitRepo, URL: "https://example.com/owner/repo.git"}

	emitted := 0
	AuthorizeSubscriptionPreScriptForTest(sub)
	if err := runSubscriptionPreScriptIfConfigured(sub, func(string) { emitted++ }); err != nil {
		t.Fatalf("expected no-op for empty pre script, got %v", err)
	}
	if emitted != 0 {
		t.Fatalf("expected no log lines for empty pre script, got %d", emitted)
	}
}

// 前置指令同样吃青龙路径改写（用户经常两处都直接粘贴青龙那套命令）。
func TestPreScriptSharesQingLongPathRewrite(t *testing.T) {
	sub := &model.Subscription{
		URL:       "https://gitee.com/hkyya/qljb.git",
		PreScript: `cd $QL_DIR/data/scripts/hkyya_qljb && npm i`,
	}

	got := normalizeSubscriptionScriptPaths(sub, sub.PreScript)
	want := `cd $SUB_DIR && npm i`
	if got != want {
		t.Fatalf("unexpected pre script rewrite: got %q want %q", got, want)
	}
}
