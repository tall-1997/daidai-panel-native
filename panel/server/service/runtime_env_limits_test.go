package service

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// resetRuntimeEnvWarningDedup 清空进程内的告警去重表。
// 去重表是包级状态，用例之间必须互不干扰，否则后跑的用例会被前一个用例静音。
func resetRuntimeEnvWarningDedup(t *testing.T) {
	t.Helper()
	runtimeEnvWarningsSeen.mu.Lock()
	defer runtimeEnvWarningsSeen.mu.Unlock()
	runtimeEnvWarningsSeen.seen = nil
}

// requireRuntimeEnvWarningEmit 跳过 Windows：那里 emit 直接返回，去重行为无从观察。
func requireRuntimeEnvWarningEmit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows 没有 MAX_ARG_STRLEN / ARG_MAX 限制，emit 本身就不输出")
	}
}

// taskEnvWith 造一个贴近真实的任务环境：真实环境永远不止一条变量，
// 「变量还在不在」这个判断必须建立在完整环境上。
// sizes 指定某些变量按 execve 口径要占多少字节。
func taskEnvWith(sizes map[string]int) map[string]string {
	env := map[string]string{
		"PATH": "/usr/local/bin:/usr/bin",
		"TZ":   "Asia/Shanghai",
	}
	for name, entryBytes := range sizes {
		env[name] = envValueForEntryBytes(name, entryBytes)
	}
	return env
}

func emitWarningsForTest(env map[string]string, interpreter string) []string {
	var lines []string
	emitRuntimeEnvLimitWarnings(env, interpreter, func(line string) {
		lines = append(lines, line)
	})
	return lines
}

// buildEnvWithTotalBytes 造一个「按 execve 口径合计正好 total 字节」的环境，
// 用来卡总量阈值的边界。count 要足够大，保证每条都不会自己撞上 MAX_ARG_STRLEN。
func buildEnvWithTotalBytes(count, total int) map[string]string {
	env := make(map[string]string, count)
	remaining := total
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("V%02d", i)
		overhead := len(name) + 2
		share := remaining / (count - i)
		if share < overhead {
			share = overhead
		}
		env[name] = strings.Repeat("x", share-overhead)
		remaining -= share
	}
	return env
}

func envValueForEntryBytes(name string, entryBytes int) string {
	return strings.Repeat("c", entryBytes-len(name)-2)
}

func joinWarnings(lines []string) string {
	return strings.Join(lines, "\n")
}

func TestRuntimeEnvEntryBytesMatchesExecveAccounting(t *testing.T) {
	// execve 的 copy_strings 统计的是 "KEY=VALUE" 加结尾 NUL。
	if got, want := runtimeEnvEntryBytes("AB", "cde"), len("AB=cde")+1; got != want {
		t.Fatalf("expected entry bytes %d, got %d", want, got)
	}
}

func TestInspectRuntimeEnvSizeReportsTotalAndLargest(t *testing.T) {
	report := inspectRuntimeEnvSize(map[string]string{
		"A":  "1",
		"BB": "2222",
	})

	want := runtimeEnvEntryBytes("A", "1") + runtimeEnvEntryBytes("BB", "2222")
	if report.TotalBytes != want {
		t.Fatalf("expected total %d, got %d", want, report.TotalBytes)
	}
	if report.Largest.Name != "BB" {
		t.Fatalf("expected largest BB, got %q", report.Largest.Name)
	}
	if len(report.Oversized) != 0 {
		t.Fatalf("expected no oversized entries, got %#v", report.Oversized)
	}
}

func TestBuildRuntimeEnvLimitWarningsQuietForSmallEnv(t *testing.T) {
	if lines := buildRuntimeEnvLimitWarnings(map[string]string{"JD_COOKIE": "pt_key=a"}, "python3"); len(lines) != 0 {
		t.Fatalf("expected no warnings for a small env, got %#v", lines)
	}
	if lines := buildRuntimeEnvLimitWarnings(nil, "python3"); len(lines) != 0 {
		t.Fatalf("expected no warnings for empty env, got %#v", lines)
	}
}

func TestBuildRuntimeEnvLimitWarningsAtExactMaxArgStrLen(t *testing.T) {
	// 正好 MAX_ARG_STRLEN（含结尾 NUL）仍然能 exec，不该报警。
	env := map[string]string{
		"JD_COOKIE": envValueForEntryBytes("JD_COOKIE", linuxMaxArgStrLenBytes),
	}
	if got := runtimeEnvEntryBytes("JD_COOKIE", env["JD_COOKIE"]); got != linuxMaxArgStrLenBytes {
		t.Fatalf("test fixture wrong: entry bytes %d", got)
	}

	if lines := buildRuntimeEnvLimitWarnings(env, "python3"); len(lines) != 0 {
		t.Fatalf("expected no warning at exactly MAX_ARG_STRLEN, got:\n%s", joinWarnings(lines))
	}
}

func TestBuildRuntimeEnvLimitWarningsJustOverMaxArgStrLen(t *testing.T) {
	env := map[string]string{
		"JD_COOKIE": envValueForEntryBytes("JD_COOKIE", linuxMaxArgStrLenBytes+1),
	}

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "python3"))
	if text == "" {
		t.Fatal("expected a warning one byte over MAX_ARG_STRLEN")
	}
	for _, marker := range []string{
		runtimeEnvWarningPrefix,
		"JD_COOKIE",
		"Argument list too long",
		"errno 7",
		"E2BIG",
		"子进程",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("expected warning to mention %q, got:\n%s", marker, text)
		}
	}
}

func TestBuildRuntimeEnvLimitWarningsListsEveryOversizedVariable(t *testing.T) {
	env := map[string]string{
		"SMALL":   "ok",
		"BIG_ONE": envValueForEntryBytes("BIG_ONE", linuxMaxArgStrLenBytes+1),
		"BIG_TWO": envValueForEntryBytes("BIG_TWO", linuxMaxArgStrLenBytes+2048),
	}

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "node"))
	if !strings.Contains(text, "BIG_ONE") || !strings.Contains(text, "BIG_TWO") {
		t.Fatalf("expected both oversized names, got:\n%s", text)
	}
	if strings.Contains(text, "SMALL =") {
		t.Fatalf("expected small variable to be left out, got:\n%s", text)
	}
	// 按体积降序，最大的先说。
	if strings.Index(text, "BIG_TWO") > strings.Index(text, "BIG_ONE") {
		t.Fatalf("expected larger variable listed first, got:\n%s", text)
	}
}

func TestBuildRuntimeEnvLimitWarningsShellVariantDoesNotClaimExecFailure(t *testing.T) {
	// bash 任务里超限变量只赋值不 export，不会 E2BIG，只会让子进程读不到，
	// 提示必须说清这个差别，不能照抄 Python/Node 的说法。
	env := map[string]string{
		"JD_COOKIE": envValueForEntryBytes("JD_COOKIE", linuxMaxArgStrLenBytes+1),
	}

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "bash"))
	if !strings.Contains(text, "JD_COOKIE") {
		t.Fatalf("expected shell warning to mention the variable, got:\n%s", text)
	}
	if strings.Contains(text, "Argument list too long") {
		t.Fatalf("shell tasks never hit E2BIG from panel env, warning must not claim it:\n%s", text)
	}
	if !strings.Contains(text, "export") {
		t.Fatalf("expected shell warning to explain the export behaviour, got:\n%s", text)
	}
}

func TestBuildRuntimeEnvLimitWarningsTotalNearArgMax(t *testing.T) {
	env := buildEnvWithTotalBytes(20, linuxArgMaxWarnBytes)
	report := inspectRuntimeEnvSize(env)
	if report.TotalBytes != linuxArgMaxWarnBytes {
		t.Fatalf("test fixture wrong: total %d", report.TotalBytes)
	}
	if len(report.Oversized) != 0 {
		t.Fatalf("test fixture wrong: single entries must stay under MAX_ARG_STRLEN, got %#v", report.Oversized)
	}

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "python3"))
	if !strings.Contains(text, "RLIMIT_STACK/4") {
		t.Fatalf("expected total-size warning, got:\n%s", text)
	}
	if !strings.Contains(text, "errno 7") {
		t.Fatalf("expected total-size warning to name the errno, got:\n%s", text)
	}
}

func TestBuildRuntimeEnvLimitWarningsTotalJustUnderThreshold(t *testing.T) {
	env := buildEnvWithTotalBytes(20, linuxArgMaxWarnBytes-1)
	if got := inspectRuntimeEnvSize(env).TotalBytes; got != linuxArgMaxWarnBytes-1 {
		t.Fatalf("test fixture wrong: total %d", got)
	}

	if lines := buildRuntimeEnvLimitWarnings(env, "python3"); len(lines) != 0 {
		t.Fatalf("expected silence one byte below the threshold, got:\n%s", joinWarnings(lines))
	}
}

func TestBuildRuntimeEnvLimitWarningsSkipsTotalSizeForShell(t *testing.T) {
	// bash 的导出量被 shellEnvExportBudgetBytes 卡死，报总量只会是误报。
	env := buildEnvWithTotalBytes(20, linuxArgMaxWarnBytes)

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "bash"))
	if strings.Contains(text, "RLIMIT_STACK/4") {
		t.Fatalf("shell tasks must not get the ARG_MAX total warning, got:\n%s", text)
	}
}

func TestBuildRuntimeEnvLimitWarningsReportsShellExportBudgetDrops(t *testing.T) {
	env := buildEnvWithTotalBytes(10, shellEnvExportBudgetBytes*2)
	skipped := shellEnvExportSkippedByBudget(env)
	if len(skipped) == 0 {
		t.Fatalf("test fixture wrong: expected some variables to lose the export budget")
	}

	text := joinWarnings(buildRuntimeEnvLimitWarnings(env, "bash"))
	if !strings.Contains(text, "未被 export") {
		t.Fatalf("expected shell export-budget warning, got:\n%s", text)
	}
	if !strings.Contains(text, skipped[0].Name) {
		t.Fatalf("expected dropped variable %q to be listed, got:\n%s", skipped[0].Name, text)
	}
}

func TestPlanShellEnvExportMatchesWriterRules(t *testing.T) {
	env := map[string]string{
		"OK":              "value",
		"LD_PRELOAD":      "/tmp/evil.so",
		"bad-name":        "value",
		"WITH_NUL":        "a\x00b",
		"TOO_LONG":        envValueForEntryBytes("TOO_LONG", shellEnvExportValueMaxBytes+1),
		"EXACT_MAX_VALUE": envValueForEntryBytes("EXACT_MAX_VALUE", shellEnvExportValueMaxBytes),
	}

	plan := planShellEnvExport(env)
	exported := strings.Join(plan.Exported, ",")
	if !strings.Contains(exported, "OK") {
		t.Fatalf("expected OK exported, got %q", exported)
	}
	if !strings.Contains(exported, "EXACT_MAX_VALUE") {
		t.Fatalf("expected value at exactly the cap to still export, got %q", exported)
	}
	for _, name := range []string{"LD_PRELOAD", "bad-name", "WITH_NUL", "TOO_LONG"} {
		if strings.Contains(exported, name) {
			t.Fatalf("expected %s to stay unexported, got %q", name, exported)
		}
	}
	if len(plan.SkippedTooLong) != 1 || plan.SkippedTooLong[0].Name != "TOO_LONG" {
		t.Fatalf("expected TOO_LONG recorded as oversized, got %#v", plan.SkippedTooLong)
	}
	if len(plan.SkippedBudget) != 0 {
		t.Fatalf("expected no budget drops here, got %#v", plan.SkippedBudget)
	}
}

func TestPlanShellEnvExportStopsAtBudget(t *testing.T) {
	env := buildEnvWithTotalBytes(10, shellEnvExportBudgetBytes*2)

	plan := planShellEnvExport(env)
	if len(plan.Exported) == 0 {
		t.Fatal("expected the first variables to still export")
	}
	if len(plan.SkippedBudget) == 0 {
		t.Fatal("expected later variables to lose the budget")
	}

	exportedBytes := 0
	for _, name := range plan.Exported {
		exportedBytes += runtimeEnvEntryBytes(name, env[name])
	}
	if exportedBytes > shellEnvExportBudgetBytes {
		t.Fatalf("exported %d bytes, over budget %d", exportedBytes, shellEnvExportBudgetBytes)
	}
}

func TestEmitRuntimeEnvLimitWarningsFollowsPlatform(t *testing.T) {
	resetRuntimeEnvWarningDedup(t)

	env := map[string]string{
		"JD_COOKIE": envValueForEntryBytes("JD_COOKIE", linuxMaxArgStrLenBytes+1),
	}

	lines := emitWarningsForTest(env, "python3")

	if runtime.GOOS == "windows" {
		// Windows 没有 MAX_ARG_STRLEN / ARG_MAX 这套限制，提示只会是噪音。
		if len(lines) != 0 {
			t.Fatalf("expected no warnings on windows, got:\n%s", joinWarnings(lines))
		}
		return
	}
	if len(lines) == 0 {
		t.Fatal("expected warnings to be emitted on unix")
	}
}

func TestEmitRuntimeEnvLimitWarningsToleratesNilSink(t *testing.T) {
	emitRuntimeEnvLimitWarnings(map[string]string{"A": "b"}, "python3", nil)
}

func TestEmitRuntimeEnvLimitWarningsReportsEachSituationOnlyOnce(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	env := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1})

	first := emitWarningsForTest(env, "python3")
	if len(first) == 0 {
		t.Fatal("expected the first run to report")
	}

	// 用户已经知道这回事了，再报就是噪音：第二次必须**完全**静默，
	// 连「详见首次告警」这种一行简报都不能留。
	if second := emitWarningsForTest(env, "python3"); len(second) != 0 {
		t.Fatalf("expected total silence on the second run, got:\n%s", joinWarnings(second))
	}
}

func TestEmitRuntimeEnvLimitWarningsStaysSilentWhileTheSameVariableKeepsGrowing(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	if len(emitWarningsForTest(taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1}), "python3")) == 0 {
		t.Fatal("expected the first run to report")
	}

	// 198 KB 涨到 297.6 KB 是「同一个问题在恶化」，不是新情况。
	grown := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes * 4})
	if lines := emitWarningsForTest(grown, "python3"); len(lines) != 0 {
		t.Fatalf("a bigger value is the same problem, expected silence, got:\n%s", joinWarnings(lines))
	}
}

func TestEmitRuntimeEnvLimitWarningsReportsAgainWhenAnotherVariableGoesOversized(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	if len(emitWarningsForTest(taskEnvWith(map[string]int{"elmck1": linuxMaxArgStrLenBytes + 1}), "python3")) == 0 {
		t.Fatal("expected the first run to report")
	}

	// 多出一条超限变量是新情况，必须重新告警。
	// 顺带盯住指纹别把 elmck1 / elmck2 当成同一条（数字不能被抹掉）。
	both := taskEnvWith(map[string]int{
		"elmck1": linuxMaxArgStrLenBytes + 1,
		"elmck2": linuxMaxArgStrLenBytes + 1,
	})
	lines := emitWarningsForTest(both, "python3")
	if len(lines) == 0 {
		t.Fatal("expected a fresh warning after a new variable went oversized")
	}
	if text := joinWarnings(lines); !strings.Contains(text, "elmck2") {
		t.Fatalf("expected the new variable to be named, got:\n%s", text)
	}
}

func TestEmitRuntimeEnvLimitWarningsReportsAgainAfterTheVariableWasRemovedAndCameBack(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	broken := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1})
	if len(emitWarningsForTest(broken, "python3")) == 0 {
		t.Fatal("expected the first run to report")
	}

	// 用户在环境变量页把它删了 / 停用了：本来就没什么可报的。
	if lines := emitWarningsForTest(taskEnvWith(nil), "python3"); len(lines) != 0 {
		t.Fatalf("expected silence once the variable is gone, got:\n%s", joinWarnings(lines))
	}

	// 又加回来并且又超限了：用户以为这事已经过去了，所以这是新情况，得重新报。
	if lines := emitWarningsForTest(broken, "python3"); len(lines) == 0 {
		t.Fatal("expected a fresh warning after the variable came back oversized")
	}
}

func TestEmitRuntimeEnvLimitWarningsKeepsSilenceWhenOneTaskNarrowsTheVariable(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	broken := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1})
	if len(emitWarningsForTest(broken, "python3")) == 0 {
		t.Fatal("expected the first run to report")
	}

	// `task 脚本 desi elmck 1-2` 会把变量收窄成几个账号：键还在，值变小了。
	// 这不代表用户修好了，绝不能当成痊愈信号 —— 否则下面那个普通任务又会报一遍，
	// 两种任务交替跑就等于告警从来没被静音过。
	narrowed := taskEnvWith(map[string]int{"elmck": 1024})
	if lines := emitWarningsForTest(narrowed, "python3"); len(lines) != 0 {
		t.Fatalf("a narrowed run has nothing to warn about, got:\n%s", joinWarnings(lines))
	}
	if lines := emitWarningsForTest(broken, "python3"); len(lines) != 0 {
		t.Fatalf("a narrowed run must not lift the mute, got:\n%s", joinWarnings(lines))
	}
}

func TestEmitRuntimeEnvLimitWarningsSeparatesShellFromOtherInterpreters(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	env := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1})

	// 同一组变量，两种解释器的后果和文案完全不同，各自都值得报一次。
	shell := joinWarnings(emitWarningsForTest(env, "bash"))
	if !strings.Contains(shell, "export") {
		t.Fatalf("expected the shell wording first, got:\n%s", shell)
	}
	if lines := emitWarningsForTest(env, "bash"); len(lines) != 0 {
		t.Fatalf("expected the second bash run to stay silent, got:\n%s", joinWarnings(lines))
	}

	other := joinWarnings(emitWarningsForTest(env, "python3"))
	if !strings.Contains(other, "Argument list too long") {
		t.Fatalf("expected the non-shell wording to still be reported once, got:\n%s", other)
	}
	if lines := emitWarningsForTest(env, "python3"); len(lines) != 0 {
		t.Fatalf("expected the second python run to stay silent, got:\n%s", joinWarnings(lines))
	}
}

func TestEmitRuntimeEnvLimitWarningsReportsOnceUnderConcurrentTaskStarts(t *testing.T) {
	requireRuntimeEnvWarningEmit(t)
	resetRuntimeEnvWarningDedup(t)

	env := taskEnvWith(map[string]int{"elmck": linuxMaxArgStrLenBytes + 1})
	wantLines := len(buildRuntimeEnvLimitWarnings(env, "python3"))
	if wantLines == 0 {
		t.Fatal("test fixture wrong: expected this env to produce warnings")
	}

	const runners = 16
	var mu sync.Mutex
	counted := make([]int, runners)

	var waitGroup sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < runners; i++ {
		i := i
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			emitRuntimeEnvLimitWarnings(env, "python3", func(string) {
				mu.Lock()
				counted[i]++
				mu.Unlock()
			})
		}()
	}
	close(start)
	waitGroup.Wait()

	reporters, total := 0, 0
	for _, lines := range counted {
		total += lines
		if lines > 0 {
			reporters++
		}
	}
	if reporters != 1 {
		t.Fatalf("expected exactly one task to report, got %d tasks / %d lines", reporters, total)
	}
	// 报出来的那一份必须是完整的，不能被别的 goroutine 抢成半截。
	if total != wantLines {
		t.Fatalf("expected the single report to carry all %d lines, got %d", wantLines, total)
	}
}

func TestRuntimeEnvWarningFingerprintIgnoresSizeButNotNames(t *testing.T) {
	factsFor := func(sizes map[string]int, interpreter string) runtimeEnvWarningFacts {
		return collectRuntimeEnvWarningFacts(taskEnvWith(sizes), interpreter)
	}
	keyOf := func(facts runtimeEnvWarningFacts) string {
		fingerprint, _ := runtimeEnvWarningFingerprint(facts)
		return fingerprint
	}

	small := keyOf(factsFor(map[string]int{"elmck1": linuxMaxArgStrLenBytes + 1}, "python3"))
	grown := keyOf(factsFor(map[string]int{"elmck1": linuxMaxArgStrLenBytes * 4}, "python3"))
	if small != grown {
		t.Fatal("the same variable getting bigger must keep the same fingerprint")
	}

	// 变量名里的数字必须原样参与指纹，否则 elmck1 / elmck2 会被当成同一条。
	renamed := keyOf(factsFor(map[string]int{"elmck2": linuxMaxArgStrLenBytes + 1}, "python3"))
	if renamed == small {
		t.Fatal("elmck1 and elmck2 must not share a fingerprint")
	}

	added := keyOf(factsFor(map[string]int{
		"elmck1": linuxMaxArgStrLenBytes + 1,
		"elmck2": linuxMaxArgStrLenBytes + 1,
	}, "python3"))
	if added == small {
		t.Fatal("one more oversized variable is a new situation")
	}

	if shell := keyOf(factsFor(map[string]int{"elmck1": linuxMaxArgStrLenBytes + 1}, "bash")); shell == small {
		t.Fatal("bash and non-bash warnings must not share a fingerprint")
	}

	_, subjects := runtimeEnvWarningFingerprint(factsFor(map[string]int{"elmck1": linuxMaxArgStrLenBytes + 1}, "python3"))
	if len(subjects) != 1 || subjects[0] != "elmck1" {
		t.Fatalf("expected the offending variable to be tracked, got %#v", subjects)
	}
}

func TestRuntimeEnvWarningDedupTableStaysBounded(t *testing.T) {
	// 环境变量名不变，所以没有任何指纹会因为「变量消失了」被回收，
	// 表只可能靠上限收住。
	deduper := &runtimeEnvWarningDeduper{}
	env := map[string]string{"KEEP": "1"}

	for i := 0; i < runtimeEnvWarningDedupCap*2+5; i++ {
		fingerprint := fmt.Sprintf("fake-%d", i)
		if !deduper.shouldReport(env, fingerprint, []string{"KEEP"}) {
			t.Fatalf("expected fingerprint %q to be reported once", fingerprint)
		}
	}

	deduper.mu.Lock()
	size := len(deduper.seen)
	deduper.mu.Unlock()
	if size > runtimeEnvWarningDedupCap {
		t.Fatalf("dedup table grew to %d entries, over the cap %d", size, runtimeEnvWarningDedupCap)
	}
}
