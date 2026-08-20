package service

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Linux 上 "[Errno 7] Argument list too long"（E2BIG）只可能来自 execve 的两道墙：
//
//  1. MAX_ARG_STRLEN = PAGE_SIZE * 32 = 128 KiB，限制**单个** argv/envp 字符串。
//     一条 "KEY=VALUE\0" 只要超过它，任何 exec 都会失败，和环境总量无关。
//  2. argv+envp 的**总大小**，内核取 min(_STK_LIM/4*3, RLIMIT_STACK/4)，
//     默认 8 MiB 栈下约等于 2 MiB。
//
// 面板自己 exec 脚本时撞不上这两堵墙：任务变量走 env 文件传递，真实进程环境
// （buildBootstrapProcessEnv）只有 PATH/HOME/TZ 等十来个键。但 Python / Node / Go
// 的 bootstrap 会把 env 文件里的变量**全量还原**进 os.environ / process.env，
// 脚本随后自己 spawn 子进程（subprocess、child_process、直接调 node/python）时，
// 子进程会继承这份巨大的环境 —— 墙是在脚本内部才被撞破的。
//
// 结果就是用户只看到脚本自己抛出的 "[Errno 7]"，完全定位不到面板，也想不到
// 「面板能跑起来，但脚本一开子进程就炸」这种反直觉现象。所以这里在启动脚本之前
// 就把风险摊开写进任务日志。
const (
	// linuxMaxArgStrLenBytes 对应内核的 MAX_ARG_STRLEN。
	// 口径与 copy_strings 一致：统计 "KEY=VALUE" 再加结尾 NUL。
	linuxMaxArgStrLenBytes = 128 * 1024
	// linuxTypicalArgMaxBytes 是默认 8 MiB 栈下 argv+envp 的实际总上限。
	// 真实值取 RLIMIT_STACK/4，这里只用于提示，不做精确判定。
	linuxTypicalArgMaxBytes = 2 * 1024 * 1024
	// linuxArgMaxWarnBytes 是总量提示阈值：到 75% 就提醒，别等真炸了才说。
	linuxArgMaxWarnBytes = linuxTypicalArgMaxBytes / 4 * 3
)

const runtimeEnvWarningPrefix = "[环境变量告警] "

// runtimeEnvEntrySize 记录一条环境变量在 execve 里实际占用的字节数。
type runtimeEnvEntrySize struct {
	Name  string
	Bytes int
}

// runtimeEnvSizeReport 是一次任务环境的体检结果。
type runtimeEnvSizeReport struct {
	// TotalBytes 是所有变量按 execve 口径累加的字节数。
	TotalBytes int
	// Oversized 是单条就超过 MAX_ARG_STRLEN 的变量，按大小降序。
	Oversized []runtimeEnvEntrySize
	// Largest 是最大的一条，用于总量超标时给出线索。
	Largest runtimeEnvEntrySize
}

// runtimeEnvEntryBytes 返回一条 envp 字符串的真实长度：len("KEY=VALUE") + 1（结尾 NUL）。
func runtimeEnvEntryBytes(name, value string) int {
	return len(name) + 1 + len(value) + 1
}

func inspectRuntimeEnvSize(envVars map[string]string) runtimeEnvSizeReport {
	report := runtimeEnvSizeReport{}
	for name, value := range envVars {
		entry := runtimeEnvEntrySize{Name: name, Bytes: runtimeEnvEntryBytes(name, value)}
		report.TotalBytes += entry.Bytes
		if entry.Bytes > report.Largest.Bytes ||
			(entry.Bytes == report.Largest.Bytes && entry.Name < report.Largest.Name) {
			report.Largest = entry
		}
		if entry.Bytes > linuxMaxArgStrLenBytes {
			report.Oversized = append(report.Oversized, entry)
		}
	}
	sortRuntimeEnvEntries(report.Oversized)
	return report
}

func sortRuntimeEnvEntries(entries []runtimeEnvEntrySize) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Bytes != entries[j].Bytes {
			return entries[i].Bytes > entries[j].Bytes
		}
		return entries[i].Name < entries[j].Name
	})
}

// runtimeEnvWarningFacts 是一次环境体检的结构化结论：谁超限、总量是否接近上限、
// bash 的导出预算又挤掉了谁。
//
// 抽出这一层是为了让「日志文案」和「去重指纹」共用同一份事实：
// renderRuntimeEnvWarningLines 拿它渲染日志行，runtimeEnvWarningFingerprint 拿它算去重键。
//
// 【维护提醒】以后新增告警类别时，除了在 renderRuntimeEnvWarningLines 里加文案，
// 必须同时把它带进 runtimeEnvWarningFingerprint。否则新类别会被同一套环境算出的旧指纹
// 静音掉，用户一次都看不到。
type runtimeEnvWarningFacts struct {
	// IsShell 区分 bash 与其它解释器：两者的后果和文案完全不同。
	IsShell bool
	// Oversized 是单条就超过 MAX_ARG_STRLEN 的变量，按体积降序。
	Oversized []runtimeEnvEntrySize
	// TotalNearLimit 表示总量已到 ARG_MAX 提示阈值（只对非 bash 判定）。
	TotalNearLimit bool
	// TotalBytes、Largest 供总量文案使用。
	TotalBytes int
	Largest    runtimeEnvEntrySize
	// BudgetSkipped 是 bash 下因为导出预算被挤掉的变量，按体积降序。
	BudgetSkipped []runtimeEnvEntrySize
}

// isEmpty 表示这次体检没什么可说的。
func (f runtimeEnvWarningFacts) isEmpty() bool {
	return len(f.Oversized) == 0 && !f.TotalNearLimit && len(f.BudgetSkipped) == 0
}

// collectRuntimeEnvWarningFacts 只做判定，不产出任何文案。
// interpreter 用来区分两种完全不同的后果：
//   - bash：面板已经不会 export 超限变量，脚本自己读得到，但子进程读不到（静默为空）
//   - 其它：变量会完整进入 os.environ / process.env，子进程 exec 时直接 E2BIG
func collectRuntimeEnvWarningFacts(envVars map[string]string, interpreter string) runtimeEnvWarningFacts {
	if len(envVars) == 0 {
		return runtimeEnvWarningFacts{}
	}

	isShell := strings.TrimSpace(interpreter) == "bash"
	report := inspectRuntimeEnvSize(envVars)
	facts := runtimeEnvWarningFacts{
		IsShell:    isShell,
		Oversized:  report.Oversized,
		TotalBytes: report.TotalBytes,
		Largest:    report.Largest,
	}

	// bash 的导出总量本来就被 shellEnvExportBudgetBytes 卡住，不会撞 ARG_MAX，
	// 这里再提总量只会变成误报，所以只对其它解释器判定。
	facts.TotalNearLimit = !isShell && report.TotalBytes >= linuxArgMaxWarnBytes
	if isShell {
		facts.BudgetSkipped = shellEnvExportSkippedByBudget(envVars)
	}
	return facts
}

// buildRuntimeEnvLimitWarnings 产出可直接写进任务日志的提示行（不带结尾换行）。
// 保持纯函数：同样的入参永远得到同样的结果，不受「这条告警之前报没报过」影响。
// 去重是 emitRuntimeEnvLimitWarnings 那一层的事。
func buildRuntimeEnvLimitWarnings(envVars map[string]string, interpreter string) []string {
	return renderRuntimeEnvWarningLines(collectRuntimeEnvWarningFacts(envVars, interpreter))
}

func renderRuntimeEnvWarningLines(facts runtimeEnvWarningFacts) []string {
	if facts.isEmpty() {
		return nil
	}

	lines := make([]string, 0, 8)

	if len(facts.Oversized) > 0 {
		lines = append(lines, fmt.Sprintf(
			"检测到 %d 条环境变量超过 Linux 单个环境变量上限 %s（MAX_ARG_STRLEN = PAGE_SIZE * 32）：",
			len(facts.Oversized), formatEnvByteSize(linuxMaxArgStrLenBytes)))
		for _, entry := range facts.Oversized {
			lines = append(lines, fmt.Sprintf("  %s = %s", entry.Name, formatEnvByteSize(entry.Bytes)))
		}
		if facts.IsShell {
			lines = append(lines,
				"bash 任务里这类变量只会作为普通 shell 变量存在、不会 export，脚本内 $变量 仍然读得到，",
				"但脚本启动的子进程（node / python 等）不会继承到它，在子进程里取值为空。")
		} else {
			lines = append(lines,
				"面板通过 env 文件注入，脚本自身读取不受影响；但脚本内再启动子进程时（Python subprocess、",
				"Node child_process、或脚本里直接调 node / python），子进程会继承这条超长变量，",
				`execve 立即失败并报 "Argument list too long"（errno 7 / E2BIG）。`,
				"这就是「面板能把脚本跑起来，脚本一开子进程就炸」的原因。")
		}
		lines = append(lines,
			"处理建议：把账号拆成多条同名环境变量或多个变量名；用 `task 脚本 desi 变量名 1-20` /",
			"`task 脚本 conc 变量名` 按账号分批执行；或改用文件传递（变量里只放文件路径）。")
	}

	if facts.TotalNearLimit {
		lines = append(lines, fmt.Sprintf(
			"任务环境变量合计 %s，已接近 Linux execve 参数总上限（约 %s，实际取 RLIMIT_STACK/4）。",
			formatEnvByteSize(facts.TotalBytes), formatEnvByteSize(linuxTypicalArgMaxBytes)))
		if facts.Largest.Name != "" {
			lines = append(lines, fmt.Sprintf(
				`脚本内启动子进程时可能报 "Argument list too long"（errno 7 / E2BIG）。最大的一条是 %s（%s）。`,
				facts.Largest.Name, formatEnvByteSize(facts.Largest.Bytes)))
		}
		lines = append(lines, "处理建议：在「环境变量」页禁用或清理任务用不到的变量。")
	}

	if skipped := facts.BudgetSkipped; len(skipped) > 0 {
		lines = append(lines, fmt.Sprintf(
			"另有 %d 条环境变量因为导出总量超过 %s 未被 export，bash 任务的子进程读不到它们：",
			len(skipped), formatEnvByteSize(shellEnvExportBudgetBytes)))
		for _, entry := range skipped {
			lines = append(lines, fmt.Sprintf("  %s = %s", entry.Name, formatEnvByteSize(entry.Bytes)))
		}
	}

	if len(lines) == 0 {
		return nil
	}

	prefixed := make([]string, 0, len(lines))
	for _, line := range lines {
		prefixed = append(prefixed, runtimeEnvWarningPrefix+line)
	}
	return prefixed
}

// runtimeEnvWarningFingerprint 把一次告警压成「结构指纹」，同时返回它涉及的变量名。
//
// 指纹只带**变量名集合 + 告警类别 + 解释器种类**，刻意不带任何字节数：
// 同一条变量从 198 KB 涨到 297.6 KB，是「同一个问题在恶化」，用户第一次就已经知道了，
// 每跑一次任务再刷十行只是噪音。但只要超限的变量名集合变了（多一条、少一条、换一条），
// 那就是新情况，必须重新告警。
//
// 解释器种类也必须进指纹：同一组变量在 bash 任务和 python 任务里后果完全不同
// （前者是子进程读不到，后者是 execve 直接 E2BIG），文案也完全不同，各报一次才说得清。
func runtimeEnvWarningFingerprint(facts runtimeEnvWarningFacts) (string, []string) {
	oversized := sortedRuntimeEnvEntryNames(facts.Oversized)
	budget := sortedRuntimeEnvEntryNames(facts.BudgetSkipped)

	scope := "exec"
	if facts.IsShell {
		scope = "shell"
	}
	total := "0"
	if facts.TotalNearLimit {
		total = "1"
	}

	// 用 NUL / SOH 当分隔符：环境变量名来自 C 字符串，不可能含这两个字节，
	// 所以 {"A,B"} 和 {"A","B"} 不会撞成同一个键。
	fingerprint := strings.Join([]string{
		scope,
		strings.Join(oversized, "\x00"),
		total,
		strings.Join(budget, "\x00"),
	}, "\x01")

	subjects := make([]string, 0, len(oversized)+len(budget))
	subjects = append(subjects, oversized...)
	subjects = append(subjects, budget...)
	return fingerprint, subjects
}

// sortedRuntimeEnvEntryNames 只取变量名并排序，保证同一组变量无论体积怎么变、
// 谁排在前面，都算出同一个键。
func sortedRuntimeEnvEntryNames(entries []runtimeEnvEntrySize) []string {
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// runtimeEnvWarningDedupCap 是指纹表的条数上限。
//
// 正常情况下指纹种类很少：环境变量表是全局的，所有任务读到的是同一份，
// 只有 desi / conc 会把某一条变量收窄，从而算出不同的指纹，所以量级顶天跟任务数同阶。
// 这里设上限纯粹是防御，避免脚本动态生成变量名时这张表无限涨。
// 到顶就整体清空重来：代价只是那批老告警可能再报一次，不会漏报，也不会泄漏内存。
const runtimeEnvWarningDedupCap = 512

// runtimeEnvWarningDeduper 记录本进程已经报过哪些告警。
type runtimeEnvWarningDeduper struct {
	mu sync.Mutex

	// seen 是「指纹 -> 这条告警涉及的变量名」，
	// 后者用来判断用户是不是已经把这些变量从环境里删掉了。
	seen map[string][]string
}

// runtimeEnvWarningsSeen 是进程内的去重表。
//
// 刻意只放内存、不落库：面板重启后重报一次是**有价值的**，那是一个自然的提醒点，
// 让用户知道这个问题还在。真持久化了，这条提醒就永远消失了。
// 面板重启并不频繁，对用户来说实际效果依然是「只报一次」。
// 所以下面看不到任何写库代码不是忘了，是刻意的取舍。
var runtimeEnvWarningsSeen = &runtimeEnvWarningDeduper{}

// shouldReport 先忘掉那些「涉及的变量已经从环境里彻底消失」的旧告警，
// 再判断本次指纹是不是第一次出现。
//
// 两件事必须在同一把锁里做完：多个任务同时启动时，判断和登记之间一旦松开锁，
// 就会有两个任务同时认定自己是第一个，同一条告警被刷两遍。
// fingerprint 为空表示本次没有任何告警，那就只做前半段的遗忘。
func (d *runtimeEnvWarningDeduper) shouldReport(envVars map[string]string, fingerprint string, subjects []string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.forgetVanishedLocked(envVars)

	if fingerprint == "" {
		return false
	}
	if _, reported := d.seen[fingerprint]; reported {
		return false
	}
	if len(d.seen) >= runtimeEnvWarningDedupCap {
		d.seen = nil
	}
	if d.seen == nil {
		d.seen = make(map[string][]string, 8)
	}
	d.seen[fingerprint] = subjects
	return true
}

// forgetVanishedLocked 处理「用户修好了，后来又坏了」。
//
// 修好又坏算新情况：用户是照着告警动手改的，他认为这事已经过去了，再坏一次他并不知道。
// 但「修好了」只认一种证据 —— 相关变量在环境里**彻底不见了**（在环境变量页删掉或停用）。
//
// 为什么不认「变量还在、只是变小了」：`task 脚本 desi 变量名 1-20` / `conc` 会把变量收窄成
// 几个账号，值随之变小，键却还在。要是把「变小了」当痊愈信号，一个 desi 任务跑一次就会解除静音，
// 下一个普通任务立刻又报一遍，告警重新开始刷屏 —— 恰好是这次要修的问题。
// 而「键不见了」不会被 desi / conc 伪造：它们只改值不删键，且每个任务读到的都是同一份
// 全局启用变量表（BuildManagedRuntimeEnvMapWithScriptToken），所以键消失只可能是用户真的动了环境变量。
func (d *runtimeEnvWarningDeduper) forgetVanishedLocked(envVars map[string]string) {
	// 空环境不算证据：正常任务环境至少有 PATH / TZ 这些键，
	// 真拿到空 map 多半是调用方异常，不能据此把所有静音都解除。
	if len(d.seen) == 0 || len(envVars) == 0 {
		return
	}
	for fingerprint, subjects := range d.seen {
		if len(subjects) == 0 {
			continue
		}
		vanished := true
		for _, name := range subjects {
			if _, exists := envVars[name]; exists {
				vanished = false
				break
			}
		}
		if vanished {
			delete(d.seen, fingerprint)
		}
	}
}

// emitRuntimeEnvLimitWarnings 把提示写进任务日志，同一条告警在本进程内只写一次。
// Windows 没有 MAX_ARG_STRLEN / ARG_MAX 这套限制，直接跳过，避免误报。
func emitRuntimeEnvLimitWarnings(envVars map[string]string, interpreter string, emit func(line string)) {
	if emit == nil || runtime.GOOS == "windows" {
		return
	}

	facts := collectRuntimeEnvWarningFacts(envVars, interpreter)

	// 先把文案渲染出来，再决定要不要登记指纹：登记的前提是「这次确实有东西要报」。
	// 反过来先登记后渲染的话，一旦渲染出了空结果，这条告警就会被永久静音，谁也看不到。
	lines := renderRuntimeEnvWarningLines(facts)
	fingerprint := ""
	var subjects []string
	if len(lines) > 0 {
		fingerprint, subjects = runtimeEnvWarningFingerprint(facts)
	}

	// 没告警时也要走一趟：这次体检本身就是「变量已经不在了」的证据来源。
	if !runtimeEnvWarningsSeen.shouldReport(envVars, fingerprint, subjects) {
		// 用户要的是「只显示一次」，所以第二次开始完全静默：
		// 连「详见首次告警」这种一行简报都不留，否则每跑一次任务照样刷一行。
		return
	}

	for _, line := range lines {
		emit(line)
	}
}

func formatEnvByteSize(size int) string {
	switch {
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
