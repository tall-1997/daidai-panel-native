package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

// playwrightDebian12Packages 是一键安装登记的系统库清单（Debian 12 bookworm，amd64 与 arm64 共用），共 26 个。
//
// 来源：microsoft/playwright 仓库 packages/playwright-core/src/server/registry/nativeDeps.ts
// 里 debian12-x64 的 chromium 依赖组（21 个，debian12-arm64 是直接复制 x64 的，包名完全相同；
// chromium-headless-shell 的依赖组也是 chromium），外加精选的 5 个字体相关包。
//
// 官方 install-deps 总会额外带上 tools 组（11 个），这里只挑了其中 5 个：
//   - fonts-wqy-zenhei：slim 镜像一个中文字体都没有，不装它中文页面截图全是方块；
//   - fonts-liberation、fonts-noto-color-emoji：常见西文字体与 emoji；
//   - libfontconfig1、libfreetype6：本来也会被 libcairo2 / libpango 依赖进来，
//     显式登记是为了容器重建后的自动重装不依赖「被谁顺带装上」这种隐式关系。
//
// 刻意不装 xvfb（会拉进 libgl1-mesa-dri、libllvm，上百 MB，只有有头模式才需要）
// 和其余几个字体包。包越多，容器重建后的自动重装窗口就越长。
//
// 维护：Playwright 升级或镜像换代（bookworm → trixie，多数包名会变成 *t64）时要重新核对；
// 其它发行版 / 版本在 PlanPlaywrightRuntime 里明确拒绝，而不是套用这份清单。
var playwrightDebian12Packages = [...]string{
	// chromium 必需库（21）
	"libasound2",
	"libatk-bridge2.0-0",
	"libatk1.0-0",
	"libatspi2.0-0",
	"libcairo2",
	"libcups2",
	"libdbus-1-3",
	"libdrm2",
	"libgbm1",
	"libglib2.0-0",
	"libnspr4",
	"libnss3",
	"libpango-1.0-0",
	"libx11-6",
	"libxcb1",
	"libxcomposite1",
	"libxdamage1",
	"libxext6",
	"libxfixes3",
	"libxkbcommon0",
	"libxrandr2",
	// 字体相关（5）
	"fonts-liberation",
	"fonts-wqy-zenhei",
	"fonts-noto-color-emoji",
	"libfontconfig1",
	"libfreetype6",
}

// PlaywrightPythonPackage 是一键安装登记的 Python 依赖名。
const PlaywrightPythonPackage = "playwright"

// PlaywrightDebian12Packages 返回清单的副本，调用方改动不会影响常量表本身。
func PlaywrightDebian12Packages() []string {
	return append([]string(nil), playwrightDebian12Packages[:]...)
}

// LinuxOSRelease 是 /etc/os-release 里一键安装关心的三个字段。
type LinuxOSRelease struct {
	ID              string
	VersionID       string
	VersionCodename string
}

// linuxOSReleaseFiles 按 os-release(5) 的约定依次尝试；抽成变量只为测试能换成临时文件。
var linuxOSReleaseFiles = []string{"/etc/os-release", "/usr/lib/os-release"}

// DetectLinuxOSRelease 读 ID、VERSION_ID、VERSION_CODENAME。
// 现有的 DetectLinuxDistribution 只读 ID，而包清单必须按版本挑（trixie 的包名大多带 t64 后缀）。
func DetectLinuxOSRelease() LinuxOSRelease {
	for _, path := range linuxOSReleaseFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseLinuxOSRelease(string(data))
	}
	return LinuxOSRelease{}
}

func parseLinuxOSRelease(content string) LinuxOSRelease {
	var release LinuxOSRelease
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "ID":
			release.ID = strings.ToLower(value)
		case "VERSION_ID":
			release.VersionID = value
		case "VERSION_CODENAME":
			release.VersionCodename = strings.ToLower(value)
		}
	}
	return release
}

func (r LinuxOSRelease) describe() string {
	if r.ID == "" && r.VersionID == "" && r.VersionCodename == "" {
		return "未能读取 /etc/os-release"
	}
	return fmt.Sprintf("ID=%s VERSION_ID=%s VERSION_CODENAME=%s", r.ID, r.VersionID, r.VersionCodename)
}

// PlaywrightRuntimePlan 是一键安装的判定结果，也直接作为 GET /deps/playwright 的主体。
type PlaywrightRuntimePlan struct {
	Supported    bool     `json:"supported"`
	Reason       string   `json:"reason"`
	Distribution string   `json:"distribution"`
	VersionID    string   `json:"version_id"`
	Arch         string   `json:"arch"`
	Packages     []string `json:"packages"`
	BrowsersPath string   `json:"browsers_path"`
}

// playwrightRuntimeFacts 是判定需要的全部环境事实。
// 「读环境」与「做判定」拆开，判定部分就能在 Windows 开发机上把每个分支跑全。
type playwrightRuntimeFacts struct {
	GOOS           string
	Arch           string
	OSRelease      LinuxOSRelease
	PackageManager string
	// ManagedBrowsersPath 是 DefaultPlaywrightBrowsersPath()：非空才说明浏览器目录由面板托管（容器部署）。
	ManagedBrowsersPath string
	Magisk              bool
	// BrowsersPath 是 ResolvePlaywrightBrowsersPath()：任务里实际会用的目录，原样回给前端展示。
	BrowsersPath string
}

var playwrightRuntimeFactsFunc = detectPlaywrightRuntimeFacts

func detectPlaywrightRuntimeFacts() playwrightRuntimeFacts {
	facts := playwrightRuntimeFacts{
		GOOS:         runtime.GOOS,
		Arch:         runtime.GOARCH,
		BrowsersPath: ResolvePlaywrightBrowsersPath(),
	}
	if facts.GOOS != "linux" {
		return facts
	}
	facts.OSRelease = DetectLinuxOSRelease()
	if manager, err := DetectLinuxPackageManager(); err == nil {
		facts.PackageManager = manager.Name
	}
	facts.Magisk = playwrightMagiskFunc()
	facts.ManagedBrowsersPath = DefaultPlaywrightBrowsersPath()
	return facts
}

// PlanPlaywrightRuntime 判定当前环境能不能一键安装 Playwright 运行环境，能的话给出系统库清单。
// 不含 root 判定：那一条由调用方用 EnsureLinuxPackageManagerPrivilege 单独检查，文案按部署形态分岔。
func PlanPlaywrightRuntime() PlaywrightRuntimePlan {
	return planPlaywrightRuntime(playwrightRuntimeFactsFunc())
}

// planPlaywrightRuntime 的判定顺序：非 Linux → Alpine/apk → 非 apt → 架构 → 非 Debian 12 → 浏览器目录不归面板管。
// 越靠前的原因越「根本」：Alpine 用户该看到的是换镜像，而不是「你的架构不对」。
func planPlaywrightRuntime(facts playwrightRuntimeFacts) PlaywrightRuntimePlan {
	plan := PlaywrightRuntimePlan{
		Distribution: facts.OSRelease.ID,
		VersionID:    facts.OSRelease.VersionID,
		Arch:         facts.Arch,
		Packages:     []string{},
		BrowsersPath: facts.BrowsersPath,
	}

	switch {
	case facts.GOOS != "linux":
		plan.Reason = fmt.Sprintf("一键安装只支持 Linux 上的 Debian 12 版 Docker 镜像，当前系统是 %s", facts.GOOS)
	case facts.OSRelease.ID == "alpine" || facts.PackageManager == "apk":
		// 与 buildPlaywrightAlpineHint 同一个说法：按标签对应换，写死 :debian 的话 latest-full 的用户照做会丢掉 Go 与编译链。
		plan.Reason = "Alpine 镜像（musl）跑不了 Playwright 官方的 Chromium（glibc 构建），请把镜像标签里的 latest 换成 debian（如 latest → debian、latest-full → debian-full）"
	case facts.PackageManager != "apt":
		manager := facts.PackageManager
		if manager == "" {
			manager = "未检测到"
		}
		plan.Reason = fmt.Sprintf("一键安装只支持 apt 系统（Debian 12），当前包管理器：%s", manager)
	case facts.Arch != "amd64" && facts.Arch != "arm64":
		plan.Reason = fmt.Sprintf("Playwright 的 Chromium 只有 amd64 / arm64 构建，当前架构是 %s", facts.Arch)
	case facts.OSRelease.ID != "debian" || facts.OSRelease.VersionID != "12":
		plan.Reason = "一键安装目前只内置 Debian 12（bookworm）的系统库清单，当前系统：" + facts.OSRelease.describe()
	case facts.ManagedBrowsersPath == "":
		// 浏览器目录只在容器部署下由面板托管（见 DefaultPlaywrightBrowsersPath）。
		// 这类部署装完系统库和 pip 包也不会自动下载浏览器，一键装出来是个半成品，不如直接说清楚。
		where := "非容器部署"
		if facts.Magisk {
			where = "面具模块版"
		}
		plan.Reason = fmt.Sprintf("一键安装只在 Docker 部署下可用：%s的浏览器目录不由面板托管，"+
			"请在终端执行 python3 -m playwright install --with-deps chromium", where)
	default:
		plan.Supported = true
		plan.Packages = PlaywrightDebian12Packages()
	}
	return plan
}

// 浏览器下载这一步前后的日志行。依赖失败提示靠这两行判断「失败发生在下载阶段」，
// 所以写日志与判定两边必须引用同一组常量。
const (
	PlaywrightDownloadStartPrefix = "[Playwright] 正在下载 Chromium 到 "
	PlaywrightDownloadReadyLine   = "[Playwright] 浏览器已就绪"
)

// playwrightDownloadSizeNote 跟在下载开始行后面，讲清楚这一步为什么会长时间「没反应」（issue #146）。
//
// Playwright 的进度条靠 \r 原地刷新，而面板的日志采集用 bufio.Scanner 按 \n 切行，
// 非 TTY 下整个下载期间一行都不会输出 —— 用户看到的就是日志停在下载开始行不动。
// 与其改 Scanner 的切分规则（会把所有依赖的进度条撑成很多行），不如把预期先说清楚。
const playwrightDownloadSizeNote = "（约 150-300MB，国内网络下可能需要十几分钟；下载期间日志不刷新属正常现象）"

// PlaywrightDownloadStartLine 生成下载开始那一行。目录为空说明用户在面板里把变量设成了空串，
// 此时 Playwright 用它自己的默认目录，照实写出来，免得日志里出现一个看不懂的空路径。
func PlaywrightDownloadStartLine(browsersPath string) string {
	if strings.TrimSpace(browsersPath) == "" {
		return PlaywrightDownloadStartPrefix + "Playwright 默认目录（PLAYWRIGHT_BROWSERS_PATH 为空）" + playwrightDownloadSizeNote
	}
	return PlaywrightDownloadStartPrefix + browsersPath + playwrightDownloadSizeNote
}

var playwrightManagedPythonFunc = ResolveManagedPythonBinaryForPythonVersion

// PlaywrightBrowserDownloadApplies 判断 pip 装完 packageName 之后要不要接着下载 Chromium。
//
// 只认按 PEP 503 归一化后恰好是 playwright 的包（含 playwright==x 这种写法），
// 并且只在浏览器目录由面板托管（容器部署）、且不是 Alpine 时才下：
//   - Windows 桌面版、裸机、面具版的用户自己管理浏览器，给每个手动装 playwright 的人
//     悄悄多下 150-300MB 是不可接受的副作用；
//   - Alpine 上官方 Chromium 本来就跑不起来，下了也白下。
//
// 网页安装、重装、一键安装都经过这里；重启后的自动重装不走这条路 ——
// 浏览器在数据卷里，重建容器不会丢。
func PlaywrightBrowserDownloadApplies(packageName string) bool {
	if CanonicalizePythonPackageName(packageName) != PlaywrightPythonPackage {
		return false
	}
	if DefaultPlaywrightBrowsersPath() == "" {
		return false
	}
	return DetectLinuxOSRelease().ID != "alpine"
}

// NewPlaywrightBrowserInstallCommand 构造 `<托管 venv 的 python> -m playwright install chromium`。
//
// 必须在 pip 装完之后才调用（此时 venv 里才有 playwright 模块）。返回的第二个值是实际下载目录，
// 供调用方写日志。环境：面板代理、可写 HOME，并显式设 PLAYWRIGHT_BROWSERS_PATH ——
// 依赖安装子进程只继承 os.Environ，看不到环境变量页里的值，不显式传就会下到与任务不一样的目录。
// 环境变量页里设的 PLAYWRIGHT_DOWNLOAD_HOST 也一并带上，这样失败提示里「设置下载镜像」那条出路在面板内就能完成。
func NewPlaywrightBrowserInstallCommand(pythonVersion string) (*exec.Cmd, string, error) {
	pythonBin := playwrightManagedPythonFunc(pythonVersion)
	if strings.TrimSpace(pythonBin) == "" {
		return nil, "", fmt.Errorf("[Playwright] 找不到 Python %s 的面板托管解释器，无法下载 Chromium，请确认该版本的依赖环境可用后重装本依赖",
			NormalizePythonVersionOrDefault(pythonVersion))
	}

	browsersPath := ResolvePlaywrightBrowsersPath()
	cmd := exec.Command(pythonBin, "-m", "playwright", "install", "chromium")
	// SanitizePipEnv 顺带剥掉 PYTHONPATH / PYTHONHOME：它们会污染 venv 解释器的模块搜索路径。
	env := WritableHomeEnv(SanitizePipEnv(AppendProxyEnv(os.Environ())))
	// 顺序契约：先写面板默认镜像，再叠用户值（issue #146）。
	// withEnvEntry 是「先剔同名再追加」，两次调用的先后天然实现「用户值优先」。
	// 用户值要先滤掉空串，否则一条 enabled 但 Value 为空的记录会把默认镜像覆盖成空串、静默失效。
	if defaultHost := DefaultPlaywrightDownloadHost(); defaultHost != "" {
		env = withEnvEntry(env, PlaywrightDownloadHostEnv, defaultHost)
	}
	env = withEnvEntries(env, nonEmptyEnvValues(panelUserEnvValues(PlaywrightDownloadHostEnv)))
	cmd.Env = withEnvEntry(env, PlaywrightBrowsersPathEnv, browsersPath)
	return cmd, browsersPath, nil
}

// playwrightHintEnv 是 BuildPlaywrightEnvironmentHint 需要的环境事实。
type playwrightHintEnv struct {
	// BrowsersPath 是任务里实际生效的 PLAYWRIGHT_BROWSERS_PATH
	BrowsersPath string
	// OneClick 表示浏览器目录由面板托管，也就是容器部署：非 Alpine 时能用「依赖管理 → Linux」里的一键安装；
	// Alpine 上一键安装一定被拒，Alpine 分支只拿它判断「是不是容器、该不该叫人换镜像」
	OneClick bool
	// PendingLinux 返回正在安装 / 排队中的 Linux 依赖数；只在命中缺库关键词时才调用，避免每次失败都查库
	PendingLinux func() int64
	// Alpine / Magisk / Arch 只给 Alpine 分支用（#154）：Alpine（musl）上 Playwright 跑不起来，
	// 出路按部署形态（容器 / 面具模块 / 其它）和 CPU 架构分岔，见 buildPlaywrightAlpineHint
	Alpine bool
	Magisk bool
	Arch   string
}

var playwrightHintEnvFunc = func() playwrightHintEnv {
	return playwrightHintEnv{
		BrowsersPath: ResolvePlaywrightBrowsersPath(),
		OneClick:     DefaultPlaywrightBrowsersPath() != "",
		PendingLinux: countPendingLinuxDependencies,
		Alpine:       DetectLinuxOSRelease().ID == "alpine",
		Magisk:       playwrightMagiskFunc(),
		Arch:         playwrightGOARCH,
	}
}

func countPendingLinuxDependencies() int64 {
	if database.DB == nil {
		return 0
	}
	var count int64
	database.DB.Model(&model.Dependency{}).
		Where("type = ? AND status IN ?", model.DepTypeLinux, []string{model.DepStatusInstalling, model.DepStatusQueued}).
		Count(&count)
	return count
}

// BuildPlaywrightEnvironmentHint 针对任务 / 调试运行里 Playwright 的环境故障给出一句提示：
// Alpine 上根本跑不了 Playwright（#154），以及缺浏览器、缺系统库两类（#142）。
//
// 与 BuildModuleCompatibilityHint 分开写：那是纯函数、有现成测试锁着，而这里要查库（正在重装的系统依赖数）
// 和读环境（浏览器目录、是不是容器、是不是 Alpine），这些都通过 playwrightHintEnvFunc 注入。
// 提示会进任务失败摘要，摘要截断到 320 字符，所以每条都要写短。
func BuildPlaywrightEnvironmentHint(output string) string {
	lower := strings.ToLower(output)
	if lower == "" {
		return ""
	}
	// 缺模块（#154）：Python 的 No module named 'playwright'；Node 用 require 时报 Cannot find module 'playwright'，
	// 用 ESM 的 import 时报的是 Cannot find package 'playwright' imported from …（错误码 ERR_MODULE_NOT_FOUND），不含前一句。
	// 只有 Alpine 上才凭它给提示，别的系统上缺模块交给任务的自动安装（见下面第 ② 段）。
	missingModule := strings.Contains(lower, "no module named 'playwright'") ||
		strings.Contains(lower, "cannot find module 'playwright'") ||
		strings.Contains(lower, "cannot find package 'playwright'")
	missingBrowser := strings.Contains(lower, "executable doesn't exist") && strings.Contains(lower, "ms-playwright")
	// 缺系统库。后两句是 musl 加载器的说法（#154）：Alpine 镜像装了 gcompat，glibc 版 Chromium 由 musl 的加载器拉起，
	// 缺库打 Error loading shared library …（needed by …），缺符号打 Error relocating …: symbol not found（musl 的 ldso/dynlink.c），
	// 不会出现 glibc 的 error while loading shared libraries；Playwright 的依赖校验在 musl 上也报不出 Host system is missing dependencies
	// （musl 的 ldd 缺库时以 127 退出，Playwright 遇到非 0 退出码直接当作没缺库）。不认这两句，Alpine 上的缺库就认不出来。
	// glibc 不会打这两句，非 Alpine 上真实输入的行为不变。
	missingLibs := strings.Contains(lower, "host system is missing dependencies") ||
		strings.Contains(lower, "error while loading shared libraries") ||
		strings.Contains(lower, "error loading shared library") ||
		strings.Contains(lower, "error relocating")
	// 一个关键词都不命中就不读环境：任务每次失败都会调到这里，不能每次都查库。
	if !missingModule && !missingBrowser && !missingLibs {
		return ""
	}
	env := playwrightHintEnvFunc()

	// ① Alpine 必须排在最前面（#154）：原逻辑的缺浏览器、缺库两段会引人去点「安装 Playwright 运行环境」，
	// 而一键安装在 Alpine 上一定被 planPlaywrightRuntime 拒绝，用户得多走一圈才知道要换镜像。
	// 缺模块、缺浏览器（路径里带 ms-playwright）、Playwright 自己报的缺库，报错里都会出现 playwright，
	// Host system is missing dependencies 也是 Playwright 专有的说法；剩下的只有与 Playwright 无关的
	// 通用缺库报错，它不归这里管，交给下面的原逻辑（那里对它的口径是「宁可不给提示」）。
	if env.Alpine && (strings.Contains(lower, "playwright") || strings.Contains(lower, "host system is missing dependencies")) {
		return buildPlaywrightAlpineHint(lower, env)
	}
	// ② 不是 Alpine、只是缺模块：交给任务的自动安装（开关关着时由用户自己装），这里不给提示。
	if !missingBrowser && !missingLibs {
		return ""
	}
	// ③ 缺浏览器 / 缺系统库，沿用 #142 的原逻辑。
	return buildPlaywrightEnvironmentHint(lower, missingBrowser, env)
}

// buildPlaywrightAlpineHint 给 Alpine（musl）上的 Playwright 故障一句确定结论（#154）。
//
// 文案按「语言 × 部署 × 架构」拼，每种组合都必须单行、≤320 字符（失败摘要的上限）：
//   - 语言只能从报错文本里认。Python 的客户端包在 musl 上根本装不上（PyPI 只发 manylinux 预编译包，
//     没有 musllinux 包也没有源码包）；Node 的 npm 包能装上，起不来的只是 Playwright 下载的 glibc 版 Chromium，
//     还可以连别处的浏览器。两边的说法对另一边都不成立，认不出语言时只讲两边都成立的结论。
//   - 部署决定「换什么」：容器按标签换成对应的 Debian 版镜像（latest → debian、latest-full → debian-full）再点一键安装；
//     面具模块没有镜像标签可换，一键安装也会被 planPlaywrightRuntime 拒绝（浏览器目录不归面板管），
//     只能改刷 Debian 版模块再在终端装；其它部署换到 glibc 系统。
//   - Playwright 的浏览器和 Debian 版镜像都只有 amd64 / arm64：别的架构换什么都没用，不能再叫人去换。
func buildPlaywrightAlpineHint(lower string, env playwrightHintEnv) string {
	python := strings.Contains(lower, "no module named 'playwright'") ||
		strings.Contains(lower, "traceback (most recent call last)") ||
		strings.Contains(lower, "playwright._impl")
	// npx playwright 出自 Playwright 给 Node 用户的安装指引（缺浏览器、缺库时都会打印），Python 版写的是 playwright install。
	// ESM 的 import 缺包报的是 Cannot find package 'playwright'：报错里没有 node:internal 堆栈时（比如脚本只打印了 message），
	// 语言只能靠这一句认出来。
	node := !python && (strings.Contains(lower, "cannot find module 'playwright'") ||
		strings.Contains(lower, "cannot find package 'playwright'") ||
		strings.Contains(lower, "npx playwright") ||
		strings.Contains(lower, "node:internal"))

	// 认不出语言时的默认值：只讲两边都成立的结论，面具版的终端命令两种都给。
	reason := "Playwright 官方的 Chromium 是 glibc 构建，在这里启动不了。"
	command := "python3 -m playwright install --with-deps chromium（Node 脚本用 npx playwright install --with-deps chromium）"
	if python {
		reason = "Playwright 的 Python 包只发布认 glibc 的 manylinux 预编译包，pip 在这里装不上（会报 from versions: none），自动安装和重试都不会有变化。"
		command = "python3 -m playwright install --with-deps chromium"
	} else if node {
		reason = "npm 的 playwright 包能装上，但 Playwright 下载的 Chromium 是 glibc 构建，在这里启动不了。"
		command = "npx playwright install --with-deps chromium"
	}
	hint := "[提示] 当前系统是 Alpine（musl libc），" + reason

	if env.Arch != "amd64" && env.Arch != "arm64" {
		// Node 仍然可以连别处的浏览器，所以只能说「起不了浏览器」，不能说「跑不了 Playwright」。
		ending := "这台机器上起不了 Playwright 的浏览器。"
		if python {
			ending = "这台机器上跑不了 Playwright。"
		} else if node {
			ending = "这台机器上起不了浏览器，只能让脚本用 connect / connectOverCDP 连接别处的浏览器。"
		}
		return hint + fmt.Sprintf("CPU 架构是 %s，而 Playwright 的浏览器与 Debian 版镜像都只有 amd64 / arm64，", env.Arch) + ending
	}

	fix := "请把面板换到 Debian / Ubuntu 等 glibc 系统上运行"
	if env.Magisk {
		// 面具版不能提 Docker 镜像标签，也不能提一键安装按钮；Debian 版模块上能不能跑起 Chromium 没人验证过，照实说。
		// 命令后面接全角逗号而不是半角空格：认不出语言时 command 以全角括号收尾，接空格会变成「…） 装」。
		fix = "请改刷 Debian 版面具模块（daidai-panel-magisk-debian-vX.Y.Z.zip），再在终端执行 " + command +
			"，装浏览器和系统库；安卓上能否跑起 Chromium 尚未验证"
	} else if env.OneClick {
		// Alpine 的正式浮动标签有 latest、latest-full、latest-3.10、latest-3.11、latest-all 五个（README 的标签表），
		// 对应的 Debian 版都是把 latest 换成 debian；写死 :debian 的话，latest-full 的用户照做会丢掉 Go 与编译链。
		fix = "请把镜像标签里的 latest 换成 debian（如 latest → debian、latest-full → debian-full），数据卷可直接沿用，" +
			"再到「依赖管理 → Linux」点「安装 Playwright 运行环境」"
	}
	if node {
		fix += "；也可以让脚本用 connect / connectOverCDP 连接别处的浏览器"
	}
	return hint + fix + "。"
}

func buildPlaywrightEnvironmentHint(lower string, missingBrowser bool, env playwrightHintEnv) string {
	if missingBrowser {
		current := env.BrowsersPath
		if strings.TrimSpace(current) == "" {
			current = "未设置（Playwright 默认目录）"
		}
		fix := "请执行 python3 -m playwright install chromium 重新下载"
		if env.OneClick {
			fix = "请到「依赖管理 → Linux」点「安装 Playwright 运行环境」重新下载"
		}
		return fmt.Sprintf("[提示] Playwright 找不到浏览器，当前 PLAYWRIGHT_BROWSERS_PATH=%s；"+
			"报错路径不在该目录下说明脚本或环境变量改了目录，升级 playwright 后所需的浏览器版本变了也会这样。%s。", current, fix)
	}

	// 缺系统库。容器重建后启动校验会在后台串行重装 Linux 依赖，这段时间里跑的任务必然报缺库，
	// 此时该让用户等，而不是再去点一次安装。
	if env.PendingLinux != nil {
		if pending := env.PendingLinux(); pending > 0 {
			return fmt.Sprintf("[提示] 容器重建后正在后台自动重装 %d 个系统依赖，完成后重试即可（进度见「依赖管理 → Linux」）。", pending)
		}
	}
	// "error while loading shared libraries" 是任何原生程序缺库都会报的通用错误，
	// 只有报错里提到 playwright 时才把人往 Playwright 一键安装上引，否则宁可不给提示。
	if !strings.Contains(lower, "host system is missing dependencies") && !strings.Contains(lower, "playwright") {
		return ""
	}
	if env.OneClick {
		return "[提示] Playwright 缺少系统库，请到「依赖管理 → Linux」点「安装 Playwright 运行环境」补装后重试。"
	}
	return "[提示] Playwright 缺少系统库，请以 root 执行 python3 -m playwright install-deps chromium 补装后重试。"
}

// BuildRuntimeFailureHint 是任务 / 调试运行 / run-code 失败时统一调用的提示入口：
// 先认 ESM 兼容问题，再认 Playwright 环境问题，两者的关键词互不相交。
func BuildRuntimeFailureHint(output string) string {
	if hint := BuildModuleCompatibilityHint(output); hint != "" {
		return hint
	}
	return BuildPlaywrightEnvironmentHint(output)
}
