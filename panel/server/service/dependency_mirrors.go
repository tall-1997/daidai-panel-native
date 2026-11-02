package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// DefaultPipMirror v3.3.2 起从阿里云换成腾讯云（issue #146）：用户实测阿里云 pypi 长期跑不满 100KB/s，
	// 而腾讯云的路径形态与阿里云一致（都是 <站点>/pypi/simple），换过去不用动 PIP_TRUSTED_HOST 的提取逻辑。
	// 阿里云没有从候选清单里删掉，只是不再是默认值 —— 阿里云 ECS 内网走 aliyun 反而最快。
	DefaultPipMirror = "https://mirrors.cloud.tencent.com/pypi/simple"
	DefaultNpmMirror = "https://registry.npmmirror.com"
)

// legacyDefaultPipMirrors 是 v3.3.2 之前的 pip 默认源。
//
// 它只服务于「旧默认源一次性迁移」（issue #146）：存量用户的 pip.conf 里实打实存着阿里云字符串，
// 光改 DefaultPipMirror 对他们毫无作用（非空的源都当成用户自己选的原样保留）。
// 判定收在 legacyDefaultMirrorMigrationPending() 后面，保证只搬一次，见该函数的说明。
var legacyDefaultPipMirrors = []string{
	"https://mirrors.aliyun.com/pypi/simple",
}

type DependencyMirrorSettings struct {
	PipMirror string `json:"pip_mirror,omitempty"`
	NpmMirror string `json:"npm_mirror,omitempty"`
}

func EffectivePipMirror(configured string) string {
	mirror := normalizePipMirrorForWrite(configured)
	// 旧默认源的一次性迁移只在「读生效值」这条路上做，不在写盘那条路上做：
	// 写盘走 normalizePipMirrorForWrite，用户在页面上主动选回阿里云时必须原样存下来，
	// 否则他会发现这个选项永远选不上（存进去立刻被改成腾讯云）。
	if isLegacyDefaultPipMirror(mirror) && legacyDefaultMirrorMigrationPending() {
		return DefaultPipMirror
	}
	return mirror
}

// normalizePipMirrorForWrite 只把「留空」回落到默认加速源，其余写法（包括 pypi 官方源）原样返回。
// 抽出来是为了让「存盘」与「读生效值」两条路分开 —— 只有后者参与旧默认源迁移。
//
// v3.3.9 起官方源不再回落（issue #165）：以前 pypi.org / pypi.python.org 和留空一样被换成默认加速源，
// 用户在页面上选了官方源，存进 pip.conf 的却是腾讯云，等于官方源这个选项永远选不上。
// 海外机器、或者镜像同步滞后时，用户就是要直连官方源，必须原样存、原样生效。
func normalizePipMirrorForWrite(configured string) string {
	mirror := strings.TrimSpace(configured)
	if mirror == "" {
		return DefaultPipMirror
	}
	return mirror
}

func isLegacyDefaultPipMirror(mirror string) bool {
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(mirror)), "/")
	for _, legacy := range legacyDefaultPipMirrors {
		if normalized == strings.TrimRight(strings.ToLower(legacy), "/") {
			return true
		}
	}
	return false
}

// ---------- v3.3.2 旧默认镜像源的一次性迁移开关（issue #146）----------
//
// 要解决的事：默认源从阿里云换成腾讯云之后，存量用户的 pip.conf / sources.list 里存的仍是阿里云字符串，
// 而阿里云不是留空也不是官方源，pip 侧的 normalizePipMirrorForWrite 与 Linux 侧的 isOfficialLinuxMirror
// 都会把它当成「用户自己选的」而不动 ——
// issue #146 的报告人正是这批人，光改默认常量对他们等于没改。
//
// 但这种「等于旧默认就搬走」的判定绝不能常驻：用户日后主动选回阿里云，下次装包又会被静默改走，
// 改不动还查不出原因。所以用一个磁盘标记把它变成一次性的，标记的语义是：
// **用户已经在本面板里显式保存过依赖镜像源设置**（handler 的 PUT /deps/mirrors 成功后落标记）。
// 一旦落了标记，阿里云就只是一个普通的用户选择，迁移判定整体失效。
//
// 为什么落磁盘而不是 system_config：system_config_registry 里每一条都是用户可见的设置项（带 label /
// 分组，会渲染到设置页），塞一个 dependency_mirror_migrated 进去会冒出一个没人看得懂的条目；
// 而镜像源本身就是磁盘态（pip.conf / sources.list），标记放数据目录语义更一致。
const legacyMirrorChoiceMarkerName = "dependency-mirror-choice.saved"

var (
	legacyMirrorChoiceMu    sync.Mutex
	legacyMirrorChoiceSaved bool
)

func legacyMirrorChoiceMarkerPath() string {
	dataDir := currentDataDir()
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, legacyMirrorChoiceMarkerName)
}

// legacyDefaultMirrorMigrationPending 报告「旧默认源 → 新默认源」这次迁移还要不要做。
func legacyDefaultMirrorMigrationPending() bool {
	legacyMirrorChoiceMu.Lock()
	defer legacyMirrorChoiceMu.Unlock()

	if legacyMirrorChoiceSaved {
		return false
	}
	path := legacyMirrorChoiceMarkerPath()
	if path == "" {
		// 数据目录还没就绪（启动早期 config.C 为空）或这次根本没有数据目录：标记落不了盘，
		// 迁移就没法保证只做一次。这种时候宁可不搬，也不能搬了却关不掉。
		// 刻意不把这个结论缓存进 legacyMirrorChoiceSaved：等数据目录就绪后还有机会。
		return false
	}
	if _, err := os.Stat(path); err == nil {
		legacyMirrorChoiceSaved = true
		return false
	}
	return true
}

// MarkDependencyMirrorChoiceSaved 记下「用户已经显式保存过依赖镜像源」，此后旧默认源一律按用户选择对待。
// 落盘失败只记日志不报错：标记丢了最坏也只是多搬一次，不该让「镜像源设置成功」变成失败。
func MarkDependencyMirrorChoiceSaved() {
	legacyMirrorChoiceMu.Lock()
	defer legacyMirrorChoiceMu.Unlock()

	legacyMirrorChoiceSaved = true
	path := legacyMirrorChoiceMarkerPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("warn: 依赖镜像源选择标记建目录失败 %s: %v", filepath.Dir(path), err)
		return
	}
	if err := os.WriteFile(path, []byte("v3.3.2\n"), 0o644); err != nil {
		log.Printf("warn: 依赖镜像源选择标记落盘失败 %s: %v", path, err)
	}
}

// EffectiveNpmMirror 只把「留空」回落到默认加速源。
// npm 官方源 registry.npmjs.org 和 pip 一样原样保留（issue #165 同一个根因：以前会被换成 npmmirror，
// 用户选了官方源也存不进 .npmrc）。
func EffectiveNpmMirror(configured string) string {
	mirror := normalizeNpmMirror(configured)
	if mirror == "" {
		return normalizeNpmMirror(DefaultNpmMirror)
	}
	return mirror
}

func PipInstallEnv(base []string, configured string) []string {
	// homeRedirectEnv 只在 HOME 不可写时才改动 env（容器配了 PUID/PGID 降权后的典型状态）。
	// pip 的 pip.conf 与 --user 落点都只认 HOME，不修它装依赖必报 EACCES。
	env := homeRedirectEnv(SanitizePipEnv(base))
	mirror := EffectivePipMirror(configured)
	if mirror == "" {
		return env
	}

	env = append(env, "PIP_INDEX_URL="+mirror)
	if host := extractMirrorHost(mirror); host != "" {
		env = append(env, "PIP_TRUSTED_HOST="+host)
	}
	return env
}

// pipConflictingEnvKeys 列出会与面板内部 pip 调用相冲突的环境变量：
//   - PIP_PREFIX / PIP_HOME / PIP_TARGET / PIP_ROOT 都会被 pip 转成对应命令行选项；
//     宿主机如果同时通过 ~/.pydistutils.cfg、systemd unit 等地方注入多个，
//     pip 会抛出 "Cannot set --home and --prefix together" 等冲突错误。
//   - PIP_USER 会强制走 --user 安装到用户目录，覆盖 venv，破坏面板依赖隔离。
//   - PIP_INSTALL_OPTION 历史上是把任意 setup.py install 选项透传给 pip，
//     是 --home / --prefix 冲突的常见来源。
//   - PYTHONUSERBASE 决定 --user 安装的根目录，与 venv 同样冲突。
//   - PYTHONPATH / PYTHONHOME 会污染 pip / ensurepip 的解释器搜索路径，
//     Python 小版本升级后可能让 venv pip 找不到自身 pip 模块。
//
// 面板调用的 pip 始终来自托管 venv 或系统 pip，自带正确的安装目标，
// 不需要也不应该让上述变量参与。
var pipConflictingEnvKeys = map[string]struct{}{
	"PIP_PREFIX":         {},
	"PIP_HOME":           {},
	"PIP_TARGET":         {},
	"PIP_ROOT":           {},
	"PIP_USER":           {},
	"PIP_INSTALL_OPTION": {},
	"PYTHONUSERBASE":     {},
	"PYTHONPATH":         {},
	"PYTHONHOME":         {},
}

// SanitizePipEnv 移除所有可能与面板内部 pip 调用相冲突的环境变量。
// 已通过 PipInstallEnv 显式注入的变量（如 PIP_INDEX_URL）不会被剥离。
func SanitizePipEnv(base []string) []string {
	cleaned := make([]string, 0, len(base))
	for _, entry := range base {
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 {
			cleaned = append(cleaned, entry)
			continue
		}
		key := strings.ToUpper(entry[:idx])
		if _, conflicting := pipConflictingEnvKeys[key]; conflicting {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}

func NpmInstallEnv(base []string, configured string) []string {
	// 本机版（Android）：套用节点运行时策略（生命周期脚本开关），再强制打开生命周期脚本 ——
	// 原生插件（sharp/better-sqlite3 等）没有 install 脚本就装不出可用的二进制。
	env := ApplyNodeRuntimePolicy(append([]string{}, base...))
	env = appendEnvOverride(env, "NPM_CONFIG_IGNORE_SCRIPTS", "false")
	env = appendEnvOverride(env, "npm_config_ignore_scripts", "false")
	// 同 PipInstallEnv：npm 的 cache（$HOME/.npm）与 .npmrc 同样只认 HOME。
	env = homeRedirectEnv(env)
	mirror := EffectiveNpmMirror(configured)
	if mirror == "" {
		return env
	}

	env = append(env, "npm_config_registry="+mirror)
	env = append(env, "NPM_CONFIG_REGISTRY="+mirror)
	return env
}

func CurrentDependencyMirrorSettings() DependencyMirrorSettings {
	return DependencyMirrorSettings{
		PipMirror: strings.TrimSpace(CurrentPipMirror()),
		NpmMirror: strings.TrimSpace(CurrentNpmMirror()),
	}
}

// ApplyDependencyMirrorSettings 由备份恢复调用，把备份里存的 pip / npm 源原样写回。
//
// ⚠️ v3.3.2 的默认源变更（issue #146）在这条路上会被回灌覆盖：老备份里存的是阿里云，
// 恢复后 pip.conf 里就是阿里云，而 EffectivePipMirror 的一次性迁移只在「用户还没显式保存过」时生效，
// 所以恢复完是否会被搬到腾讯云，取决于该部署有没有落过 dependency-mirror-choice.saved 标记。
// 这是刻意的：备份恢复的语义就是「还原用户当时的选择」，不该由默认值变更来推翻。
func ApplyDependencyMirrorSettings(settings DependencyMirrorSettings) error {
	var errs []string
	if err := SetPipMirror(settings.PipMirror); err != nil {
		errs = append(errs, err.Error())
	}
	if err := SetNpmMirror(settings.NpmMirror); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func SetPipMirror(mirror string) error {
	mirror = strings.TrimSpace(mirror)
	if mirror != "" {
		// 这里刻意用 normalizePipMirrorForWrite 而不是 EffectivePipMirror：
		// 存盘要存用户真正选的值，旧默认源迁移只发生在读生效值那一侧（issue #146）。
		mirror = normalizePipMirrorForWrite(mirror)
		if !strings.HasPrefix(mirror, "http://") && !strings.HasPrefix(mirror, "https://") {
			return fmt.Errorf("pip 镜像源必须以 http:// 或 https:// 开头")
		}
	}
	return writePipMirrorConfig(mirror)
}

func SetNpmMirror(mirror string) error {
	mirror = strings.TrimSpace(mirror)
	if mirror != "" {
		mirror = EffectiveNpmMirror(mirror)
		if !strings.HasPrefix(mirror, "http://") && !strings.HasPrefix(mirror, "https://") {
			return fmt.Errorf("npm 镜像源必须以 http:// 或 https:// 开头")
		}
	}
	return writeNpmMirrorConfig(mirror)
}

func CurrentPipMirror() string {
	if out, err := os.ReadFile(pipMirrorConfigPath()); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "index-url") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

func CurrentNpmMirror() string {
	if out, err := os.ReadFile(npmConfigPath()); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "registry=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

func CurrentEffectivePipMirror() string {
	return EffectivePipMirror(CurrentPipMirror())
}

func CurrentEffectiveNpmMirror() string {
	return EffectiveNpmMirror(CurrentNpmMirror())
}

// v3.3.9 删掉了 isOfficialPipMirror / isOfficialNpmMirror：它们唯一的用途就是把官方源换成默认加速源，
// issue #165 起官方源原样保留，这两个判定没有调用方了。Linux 源的 isOfficialLinuxMirror 另有用途，不受影响。
func normalizeNpmMirror(mirror string) string {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		return ""
	}
	if !strings.HasSuffix(mirror, "/") {
		mirror += "/"
	}
	return mirror
}

func extractMirrorHost(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	hostPort := strings.SplitN(parts[0], ":", 2)
	return strings.TrimSpace(hostPort[0])
}

// 这两处读的必须是 EffectiveHomeDir 而不是裸 HOME：安装时 pip / npm 拿到的是
// 重定向之后的 HOME，配置要是还写在旧 HOME 下，就会出现「面板里改了镜像源、
// 装依赖时却读不到」的读写不对称。
func pipMirrorConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "pip", "pip.conf")
	}
	if home := EffectiveHomeDir(); home != "" {
		return filepath.Join(home, ".config", "pip", "pip.conf")
	}
	return ""
}

func npmConfigPath() string {
	if home := EffectiveHomeDir(); home != "" {
		return filepath.Join(home, ".npmrc")
	}
	return ""
}

func writePipMirrorConfig(mirror string) error {
	path := pipMirrorConfigPath()
	if path == "" {
		return nil
	}

	lines, err := readConfigLines(path)
	if err != nil {
		return err
	}

	host := ""
	if mirror != "" {
		host = extractMirrorHost(mirror)
	}

	result := make([]string, 0, len(lines)+4)
	inGlobal := false
	seenGlobal := false
	wroteIndexURL := false
	wroteTrustedHost := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isINISectionLine(trimmed) {
			if inGlobal {
				if mirror != "" && !wroteIndexURL {
					result = append(result, "index-url = "+mirror)
					wroteIndexURL = true
				}
				if host != "" && !wroteTrustedHost {
					result = append(result, "trusted-host = "+host)
					wroteTrustedHost = true
				}
			}

			inGlobal = isGlobalPipSection(trimmed)
			if inGlobal {
				seenGlobal = true
			}
			result = append(result, line)
			continue
		}

		if inGlobal {
			switch pipConfigKey(trimmed) {
			case "index-url":
				if mirror != "" && !wroteIndexURL {
					result = append(result, "index-url = "+mirror)
					wroteIndexURL = true
				}
				continue
			case "trusted-host":
				if host != "" && !wroteTrustedHost {
					result = append(result, "trusted-host = "+host)
					wroteTrustedHost = true
				}
				continue
			}
		}

		result = append(result, line)
	}

	if seenGlobal {
		if inGlobal {
			if mirror != "" && !wroteIndexURL {
				result = append(result, "index-url = "+mirror)
				wroteIndexURL = true
			}
			if host != "" && !wroteTrustedHost {
				result = append(result, "trusted-host = "+host)
				wroteTrustedHost = true
			}
		}
	} else if mirror != "" || host != "" {
		if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
			result = append(result, "")
		}
		result = append(result, "[global]")
		if mirror != "" {
			result = append(result, "index-url = "+mirror)
		}
		if host != "" {
			result = append(result, "trusted-host = "+host)
		}
	}

	return writeConfigLines(path, result)
}

func writeNpmMirrorConfig(mirror string) error {
	path := npmConfigPath()
	if path == "" {
		return nil
	}

	lines, err := readConfigLines(path)
	if err != nil {
		return err
	}

	result := make([]string, 0, len(lines)+1)
	wroteRegistry := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "registry=") {
			if mirror != "" && !wroteRegistry {
				result = append(result, "registry="+mirror)
				wroteRegistry = true
			}
			continue
		}
		result = append(result, line)
	}

	if mirror != "" && !wroteRegistry {
		result = append(result, "registry="+mirror)
	}

	return writeConfigLines(path, result)
}

func readConfigLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeConfigLines(path string, lines []string) error {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	hasContent := false
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			hasContent = true
			break
		}
	}

	if !hasContent {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func isINISectionLine(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func isGlobalPipSection(line string) bool {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
	return strings.EqualFold(name, "global")
}

func pipConfigKey(line string) string {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return ""
	}
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}
