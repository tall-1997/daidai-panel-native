package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultPipMirror = "https://mirrors.aliyun.com/pypi/simple"
	DefaultNpmMirror = "https://registry.npmmirror.com"
)

type DependencyMirrorSettings struct {
	PipMirror string `json:"pip_mirror,omitempty"`
	NpmMirror string `json:"npm_mirror,omitempty"`
}

func EffectivePipMirror(configured string) string {
	mirror := strings.TrimSpace(configured)
	if mirror == "" || isOfficialPipMirror(mirror) {
		return DefaultPipMirror
	}
	return mirror
}

func EffectiveNpmMirror(configured string) string {
	mirror := normalizeNpmMirror(configured)
	if mirror == "" || isOfficialNpmMirror(mirror) {
		return normalizeNpmMirror(DefaultNpmMirror)
	}
	return mirror
}

func PipInstallEnv(base []string, configured string) []string {
	env := SanitizePipEnv(base)
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
	env := append([]string{}, base...)
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
		mirror = EffectivePipMirror(mirror)
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

func isOfficialPipMirror(mirror string) bool {
	mirror = strings.TrimRight(strings.ToLower(strings.TrimSpace(mirror)), "/")
	switch mirror {
	case "https://pypi.org/simple", "http://pypi.org/simple",
		"https://pypi.python.org/simple", "http://pypi.python.org/simple":
		return true
	default:
		return false
	}
}

func isOfficialNpmMirror(mirror string) bool {
	mirror = normalizeNpmMirror(mirror)
	return mirror == "https://registry.npmjs.org/"
}

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

func pipMirrorConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "pip", "pip.conf")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "pip", "pip.conf")
	}
	return ""
}

func npmConfigPath() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
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
