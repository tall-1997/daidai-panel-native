package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// hookEnvDumpPathEnvKey 是「前置钩子环境变量回传」的总开关，也是 dump 文件的目标路径。
//
// 它必须是一个**只在任务前置链路才传**的环境变量：RunInlineScript 除了任务前置脚本
// 还有第三个调用方 —— 订阅钩子（subscription_hook.go），RunHookScript 也同时服务
// task_after.sh / extra.sh。把 dump 逻辑无条件做进 shell bootstrap 会改变这些调用方的
// 输出与退出码，所以 shellEnvBootstrap 里那段采集代码以「这个变量是否非空」做 no-op 门禁。
//
// 名字带 DAIDAI_ 前缀还有第二重作用：它天然落在 hookEnvContractPrefixes 里，
// 前置脚本改它也回传不回来，不会自己把自己的采集路径改掉。
const hookEnvDumpPathEnvKey = "DAIDAI_HOOK_ENV_DUMP"

// hookEnvVolatileNames 是 shell 自己维护、每一跳都会变的内部变量。
//
// 它们一律**静默**丢弃：前置脚本里一句 cd 就会让 PWD / OLDPWD 与基线不同，
// 报进任务日志纯属噪音；而 IFS / PS4 / OPTIND 这类真被带进任务环境，
// 还会改坏后续脚本的解析行为。
var hookEnvVolatileNames = map[string]struct{}{
	"_":          {},
	"PWD":        {},
	"OLDPWD":     {},
	"SHLVL":      {},
	"RANDOM":     {},
	"SECONDS":    {},
	"LINENO":     {},
	"PPID":       {},
	"EUID":       {},
	"UID":        {},
	"IFS":        {},
	"OPTIND":     {},
	"PS1":        {},
	"PS2":        {},
	"PS3":        {},
	"PS4":        {},
	"COLUMNS":    {},
	"LINES":      {},
	"HISTFILE":   {},
	"FUNCNAME":   {},
	"GROUPS":     {},
	"DIRSTACK":   {},
	"PIPESTATUS": {},
}

// hookEnvVolatilePrefixes 同理，覆盖 bash 自身的内部变量族
// （BASH、BASH_VERSION、BASH_SOURCE、BASHOPTS、COMP_WORDBREAKS……）。
var hookEnvVolatilePrefixes = []string{"BASH", "COMP_"}

// hookEnvContractNames 是面板的运行时契约变量，用户是**有意**去改的，
// 所以拦下来之后必须回一条日志，否则他会一直以为改生效了。
//
// TZ：面板时区是全局运行时配置，契约上必须覆盖用户同名变量
// （见 quality-guidelines.md 的任务环境变量一节，实现在 BuildManagedRuntimeEnvMap*）。
// buildBootstrapProcessEnv 直接读 envVars["TZ"] 决定子进程时区，Windows 下 Python
// 还要据此换算 POSIX 固定偏移。前置脚本一句 `export TZ=UTC` 就能静默推翻整条时区链路。
var hookEnvContractNames = map[string]struct{}{
	"TZ": {},
}

// hookEnvContractPrefixes 目前只有 DAIDAI_：面板注入的运行时契约变量，全部不接受回传。
//
// 这里最危险的一条是 DAIDAI_NOTIFY_CHANNEL_ID —— 前置脚本把它改空，任务通知就会从
// 「定向到绑定渠道」静默退化成广播，脚本里、日志里都不会有任何提示。同理还有
// DAIDAI_TOKEN / DAIDAI_API_BASE / DAIDAI_NOTIFY_* / DAIDAI_SCRIPTS_DIR /
// DAIDAI_PYTHON_VERSION，以及 DAIDAI_RUNTIME_SHELL_ENV_FILE 和上面那个 dump 路径本身。
// 真要支持「前置脚本切换通知渠道」，应该另开显式机制，不能让它从 env 漏进去。
//
// 注意 PATH **不在**任何保护名单里：托管解释器的解析只在面板自己 PATH 算出的目录里找、
// 用绝对路径 exec（resolveManagedBinary + sanitizeManagedPath），完全不受 envVars["PATH"] 影响；
// envVars["PATH"] 只决定脚本自己 fork 出来的 pip / npm / git 用哪个 PATH，
// 那正是 shell 语义下用户想要的效果。
var hookEnvContractPrefixes = []string{"DAIDAI_"}

// hookEnvRuntimeNotice 是一条「运行时关键变量被整体覆盖」的诊断提示素材：
// symptom 说清坏掉之后会以什么现象出现，appendHint 给出用户真正需要的追加写法。
type hookEnvRuntimeNotice struct {
	symptom    string
	appendHint string
}

// hookEnvRuntimeCriticalNames 是「照常生效、但值得提醒一句」的运行时关键变量。
//
// 为什么是提示而不是保护：这四个键都是 PATH 类语义，「整体覆盖」和「追加」在 shell 里
// 都是合法且常见的写法——用户完全可能就是想把某个目录顶到最前面。把它们加进保护名单，
// 等于连 `export PYTHONPATH=/my/lib:$PYTHONPATH` 这种**正确的追加写法**也一并挡掉，
// 那是比问题本身更糟的回归。对照 hookEnvContractNames 里的 TZ：那类变量用户改了只会
// 静默破坏面板自己的链路，没有任何合法用途，所以才真拦。
//
// 但它们又确实都是面板注入的：
//   - PYTHONPATH / NODE_PATH / NODE_OPTIONS 由 AppendScriptHelperPaths 注入
//     （venv 的 site-packages、托管 node_modules、sendNotify.js 的 --require）；
//   - PATH 由 BuildManagedRuntimeEnvMapWithScriptToken 注入。
//
// 被整体覆盖之后坏掉的方式极难定位：目标脚本突然找不到全部已装依赖，或者脚本自己
// fork 出来的嵌套 node 进程静默失去 notify 注入（目标脚本本身走 createManagedNodeCommand
// 里显式的 --require，不受影响，所以更难联想到是 NODE_OPTIONS 被冲掉）。
// 折中做法就是：照常生效，额外补一行带「追加写法」的可诊断提示。
var hookEnvRuntimeCriticalNames = map[string]hookEnvRuntimeNotice{
	"PATH": {
		symptom:    "脚本里 fork 出的 pip / npm / git 等命令找不到",
		appendHint: `export PATH=...:$PATH`,
	},
	"PYTHONPATH": {
		symptom:    "Python 脚本报找不到已安装的依赖模块",
		appendHint: `export PYTHONPATH=...:$PYTHONPATH`,
	},
	"NODE_PATH": {
		symptom:    "Node 脚本报找不到已安装的 node_modules",
		appendHint: `export NODE_PATH=...:$NODE_PATH`,
	},
	"NODE_OPTIONS": {
		symptom:    "脚本再 fork 出来的 node 进程拿不到 sendNotify 注入",
		appendHint: `export NODE_OPTIONS="$NODE_OPTIONS ..."`,
	},
}

// hookEnvRuntimeOverrideNotice 判断一次**已经生效**的覆盖要不要额外提示一句。
//
// before 是面板注入的旧值。旧值原样保留在新值里 = 用户用的正是追加写法，
// 这恰恰是我们希望的用法，一个字都不该多打；面板压根没往这个键注入过东西时同理。
func hookEnvRuntimeOverrideNotice(name, before, after string) string {
	notice, ok := hookEnvRuntimeCriticalNames[name]
	if !ok {
		return ""
	}
	if strings.TrimSpace(before) == "" {
		return ""
	}
	if strings.Contains(after, before) {
		return ""
	}
	return fmt.Sprintf(
		"[前置脚本环境变量] 注意：%s 是面板注入的运行时变量，已被前置脚本整体覆盖；若%s，请改用 %s 的追加写法",
		name, notice.symptom, notice.appendHint,
	)
}

// hookEnvProtection 返回「这个键要不要拦」以及「拦下来要不要写进任务日志」。
func hookEnvProtection(name string) (protected bool, report bool) {
	if _, ok := hookEnvContractNames[name]; ok {
		return true, true
	}
	for _, prefix := range hookEnvContractPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true, true
		}
	}
	if _, ok := hookEnvVolatileNames[name]; ok {
		return true, false
	}
	for _, prefix := range hookEnvVolatilePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true, false
		}
	}
	return false, false
}

// hookEnvCapture 是一次前置钩子执行的环境采集现场：一个临时目录 + 两个 dump 文件，
// 外加一份带 dump 路径的 envVars 副本（副本而不是原地加键，是为了不把开关泄漏给目标脚本）。
type hookEnvCapture struct {
	dir          string
	finalPath    string
	baselinePath string

	// hookEnv 是交给钩子进程的环境。除了多一条 dump 路径，其余与传入的 envVars 完全一致。
	hookEnv map[string]string
}

func newHookEnvCapture(envVars map[string]string) (*hookEnvCapture, error) {
	dir, err := os.MkdirTemp("", "daidai-hook-env-*")
	if err != nil {
		return nil, err
	}

	// dump 文件名固定，基线文件由 shell 侧在同一路径后面追加 .base，两边约定要一致。
	// 路径统一转成正斜杠：这个值要交给 bash 做重定向目标，Windows 下的 Git Bash
	// 对 C:\a\b 这种反斜杠路径会把反斜杠当转义符，C:/a/b 才是两边都认的写法
	// （Go 在 Windows 上同样接受正斜杠）。
	finalPath := filepath.ToSlash(filepath.Join(dir, "hook-env.dump"))

	// .ok 标记：shell 侧除了「变量非空」还要求它存在才装采集逻辑。
	// 用户在「环境变量」页手建一条同名变量时，因为拿不到这个标记，只会空转，
	// 不会拿 `>` 去截断他填的那个路径。
	if err := os.WriteFile(finalPath+".ok", nil, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	hookEnv := make(map[string]string, len(envVars)+1)
	for key, value := range envVars {
		hookEnv[key] = value
	}
	hookEnv[hookEnvDumpPathEnvKey] = finalPath

	return &hookEnvCapture{
		dir:          dir,
		finalPath:    finalPath,
		baselinePath: finalPath + ".base",
		hookEnv:      hookEnv,
	}, nil
}

func (c *hookEnvCapture) close() {
	if c == nil || c.dir == "" {
		return
	}
	_ = os.RemoveAll(c.dir)
}

// mergeInto 把钩子里 export 的变量增量合并回 envVars，并把结果写进任务日志。
func (c *hookEnvCapture) mergeInto(envVars map[string]string, onOutput OnOutputFunc) {
	if c == nil || envVars == nil {
		return
	}

	baselineRaw, baselineErr := os.ReadFile(c.baselinePath)
	if baselineErr != nil || len(baselineRaw) == 0 {
		// 基线都没落盘，说明 bootstrap 压根没跑起来（钩子文件不存在、bash 起不来）。
		// 这是最常见的正常情况（绝大多数用户没有全局 task_before.sh），必须完全静默，
		// 否则每个任务都要平白多出一行噪音。
		return
	}

	finalRaw, finalErr := os.ReadFile(c.finalPath)
	if finalErr != nil || len(finalRaw) == 0 {
		// 基线在、最终快照不在：钩子确实跑了但 trap 没能落盘。
		// 典型原因是用户脚本自己装了 EXIT trap 把我们的覆盖掉，或者进程被 SIGKILL 强杀。
		// 这种情况必须出声，否则用户会以为 export 生效了。
		emitHookEnvLine(onOutput, "[前置脚本环境变量] 未采集到回传数据，本次 export 不会对目标脚本生效")
		return
	}

	applied, ignored, notices := mergeHookEnvExports(envVars, parseHookEnvDump(baselineRaw), parseHookEnvDump(finalRaw))
	if len(applied) > 0 {
		emitHookEnvLine(onOutput, fmt.Sprintf("[前置脚本环境变量] 已生效: %s", strings.Join(applied, ", ")))
	}
	if len(ignored) > 0 {
		emitHookEnvLine(onOutput, fmt.Sprintf("[前置脚本环境变量] 已忽略受保护变量: %s", strings.Join(ignored, ", ")))
	}
	// 运行时关键变量：覆盖照常生效，只是额外给一条能定位问题的提示。
	for _, notice := range notices {
		emitHookEnvLine(onOutput, notice)
	}
}

func emitHookEnvLine(onOutput OnOutputFunc, line string) {
	if onOutput == nil {
		return
	}
	onOutput(line + "\n")
}

// parseHookEnvDump 解析 shell 侧落盘的环境快照。
//
// 主格式是 NUL 分隔的 "KEY=VALUE\0"；只有降级到最后一档「逐行 env」时才是换行分隔。
// 用「文件里有没有 NUL 字节」区分两种格式：NUL 格式每条记录后面都跟一个 NUL，
// 只要有内容就必然含 NUL；而 env 逐行输出永远不可能含 NUL（环境变量是 C 字符串）。
func parseHookEnvDump(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}

	separator := "\n"
	if bytes.IndexByte(data, 0) >= 0 {
		separator = "\x00"
	}

	records := strings.Split(string(data), separator)
	parsed := make(map[string]string, len(records))
	for _, record := range records {
		if record == "" {
			continue
		}
		index := strings.IndexByte(record, '=')
		if index <= 0 {
			// 没有 '=' 或以 '=' 开头都不是合法记录。逐行降级格式下，多行值的续行
			// 会落到这里被丢掉 —— 那正是「最后兜底档会丢多行值」的具体表现。
			continue
		}
		parsed[record[:index]] = record[index+1:]
	}
	return parsed
}

// mergeHookEnvExports 做纯增量覆盖：只把「钩子里新增的键」和「钩子里改过值的键」写回 envVars。
//
// 为什么绝对不能用 final 整体替换 envVars：planShellEnvExport 会把单条超过 MAX_ARG_STRLEN、
// 或累计超过导出预算的变量「只赋值不 export」，这类变量在钩子进程里 env -0 根本看不到。
// 一旦做替换式合并，它们会在目标脚本里凭空消失，表现成「加了前置脚本之后某个账号变量突然没了」。
//
// 为什么不支持 unset 传导：增量差集只能区分「新增」和「变更」，「缺席」既可能是被 unset，
// 也可能是上面那批从来就没进过钩子环境的大变量。按缺席删键会直接删掉用户的账号变量，
// 风险远大于收益，所以明确只支持 export 新增 / 覆盖。
//
// 返回值依次是：实际生效的键名、「想改契约变量但被挡下」的键名、以及运行时关键变量
// 被整体覆盖时的诊断提示行。前两者都已排序，供日志展示。
// shell 内部易变量（PWD / SHLVL / BASH_* ……）同样会被挡下，但不进 ignored：
// 它们不是用户的意图，报出来只是噪音。
//
// notices 只从 applied 里挑（见 hookEnvRuntimeCriticalNames）：没被改动的键不提示，
// 用追加写法改的键也不提示，避免正常使用被刷屏。
func mergeHookEnvExports(envVars, baseline, final map[string]string) (applied []string, ignored []string, notices []string) {
	if envVars == nil || len(final) == 0 {
		return nil, nil, nil
	}

	for key, value := range final {
		if !isValidShellEnvName(key) || isDangerousShellEnvName(key) {
			continue
		}
		if before, existed := baseline[key]; existed && before == value {
			// 钩子没动过它，不算回传。
			continue
		}
		if protected, report := hookEnvProtection(key); protected {
			if report {
				ignored = append(ignored, key)
			}
			continue
		}
		current, existed := envVars[key]
		if existed && current == value {
			// 值本来就一样，合并是个空操作，也没必要往日志里报。
			continue
		}
		envVars[key] = value
		applied = append(applied, key)
		if notice := hookEnvRuntimeOverrideNotice(key, current, value); notice != "" {
			notices = append(notices, notice)
		}
	}

	sort.Strings(applied)
	sort.Strings(ignored)
	sort.Strings(notices)
	return applied, ignored, notices
}

// captureHookEnvExports 包住一次前置钩子的执行：先备好采集现场，把带 dump 路径的
// 环境副本交给 run，跑完再把钩子里 export 的变量增量合并回 envVars。
//
// 合并后的 envVars 直接被目标脚本、task_after.sh、extra.sh 和任务后置脚本复用；
// ARG_MAX / MAX_ARG_STRLEN 体检不需要在这里重做一遍 —— 它在 RunCommandWithPlan 里、
// 也就是拿到合并结果之后才执行（见 script_runner.go 里 emitRuntimeEnvLimitWarnings 的调用点）。
func captureHookEnvExports(envVars map[string]string, onOutput OnOutputFunc, run func(hookEnv map[string]string)) {
	if run == nil {
		return
	}
	if envVars == nil {
		// 没有可回写的目标，退回原行为直接执行。
		run(envVars)
		return
	}

	capture, err := newHookEnvCapture(envVars)
	if err != nil {
		// 临时目录都建不出来时不能连钩子都不跑了，退回「执行但不回传」的旧行为。
		emitHookEnvLine(onOutput, fmt.Sprintf("[前置脚本环境变量] 采集准备失败，本次不回传: %s", err.Error()))
		run(envVars)
		return
	}
	defer capture.close()

	run(capture.hookEnv)
	capture.mergeInto(envVars, onOutput)
}
