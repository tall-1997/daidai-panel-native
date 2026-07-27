package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

const defaultPythonRuntimeVersion = "3.12"

var allPythonRuntimeVersions = []string{"3.10", "3.11", "3.12"}

type PythonRuntimeInfo struct {
	Version     string `json:"version"`
	Label       string `json:"label"`
	Default     bool   `json:"default"`
	VenvPath    string `json:"venv_path"`
	VenvHealthy bool   `json:"venv_healthy"`
	PythonPath  string `json:"python_path"`
	PipPath     string `json:"pip_path"`
	Available   bool   `json:"available"`
	Message     string `json:"message"`
}

func SupportedPythonVersions() []string {
	return CurrentPythonRuntimeVersions()
}

func CurrentPythonRuntimeVersions() []string {
	version, single := SinglePythonRuntimeVersion()
	if single {
		return []string{version}
	}
	return append([]string(nil), allPythonRuntimeVersions...)
}

func SinglePythonRuntimeVersion() (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("DAIDAI_PYTHON_RUNTIME_MODE")))
	if mode != "single" {
		return "", false
	}

	rawVersion := strings.TrimSpace(os.Getenv("DAIDAI_PYTHON_VERSION"))
	if rawVersion == "" {
		return defaultPythonRuntimeVersion, true
	}
	value := strings.ToLower(rawVersion)
	value = strings.TrimPrefix(value, "python")
	value = strings.TrimPrefix(value, "py")
	value = strings.TrimSpace(value)
	switch value {
	case "3.10", "310":
		return "3.10", true
	case "3.11", "311":
		return "3.11", true
	case "3", "3.12", "312":
		return "3.12", true
	default:
		return defaultPythonRuntimeVersion, true
	}
}

func PythonVersionSupportedByCurrentRuntime(version string) bool {
	version = NormalizePythonVersionOrDefault(version)
	for _, candidate := range CurrentPythonRuntimeVersions() {
		if candidate == version {
			return true
		}
	}
	return false
}

// 模块版（Magisk / KernelSU / APatch）目前通常只有一个系统 python3，
// 不一定真的同时具备 3.10 / 3.11 / 3.12 三套解释器。
// v2.2.19 之后默认 Python 版本固定走 3.12，多版本逻辑在 Docker / Windows 没问题，
// 但在模块版里会把所有历史任务都打成“Python 3.12 不可用”。
//
// 这里把“模块运行态”的判断提到 service 层，供 Python 版本决策直接复用，
// 避免只在 handler / shell 脚本里知道自己是模块版，真正执行任务时却还按通用服务器逻辑硬判 3.12。
func IsMagiskModuleRuntime() bool {
	if strings.TrimSpace(os.Getenv("DAIDAI_MAGISK_MODULE")) != "" {
		return true
	}
	for _, marker := range []string{
		"/data/adb/daidai-panel/ports.conf",
		"/data/adb/modules/daidai-panel/module.prop",
		"/data/adb/modules_update/daidai-panel/module.prop",
	} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

func resolveEffectivePythonVersionForCurrentRuntime(raw string) string {
	requested := NormalizePythonVersionOrDefault(raw)
	version, fellBack := resolvePythonFallbackVersion(
		requested,
		CurrentPythonRuntimeVersions(),
		func(v string) bool { return discoverSystemPythonForVersion(v) != "" },
	)
	if fellBack {
		// 回退实际发生，记录 panel 日志，便于用户理解任务为何改用了其它 Python 版本。
		log.Printf("任务请求 Python %s 未安装，已回退到 %s", requested, version)
	}
	return version
}

// resolvePythonFallbackVersion 决定运行时最终使用的 Python 小版本。
// 核心不变量：请求版本只要已安装(probe 命中)就绝不回退，始终尊重请求版本；
// 只有请求版本探测不到、且受支持集合里另有已装版本时，才回退到那个已装版本。
// 单版本运行时(Docker 固定版本)受支持集合只含一个版本，因此永远不会回退到别的版本，语义安全。
// 桌面/二进制版与 Magisk 版共用本函数，回退行为一致。
// probe 抽为参数便于单测注入，避免真实 exec 探测；返回 (最终版本, 是否发生回退)。
func resolvePythonFallbackVersion(requested string, supported []string, probe func(string) bool) (string, bool) {
	if probe(requested) {
		return requested, false
	}
	for _, candidate := range supported {
		if candidate == requested {
			continue
		}
		if probe(candidate) {
			return candidate, true
		}
	}
	return requested, false
}

func LegacyPythonVersion() string {
	return defaultPythonRuntimeVersion
}

func NormalizeDependencyPythonVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return LegacyPythonVersion()
	}
	return NormalizePythonVersionOrDefault(raw)
}

func NormalizePythonVersionStrict(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "python")
	value = strings.TrimPrefix(value, "py")
	value = strings.TrimSpace(value)
	switch value {
	case "", "3", "3.12", "312":
		if value == "" {
			return DefaultPythonVersion(), nil
		}
		return "3.12", nil
	case "3.10", "310":
		return "3.10", nil
	case "3.11", "311":
		return "3.11", nil
	default:
		return "", fmt.Errorf("Python 版本仅支持 3.10、3.11、3.12")
	}
}

func NormalizePythonVersionOrDefault(raw string) string {
	version, err := NormalizePythonVersionStrict(raw)
	if err != nil || strings.TrimSpace(version) == "" {
		return defaultPythonRuntimeVersion
	}
	return version
}

func DefaultPythonVersion() string {
	if version, single := SinglePythonRuntimeVersion(); single {
		return resolveEffectivePythonVersionForCurrentRuntime(version)
	}

	raw := ""
	if config.C != nil && config.C.Data.Dir != "" && database.DB != nil {
		raw = model.GetRegisteredConfig("python_default_version")
	}
	if strings.TrimSpace(raw) == "" {
		return resolveEffectivePythonVersionForCurrentRuntime(defaultPythonRuntimeVersion)
	}
	version, err := NormalizePythonVersionStrict(raw)
	if err != nil || version == "" {
		return resolveEffectivePythonVersionForCurrentRuntime(defaultPythonRuntimeVersion)
	}
	return resolveEffectivePythonVersionForCurrentRuntime(version)
}

func ResolvePythonVersionFromEnv(envVars map[string]string) string {
	if envVars == nil {
		return DefaultPythonVersion()
	}
	version := resolveEffectivePythonVersionForCurrentRuntime(envVars["DAIDAI_PYTHON_VERSION"])
	if !PythonVersionSupportedByCurrentRuntime(version) {
		return DefaultPythonVersion()
	}
	return version
}

func ResolvePythonVersionFromInterpreter(interpreter string) string {
	value := strings.ToLower(strings.TrimSpace(interpreter))
	switch value {
	case "python3.10":
		return "3.10"
	case "python3.11":
		return "3.11"
	case "python3.12":
		return "3.12"
	default:
		return ""
	}
}

func IsPythonInterpreter(interpreter string) bool {
	switch strings.ToLower(strings.TrimSpace(interpreter)) {
	case "python", "python3", "python3.10", "python3.11", "python3.12":
		return true
	default:
		return false
	}
}

func ManagedPythonVenvDir(version string) string {
	version = NormalizePythonVersionOrDefault(version)
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	pythonDir := filepath.Join(dataDir, "deps", "python")
	return filepath.Join(pythonDir, version)
}

func legacyManagedPythonVenvDir() string {
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	return filepath.Join(dataDir, "deps", "python", "venv")
}

func NormalizeLegacyPythonVersionColumns(version string) {
	if database.DB == nil {
		return
	}

	version = NormalizePythonVersionOrDefault(version)
	if err := database.DB.Exec("UPDATE dependencies SET python_version = ? WHERE type = ? AND (python_version IS NULL OR python_version = '')", version, model.DepTypePython).Error; err != nil {
		log.Printf("warn: failed to normalize legacy python dependency versions: %v", err)
	}
	if err := database.DB.Exec("UPDATE tasks SET python_version = ? WHERE python_version IS NULL OR python_version = ''", version).Error; err != nil {
		log.Printf("warn: failed to normalize legacy task python versions: %v", err)
	}
}

func NormalizeLegacyPythonVersionColumnsAfterVenvMigration(migration LegacyPythonVenvMigration) {
	version := NormalizePythonVersionOrDefault(migration.Version)
	NormalizeLegacyPythonVersionColumns(version)

	if !migration.MigratedRoot || version == defaultPythonRuntimeVersion || migration.DefaultVersionExisted || database.DB == nil {
		return
	}

	if err := database.DB.Exec("UPDATE dependencies SET python_version = ? WHERE type = ? AND python_version = ?", version, model.DepTypePython, defaultPythonRuntimeVersion).Error; err != nil {
		log.Printf("warn: failed to move legacy python dependency records to detected version %s: %v", version, err)
	}
	if err := database.DB.Exec("UPDATE tasks SET python_version = ? WHERE python_version = ?", version, defaultPythonRuntimeVersion).Error; err != nil {
		log.Printf("warn: failed to move legacy python task records to detected version %s: %v", version, err)
	}
	if strings.TrimSpace(model.GetRegisteredConfig("python_default_version")) == defaultPythonRuntimeVersion {
		if err := model.SetConfig("python_default_version", version); err != nil {
			log.Printf("warn: failed to update default python version to detected legacy version %s: %v", version, err)
		}
	}
}

func ApplySinglePythonRuntimePolicyOnStartup() {
	version, single := SinglePythonRuntimeVersion()
	if !single || database.DB == nil {
		return
	}

	// 单版本 Docker 镜像只保留一个 Python 小版本。旧版 latest 曾经内置三套 Python，
	// 用户升级到新的 single 镜像后，需要把系统默认值和任务显式版本统一切回镜像版本，
	// 否则历史任务仍可能指向已被删除的 3.10 / 3.11 环境。
	if err := model.SetConfig("python_default_version", version); err != nil {
		log.Printf("warn: failed to reset python_default_version to image runtime %s: %v", version, err)
	}
	if err := database.DB.Model(&model.Task{}).
		Where("python_version IS NULL OR python_version = '' OR python_version <> ?", version).
		Update("python_version", version).Error; err != nil {
		log.Printf("warn: failed to reset task python versions to image runtime %s: %v", version, err)
	}
}

func PythonRuntimeInfos() []PythonRuntimeInfo {
	defaultVersion := DefaultPythonVersion()
	versions := CurrentPythonRuntimeVersions()
	infos := make([]PythonRuntimeInfo, 0, len(versions))
	for _, version := range versions {
		venvDir := ManagedPythonVenvDir(version)
		info := PythonRuntimeInfo{
			Version:     version,
			Label:       "Python " + version,
			Default:     version == defaultVersion,
			VenvPath:    venvDir,
			VenvHealthy: managedPythonVenvHealthyForVersion(venvDir, version),
		}
		if info.VenvHealthy {
			info.PythonPath = resolveManagedPythonBinaryInVenv(venvDir)
			info.PipPath = resolveManagedPipBinaryInVenv(venvDir)
			info.Available = true
			info.Message = "托管环境可用"
		} else if binary := discoverSystemPythonForVersion(version); binary != "" {
			info.PythonPath = binary
			info.Available = true
			info.Message = "检测到系统解释器，首次使用时会创建独立依赖环境"
		} else {
			info.Message = fmt.Sprintf("未检测到 Python %s，请先在服务器安装 python%s 或 Windows py -%s", version, version, version)
		}
		infos = append(infos, info)
	}
	return infos
}

func discoverSystemPythonForVersion(version string) string {
	for _, candidate := range managedPythonBootstrapCommandsForVersion(version) {
		if managedBootstrapCommandMatchesVersion(candidate, version) {
			return strings.Join(append([]string{candidate.binary}, candidate.versionArgsPrefix...), " ")
		}
	}
	return ""
}

func managedBootstrapCommandMatchesVersion(candidate managedBootstrapCommand, version string) bool {
	args := append([]string{}, candidate.versionArgsPrefix...)
	args = append(args, "--version")
	cmd := exec.Command(candidate.binary, args...)
	cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return pythonVersionOutputMatches(out, version)
}

func pythonVersionOutputMatches(out []byte, version string) bool {
	text := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(text, "python "+version+".") || strings.Contains(text, "python "+version)
}

func windowsPythonPreferredDirsForVersion(version string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	suffix := strings.ReplaceAll(version, ".", "")
	dirs := []string{
		filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python"+suffix),
		filepath.Join(os.Getenv("ProgramFiles"), "Python"+suffix),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Python"+suffix),
	}
	return append(dirs, windowsPythonPreferredDirs...)
}
