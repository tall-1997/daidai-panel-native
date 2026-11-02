package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 用户没设过 NO_PROXY 时，大小写两个键都要拿到回环白名单，且必须是纯主机名（不能是 CIDR）。
func TestBuildManagedRuntimeEnvMapInjectsLoopbackNoProxy(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	const want = "localhost,127.0.0.1,::1"
	if got := envMap["NO_PROXY"]; got != want {
		t.Fatalf("expected NO_PROXY=%q, got %q", want, got)
	}
	if got := envMap["no_proxy"]; got != want {
		t.Fatalf("expected no_proxy=%q, got %q", want, got)
	}
	// CIDR 写法 Python 匹配不上，一旦有人"顺手改成更严谨的写法"就会把 #111 改回去。
	if strings.Contains(envMap["NO_PROXY"], "/") {
		t.Fatalf("NO_PROXY must stay a plain host list without CIDR, got %q", envMap["NO_PROXY"])
	}
}

// 用户设了 NO_PROXY 时是合并追加，不能把他的内网白名单洗掉。
func TestBuildManagedRuntimeEnvMapMergesUserNoProxy(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.EnvVar{
		Name:    "NO_PROXY",
		Value:   "corp.internal",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	const want = "corp.internal,localhost,127.0.0.1,::1"
	if got := envMap["NO_PROXY"]; got != want {
		t.Fatalf("expected merged NO_PROXY=%q, got %q", want, got)
	}
	// 合并结果要同时写回小写键，否则只读小写的运行时会拿到旧值。
	if got := envMap["no_proxy"]; got != want {
		t.Fatalf("expected merged no_proxy=%q, got %q", want, got)
	}
}

// 用户只设了小写 no_proxy：绝不能因为"先读大写、大写为空才退小写"而把这份值丢掉。
func TestBuildManagedRuntimeEnvMapKeepsLowercaseOnlyNoProxy(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.EnvVar{
		Name:    "no_proxy",
		Value:   "corp.internal",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	const want = "corp.internal,localhost,127.0.0.1,::1"
	if got := envMap["no_proxy"]; got != want {
		t.Fatalf("expected lowercase value to survive, got no_proxy=%q", got)
	}
	if got := envMap["NO_PROXY"]; got != want {
		t.Fatalf("expected uppercase key to carry the merged value, got NO_PROXY=%q", got)
	}
}

// 幂等：用户已经写了回环条目（含大小写、前后空格变体）时不能重复追加，
// 也不能把他写的形态改掉。
func TestBuildManagedRuntimeEnvMapDoesNotDuplicateLoopbackNoProxy(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.EnvVar{
		Name:    "NO_PROXY",
		Value:   " 127.0.0.1 , LocalHost ",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	const want = "127.0.0.1,LocalHost,::1"
	if got := envMap["NO_PROXY"]; got != want {
		t.Fatalf("expected no duplicated loopback entries, want %q got %q", want, got)
	}
	if got := strings.Count(envMap["NO_PROXY"], "127.0.0.1"); got != 1 {
		t.Fatalf("expected 127.0.0.1 to appear once, got %d in %q", got, envMap["NO_PROXY"])
	}
}

// AppendProxyEnv 是兜底层：即使面板没配代理（proxy_url 为空）也要补白名单，
// 因为容器/宿主机上外部设置的 HTTP_PROXY 一样会劫持回环请求。
func TestAppendProxyEnvAddsLoopbackNoProxyWithoutPanelProxy(t *testing.T) {
	testutil.SetupTestEnv(t)

	env := AppendProxyEnv([]string{"PATH=/usr/bin"})

	foundUpper := false
	foundLower := false
	for _, entry := range env {
		if entry == "NO_PROXY=localhost,127.0.0.1,::1" {
			foundUpper = true
		}
		if entry == "no_proxy=localhost,127.0.0.1,::1" {
			foundLower = true
		}
		if strings.HasPrefix(entry, "HTTP_PROXY=") {
			t.Fatalf("proxy env must stay untouched when proxy_url is empty, got %q", entry)
		}
	}
	if !foundUpper || !foundLower {
		t.Fatalf("expected both NO_PROXY keys to be appended, got %v", env)
	}
}

// 传入 env 里已经带了 NO_PROXY（典型来源：buildEnv 先铺了用户 envVars）时，
// 兜底层不能再追加一份，更不能覆盖。
func TestAppendProxyEnvKeepsExistingNoProxy(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := model.SetConfig("proxy_url", "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("set proxy_url: %v", err)
	}

	env := AppendProxyEnv([]string{"PATH=/usr/bin", "NO_PROXY=corp.internal,localhost"})

	noProxyCount := 0
	proxyCount := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "NO_PROXY=") || strings.HasPrefix(entry, "no_proxy=") {
			noProxyCount++
			if entry != "NO_PROXY=corp.internal,localhost" {
				t.Fatalf("existing NO_PROXY must not be rewritten, got %q", entry)
			}
		}
		if strings.HasPrefix(entry, "HTTP_PROXY=") {
			proxyCount++
		}
	}
	if noProxyCount != 1 {
		t.Fatalf("expected exactly one NO_PROXY entry, got %d in %v", noProxyCount, env)
	}
	// 代理注入本身不能被这次改动带偏，git/pip/npm 还得照常走代理。
	if proxyCount != 1 {
		t.Fatalf("expected HTTP_PROXY to still be injected, got %d in %v", proxyCount, env)
	}
}

// 模板是 fmt.Sprintf，新增 JS 里漏写 %% 会让所有 Node 任务起不来。
// 这条不依赖 node，纯文本层就能把格式化事故拦住。
func TestNodePreloadDefinesDaidaiFlush(t *testing.T) {
	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "env.json")
	if err := os.WriteFile(envFile, []byte(`{"FLUSH_TEST":"1"}`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	preloadFile, err := writeNodePreloadScript(tempDir, envFile, map[string]string{}, false)
	if err != nil {
		t.Fatalf("write node preload: %v", err)
	}
	raw, err := os.ReadFile(preloadFile)
	if err != nil {
		t.Fatalf("read node preload: %v", err)
	}

	script := string(raw)
	if !strings.Contains(script, "daidaiFlush") {
		t.Fatalf("expected daidaiFlush in preload, got %q", script)
	}
	if !strings.Contains(script, "setBlocking") {
		t.Fatalf("expected stdout setBlocking guard in preload, got %q", script)
	}
	// fmt.Sprintf 遇到多余/缺失的格式动词会留下 %!、%!(EXTRA 这类标记。
	if strings.Contains(script, "%!") {
		t.Fatalf("preload template has a broken format verb: %q", script)
	}
}

// 真跑一遍 node：既验证 preload 能被解析执行，也验证 daidaiFlush 真的会 resolve，
// 且 writable:true 让脚本里的同名赋值在严格模式下不抛错。
func TestNodePreloadDaidaiFlushRunsInNode(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "env.json")
	if err := os.WriteFile(envFile, []byte(`{"FLUSH_TEST":"1"}`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	preloadFile, err := writeNodePreloadScript(tempDir, envFile, map[string]string{}, false)
	if err != nil {
		t.Fatalf("write node preload: %v", err)
	}

	scriptFile := filepath.Join(tempDir, "target.js")
	targetScript := `'use strict';
(async () => {
  if (typeof globalThis.daidaiFlush !== 'function') {
    process.stdout.write('missing');
    return;
  }
  await globalThis.daidaiFlush();
  // writable:true 才允许脚本里出现同名赋值；写成 false 时这一行在严格模式下会抛 TypeError。
  globalThis.daidaiFlush = function () {};
  process.stdout.write('flush-ok:' + (process.env.FLUSH_TEST || 'no-env'));
})();
`
	if err := os.WriteFile(scriptFile, []byte(targetScript), 0o600); err != nil {
		t.Fatalf("write target script: %v", err)
	}

	cmd := exec.Command(nodeBin, "--require", preloadFile, scriptFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node process failed: %v, output=%s", err, string(out))
	}
	if got := string(out); got != "flush-ok:1" {
		t.Fatalf("expected daidaiFlush to resolve and env injection to survive, got %q", got)
	}
}
