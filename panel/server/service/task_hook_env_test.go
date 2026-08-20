package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// syncOutputCollector 收集任务日志。RunInlineScript 的读管道协程与调用方是并发的，
// 直接往 strings.Builder 里写会被 -race 抓到。
type syncOutputCollector struct {
	mu      sync.Mutex
	builder strings.Builder
}

func (c *syncOutputCollector) write(chunk string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.builder.WriteString(chunk)
}

func (c *syncOutputCollector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.builder.String()
}

// ---------------------------------------------------------------------------
// 纯函数用例：不依赖 bash，任何平台都跑
// ---------------------------------------------------------------------------

// 合并必须是「纯增量覆盖」。这条用例锁的是那个最贵的坑：
// planShellEnvExport 会把超过 MAX_ARG_STRLEN 的变量「只赋值不 export」，
// 它在钩子里 env -0 根本看不到；一旦改成用 dump 结果整体替换 envVars，
// 用户的大账号变量会在目标脚本里凭空消失。
func TestMergeHookEnvExportsOnlyAppliesNewOrChangedKeys(t *testing.T) {
	bigValue := strings.Repeat("x", 200*1024)
	envVars := map[string]string{
		"BIG_ENV":  bigValue,
		"KEEP_ME":  "unchanged",
		"OVERRIDE": "old",
	}
	baseline := map[string]string{
		"KEEP_ME":  "unchanged",
		"OVERRIDE": "old",
		"HOME":     "/root",
	}
	final := map[string]string{
		"KEEP_ME":   "unchanged",
		"OVERRIDE":  "new",
		"HOME":      "/root",
		"BRAND_NEW": "hello",
	}

	applied, ignored, notices := mergeHookEnvExports(envVars, baseline, final)

	if len(notices) != 0 {
		t.Fatalf("普通变量不该触发运行时关键变量提示，got %v", notices)
	}
	if got, want := strings.Join(applied, ","), "BRAND_NEW,OVERRIDE"; got != want {
		t.Fatalf("expected applied keys %q, got %q", want, got)
	}
	if len(ignored) != 0 {
		t.Fatalf("expected no ignored keys, got %v", ignored)
	}
	if envVars["OVERRIDE"] != "new" {
		t.Fatalf("expected changed key to be merged, got %q", envVars["OVERRIDE"])
	}
	if envVars["BRAND_NEW"] != "hello" {
		t.Fatalf("expected new key to be merged, got %q", envVars["BRAND_NEW"])
	}
	if len(envVars["BIG_ENV"]) != len(bigValue) {
		t.Fatalf("expected oversized env untouched, got %d bytes", len(envVars["BIG_ENV"]))
	}
	// HOME 在基线里就存在且没被改过，属于 bootstrap 进程自带的噪音，不能混进任务环境。
	if _, exists := envVars["HOME"]; exists {
		t.Fatal("expected untouched process env key to stay out of task env")
	}
}

// unset 不传导：缺席既可能是被 unset，也可能是那批「只赋值不 export」的大变量，
// 两者无法区分，按缺席删键会直接删掉用户的账号变量。
func TestMergeHookEnvExportsDoesNotPropagateUnset(t *testing.T) {
	envVars := map[string]string{"JD_COOKIE": "a&b"}
	baseline := map[string]string{"JD_COOKIE": "a&b"}
	final := map[string]string{}

	applied, ignored, _ := mergeHookEnvExports(envVars, baseline, final)

	if len(applied) != 0 || len(ignored) != 0 {
		t.Fatalf("expected nothing merged, applied=%v ignored=%v", applied, ignored)
	}
	if envVars["JD_COOKIE"] != "a&b" {
		t.Fatalf("expected unset not to be propagated, got %q", envVars["JD_COOKIE"])
	}
}

func TestMergeHookEnvExportsProtectsRuntimeContractKeys(t *testing.T) {
	envVars := map[string]string{
		"TZ":                       "Asia/Shanghai",
		"DAIDAI_NOTIFY_CHANNEL_ID": "7",
		"DAIDAI_TOKEN":             "real-token",
	}
	baseline := map[string]string{
		"TZ":                       "Asia/Shanghai",
		"DAIDAI_NOTIFY_CHANNEL_ID": "7",
		"DAIDAI_TOKEN":             "real-token",
		"PWD":                      "/data/scripts",
		"BASH_VERSION":             "5.2.15",
		"SHLVL":                    "1",
	}
	final := map[string]string{
		"TZ":                       "UTC",
		"DAIDAI_NOTIFY_CHANNEL_ID": "",
		"DAIDAI_TOKEN":             "stolen",
		"PWD":                      "/tmp",
		"BASH_VERSION":             "5.2.21",
		"SHLVL":                    "2",
		"COMP_WORDBREAKS":          " \t\n",
		"NORMAL_ONE":               "ok",
	}

	applied, ignored, _ := mergeHookEnvExports(envVars, baseline, final)

	if got, want := strings.Join(applied, ","), "NORMAL_ONE"; got != want {
		t.Fatalf("expected only unprotected key applied, want %q got %q", want, got)
	}
	// 只有「用户有意去改的契约变量」才进日志；PWD / SHLVL / BASH_* / COMP_* 这类
	// shell 内部易变量同样被挡下，但静默处理（前置脚本一句 cd 就会让 PWD 变化）。
	wantIgnored := "DAIDAI_NOTIFY_CHANNEL_ID,DAIDAI_TOKEN,TZ"
	if got := strings.Join(ignored, ","); got != wantIgnored {
		t.Fatalf("expected ignored %q, got %q", wantIgnored, got)
	}
	for _, volatile := range []string{"PWD", "SHLVL", "BASH_VERSION", "COMP_WORDBREAKS"} {
		if _, merged := envVars[volatile]; merged {
			t.Fatalf("expected volatile shell variable %s to stay out of task env", volatile)
		}
	}
	if envVars["TZ"] != "Asia/Shanghai" {
		t.Fatalf("expected panel timezone contract to survive, got %q", envVars["TZ"])
	}
	if envVars["DAIDAI_NOTIFY_CHANNEL_ID"] != "7" {
		t.Fatalf("expected notify channel binding to survive, got %q", envVars["DAIDAI_NOTIFY_CHANNEL_ID"])
	}
	if envVars["DAIDAI_TOKEN"] != "real-token" {
		t.Fatalf("expected script token to survive, got %q", envVars["DAIDAI_TOKEN"])
	}
}

// 运行时关键变量（PATH / PYTHONPATH / NODE_OPTIONS / NODE_PATH）**不在保护名单里**：
// 覆盖照常生效，但整体覆盖时要多打一行带「追加写法」的提示，否则用户只会看到
// 「脚本突然找不到依赖」而完全联想不到是前置脚本冲掉了面板注入的路径。
func TestMergeHookEnvExportsWarnsOnRuntimeCriticalOverride(t *testing.T) {
	envVars := map[string]string{
		"PYTHONPATH":   "/app/venv/lib/python3.11/site-packages:/data/scripts",
		"NODE_OPTIONS": `--require="/data/scripts/sendNotify.js"`,
		"PATH":         "/usr/local/bin:/usr/bin",
		"NORMAL_ONE":   "old",
	}
	baseline := map[string]string{
		"PYTHONPATH":   "/app/venv/lib/python3.11/site-packages:/data/scripts",
		"NODE_OPTIONS": `--require="/data/scripts/sendNotify.js"`,
		"PATH":         "/usr/local/bin:/usr/bin",
		"NORMAL_ONE":   "old",
	}
	final := map[string]string{
		// 整体覆盖：面板注入的 site-packages 被冲掉，必须提示。
		"PYTHONPATH": "/my/lib",
		// 追加写法：面板注入的那段原样还在，这正是我们想要的用法，不提示。
		"NODE_OPTIONS": `--require="/data/scripts/sendNotify.js" --max-old-space-size=2048`,
		"PATH":         "/opt/custom/bin:/usr/local/bin:/usr/bin",
		// 普通变量：无论怎么改都不该出现提示。
		"NORMAL_ONE": "new",
	}

	applied, ignored, notices := mergeHookEnvExports(envVars, baseline, final)

	if got, want := strings.Join(applied, ","), "NODE_OPTIONS,NORMAL_ONE,PATH,PYTHONPATH"; got != want {
		t.Fatalf("运行时关键变量必须照常生效，期望 applied=%q，实际 %q", want, got)
	}
	if len(ignored) != 0 {
		t.Fatalf("运行时关键变量不属于保护名单，不该被忽略，got %v", ignored)
	}
	if envVars["PYTHONPATH"] != "/my/lib" {
		t.Fatalf("覆盖必须真的生效，实际 %q", envVars["PYTHONPATH"])
	}

	if len(notices) != 1 {
		t.Fatalf("只有被整体覆盖的 PYTHONPATH 该出提示，实际 %v", notices)
	}
	notice := notices[0]
	if !strings.Contains(notice, "PYTHONPATH") {
		t.Fatalf("提示行必须点名是哪个键，实际 %q", notice)
	}
	if !strings.Contains(notice, "追加写法") || !strings.Contains(notice, "$PYTHONPATH") {
		t.Fatalf("提示行必须给出追加写法这个行动指引，实际 %q", notice)
	}
	if !strings.HasPrefix(notice, "[前置脚本环境变量] ") {
		t.Fatalf("提示行要与其它钩子日志同前缀，实际 %q", notice)
	}
	// NODE_OPTIONS / PATH 用的是追加写法，NORMAL_ONE 压根不在名单里，都不该被提示。
	for _, quiet := range []string{"NORMAL_ONE", "NODE_OPTIONS", "$PATH"} {
		if strings.Contains(notice, quiet) {
			t.Fatalf("%s 不该出现在提示里，实际 %q", quiet, notice)
		}
	}
}

func TestParseHookEnvDumpHandlesNulAndNewlineFormats(t *testing.T) {
	nulDump := []byte("FOO=bar\x00MULTI=line1\nline2\x00WITH_EQ=a=b=c\x00")
	parsed := parseHookEnvDump(nulDump)
	if parsed["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar, got %q", parsed["FOO"])
	}
	if parsed["MULTI"] != "line1\nline2" {
		t.Fatalf("expected multiline value preserved, got %q", parsed["MULTI"])
	}
	if parsed["WITH_EQ"] != "a=b=c" {
		t.Fatalf("expected only the first '=' to split, got %q", parsed["WITH_EQ"])
	}

	// 最后一档降级是逐行 env，靠「文件里没有 NUL 字节」识别。
	lineDump := []byte("FOO=bar\nWITH_EQ=a=b=c\n")
	parsed = parseHookEnvDump(lineDump)
	if parsed["FOO"] != "bar" || parsed["WITH_EQ"] != "a=b=c" {
		t.Fatalf("expected newline fallback format parsed, got %#v", parsed)
	}

	if parseHookEnvDump(nil) != nil {
		t.Fatal("expected empty dump to parse into nil")
	}
}

// ---------------------------------------------------------------------------
// 真跑 bash 的用例
// ---------------------------------------------------------------------------

// trap 方案的核心验收：前置脚本以 exit 0 结尾时合并照样发生。
// 前置脚本是被 `. source` 进 bootstrap shell 的，任何追加在用户内容之后的
// dump 代码都会被这句 exit 跳过。顺带把保护名单、大变量、含换行 / 含 '=' 的值
// 一起锁在同一次真实执行里。
func TestTaskBeforeInlineScriptExportsMergeIntoTaskEnv(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	bigValue := strings.Repeat("x", 200*1024)
	envVars := map[string]string{
		"PATH":                     os.Getenv("PATH"),
		"TZ":                       "Asia/Shanghai",
		"DAIDAI_NOTIFY_CHANNEL_ID": "7",
		"BIG_ENV":                  bigValue,
		"KEEP_ME":                  "unchanged",
	}

	script := `export FOO=bar
export MULTILINE='line1
line2'
export WITH_EQ='a=b=c'
export TZ=UTC
export DAIDAI_NOTIFY_CHANNEL_ID=
export PATH="/opt/custom/bin:$PATH"
echo pre-script done
exit 0
echo never reached
`

	output := &syncOutputCollector{}

	captureHookEnvExports(envVars, output.write, func(hookEnv map[string]string) {
		if err := RunInlineScript(script, config.C.Data.ScriptsDir, hookEnv, 30, output.write); err != nil {
			t.Fatalf("run task before script: %v, output=%s", err, output.String())
		}
	})

	if envVars["FOO"] != "bar" {
		t.Fatalf("expected exported FOO to survive `exit 0`, got %q (output=%s)", envVars["FOO"], output.String())
	}
	if envVars["MULTILINE"] != "line1\nline2" {
		t.Fatalf("expected multiline value preserved, got %q", envVars["MULTILINE"])
	}
	if envVars["WITH_EQ"] != "a=b=c" {
		t.Fatalf("expected value containing '=' preserved, got %q", envVars["WITH_EQ"])
	}
	if envVars["TZ"] != "Asia/Shanghai" {
		t.Fatalf("expected TZ contract to survive, got %q", envVars["TZ"])
	}
	if envVars["DAIDAI_NOTIFY_CHANNEL_ID"] != "7" {
		t.Fatalf("expected DAIDAI_* to survive, got %q", envVars["DAIDAI_NOTIFY_CHANNEL_ID"])
	}
	if len(envVars["BIG_ENV"]) != len(bigValue) {
		t.Fatalf("expected oversized env not to be dropped, got %d bytes", len(envVars["BIG_ENV"]))
	}
	if envVars["KEEP_ME"] != "unchanged" {
		t.Fatalf("expected untouched env preserved, got %q", envVars["KEEP_ME"])
	}
	// PATH 刻意不进保护名单：托管解释器解析走面板自己的 PATH，改这里只影响脚本自己
	// fork 出来的 pip / npm / git，那正是 shell 语义。
	if !strings.HasPrefix(envVars["PATH"], "/opt/custom/bin:") {
		t.Fatalf("expected PATH override to be merged, got %q", envVars["PATH"])
	}
	if _, leaked := envVars[hookEnvDumpPathEnvKey]; leaked {
		t.Fatal("expected dump switch not to leak into task env")
	}
	if !strings.Contains(output.String(), "已生效") {
		t.Fatalf("expected merge to be reported in task log, output=%s", output.String())
	}
}

// 全局 task_before.sh 走的是 RunHookScript（直接执行脚本文件），与任务专属前置脚本
// 是两条不同的调用路径，必须各锁一条。
func TestGlobalTaskBeforeHookExportsMergeIntoTaskEnv(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	hookPath := filepath.Join(config.C.Data.ScriptsDir, "task_before.sh")
	hookContent := []byte("export GLOBAL_HOOK_VALUE=from-global\nexit 0\n")
	if err := os.WriteFile(hookPath, hookContent, 0o755); err != nil {
		t.Fatalf("write global hook: %v", err)
	}

	envVars := map[string]string{"PATH": os.Getenv("PATH")}
	captureHookEnvExports(envVars, nil, func(hookEnv map[string]string) {
		RunHookScript("task_before.sh", config.C.Data.ScriptsDir, hookEnv, nil)
	})

	if envVars["GLOBAL_HOOK_VALUE"] != "from-global" {
		t.Fatalf("expected global task_before.sh export to be merged, got %q", envVars["GLOBAL_HOOK_VALUE"])
	}
}

// 没有全局 task_before.sh 时不能往任务日志里刷噪音——绝大多数用户都属于这种情况。
func TestMissingGlobalTaskBeforeHookStaysSilent(t *testing.T) {
	testutil.SetupTestEnv(t)

	envVars := map[string]string{"PATH": os.Getenv("PATH")}
	output := &syncOutputCollector{}
	captureHookEnvExports(envVars, output.write, func(hookEnv map[string]string) {
		RunHookScript("task_before.sh", config.C.Data.ScriptsDir, hookEnv, nil)
	})

	if got := output.String(); got != "" {
		t.Fatalf("expected no log output when the hook does not exist, got %q", got)
	}
}

// RunInlineScript 的第三个调用方是订阅钩子。dump 开关只在任务前置链路才传，
// 订阅钩子的环境、输出、退出码都必须与改动前完全一致。
func TestSubscriptionHookStaysUnaffectedByHookEnvCapture(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	sub := &model.Subscription{Name: "demo", Type: model.SubTypeGitRepo, URL: "https://example.com/owner/repo.git"}
	hookEnv := buildSubscriptionHookEnv(sub, config.C.Data.ScriptsDir)
	if _, leaked := hookEnv[hookEnvDumpPathEnvKey]; leaked {
		t.Fatal("subscription hook env must not carry the dump switch")
	}

	// 结论写文件而不是走 stdout：RunInlineScript 的读管道协程与 cmd.Wait 是并发的，
	// 靠 stdout 断言会引入偶发的截断。
	script := `printf 'dump=[%s]\n' "$DAIDAI_HOOK_ENV_DUMP" > sub-hook.out
printf 'trap=[%s]\n' "$(trap -p EXIT)" >> sub-hook.out
exit 3
`
	err := RunInlineScript(script, config.C.Data.ScriptsDir, hookEnv, 30, nil)
	if err == nil {
		t.Fatal("expected subscription hook exit code to be surfaced unchanged")
	}

	probe, readErr := os.ReadFile(filepath.Join(config.C.Data.ScriptsDir, "sub-hook.out"))
	if readErr != nil {
		t.Fatalf("read subscription hook probe: %v", readErr)
	}
	if got, want := string(probe), "dump=[]\ntrap=[]\n"; got != want {
		t.Fatalf("expected subscription hook to see neither dump switch nor EXIT trap, want %q got %q", want, got)
	}
	if len(hookEnv) != len(buildSubscriptionHookEnv(sub, config.C.Data.ScriptsDir)) {
		t.Fatal("subscription hook env map must not be mutated")
	}
}

// 前置脚本失败时：trap EXIT 不得吞掉退出码（RunInlineScript 的返回值要照常非 nil），
// 而且异常退出路径上 trap 一样会跑完，已经 export 的变量仍然回传。
//
// bash 在跑完 EXIT trap 之后会恢复「进入 trap 时的 $?」（前提是 trap 自己不调 exit，
// 我们这条只调 __dd_dump_env），所以装 trap 不改变任何成败判定 ——
// 前置脚本失败本来就不中断任务，只是现在多打一行 [前置脚本执行失败]。
// 少了这条用例，把退出码写没了也没人发现：task_executor 那边只是少打一行日志。
func TestTaskBeforeFailureKeepsExitCodeAndStillMergesExports(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	envVars := map[string]string{"PATH": os.Getenv("PATH")}
	output := &syncOutputCollector{}

	var runErr error
	captureHookEnvExports(envVars, output.write, func(hookEnv map[string]string) {
		runErr = RunInlineScript("export BEFORE_FAIL=kept\nexit 3\n", config.C.Data.ScriptsDir, hookEnv, 30, output.write)
	})

	if runErr == nil {
		t.Fatalf("前置脚本以 exit 3 结束时错误必须原样冒出来，output=%s", output.String())
	}
	if !strings.Contains(runErr.Error(), "3") {
		t.Fatalf("退出码必须保持 3（EXIT trap 不得改写它），实际 %v", runErr)
	}
	if envVars["BEFORE_FAIL"] != "kept" {
		t.Fatalf("失败退出路径上 trap 同样要落盘，期望 kept，实际 %q", envVars["BEFORE_FAIL"])
	}
}

// `.ok` 门禁：DAIDAI_HOOK_ENV_DUMP 非空【不足以】装上采集逻辑，还必须有面板落下的
// 同名 .ok 标记文件。
//
// 用户完全可以在「环境变量」页手建一条同名变量、值随手填成某个真实路径
// （比如 /app/config.yaml）。少了这道门禁，每个 bash 任务的 bootstrap 都会用
// `>` 把那个文件截断，而且全程无声。
func TestHookEnvDumpWithoutOkMarkerLeavesTargetFileUntouched(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	victim := filepath.Join(config.C.Data.ScriptsDir, "user-owned.yaml")
	const victimContent = "server:\n  port: 5700\n"
	if err := os.WriteFile(victim, []byte(victimContent), 0o600); err != nil {
		t.Fatalf("write victim file: %v", err)
	}

	// 刻意不走 captureHookEnvExports：那条路径一定会落 .ok，这里要模拟的正是
	// 「只有变量、没有标记」的手工误设。
	envVars := map[string]string{
		"PATH":                os.Getenv("PATH"),
		hookEnvDumpPathEnvKey: filepath.ToSlash(victim),
	}

	output := &syncOutputCollector{}
	if err := RunInlineScript("echo hook-ran\n", config.C.Data.ScriptsDir, envVars, 30, output.write); err != nil {
		t.Fatalf("run hook: %v, output=%s", err, output.String())
	}
	if !strings.Contains(output.String(), "hook-ran") {
		t.Fatalf("前置条件不成立：钩子没有真正执行，output=%s", output.String())
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim file: %v", err)
	}
	if string(got) != victimContent {
		t.Fatalf("没有 .ok 标记时不得写这个路径，实际 %q", string(got))
	}
	if _, err := os.Stat(victim + ".base"); !os.IsNotExist(err) {
		t.Fatalf("没有 .ok 标记时连基线快照都不该落盘（err=%v）", err)
	}
}

// 用户原始场景：多值账号变量 + 前置脚本 export 单值，目标脚本必须只看到那一个值。
func TestTaskBeforeExportNarrowsMultiValueEnvForTargetScript(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	envVars := map[string]string{
		"PATH":       os.Getenv("PATH"),
		"YYB_SERVER": "yyb-go:8000@11&yyb-go:8000@12&yyb-go:8000@13",
	}

	captureHookEnvExports(envVars, nil, func(hookEnv map[string]string) {
		if err := RunInlineScript("export YYB_SERVER='yyb-go:8000@11'\n", config.C.Data.ScriptsDir, hookEnv, 30, nil); err != nil {
			t.Fatalf("run task before script: %v", err)
		}
	})

	targetPath := filepath.Join(config.C.Data.ScriptsDir, "target.sh")
	if err := os.WriteFile(targetPath, []byte("printf '%s' \"$YYB_SERVER\" > yyb.out\n"), 0o755); err != nil {
		t.Fatalf("write target script: %v", err)
	}

	if _, _, err := RunCommand("bash target.sh", config.C.Data.ScriptsDir, 30, envVars, 4096, nil); err != nil {
		t.Fatalf("run target script: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(config.C.Data.ScriptsDir, "yyb.out"))
	if err != nil {
		t.Fatalf("read target output: %v", err)
	}
	if string(got) != "yyb-go:8000@11" {
		t.Fatalf("expected target script to see the narrowed value, got %q", string(got))
	}
}

// 合并后的变量必须对全部三种解释器生效。这里直接喂真实的 bootstrap
// （与 runtime_exec_timezone_test.go 同一手法），避免为了这条断言去创建托管 venv。
func TestTaskBeforeExportedEnvReachesBashPythonAndNodeTargets(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	envVars := map[string]string{"PATH": os.Getenv("PATH")}
	captureHookEnvExports(envVars, nil, func(hookEnv map[string]string) {
		if err := RunInlineScript("export HOOK_SHARED=from-hook\nexit 0\n", config.C.Data.ScriptsDir, hookEnv, 30, nil); err != nil {
			t.Fatalf("run task before script: %v", err)
		}
	})
	if envVars["HOOK_SHARED"] != "from-hook" {
		t.Fatalf("expected hook export merged, got %q", envVars["HOOK_SHARED"])
	}

	t.Run("bash", func(t *testing.T) {
		scriptPath := filepath.Join(config.C.Data.ScriptsDir, "read_env.sh")
		if err := os.WriteFile(scriptPath, []byte("printf '%s' \"$HOOK_SHARED\" > bash.out\n"), 0o755); err != nil {
			t.Fatalf("write bash script: %v", err)
		}
		if _, _, err := RunCommand("bash read_env.sh", config.C.Data.ScriptsDir, 30, envVars, 4096, nil); err != nil {
			t.Fatalf("run bash script: %v", err)
		}
		assertHookEnvOutputFile(t, filepath.Join(config.C.Data.ScriptsDir, "bash.out"))
	})

	t.Run("python", func(t *testing.T) {
		pythonBin := lookupOptionalBinary(t, "python3", "python")
		_, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
		if err != nil {
			t.Fatalf("write runtime env file: %v", err)
		}
		defer cleanup()

		scriptPath := filepath.Join(config.C.Data.ScriptsDir, "read_env.py")
		script := "import os\nprint(os.environ.get('HOOK_SHARED', ''), end='')\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
			t.Fatalf("write python script: %v", err)
		}

		cmd := exec.Command(pythonBin, "-u", "-c", pythonEnvBootstrap, envFile, scriptPath, "")
		cmd.Env = buildPythonBootstrapProcessEnv(envVars)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run python script: %v, output=%s", err, string(out))
		}
		if strings.TrimSpace(string(out)) != "from-hook" {
			t.Fatalf("expected python to read merged env, got %q", string(out))
		}
	})

	t.Run("node", func(t *testing.T) {
		nodeBin := lookupOptionalBinary(t, "node")
		_, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
		if err != nil {
			t.Fatalf("write runtime env file: %v", err)
		}
		defer cleanup()

		preloadFile, err := writeNodePreloadScript(filepath.Dir(envFile), envFile, envVars, false)
		if err != nil {
			t.Fatalf("write node preload: %v", err)
		}

		scriptPath := filepath.Join(config.C.Data.ScriptsDir, "read_env.js")
		if err := os.WriteFile(scriptPath, []byte("process.stdout.write(process.env.HOOK_SHARED || '')\n"), 0o600); err != nil {
			t.Fatalf("write node script: %v", err)
		}

		cmd := exec.Command(nodeBin, "--require", preloadFile, scriptPath)
		cmd.Env = buildBootstrapProcessEnv(envVars)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run node script: %v, output=%s", err, string(out))
		}
		if strings.TrimSpace(string(out)) != "from-hook" {
			t.Fatalf("expected node to read merged env, got %q", string(out))
		}
	})
}

func assertHookEnvOutputFile(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != "from-hook" {
		t.Fatalf("expected merged env value in %s, got %q", path, string(got))
	}
}

func lookupOptionalBinary(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skipf("%s unavailable on this machine", strings.Join(names, " / "))
	return ""
}
