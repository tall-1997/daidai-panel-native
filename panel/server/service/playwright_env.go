package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

// PlaywrightBrowsersPathEnv 是 Playwright 找浏览器的目录变量（issue #142）。
//
// Playwright 没设它时会落到 $XDG_CACHE_HOME 或 ~/.cache/ms-playwright：
// root 运行的容器里那是 /root/.cache，在容器可写层，一重建就丢。
// 所以容器部署下由面板把它钉到数据卷里的 <data.dir>/deps/ms-playwright。
//
// 唯一真源在 Go 侧（本文件）：进程环境、任务环境、一键安装的下载子进程都从这里取值，
// entrypoint.sh 不另写一套公式，免得 DATA_DIR 与 config.yaml 的 data.dir 两套算法分叉。
const PlaywrightBrowsersPathEnv = "PLAYWRIGHT_BROWSERS_PATH"

// PlaywrightDownloadHostEnv 是 Playwright 下载浏览器用的镜像地址变量。
// 国内直连官方 CDN 常常很慢或失败，浏览器下载失败的提示里会让用户设它。
const PlaywrightDownloadHostEnv = "PLAYWRIGHT_DOWNLOAD_HOST"

// DefaultPlaywrightDownloadHostARM64 是 arm64 上默认使用的 Chromium 下载镜像（v3.3.2 / issue #146）。
//
// 2026-09-20 用 curl 实测的结论，写下来是因为它推翻了几个看起来更顺手的选择：
//   - registry.npmmirror.com 这条是【真镜像】：/-/binary/playwright/builds/chromium/ 下能列出目录，
//     下载回来是 application/zip、文件头是 PK，实测 2.2MB/s。
//   - 但它【只有 arm64】：最近 4 个 revision（1228 / 1234 / 1237 / 1243）下只有
//     chromium-linux-arm64.zip 与 chromium-headless-shell-linux-arm64.zip，没有 x64 的 chromium-linux.zip。
//     所以 amd64 强推这个镜像会把「慢」变成「直接 404 失败」，比现状更糟 —— amd64 一律不设默认。
//   - 华为云的 mirrors.huaweicloud.com/playwright/ 是【假镜像】：任何路径都回 200，但 content-type 是
//     text/html、内容是门户页的 <!DOCTYPE html>（SPA 全路径回首页）。验证镜像不能只看状态码，
//     必须看 content-type 和文件头。
//
// 面板的 Magisk / Android 部署全是 arm64，正好被这条默认值全覆盖。
const DefaultPlaywrightDownloadHostARM64 = "https://registry.npmmirror.com/-/binary/playwright"

// 以下几项抽成包级变量只为一件事：让测试能在 Windows 开发机上模拟「Linux 容器」，
// 否则默认目录这条逻辑在开发机上零覆盖。生产代码不会改它们。
var (
	playwrightGOOS            = runtime.GOOS
	playwrightGOARCH          = runtime.GOARCH
	playwrightInContainerFunc = runningInContainer
	playwrightMagiskFunc      = playwrightMagiskRuntime
)

// DefaultPlaywrightDownloadHost 按运行架构给出 Chromium 下载镜像的默认值；不该设默认的架构返回空串。
// 只有 arm64 有可用的国内镜像，理由见 DefaultPlaywrightDownloadHostARM64 的注释。
func DefaultPlaywrightDownloadHost() string {
	if playwrightGOARCH == "arm64" {
		return DefaultPlaywrightDownloadHostARM64
	}
	return ""
}

// containerMarkerFiles / containerCgroupFile 是 runningInContainer 的判据来源，
// 同样只为测试能换成临时文件。
var (
	containerMarkerFiles = []string{"/.dockerenv", "/run/.containerenv"}
	containerCgroupFile  = "/proc/1/cgroup"
)

// runningInContainer 判断当前进程是否跑在 Docker / Podman 等容器运行时里。
//
// 从 qingLongCompatApplicable 里抽出来共用。刻意【不含】Magisk 分支：
// 青龙兼容层把 Magisk 的 ruri chroot 也当成「可以动 /」的环境，
// 而 Playwright 默认目录恰恰要把 Magisk 排除在外（原因见 DefaultPlaywrightBrowsersPath），
// 两边对 Magisk 的口径相反，只能各自在调用方判断。
func runningInContainer() bool {
	for _, marker := range containerMarkerFiles {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}

	// 兜底：cgroup 里带运行时名字。cgroup v2 下这里可能只有 "0::/"，认不出来也没关系 ——
	// 上面那两个标志文件已经覆盖了 Docker 与 Podman，认不出就退回「不是容器」这个保守方向。
	if data, err := os.ReadFile(containerCgroupFile); err == nil {
		text := string(data)
		for _, keyword := range []string{"docker", "containerd", "kubepods", "lxc", "podman"} {
			if strings.Contains(text, keyword) {
				return true
			}
		}
	}
	return false
}

// playwrightMagiskRuntime 判断是不是 Magisk / KernelSU / APatch 模块版。
// 两个标志都认：DAIDAI_MAGISK_MODULE（及 /data/adb 下的模块文件）由 IsMagiskModuleRuntime 负责，
// DAIDAI_MAGISK_SHELL_VERSION 是 service.sh 拉起面板时 export 的，青龙兼容层也认它。
func playwrightMagiskRuntime() bool {
	if strings.TrimSpace(os.Getenv("DAIDAI_MAGISK_SHELL_VERSION")) != "" {
		return true
	}
	return IsMagiskModuleRuntime()
}

// DefaultPlaywrightBrowsersPath 返回面板托管的浏览器目录；只有容器部署才有值，其余一律返回空串。
//
//   - Windows 桌面版、裸机二进制：不设默认值。那里的浏览器本来就在
//     %LOCALAPPDATA%\ms-playwright、~/.cache/ms-playwright，不会随容器重建丢失；
//     改掉默认目录反而会让已经装好的浏览器「消失」，脚本立刻报 Executable doesn't exist。
//   - Magisk 模块版：同样不设。deps/ 目录会被 SnapshotDepsToHost 在每次装依赖后整体 cp -rf、
//     被 service.sh 每 10 分钟同步一次、开机再回填一次，数百 MB 的浏览器放进去会被反复拷贝。
func DefaultPlaywrightBrowsersPath() string {
	// 先判平台再判容器：Windows 上 os.Stat("/.dockerenv") 查的是当前盘符根目录，没必要去碰。
	if playwrightGOOS != "linux" {
		return ""
	}
	if playwrightMagiskFunc() {
		return ""
	}
	if !playwrightInContainerFunc() {
		return ""
	}
	dataDir := currentDataDir()
	if dataDir == "" {
		return ""
	}
	// 必须是绝对路径：任务、调试运行、依赖安装各自的工作目录都不一样，
	// 相对路径会被 Playwright 按各自的 cwd 解析成不同的目录。
	if !filepath.IsAbs(dataDir) {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return ""
		}
		dataDir = abs
	}
	return filepath.Join(dataDir, "deps", "ms-playwright")
}

// managedPlaywrightBrowsersPath 是「用户没在面板里设这个变量」时任务该拿到的值：
// 进程环境优先（compose 的 -e、systemd 的 Environment=，或 main.go 启动时写入的默认值），
// 其次才是面板默认目录。ddp 进程不走 main.go，默认值就靠这里的第二档补上。
//
// 空串视同没设：Playwright 自己也是这么判的（JS 里空串为假），
// 但 "0" 这类非空的用户值必须原样透传 —— Node 版里它表示「浏览器装进 node_modules」。
func managedPlaywrightBrowsersPath() string {
	if value := os.Getenv(PlaywrightBrowsersPathEnv); value != "" {
		return value
	}
	return DefaultPlaywrightBrowsersPath()
}

// ResolvePlaywrightBrowsersPath 返回任务里 Playwright 实际会用的浏览器目录，
// 给一键安装的下载子进程与失败提示用，保证「下载到哪」与「任务去哪找」是同一个目录。
//
// 优先级与 BuildManagedRuntimeEnvMapWithScriptToken 算出来的任务环境一致：
// 环境变量页里启用的同名变量（其次 config.sh）> 进程环境 > 面板默认目录。
// 依赖安装子进程只继承 os.Environ，看不到环境变量页里的值，所以下载时必须显式传这个结果。
func ResolvePlaywrightBrowsersPath() string {
	if value, ok := panelUserEnvValues(PlaywrightBrowsersPathEnv)[PlaywrightBrowsersPathEnv]; ok {
		return value
	}
	return managedPlaywrightBrowsersPath()
}

// applyPlaywrightBrowsersPathDefault 给任务 / 订阅钩子的环境补上 PLAYWRIGHT_BROWSERS_PATH。
//
// 语义与 QL_DIR 那批青龙兼容变量一样：只在 key 不存在时才写。
// 用户在环境变量页（或 config.sh）里自己设了就原样生效，不能像 TZ 那样强制覆盖 ——
// 否则他在页面上看到自己设的值、脚本里实际却是另一个目录，排查毫无线索。
func applyPlaywrightBrowsersPathDefault(envMap map[string]string) {
	if envMap == nil {
		return
	}
	if _, exists := envMap[PlaywrightBrowsersPathEnv]; exists {
		return
	}
	if value := managedPlaywrightBrowsersPath(); value != "" {
		envMap[PlaywrightBrowsersPathEnv] = value
	}
}

// panelUserEnvValues 取「用户自己在面板里设的」若干变量：环境变量页（启用的）优先，config.sh 次之。
//
// 口径与 BuildManagedRuntimeEnvMapWithScriptToken 的前半段一致：同样的排序，
// 同名多条同样用 joinTaskEnvValues 合并。返回的 map 只含真正设过的 key，
// 设成空串也算设过（任务里拿到的就是空串，这里必须与之一致）。
func panelUserEnvValues(names ...string) map[string]string {
	result := make(map[string]string, len(names))
	if len(names) == 0 {
		return result
	}

	if database.DB != nil {
		var records []model.EnvVar
		database.DB.Where("enabled = ? AND name IN ?", true, names).
			Order("sort_order DESC, position ASC, created_at ASC, id ASC").
			Find(&records)
		grouped := make(map[string][]string, len(names))
		for _, record := range records {
			grouped[record.Name] = append(grouped[record.Name], record.Value)
		}
		for name, values := range grouped {
			result[name] = joinTaskEnvValues(values)
		}
	}

	configValues := make(map[string]string)
	loadConfigShellVars(configValues)
	for _, name := range names {
		if _, exists := result[name]; exists {
			continue
		}
		if value, ok := configValues[name]; ok {
			result[name] = value
		}
	}
	return result
}

// ApplyPlaywrightBrowsersPathProcessEnv 在启动时把默认浏览器目录写进面板进程环境。
//
// 系统命令行、依赖安装（pip / apt）、任务报缺包时的自动安装都直接继承 os.Environ，
// 只有写进进程环境它们才看得到。任务、调试运行、ddp 走的是白名单环境 + envMap，
// 由 applyPlaywrightBrowsersPathDefault 另行注入，不依赖这一步。
//
// 只在进程环境没设、且当前部署有默认目录（容器）时才写：用户在 compose / systemd 里
// 显式设了就原样尊重。全程 best-effort，失败只打日志，不阻塞启动。
func ApplyPlaywrightBrowsersPathProcessEnv() {
	if os.Getenv(PlaywrightBrowsersPathEnv) != "" {
		return
	}
	target := DefaultPlaywrightBrowsersPath()
	if target == "" {
		return
	}

	// PUID 降权部署的存量搬迁：entrypoint 把这类用户的 HOME 钉成 <data.dir>/.home，
	// 所以他们以前装的浏览器在 <data.dir>/.home/.cache/ms-playwright（本来就在数据卷里、重建不丢）。
	// 默认目录一改，不搬过去就会在升级后报找不到浏览器。同一个卷里 rename 是瞬时的。
	//
	// 用户在环境变量页 / config.sh 里自己指定了目录时不搬：他的任务用的是他自己的目录，
	// 那个目录甚至可能就是这里的旧目录，搬走等于把他正在用的浏览器挪没了。
	if _, userSet := panelUserEnvValues(PlaywrightBrowsersPathEnv)[PlaywrightBrowsersPathEnv]; !userSet {
		legacy := filepath.Join(currentDataDir(), ".home", ".cache", "ms-playwright")
		if moved, err := migrateLegacyPlaywrightBrowsers(legacy, target); err != nil {
			log.Printf("playwright: migrate legacy browsers %s -> %s failed: %v", legacy, target, err)
		} else if moved {
			log.Printf("playwright: migrated legacy browsers from %s to %s", legacy, target)
		}
	}

	// 目录建不出来也照样写环境变量：任务环境里的默认值与这里是同一个目录，两边必须一致；
	// 真要下载时 Playwright 会自己再建一次，建不出来会给出明确的报错。
	if err := os.MkdirAll(target, 0o755); err != nil {
		log.Printf("playwright: mkdir %s failed: %v", target, err)
	}
	if err := os.Setenv(PlaywrightBrowsersPathEnv, target); err != nil {
		log.Printf("playwright: set %s failed: %v", PlaywrightBrowsersPathEnv, err)
	}
}

// migrateLegacyPlaywrightBrowsers 把旧目录整体搬到新目录。
//
// 只在「旧目录里有东西、新目录不存在或为空」时搬；新目录已经有内容就不动，
// 不做合并、不覆盖 —— 两边各有一份时宁可让旧的闲置，也不能冒险弄坏正在用的那份。
// 返回值 moved 表示真的搬了。
func migrateLegacyPlaywrightBrowsers(legacyDir, targetDir string) (bool, error) {
	if !dirHasEntries(legacyDir) {
		return false, nil
	}
	if dirHasEntries(targetDir) {
		return false, nil
	}
	if info, err := os.Stat(targetDir); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s 已存在且不是目录", targetDir)
		}
		// 空目录先删掉：rename 到一个已存在的目录在有的平台上会直接失败。
		if err := os.Remove(targetDir); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(legacyDir, targetDir); err != nil {
		return false, err
	}
	return true, nil
}

func dirHasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// withEnvEntry 把 key=value 写进 env：先剔掉已有的同名项再追加，保证只有一份、以这里为准。
func withEnvEntry(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, prefix+value)
}

// nonEmptyEnvValues 滤掉值为空串的项。
//
// 只给「面板先写了默认值、再叠用户值」这种场景用（目前是 PLAYWRIGHT_DOWNLOAD_HOST，issue #146）：
// 用户若在环境变量页建了一条 enabled 但 Value 为空的同名记录，panelUserEnvValues 会原样返回空串，
// withEnvEntries 随即把默认镜像覆盖成空串，镜像静默失效、用户完全看不出原因。
//
// ⚠️ 刻意不在 panelUserEnvValues 里过滤：PLAYWRIGHT_BROWSERS_PATH 那边「设成空串」是有意义的
//（表示让 Playwright 用它自己的默认目录，PlaywrightDownloadStartLine 专门为此写了一个分支），
// 在公共函数里滤空会把那条语义一并改掉。
func nonEmptyEnvValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		result[key] = value
	}
	return result
}

// withEnvEntries 按 key 排序依次写入，结果与 map 的遍历顺序无关，便于测试断言。
func withEnvEntries(env []string, values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = withEnvEntry(env, key, values[key])
	}
	return env
}
