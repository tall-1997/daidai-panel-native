package handler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestScriptCommandParts(t *testing.T) {
	parts, err := scriptCommandParts(".py", "demo.py")
	if err != nil {
		t.Fatalf("expected python command, got error: %v", err)
	}
	if len(parts) != 3 || parts[0] != "python" || parts[1] != "-u" || parts[2] != "demo.py" {
		t.Fatalf("unexpected command parts: %#v", parts)
	}
}

func TestScriptCommandPartsSupportsGo(t *testing.T) {
	parts, err := scriptCommandParts(".go", "demo.go")
	if err != nil {
		t.Fatalf("expected go command, got error: %v", err)
	}
	if len(parts) != 3 || parts[0] != "go" || parts[1] != "run" || parts[2] != "demo.go" {
		t.Fatalf("unexpected go command parts: %#v", parts)
	}
}

func TestScriptCommandPartsSupportsMJS(t *testing.T) {
	parts, err := scriptCommandParts(".mjs", "demo.mjs")
	if err != nil {
		t.Fatalf("expected mjs command, got error: %v", err)
	}
	if len(parts) != 2 || parts[0] != "node" || parts[1] != "demo.mjs" {
		t.Fatalf("unexpected mjs command parts: %#v", parts)
	}
}

func TestScriptCommandPartsRejectsUnsupportedExtension(t *testing.T) {
	if _, err := scriptCommandParts(".rb", "demo.rb"); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}

func TestScriptLanguageExtMapSupportsGo(t *testing.T) {
	if got := scriptLanguageExtMap["go"]; got != ".go" {
		t.Fatalf("expected go language map to .go, got %q", got)
	}
}

func TestScriptLanguageExtMapSupportsNodeMJS(t *testing.T) {
	if got := scriptLanguageExtMap["node"]; got != ".mjs" {
		t.Fatalf("expected node language map to .mjs, got %q", got)
	}
}

func TestDebugRunFinishDoesNotOverrideStoppedStatus(t *testing.T) {
	exitCode := -1
	run := &debugRun{
		Logs:     []string{"before"},
		Done:     true,
		ExitCode: &exitCode,
		Status:   "stopped",
	}

	run.finish(1, nil, 0.25)

	if run.Status != "stopped" {
		t.Fatalf("expected stopped status to be preserved, got %q", run.Status)
	}
	if !run.Done {
		t.Fatal("expected done flag to stay true")
	}
	if got := len(run.Logs); got != 1 {
		t.Fatalf("expected finish to avoid appending logs for stopped run, got %d entries", got)
	}
}

// newDebugRegistryRun 造一条调试运行记录：done 为真时是「结束于 finishedAgo 之前」的已结束记录，否则仍在跑。
func newDebugRegistryRun(done bool, finishedAgo time.Duration) *debugRun {
	run := newDebugRun()
	if done {
		exitCode := 0
		run.Done = true
		run.Status = "success"
		run.ExitCode = &exitCode
		run.finishedAt = time.Now().Add(-finishedAgo)
	}
	return run
}

func debugRunCount(h *ScriptHandler) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.debugRuns)
}

// #159 修复 C：调试运行 / 运行代码的记录原来没有条数上限也不过期，网页和 APP 又从不调 DELETE，
// 实测 50 次调试 RSS 29→216MB、闲置加强制 GC 也不降。现在写入新记录前修剪：总数压到 20 条，从最老的已结束记录淘汰起。
func TestDebugRunRegistryEvictsOldestFinishedRunsBeyondLimit(t *testing.T) {
	h := NewScriptHandler()

	finished := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		runID := fmt.Sprintf("finished_%02d", i)
		h.storeRun(runID, newDebugRegistryRun(true, time.Minute))
		finished = append(finished, runID)
	}
	running := []string{"running_a", "running_b"}
	for _, runID := range running {
		h.storeRun(runID, newDebugRegistryRun(false, 0))
	}
	if got := debugRunCount(h); got != maxDebugRuns {
		t.Fatalf("刚好塞满时不该淘汰任何记录，期望 %d 条，实际 %d 条", maxDebugRuns, got)
	}

	extra := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		runID := fmt.Sprintf("extra_%d", i)
		h.storeRun(runID, newDebugRegistryRun(true, 0))
		extra = append(extra, runID)
	}

	if got := debugRunCount(h); got != maxDebugRuns {
		t.Fatalf("超出上限后应被压回 %d 条，实际 %d 条", maxDebugRuns, got)
	}
	if _, exists := h.loadRun(finished[0]); exists {
		t.Fatalf("最老的已结束记录 %s 应当被淘汰", finished[0])
	}
	for _, runID := range running {
		if _, exists := h.loadRun(runID); !exists {
			t.Fatalf("仍在运行的记录 %s 不许被淘汰——用户正盯着它的输出，进程也会失去停止入口", runID)
		}
	}
	if _, exists := h.loadRun(extra[len(extra)-1]); !exists {
		t.Fatalf("最新的记录 %s 必须还在", extra[len(extra)-1])
	}
}

// 结束超过 30 分钟的记录在下一次写入时被删掉；没过 30 分钟的、还在跑的都保留。order 也要同步摘掉，否则它会一直变长。
func TestDebugRunRegistryExpiresRunsFinishedOver30Minutes(t *testing.T) {
	h := NewScriptHandler()

	expired := map[string]*debugRun{}
	for _, runID := range []string{"old_1", "old_2", "old_3"} {
		run := newDebugRegistryRun(true, 0)
		h.storeRun(runID, run)
		expired[runID] = run
	}
	recent := newDebugRegistryRun(true, 0)
	h.storeRun("recent", recent)
	h.storeRun("running", newDebugRegistryRun(false, 0))

	// 存进去之后再把结束时刻拨回去：storeRun 每次写入都会修剪，先拨回去的话前几条会在后面的写入里提前被删
	for _, run := range expired {
		run.finishedAt = time.Now().Add(-31 * time.Minute)
	}
	recent.finishedAt = time.Now().Add(-29 * time.Minute)

	h.storeRun("newest", newDebugRegistryRun(false, 0))

	for runID := range expired {
		if _, exists := h.loadRun(runID); exists {
			t.Fatalf("结束超过 30 分钟的记录 %s 应当在这次写入时被删掉", runID)
		}
	}
	for _, runID := range []string{"recent", "running", "newest"} {
		if _, exists := h.loadRun(runID); !exists {
			t.Fatalf("记录 %s 不该被删：没过 30 分钟、还在跑或刚写入的都要保留", runID)
		}
	}
	h.mu.Lock()
	orderLen := len(h.order)
	h.mu.Unlock()
	if got := debugRunCount(h); got != 3 || orderLen != 3 {
		t.Fatalf("应剩 3 条记录且 order 同步，实际 map %d 条、order %d 条", got, orderLen)
	}
}

// 一条都淘汰不掉时（全都在跑）宁可暂时超限，也不能删掉正在运行的记录，更不能死循环。
func TestDebugRunRegistryNeverEvictsRunningRuns(t *testing.T) {
	h := NewScriptHandler()

	total := maxDebugRuns + 3
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		runID := fmt.Sprintf("busy_%02d", i)
		h.storeRun(runID, newDebugRegistryRun(false, 0))
		ids = append(ids, runID)
	}

	if got := debugRunCount(h); got != total {
		t.Fatalf("全部在跑时应保留全部 %d 条，实际 %d 条", total, got)
	}
	for _, runID := range ids {
		if _, exists := h.loadRun(runID); !exists {
			t.Fatalf("运行中的记录 %s 丢了", runID)
		}
	}
}

// debugRunStopHelperEnvKey 让下面那个 Test 在被当作【子进程】拉起时改走「睡着等被停」的分支。
// 用测试二进制自己当被停的调试进程（做法同 service/process_signal_test.go），不依赖 bash / sleep，Windows 与 Linux 上都能跑。
const debugRunStopHelperEnvKey = "DAIDAI_DEBUG_STOP_HELPER"

// TestDebugRunStopHelperProcess 不是真正的用例，而是上面说的那个子进程。正常 go test 时没有那个环境变量，直接返回。
func TestDebugRunStopHelperProcess(t *testing.T) {
	if os.Getenv(debugRunStopHelperEnvKey) != "1" {
		return
	}
	// 睡到被父进程结束为止；真跑满 60 秒说明 stop() 没把它结束掉
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

// finish() 和 stop() 都要写 finishedAt（与 Done 同一处、同一把锁）：注册表靠它判断「结束超过 30 分钟」，
// 漏写的话这条记录永远不会按时间过期，只能等条数淘汰。
func TestDebugRunFinishAndStopRecordFinishedAt(t *testing.T) {
	finished := newDebugRun()
	before := time.Now()
	finished.finish(0, nil, 0.1)
	if got := finished.finishedAtTime(); got.IsZero() || got.Before(before) {
		t.Fatalf("finish() 应写下结束时刻，got %v（调用前 %v）", got, before)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDebugRunStopHelperProcess$")
	cmd.Env = append(os.Environ(), debugRunStopHelperEnvKey+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	stopped := newDebugRun()
	stopped.setProcess(cmd.Process)
	before = time.Now()
	stopped.stop()
	if got := stopped.finishedAtTime(); got.IsZero() || got.Before(before) {
		t.Fatalf("stop() 应写下结束时刻，got %v（调用前 %v）", got, before)
	}
	if !stopped.isStopped() || !stopped.isDone() {
		t.Fatal("stop() 之后记录应是 stopped 且已结束")
	}

	// stop() 走不阻塞的 TerminateProcessGroup：helper 没有自己的进程组（没设 Setpgid），TERM 发不出去，
	// 会立即走 KillProcessGroup；Windows 上直接 p.Kill()。两种情况下进程都应很快结束。
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("stop() 之后调试进程应当很快被结束")
	}
}

func requireUsableBash(t *testing.T) {
	t.Helper()

	// Windows 上通常能找到 Git Bash，但它拿到的是 Windows 形式的路径，
	// 反斜杠会被当成转义符吃掉（日志里表现为 C:UserslinziAppData...），
	// 于是脚本里 `> xxx.out` 这种相对重定向根本落不了盘，用例读文件必失败。
	// 这些用例验的是 POSIX shell 的行为（ARG_MAX、环境变量传递），
	// 面板的生产环境是 Linux，CI 也在 Linux 上跑全量，这里直接跳过。
	if runtime.GOOS == "windows" {
		t.Skip("windows 下的 bash 不提供等价的 POSIX 路径语义，该用例只在 Linux 有意义")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
	if err := exec.Command(bashPath, "--version").Run(); err != nil {
		t.Skipf("bash is present but not usable: %v", err)
	}
}

func TestNewScriptCommandLoadsLargeShellEnvFromFile(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "large-env.sh")
	outputPath := filepath.Join(config.C.Data.ScriptsDir, "large-env.out")
	if err := os.WriteFile(scriptPath, []byte(`printf '%s' "${#BIG_ENV}" > large-env.out`+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd, cleanup, err := newScriptCommand(
		"bash",
		scriptPath,
		nil,
		config.C.Data.ScriptsDir,
		map[string]string{"BIG_ENV": strings.Repeat("x", 3*1024*1024)},
	)
	if err != nil {
		t.Fatalf("new script command: %v", err)
	}
	defer cleanup()

	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "BIG_ENV=") {
			t.Fatalf("large env must not be passed through process environment")
		}
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run script: %v: %s", err, out)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got := string(content); got != "3145728" {
		t.Fatalf("expected large env length 3145728, got %q", got)
	}
}

func TestNewScriptCommandDoesNotExportLargeShellEnvToChildren(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)
	if _, err := exec.LookPath("mktemp"); err != nil {
		t.Skipf("mktemp unavailable: %v", err)
	}

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "large-env-child.sh")
	outputPath := filepath.Join(config.C.Data.ScriptsDir, "large-env-child.out")
	script := strings.Join([]string{
		`tmp="$(mktemp)"`,
		`printf '%s:%s' "${#BIG_ENV}" "$SMALL_ENV" > large-env-child.out`,
		`rm -f "$tmp"`,
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd, cleanup, err := newScriptCommand(
		"bash",
		scriptPath,
		nil,
		config.C.Data.ScriptsDir,
		map[string]string{
			"BIG_ENV":   strings.Repeat("x", 3*1024*1024),
			"SMALL_ENV": "ok",
		},
	)
	if err != nil {
		t.Fatalf("new script command: %v", err)
	}
	defer cleanup()

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run script with child process: %v: %s", err, out)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got := string(content); got != "3145728:ok" {
		t.Fatalf("expected large env and small env in shell, got %q", got)
	}
}
