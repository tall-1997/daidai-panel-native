package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

type managedRuntimePaths struct {
	NodeBin          string
	NodeModules      string
	VenvBin          string
	VenvSitePackages string
	SanitizedPath    string

	searchDirs []string
}

var managedPythonVenvMu sync.Mutex

var warmManagedPythonVenvForVersionFunc = func(version string) {
	ensureManagedPythonVenvForVersion(version, false)
}

var cleanupBrokenManagedPythonVenvsFunc = cleanupBrokenManagedPythonVenvs

var windowsShellSearchDirs = []string{
	filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin"),
	filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin"),
	filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin"),
	filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "usr", "bin"),
}

var windowsPythonPreferredDirs = []string{
	filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python314"),
	filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python313"),
	filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python312"),
	filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python311"),
	filepath.Join(os.Getenv("LocalAppData"), "Programs", "Python", "Python310"),
}

// pythonEnvBootstrap 只负责四件事：
//  1. Windows 下先按启动环境初始化 Python 本地时区缓存
//  2. 从 env.json 注入任务环境变量到 os.environ
//  3. 把 PYTHONPATH 里声明的目录前置到 sys.path（工作目录、脚本目录、venv site-packages）
//  4. 以 runpy.run_path 的方式执行用户脚本
//
// 历史上这里还有"AST 扫 import + importlib.find_spec 判缺失 + 自动 pip install"
// 的预检链路（v2.0.7 引入），但 find_spec 在 Alpine venv 下对 pysmx 等包会漏判，
// 导致已装好的包反复被判定缺失、循环触发 pip install。真实缺失的包由
// Go 侧 task_executor.detectAndInstallDeps 兜底——它在脚本真实抛出
// ModuleNotFoundError 时介入，基于正则抓模块名后 pip install，并自动重跑脚本，
// 比预检更精准，且最多重试 5 次覆盖多依赖场景。
const pythonEnvBootstrap = `import json, os, runpy, sys, time
env_file, script_path, extra_path_raw = sys.argv[1:4]
script_args = sys.argv[4:]
if os.name == "nt":
    time.localtime()
with open(env_file, "r", encoding="utf-8") as fh:
    payload = json.load(fh)
for key, value in payload.items():
    if value is None:
        continue
    os.environ[str(key)] = str(value)
for entry in reversed([item for item in extra_path_raw.split(os.pathsep) if item]):
    if entry not in sys.path:
        sys.path.insert(0, entry)
sys.argv = [script_path] + script_args
runpy.run_path(script_path, run_name="__main__")
`

const pythonModuleEnvBootstrap = `import json, os, runpy, sys, time
env_file, module_name, extra_path_raw = sys.argv[1:4]
module_args = sys.argv[4:]
if os.name == "nt":
    time.localtime()
with open(env_file, "r", encoding="utf-8") as fh:
    payload = json.load(fh)
for key, value in payload.items():
    if value is None:
        continue
    os.environ[str(key)] = str(value)
for entry in reversed([item for item in extra_path_raw.split(os.pathsep) if item]):
    if entry not in sys.path:
        sys.path.insert(0, entry)
sys.argv = [module_name] + module_args
runpy.run_module(module_name, run_name="__main__", alter_sys=True)
`

const shellEnvBootstrap = `__dd_env_file=$1
__dd_script=$2
shift 2
export DAIDAI_RUNTIME_SHELL_ENV_FILE="$__dd_env_file"
if [ -f "$__dd_env_file" ]; then
  . "$__dd_env_file"
fi
. "$__dd_script" "$@"
`

const shellEnvExportValueMaxBytes = 128 * 1024
const shellEnvExportBudgetBytes = 512 * 1024

const goEnvBootstrapSource = `package main

import (
	"encoding/json"
	"os"
)

func init() {
	envFile := os.Getenv("DAIDAI_RUNTIME_ENV_FILE")
	if envFile == "" {
		return
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		return
	}
	payload := map[string]string{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	for key, value := range payload {
		if key == "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
`

func BuildManagedRuntimeEnvMap(workDir, scriptsDir string, defaultChannelID *uint, ttl time.Duration) (map[string]string, error) {
	return BuildManagedRuntimeEnvMapForPythonVersion(workDir, scriptsDir, defaultChannelID, ttl, "")
}

func BuildManagedRuntimeEnvMapForPythonVersion(workDir, scriptsDir string, defaultChannelID *uint, ttl time.Duration, pythonVersion string) (map[string]string, error) {
	var envVarRecords []model.EnvVar
	// 按稳定顺序读取：置顶 > 组内位置 > 创建时间 > id；避免无 ORDER BY 导致同名变量的相对顺序抖动
	database.DB.Where("enabled = ?", true).
		Order("sort_order DESC, position ASC, created_at ASC, id ASC").
		Find(&envVarRecords)

	// 先按 name 分组保持顺序，再用 joinTaskEnvValues 做带转义合并，
	// 解决值内含 '&' 时脚本按 '&' 切分会错位的问题（与 splitTaskEnvValues 对称）。
	grouped := make(map[string][]string)
	order := make([]string, 0, len(envVarRecords))
	for _, ev := range envVarRecords {
		if _, ok := grouped[ev.Name]; !ok {
			order = append(order, ev.Name)
		}
		grouped[ev.Name] = append(grouped[ev.Name], ev.Value)
	}

	envMap := make(map[string]string, len(grouped))
	for _, name := range order {
		envMap[name] = joinTaskEnvValues(grouped[name])
	}

	loadConfigShellVars(envMap)
	// 面板时区是全局运行时配置，优先级高于普通环境变量，避免任务脚本继续继承 UTC。
	envMap["TZ"] = CurrentPanelTimezone()

	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		pythonVersion = DefaultPythonVersion()
	}
	envMap["DAIDAI_PYTHON_VERSION"] = pythonVersion

	runtimePaths := currentManagedRuntimePathsForPythonVersion(pythonVersion)
	if runtimePaths.NodeModules != "" {
		envMap["NODE_PATH"] = runtimePaths.NodeModules
	}
	if runtimePaths.SanitizedPath != "" {
		envMap["PATH"] = joinPathSegments(runtimePaths.VenvBin, runtimePaths.SanitizedPath, runtimePaths.NodeBin)
	}
	if pythonPath := buildManagedPythonPath(envMap["PYTHONPATH"], workDir, scriptsDir, runtimePaths.VenvSitePackages); pythonPath != "" {
		envMap["PYTHONPATH"] = pythonPath
	}
	AppendScriptHelperPaths(envMap, scriptsDir)
	var helperErr error
	if helperEnv, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, defaultChannelID, ttl); err == nil {
		for key, value := range helperEnv {
			envMap[key] = value
		}
	} else {
		helperErr = err
	}

	return envMap, helperErr
}

func buildManagedPythonPath(existingPythonPath, workDir, scriptsDir, venvSitePackages string) string {
	return joinPathSegments(workDir, scriptsDir, existingPythonPath, venvSitePackages)
}

func CreateManagedCommand(interpreter, scriptPath string, scriptArgs []string, workDir string, envVars map[string]string) (*exec.Cmd, func(), error) {
	pythonVersion := ResolvePythonVersionFromEnv(envVars)
	if versionFromInterpreter := ResolvePythonVersionFromInterpreter(interpreter); versionFromInterpreter != "" {
		pythonVersion = versionFromInterpreter
		if envVars != nil {
			envVars["DAIDAI_PYTHON_VERSION"] = pythonVersion
		}
	}
	runtimePaths := currentManagedRuntimePathsForPythonVersion(pythonVersion)

	switch {
	case IsPythonInterpreter(interpreter):
		return createManagedPythonCommand(scriptPath, scriptArgs, workDir, envVars, runtimePaths, pythonVersion)
	}

	switch interpreter {
	case "node":
		return createManagedNodeCommand(scriptPath, scriptArgs, workDir, envVars, runtimePaths)
	case "ts-node":
		return createManagedTSNodeCommand(scriptPath, scriptArgs, workDir, envVars, runtimePaths)
	default:
		return createStandardManagedCommand(interpreter, scriptPath, scriptArgs, workDir, envVars, runtimePaths)
	}
}

func currentManagedRuntimePaths() managedRuntimePaths {
	return currentManagedRuntimePathsForPythonVersion("")
}

func currentManagedRuntimePathsForPythonVersion(pythonVersion string) managedRuntimePaths {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	depsDir := filepath.Join(dataDir, "deps")
	venvDir := ManagedPythonVenvDir(pythonVersion)
	venvBin := resolveManagedVenvBin(venvDir)
	nodeBin := filepath.Join(depsDir, "nodejs", "node_modules", ".bin")
	sanitizedPath := sanitizeManagedPath(os.Getenv("PATH"), nodeBin, venvBin)

	return managedRuntimePaths{
		NodeBin:          nodeBin,
		NodeModules:      filepath.Join(depsDir, "nodejs", "node_modules"),
		VenvBin:          venvBin,
		VenvSitePackages: findVenvSitePackages(venvDir),
		SanitizedPath:    sanitizedPath,
		searchDirs:       splitPathDirs(sanitizedPath),
	}
}

func managedPythonVenvHealthy(venvDir string) bool {
	return managedPythonVenvHealthyForVersion(venvDir, "")
}

func managedPythonVenvHealthyForVersion(venvDir, pythonVersion string) bool {
	venvBin := resolveManagedVenvBin(venvDir)
	if info, err := os.Stat(venvBin); err != nil || !info.IsDir() {
		return false
	}
	pythonBin := resolveManagedPythonBinaryInVenv(venvDir)
	if pythonBin == "" {
		return false
	}
	if version := strings.TrimSpace(pythonVersion); version != "" && !managedPythonBinaryMatchesVersion(pythonBin, version) {
		return false
	}

	pipBin := resolveManagedPipBinaryInVenv(venvDir)
	if pipBin == "" {
		return false
	}

	cmd := exec.Command(pipBin, "--version")
	cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "pip")
}

func managedPythonBinaryMatchesVersion(pythonBin, pythonVersion string) bool {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	cmd := exec.Command(pythonBin, "-c", "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
	cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) == pythonVersion
}

func resolveManagedPythonBinaryInVenv(venvDir string) string {
	venvBin := resolveManagedVenvBin(venvDir)
	for _, name := range []string{"python", "python3"} {
		if binary := findExecutableInDir(venvBin, name); binary != "" {
			return binary
		}
	}
	return ""
}

func resolveManagedPipBinaryInVenv(venvDir string) string {
	venvBin := resolveManagedVenvBin(venvDir)
	for _, name := range []string{"pip3", "pip"} {
		if binary := findExecutableInDir(venvBin, name); binary != "" {
			return binary
		}
	}
	return ""
}

func repairManagedPythonVenvPip(venvDir string) bool {
	pythonBin := resolveManagedPythonBinaryInVenv(venvDir)
	if pythonBin == "" {
		return false
	}

	cmd := exec.Command(pythonBin, "-m", "ensurepip", "--upgrade")
	cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("warn: managed python venv pip repair failed at %s: %v: %s", venvDir, err, strings.TrimSpace(string(out)))
		return false
	}
	return managedPythonVenvHealthy(venvDir)
}

func quarantineManagedPythonVenv(venvDir string) {
	venvDir = strings.TrimSpace(venvDir)
	if venvDir == "" {
		return
	}
	if _, err := os.Stat(venvDir); err != nil {
		return
	}

	backup := venvDir + ".broken-" + time.Now().Format("20060102150405")
	if err := os.Rename(venvDir, backup); err == nil {
		log.Printf("managed python venv moved aside for rebuild: %s -> %s", venvDir, backup)
		return
	}
	if err := os.RemoveAll(venvDir); err != nil {
		log.Printf("warn: failed to remove broken managed python venv %s: %v", venvDir, err)
	}
}

func cleanupBrokenManagedPythonVenvs() {
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	if strings.TrimSpace(dataDir) == "" {
		return
	}

	pythonDir := filepath.Join(dataDir, "deps", "python")
	entries, err := os.ReadDir(pythonDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if !strings.Contains(name, ".broken-") {
			continue
		}
		fullPath := filepath.Join(pythonDir, name)
		if err := os.RemoveAll(fullPath); err != nil {
			log.Printf("warn: failed to cleanup broken managed python venv backup %s: %v", fullPath, err)
			continue
		}
		log.Printf("cleanup broken managed python venv backup: %s", fullPath)
	}
}

func CleanupManagedPythonArtifactsOnStartup() {
	managedPythonVenvMu.Lock()
	defer managedPythonVenvMu.Unlock()

	migrateLegacyManagedPythonVenvLocked()
	cleanupUnsupportedManagedPythonVenvsLocked()
	cleanupBrokenManagedPythonVenvs()
}

func detectManagedPythonVenvVersion(venvDir string) string {
	pythonBin := resolveManagedPythonBinaryInVenv(venvDir)
	if pythonBin == "" {
		return ""
	}

	cmd := exec.Command(pythonBin, "-c", "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
	cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	version, err := NormalizePythonVersionStrict(strings.TrimSpace(string(out)))
	if err != nil {
		return ""
	}
	return version
}

type LegacyPythonVenvMigration struct {
	Version               string
	FoundRoot             bool
	MigratedRoot          bool
	DefaultVersionExisted bool
}

func managedPythonVersionDir(version string) string {
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	return filepath.Join(dataDir, "deps", "python", NormalizePythonVersionOrDefault(version))
}

func cleanupUnsupportedManagedPythonVenvsLocked() {
	allowed := make(map[string]bool)
	for _, version := range CurrentPythonRuntimeVersions() {
		allowed[version] = true
	}
	if len(allowed) >= len(allPythonRuntimeVersions) {
		return
	}

	// 只删除面板托管的 Python 小版本目录，不碰 nodejs、脚本、备份和未知目录。
	// 这样旧版三 Python 镜像升级到单版本镜像后，会自动释放 3.10 / 3.11 venv 空间，
	// 同时保留当前镜像版本（默认 latest 为 3.12）的依赖环境。
	for _, version := range allPythonRuntimeVersions {
		if allowed[version] {
			continue
		}
		versionDir := managedPythonVersionDir(version)
		if !directoryExists(versionDir) {
			continue
		}
		if err := os.RemoveAll(versionDir); err != nil {
			log.Printf("warn: failed to remove unsupported managed python %s venv %s: %v", version, versionDir, err)
			continue
		}
		log.Printf("unsupported managed python %s venv removed for current image policy: %s", version, versionDir)
	}
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func flattenLegacyVersionedManagedPythonVenvsLocked() {
	for _, version := range allPythonRuntimeVersions {
		versionDir := managedPythonVersionDir(version)
		nestedVenvDir := filepath.Join(versionDir, "venv")
		if !directoryExists(nestedVenvDir) {
			continue
		}
		if directoryExists(resolveManagedVenvBin(versionDir)) {
			backup := nestedVenvDir + ".nested-" + time.Now().Format("20060102150405")
			if err := os.Rename(nestedVenvDir, backup); err == nil {
				log.Printf("legacy nested managed python %s venv moved aside because flat target already exists: %s -> %s", version, nestedVenvDir, backup)
			} else {
				log.Printf("warn: failed to move nested managed python %s venv aside %s: %v", version, nestedVenvDir, err)
			}
			continue
		}

		backupDir := versionDir + ".pre-2218-" + time.Now().Format("20060102150405")
		if err := os.Rename(versionDir, backupDir); err != nil {
			log.Printf("warn: failed to prepare legacy managed python %s venv flatten %s: %v", version, versionDir, err)
			continue
		}
		if err := os.Rename(filepath.Join(backupDir, "venv"), versionDir); err != nil {
			log.Printf("warn: failed to flatten legacy managed python %s venv %s -> %s: %v", version, filepath.Join(backupDir, "venv"), versionDir, err)
			_ = os.Rename(backupDir, versionDir)
			continue
		}
		_ = os.Remove(backupDir)
		log.Printf("legacy managed python %s venv flattened: %s -> %s", version, filepath.Join(backupDir, "venv"), versionDir)
	}
}

func migrateLegacyManagedPythonVenvLocked() LegacyPythonVenvMigration {
	flattenLegacyVersionedManagedPythonVenvsLocked()

	result := LegacyPythonVenvMigration{
		DefaultVersionExisted: directoryExists(ManagedPythonVenvDir(defaultPythonRuntimeVersion)),
	}

	legacyDir := legacyManagedPythonVenvDir()
	if strings.TrimSpace(legacyDir) == "" {
		return result
	}
	if info, err := os.Stat(legacyDir); err != nil || !info.IsDir() {
		return result
	}

	version := detectManagedPythonVenvVersion(legacyDir)
	if version == "" {
		version = defaultPythonRuntimeVersion
		log.Printf("warn: legacy managed python venv version detection failed at %s, assuming Python %s", legacyDir, version)
	}
	result.Version = version
	result.FoundRoot = true

	targetDir := ManagedPythonVenvDir(version)
	if filepath.Clean(targetDir) == filepath.Clean(legacyDir) {
		return result
	}
	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		backup := legacyDir + ".migrated-" + time.Now().Format("20060102150405")
		if err := os.Rename(legacyDir, backup); err == nil {
			log.Printf("legacy managed python venv kept aside because target already exists: %s -> %s", legacyDir, backup)
		} else {
			log.Printf("warn: failed to move legacy managed python venv aside %s: %v", legacyDir, err)
		}
		return result
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		log.Printf("warn: failed to create managed python version dir parent %s: %v", filepath.Dir(targetDir), err)
		return result
	}
	if err := os.Rename(legacyDir, targetDir); err != nil {
		log.Printf("warn: failed to migrate legacy managed python venv %s -> %s: %v", legacyDir, targetDir, err)
		return result
	}
	log.Printf("legacy managed python venv migrated: %s -> %s (Python %s)", legacyDir, targetDir, version)
	result.MigratedRoot = true
	return result
}

func MigrateLegacyManagedPythonVenv() string {
	return MigrateLegacyManagedPythonVenvInfo().Version
}

func MigrateLegacyManagedPythonVenvInfo() LegacyPythonVenvMigration {
	managedPythonVenvMu.Lock()
	defer managedPythonVenvMu.Unlock()
	return migrateLegacyManagedPythonVenvLocked()
}

func ensureManagedPythonVenv(syncCreate bool) bool {
	return ensureManagedPythonVenvForVersion("", syncCreate)
}

func ensureManagedPythonVenvForVersion(pythonVersion string, syncCreate bool) bool {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		return false
	}
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	if strings.TrimSpace(dataDir) == "" {
		return false
	}

	venvDir := ManagedPythonVenvDir(pythonVersion)
	if managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
		return true
	}

	if !syncCreate {
		go ensureManagedPythonVenvForVersion(pythonVersion, true)
		return false
	}

	managedPythonVenvMu.Lock()
	defer managedPythonVenvMu.Unlock()

	migrateLegacyManagedPythonVenvLocked()

	if managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
		return true
	}

	if info, err := os.Stat(resolveManagedVenvBin(venvDir)); err == nil && info.IsDir() {
		if repairManagedPythonVenvPip(venvDir) && managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
			log.Printf("managed python venv pip repaired at %s", venvDir)
			return true
		}
		quarantineManagedPythonVenv(venvDir)
	}

	_ = os.MkdirAll(filepath.Dir(venvDir), 0o755)
	var lastErr error
	for _, candidate := range managedPythonBootstrapCommandsForVersion(pythonVersion) {
		if !managedBootstrapCommandMatchesVersion(candidate, pythonVersion) {
			continue
		}
		// v2.2.4 重构 bootstrap 命令表时漏了把 venvDir 拼到 args 末尾，
		// 导致执行的是 `python3 -m venv`（不带目标路径）必然失败。venv 永远建不出来，
		// ResolveManagedPipBinary 返回空，自动安装 fallback 到系统 pip3，
		// Alpine/Debian 上的 PEP 668 把"externally-managed-environment"砸到用户脸上。
		args := append(append([]string(nil), candidate.args...), venvDir)
		cmd := exec.Command(candidate.binary, args...)
		cmd.Env = appendPythonBootstrapEnv(SanitizePipEnv(os.Environ()))
		out, runErr := cmd.CombinedOutput()
		if runErr == nil {
			if managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
				log.Printf("managed python %s venv created at %s using %s", pythonVersion, venvDir, candidate.binary)
				return true
			}
			if repairManagedPythonVenvPip(venvDir) && managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
				log.Printf("managed python %s venv created and pip repaired at %s using %s", pythonVersion, venvDir, candidate.binary)
				return true
			}
			lastErr = fmt.Errorf("%s %v created venv but pip is unavailable", candidate.binary, args)
			quarantineManagedPythonVenv(venvDir)
			continue
		}
		lastErr = fmt.Errorf("%s %v failed: %v: %s", candidate.binary, args, runErr, strings.TrimSpace(string(out)))
		quarantineManagedPythonVenv(venvDir)
	}
	if lastErr != nil {
		log.Printf("warn: managed python %s venv create failed: %v (auto-install will fall back to system pip with --break-system-packages)", pythonVersion, lastErr)
	}
	return false
}

func EnsureManagedPythonVenv() bool {
	return EnsureManagedPythonVenvForVersion("")
}

func EnsureManagedPythonVenvForVersion(pythonVersion string) bool {
	return ensureManagedPythonVenvForVersion(pythonVersion, true)
}

func WarmManagedPythonVenv() {
	cleanupBrokenManagedPythonVenvsFunc()

	defaultVersion := DefaultPythonVersion()
	warmed := map[string]bool{}
	if strings.TrimSpace(defaultVersion) != "" {
		WarmManagedPythonVenvForVersion(defaultVersion)
		warmed[defaultVersion] = true
	}

	for _, version := range CurrentPythonRuntimeVersions() {
		if warmed[version] {
			continue
		}
		if directoryExists(ManagedPythonVenvDir(version)) {
			WarmManagedPythonVenvForVersion(version)
		}
	}
}

func WarmManagedPythonVenvForVersion(pythonVersion string) {
	warmManagedPythonVenvForVersionFunc(pythonVersion)
}

func resolveManagedVenvBin(venvDir string) string {
	venvDir = strings.TrimSpace(venvDir)
	if venvDir == "" {
		return ""
	}

	candidates := []string{
		filepath.Join(venvDir, "Scripts"),
		filepath.Join(venvDir, "bin"),
	}
	if runtime.GOOS != "windows" {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts")
	}
	return filepath.Join(venvDir, "bin")
}

func ResolveManagedPipBinary() string {
	return ResolveManagedPipBinaryForPythonVersion("")
}

func ResolveManagedPipBinaryForPythonVersion(pythonVersion string) string {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		return ""
	}
	EnsureManagedPythonVenvForVersion(pythonVersion)
	venvDir := ManagedPythonVenvDir(pythonVersion)
	if !managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
		return ""
	}
	return resolveManagedPipBinaryInVenv(venvDir)
}

// ResolveManagedPythonBinary 返回面板默认版本托管 venv 的 python 可执行文件路径，
// 供 ddp 等容器内命令在与任务执行一致的环境里跑脚本/开 shell。venv 缺失时会先按需创建。
func ResolveManagedPythonBinary() string {
	return ResolveManagedPythonBinaryForPythonVersion("")
}

func ResolveManagedPythonBinaryForPythonVersion(pythonVersion string) string {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		return ""
	}
	EnsureManagedPythonVenvForVersion(pythonVersion)
	venvDir := ManagedPythonVenvDir(pythonVersion)
	if !managedPythonVenvHealthyForVersion(venvDir, pythonVersion) {
		return ""
	}
	return resolveManagedPythonBinaryInVenv(venvDir)
}

func createManagedPythonCommand(scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths, pythonVersion string) (*exec.Cmd, func(), error) {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		return nil, nil, fmt.Errorf("当前镜像不支持 Python %s，请切换到对应 Python 版本镜像或 all 镜像", pythonVersion)
	}
	EnsureManagedPythonVenvForVersion(pythonVersion)
	runtimePaths = currentManagedRuntimePathsForPythonVersion(pythonVersion)
	preferredDirs := append([]string{runtimePaths.VenvBin}, windowsPythonPreferredDirsForVersion(pythonVersion)...)
	pythonBin := ""
	for _, name := range []string{"python", "python3", "python" + pythonVersion} {
		candidate, err := resolveManagedBinary(name, preferredDirs, runtimePaths.searchDirs)
		if err == nil && managedPythonBinaryMatchesVersion(candidate, pythonVersion) {
			pythonBin = candidate
			break
		}
	}
	if pythonBin == "" {
		return nil, nil, fmt.Errorf("Python %s 不可用，请先安装对应版本，或切换任务 Python 版本", pythonVersion)
	}

	tempDir, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}
	_ = tempDir

	args := []string{"-u", "-c", pythonEnvBootstrap, envFile, scriptPath, strings.TrimSpace(envVars["PYTHONPATH"])}
	args = append(args, scriptArgs...)

	cmd := exec.Command(pythonBin, args...)
	cmd.Dir = workDir
	cmd.Env = buildPythonBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, cleanup, nil
}

func createManagedPythonModuleCommand(interpreter string, moduleName string, moduleArgs []string, workDir string, envVars map[string]string) (*exec.Cmd, func(), error) {
	pythonVersion := ResolvePythonVersionFromEnv(envVars)
	if versionFromInterpreter := ResolvePythonVersionFromInterpreter(interpreter); versionFromInterpreter != "" {
		pythonVersion = versionFromInterpreter
		if envVars != nil {
			envVars["DAIDAI_PYTHON_VERSION"] = pythonVersion
		}
	}
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	if !PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		return nil, nil, fmt.Errorf("当前镜像不支持 Python %s，请切换到对应 Python 版本镜像或 all 镜像", pythonVersion)
	}

	EnsureManagedPythonVenvForVersion(pythonVersion)
	runtimePaths := currentManagedRuntimePathsForPythonVersion(pythonVersion)
	preferredDirs := append([]string{runtimePaths.VenvBin}, windowsPythonPreferredDirsForVersion(pythonVersion)...)
	pythonBin := ""
	for _, name := range []string{"python", "python3", "python" + pythonVersion} {
		candidate, err := resolveManagedBinary(name, preferredDirs, runtimePaths.searchDirs)
		if err == nil && managedPythonBinaryMatchesVersion(candidate, pythonVersion) {
			pythonBin = candidate
			break
		}
	}
	if pythonBin == "" {
		return nil, nil, fmt.Errorf("Python %s 不可用，请先安装对应版本，或切换任务 Python 版本", pythonVersion)
	}

	tempDir, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}
	_ = tempDir

	args := []string{"-u", "-c", pythonModuleEnvBootstrap, envFile, moduleName, strings.TrimSpace(envVars["PYTHONPATH"])}
	args = append(args, cleanManagedProcessArgs(moduleArgs)...)

	cmd := exec.Command(pythonBin, args...)
	cmd.Dir = workDir
	cmd.Env = buildPythonBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, cleanup, nil
}

func createManagedExecutableCommand(commandName string, commandArgs []string, workDir string, envVars map[string]string) (*exec.Cmd, func(), error) {
	pythonVersion := ResolvePythonVersionFromEnv(envVars)
	runtimePaths := currentManagedRuntimePathsForPythonVersion(pythonVersion)
	preferredDirs := []string{runtimePaths.VenvBin, runtimePaths.NodeBin}

	binary, err := resolveManagedBinary(commandName, preferredDirs, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("找不到托管依赖命令 %s，请先在依赖页面安装对应 Python/Node 依赖，或改用脚本文件命令", commandName)
	}

	cmd := exec.Command(binary, cleanManagedProcessArgs(commandArgs)...)
	cmd.Dir = workDir
	// 依赖命令不是面板脚本，无法走 Python/Node bootstrap，只能通过真实进程环境传入任务变量。
	// 这里仍然过滤危险变量和 NUL，避免 LD_PRELOAD 等变量污染托管运行时。
	cmd.Env = appendPythonBootstrapEnv(buildEnv(envVars))
	setPgid(cmd)
	return cmd, func() {}, nil
}

func createManagedNodeCommand(scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths) (*exec.Cmd, func(), error) {
	nodeBin, err := resolveManagedBinary("node", nil, runtimePaths.searchDirs)
	if err != nil {
		return nil, nil, err
	}

	_, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}
	nodeModulesCleanup := ensureManagedNodeModulesAccess(workDir, runtimePaths.NodeModules)

	preloadFile, preloadErr := writeNodePreloadScript(filepath.Dir(envFile), envFile, envVars)
	if preloadErr != nil {
		cleanup()
		nodeModulesCleanup()
		return nil, nil, preloadErr
	}

	args := []string{"--require", preloadFile, scriptPath}
	args = append(args, scriptArgs...)

	cmd := exec.Command(nodeBin, args...)
	cmd.Dir = workDir
	cmd.Env = buildBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, combineCleanup(cleanup, nodeModulesCleanup), nil
}

func createManagedTSNodeCommand(scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths) (*exec.Cmd, func(), error) {
	_, envFile, cleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}
	nodeModulesCleanup := ensureManagedNodeModulesAccess(workDir, runtimePaths.NodeModules)

	preloadFile, preloadErr := writeNodePreloadScript(filepath.Dir(envFile), envFile, envVars)
	if preloadErr != nil {
		cleanup()
		nodeModulesCleanup()
		return nil, nil, preloadErr
	}

	tsNodeBin, tsErr := resolveManagedBinary("ts-node", []string{runtimePaths.NodeBin}, runtimePaths.searchDirs)
	if tsErr == nil {
		args := []string{"--require", preloadFile, scriptPath}
		args = append(args, scriptArgs...)
		cmd := exec.Command(tsNodeBin, args...)
		cmd.Dir = workDir
		cmd.Env = buildBootstrapProcessEnv(envVars)
		setPgid(cmd)
		return cmd, combineCleanup(cleanup, nodeModulesCleanup), nil
	}

	npxBin, err := resolveManagedBinary("npx", nil, runtimePaths.searchDirs)
	if err != nil {
		cleanup()
		nodeModulesCleanup()
		return nil, nil, err
	}

	args := []string{"ts-node", "--require", preloadFile, scriptPath}
	args = append(args, scriptArgs...)

	cmd := exec.Command(npxBin, args...)
	cmd.Dir = workDir
	cmd.Env = buildBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, combineCleanup(cleanup, nodeModulesCleanup), nil
}

func createStandardManagedCommand(interpreter, scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths) (*exec.Cmd, func(), error) {
	switch interpreter {
	case "bash":
		return createManagedShellCommand(scriptPath, scriptArgs, workDir, envVars, runtimePaths)
	case "go":
		return createManagedGoCommand(scriptPath, scriptArgs, workDir, envVars, runtimePaths)
	}

	binary, err := resolveManagedBinary(interpreter, standardBinaryPreferredDirs(interpreter, runtimePaths), runtimePaths.searchDirs)
	if err != nil {
		return nil, nil, err
	}

	args := append([]string{scriptPath}, cleanManagedProcessArgs(scriptArgs)...)

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	cmd.Env = buildBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, func() {}, nil
}

func createManagedShellCommand(scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths) (*exec.Cmd, func(), error) {
	if err := NormalizeShellScriptFile(scriptPath); err != nil {
		return nil, nil, fmt.Errorf("脚本换行规范化失败: %w", err)
	}

	binary, err := resolveManagedBinary("bash", standardBinaryPreferredDirs("bash", runtimePaths), runtimePaths.searchDirs)
	if err != nil {
		return nil, nil, err
	}

	_, envFile, cleanup, err := writeManagedRuntimeShellEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}

	args := []string{"-c", shellEnvBootstrap, scriptPath, envFile, scriptPath}
	args = append(args, cleanManagedProcessArgs(scriptArgs)...)

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	cmd.Env = buildBootstrapProcessEnv(envVars)
	setPgid(cmd)
	return cmd, cleanup, nil
}

func createManagedGoCommand(scriptPath string, scriptArgs []string, workDir string, envVars map[string]string, runtimePaths managedRuntimePaths) (*exec.Cmd, func(), error) {
	binary, err := resolveManagedBinary("go", standardBinaryPreferredDirs("go", runtimePaths), runtimePaths.searchDirs)
	if err != nil {
		return nil, nil, err
	}

	_, envFile, envCleanup, err := writeManagedRuntimeEnvFile(envVars)
	if err != nil {
		return nil, nil, err
	}

	wrapperPath := filepath.Join(filepath.Dir(scriptPath), fmt.Sprintf("000000_daidai_env_bootstrap_%d.go", time.Now().UnixNano()))
	if err := os.WriteFile(wrapperPath, []byte(goEnvBootstrapSource), 0o600); err != nil {
		envCleanup()
		return nil, nil, err
	}

	cleanup := func() {
		_ = os.Remove(wrapperPath)
		envCleanup()
	}

	args := []string{"run", wrapperPath, scriptPath}
	args = append(args, cleanManagedProcessArgs(scriptArgs)...)

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	cmd.Env = append(buildBootstrapProcessEnv(envVars), "DAIDAI_RUNTIME_ENV_FILE="+envFile)
	setPgid(cmd)
	return cmd, cleanup, nil
}

func standardBinaryPreferredDirs(interpreter string, runtimePaths managedRuntimePaths) []string {
	switch interpreter {
	case "bash":
		if runtime.GOOS == "windows" {
			return windowsShellSearchDirs
		}
		return nil
	case "go":
		return nil
	default:
		return nil
	}
}

type managedBootstrapCommand struct {
	binary            string
	versionArgsPrefix []string
	args              []string
}

func managedPythonBootstrapCommands() []managedBootstrapCommand {
	return managedPythonBootstrapCommandsForVersion("")
}

func managedPythonBootstrapCommandsForVersion(pythonVersion string) []managedBootstrapCommand {
	pythonVersion = NormalizePythonVersionOrDefault(pythonVersion)
	commands := []managedBootstrapCommand{
		{binary: "python" + pythonVersion, args: []string{"-m", "venv"}},
		{binary: "python3", args: []string{"-m", "venv"}, versionArgsPrefix: []string{}},
		{binary: "python", args: []string{"-m", "venv"}, versionArgsPrefix: []string{}},
	}
	if runtime.GOOS == "windows" {
		commands = append([]managedBootstrapCommand{
			{binary: "py", versionArgsPrefix: []string{"-" + pythonVersion}, args: []string{"-" + pythonVersion, "-m", "venv"}},
			{binary: "py", versionArgsPrefix: []string{"-3"}, args: []string{"-3", "-m", "venv"}},
		}, commands...)
	}
	return commands
}

func buildBootstrapProcessEnv(envVars map[string]string) []string {
	safeKeys := []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TZ", "LD_LIBRARY_PATH"}
	if runtime.GOOS == "windows" {
		safeKeys = append(safeKeys, "SYSTEMROOT", "PATHEXT", "TEMP", "TMP", "APPDATA", "LOCALAPPDATA", "USERPROFILE")
	}

	env := make([]string, 0, len(safeKeys))
	for _, key := range safeKeys {
		value := os.Getenv(key)
		if key == "PATH" && strings.TrimSpace(envVars["PATH"]) != "" {
			value = envVars["PATH"]
		}
		if key == "TZ" && strings.TrimSpace(envVars["TZ"]) != "" {
			value = envVars["TZ"]
		}
		if value == "" {
			continue
		}
		env = append(env, key+"="+value)
	}

	return AppendProxyEnv(env)
}

func buildPythonBootstrapProcessEnv(envVars map[string]string) []string {
	env := buildBootstrapProcessEnv(envVars)
	if runtime.GOOS != "windows" {
		return appendPythonBootstrapEnv(env)
	}

	ianaTimezone := strings.TrimSpace(envVars["TZ"])
	if ianaTimezone == "" {
		ianaTimezone = strings.TrimSpace(os.Getenv("TZ"))
	}
	pythonTimezone, err := windowsPythonPOSIXTimezone(ianaTimezone, time.Now())
	if err != nil {
		return appendPythonBootstrapEnv(env)
	}

	// Windows Python 启动时由 CRT 解析 TZ，只接受三字母 POSIX 格式；
	// env.json 会在 bootstrap 内把脚本最终可见的 TZ 恢复为原始 IANA 名称。
	for index, entry := range env {
		if strings.HasPrefix(entry, "TZ=") {
			env[index] = "TZ=" + pythonTimezone
			break
		}
	}
	return appendPythonBootstrapEnv(env)
}

func windowsPythonPOSIXTimezone(ianaTimezone string, now time.Time) (string, error) {
	location, err := time.LoadLocation(strings.TrimSpace(ianaTimezone))
	if err != nil {
		return "", fmt.Errorf("invalid IANA timezone %q: %w", ianaTimezone, err)
	}

	zoneName, utcOffsetSeconds := now.In(location).Zone()
	abbreviation := make([]byte, 0, 3)
	for index := 0; index < len(zoneName) && len(abbreviation) < 3; index++ {
		letter := zoneName[index]
		if letter >= 'a' && letter <= 'z' {
			letter -= 'a' - 'A'
		}
		if letter >= 'A' && letter <= 'Z' {
			abbreviation = append(abbreviation, letter)
		}
	}
	if len(abbreviation) < 3 {
		abbreviation = []byte("DDT")
	}

	// POSIX 的偏移符号与 UTC 偏移相反：UTC+8 要写成 -8，UTC-4 要写成 4。
	posixOffsetSeconds := -utcOffsetSeconds
	sign := ""
	if posixOffsetSeconds < 0 {
		sign = "-"
		posixOffsetSeconds = -posixOffsetSeconds
	}
	hours := posixOffsetSeconds / 3600
	minutes := posixOffsetSeconds % 3600 / 60
	seconds := posixOffsetSeconds % 60
	offset := fmt.Sprintf("%s%d", sign, hours)
	if minutes != 0 || seconds != 0 {
		offset += fmt.Sprintf(":%02d", minutes)
	}
	if seconds != 0 {
		offset += fmt.Sprintf(":%02d", seconds)
	}
	return string(abbreviation) + offset, nil
}

func appendPythonBootstrapEnv(env []string) []string {
	hasUTF8 := false
	hasEncoding := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "PYTHONUTF8=") {
			hasUTF8 = true
		}
		if strings.HasPrefix(entry, "PYTHONIOENCODING=") {
			hasEncoding = true
		}
	}
	if !hasUTF8 {
		env = append(env, "PYTHONUTF8=1")
	}
	if !hasEncoding {
		env = append(env, "PYTHONIOENCODING=utf-8")
	}
	return env
}

func writeManagedRuntimeEnvFile(envVars map[string]string) (string, string, func(), error) {
	tempDir, err := os.MkdirTemp("", "daidai-runtime-*")
	if err != nil {
		return "", "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	payload := make(map[string]string, len(envVars))
	for key, value := range envVars {
		if strings.ContainsRune(value, 0) {
			continue
		}
		payload[key] = value
	}

	data, err := json.Marshal(payload)
	if err != nil {
		cleanup()
		return "", "", nil, err
	}

	envFile := filepath.Join(tempDir, "env.json")
	if err := os.WriteFile(envFile, data, 0o600); err != nil {
		cleanup()
		return "", "", nil, err
	}

	return tempDir, envFile, cleanup, nil
}

func writeManagedRuntimeShellEnvFile(envVars map[string]string) (string, string, func(), error) {
	tempDir, err := os.MkdirTemp("", "daidai-runtime-*")
	if err != nil {
		return "", "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	var b strings.Builder
	b.WriteString("# daidai runtime environment\n")
	keys := make([]string, 0, len(envVars))
	for key := range envVars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := envVars[key]
		if !isValidShellEnvName(key) || isDangerousShellEnvName(key) || strings.ContainsRune(value, 0) {
			continue
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(shellSingleQuote(value))
		b.WriteByte('\n')
	}

	exportedBytes := 0
	for _, key := range keys {
		value := envVars[key]
		if !isValidShellEnvName(key) || isDangerousShellEnvName(key) || strings.ContainsRune(value, 0) {
			continue
		}
		entryBytes := len(key) + 1 + len(value) + 1
		if entryBytes > shellEnvExportValueMaxBytes || exportedBytes+entryBytes > shellEnvExportBudgetBytes {
			continue
		}
		b.WriteString("export ")
		b.WriteString(key)
		b.WriteByte('\n')
		exportedBytes += entryBytes
	}

	envFile := filepath.Join(tempDir, "env.sh")
	if err := os.WriteFile(envFile, []byte(b.String()), 0o600); err != nil {
		cleanup()
		return "", "", nil, err
	}

	return tempDir, envFile, cleanup, nil
}

func isValidShellEnvName(name string) bool {
	if name == "" {
		return false
	}
	for idx, r := range name {
		if idx == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isDangerousShellEnvName(name string) bool {
	switch name {
	case "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES":
		return true
	default:
		return false
	}
}

func shellSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func cleanManagedProcessArgs(args []string) []string {
	cleaned := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned
}

func writeNodePreloadScript(tempDir, envFile string, envVars map[string]string) (string, error) {
	helperPath := filepath.ToSlash(strings.TrimSpace(envVars["DAIDAI_SEND_NOTIFY_JS"]))
	nodePathList := strings.Split(strings.TrimSpace(envVars["NODE_PATH"]), string(os.PathListSeparator))
	filteredNodePaths := make([]string, 0, len(nodePathList))
	for _, item := range nodePathList {
		item = strings.TrimSpace(item)
		if item != "" {
			filteredNodePaths = append(filteredNodePaths, filepath.ToSlash(item))
		}
	}

	helperJSON, err := json.Marshal(helperPath)
	if err != nil {
		return "", err
	}
	nodePathsJSON, err := json.Marshal(filteredNodePaths)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf(`const fs = require('fs');
const path = require('path');
const Module = require('module');
const envPayload = JSON.parse(fs.readFileSync(%q, 'utf8'));
for (const [key, value] of Object.entries(envPayload)) {
  if (value === undefined || value === null) {
    continue;
  }
  process.env[key] = String(value);
}
const originalJSONStringify = JSON.stringify;
JSON.stringify = function(value, replacer, space) {
  if (value === process.env) {
    const envCopy = {};
    for (const [key, envValue] of Object.entries(process.env)) {
      // 兼容少数青龙脚本：它们用 JSON.stringify(process.env).indexOf("GITHUB")
      // 粗暴判断 CI 环境并 process.exit(0)。这里只隐藏 stringify 结果，不删除真实变量。
      if (String(key).includes('GITHUB') || String(envValue).includes('GITHUB')) {
        continue;
      }
      envCopy[key] = envValue;
    }
    return originalJSONStringify.call(JSON, envCopy, replacer, space);
  }
  return originalJSONStringify.call(JSON, value, replacer, space);
};
const extraNodePaths = %s;
const mergedNodePaths = [];
for (const value of [...extraNodePaths, ...(process.env.NODE_PATH ? process.env.NODE_PATH.split(path.delimiter) : [])]) {
  if (!value) {
    continue;
  }
  if (!mergedNodePaths.includes(value)) {
    mergedNodePaths.push(value);
  }
}
if (mergedNodePaths.length > 0) {
  process.env.NODE_PATH = mergedNodePaths.join(path.delimiter);
  Module._initPaths();
}
const _origResolve = Module._resolveFilename;
function _resolveExportsEntry(exp) {
  if (typeof exp === 'string') return exp;
  if (exp && typeof exp === 'object') {
    return exp.require || exp.default || exp.node || exp.import || '';
  }
  return '';
}
Module._resolveFilename = function(request, parent, isMain, options) {
  try {
    return _origResolve.call(this, request, parent, isMain, options);
  } catch (err) {
    if (err.code === 'ERR_PACKAGE_PATH_NOT_EXPORTED') {
      const parts = request.split('/');
      const pkgName = parts[0].startsWith('@') ? parts.slice(0, 2).join('/') : parts[0];
      const subPath = parts.slice(pkgName.startsWith('@') ? 2 : 1).join('/');
      for (const np of (process.env.NODE_PATH || '').split(path.delimiter)) {
        if (!np) continue;
        try {
          const pkgDir = path.join(np, pkgName);
          const pkgJson = JSON.parse(fs.readFileSync(path.join(pkgDir, 'package.json'), 'utf8'));
          let target = '';
          if (subPath) {
            const exportKey = './' + subPath;
            if (pkgJson.exports && pkgJson.exports[exportKey]) {
              target = _resolveExportsEntry(pkgJson.exports[exportKey]);
            }
            if (!target) target = subPath;
          } else {
            if (pkgJson.exports && pkgJson.exports['.']) {
              target = _resolveExportsEntry(pkgJson.exports['.']);
            }
            if (!target) target = pkgJson.main || '';
            if (!target) target = 'index.js';
          }
          const candidates = [
            path.join(pkgDir, target),
            path.join(pkgDir, target + '.js'),
            path.join(pkgDir, target, 'index.js')
          ];
          for (const c of candidates) {
            if (fs.existsSync(c)) return c;
          }
        } catch (_) {}
      }
    }
    throw err;
  }
};
const helperPath = %s;
if (helperPath) {
  require(helperPath);
}
`, filepath.ToSlash(envFile), string(nodePathsJSON), string(helperJSON))

	preloadFile := filepath.Join(tempDir, "node-preload.js")
	if err := os.WriteFile(preloadFile, []byte(script), 0o600); err != nil {
		return "", err
	}

	return preloadFile, nil
}

func resolveManagedBinary(name string, preferredDirs []string, fallbackDirs []string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) || strings.Contains(name, "/") {
		if isExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("找不到可执行文件: %s", name)
	}

	searchDirs := make([]string, 0, len(preferredDirs)+len(fallbackDirs))
	seen := make(map[string]struct{}, len(preferredDirs)+len(fallbackDirs))
	for _, dir := range append(preferredDirs, fallbackDirs...) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if _, exists := seen[dir]; exists {
			continue
		}
		seen[dir] = struct{}{}
		searchDirs = append(searchDirs, dir)
	}

	for _, dir := range searchDirs {
		if binary := findExecutableInDir(dir, name); binary != "" {
			return binary, nil
		}
	}

	return "", fmt.Errorf("找不到可执行文件: %s", name)
}

func findExecutableInDir(dir, name string) string {
	if dir == "" {
		return ""
	}

	candidates := []string{name}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		pathext := os.Getenv("PATHEXT")
		if pathext == "" {
			pathext = ".COM;.EXE;.BAT;.CMD"
		}
		for _, ext := range strings.Split(pathext, ";") {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				continue
			}
			candidates = append(candidates, name+strings.ToLower(ext))
			candidates = append(candidates, name+strings.ToUpper(ext))
		}
	}

	for _, candidate := range candidates {
		fullPath := filepath.Join(dir, candidate)
		if isExecutableFile(fullPath) {
			return fullPath
		}
	}

	return ""
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func findVenvSitePackages(venvDir string) string {
	venvDir = strings.TrimSpace(venvDir)
	if venvDir == "" {
		return ""
	}

	windowsSitePackages := filepath.Join(venvDir, "Lib", "site-packages")
	if info, err := os.Stat(windowsSitePackages); err == nil && info.IsDir() {
		return windowsSitePackages
	}

	venvLib := filepath.Join(venvDir, "lib")
	entries, err := os.ReadDir(venvLib)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "python") {
			return filepath.Join(venvLib, entry.Name(), "site-packages")
		}
	}
	return ""
}

func ensureManagedNodeModulesAccess(workDir, nodeModules string) func() {
	workDir = strings.TrimSpace(workDir)
	nodeModules = strings.TrimSpace(nodeModules)
	if workDir == "" || nodeModules == "" {
		return func() {}
	}

	if info, err := os.Stat(nodeModules); err != nil || !info.IsDir() {
		return func() {}
	}

	linkPath := filepath.Join(workDir, "node_modules")
	if _, err := os.Lstat(linkPath); err == nil || !os.IsNotExist(err) {
		return func() {}
	}

	if err := createManagedDirectoryLink(nodeModules, linkPath); err != nil {
		return func() {}
	}

	return func() {
		_ = os.Remove(linkPath)
	}
}

func createManagedDirectoryLink(target, link string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/C", "mklink", "/J", link, target)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create node_modules junction: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}

	return os.Symlink(target, link)
}

func combineCleanup(cleanups ...func()) func() {
	return func() {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				cleanup()
			}
		}
	}
}

func sanitizeManagedPath(currentPath, nodeBin, venvBin string) string {
	cleanNodeBin := filepath.Clean(strings.TrimSpace(nodeBin))
	cleanVenvBin := filepath.Clean(strings.TrimSpace(venvBin))

	segments := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range splitPathDirs(currentPath) {
		cleanItem := filepath.Clean(strings.TrimSpace(item))
		if cleanItem == "" || cleanItem == "." {
			continue
		}
		if cleanItem == cleanNodeBin || cleanItem == cleanVenvBin {
			continue
		}
		if _, exists := seen[cleanItem]; exists {
			continue
		}
		seen[cleanItem] = struct{}{}
		segments = append(segments, cleanItem)
	}

	return strings.Join(segments, string(os.PathListSeparator))
}

func splitPathDirs(raw string) []string {
	parts := strings.Split(raw, string(os.PathListSeparator))
	result := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func joinPathSegments(parts ...string) string {
	joined := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		for _, item := range splitPathDirs(part) {
			cleanItem := filepath.Clean(strings.TrimSpace(item))
			if cleanItem == "" || cleanItem == "." {
				continue
			}
			if _, exists := seen[cleanItem]; exists {
				continue
			}
			seen[cleanItem] = struct{}{}
			joined = append(joined, cleanItem)
		}
	}
	return strings.Join(joined, string(os.PathListSeparator))
}

func loadConfigShellVars(envMap map[string]string) {
	dataDir := ""
	if config.C != nil {
		dataDir = config.C.Data.Dir
	}
	if dataDir == "" {
		return
	}

	configPath := filepath.Join(dataDir, "config.sh")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	// config.sh 是用户可编辑文件，这里只解析变量赋值，不能通过 source 执行其中的 Shell 命令。
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	configValues := make(map[string]string)
	pendingKey := ""
	pendingQuote := byte(0)
	var pendingValue strings.Builder

	findClosingQuote := func(value string, quote byte) int {
		for i := 0; i < len(value); i++ {
			if value[i] != quote {
				continue
			}
			if quote == '"' {
				backslashes := 0
				for j := i - 1; j >= 0 && value[j] == '\\'; j-- {
					backslashes++
				}
				if backslashes%2 == 1 {
					continue
				}
			}
			return i
		}
		return -1
	}

	for _, rawLine := range strings.Split(content, "\n") {
		trimmedLine := strings.TrimSpace(rawLine)
		if pendingKey != "" {
			// 未闭合值后如果遇到新的 export 赋值，放弃损坏项并从新变量继续解析。
			newExport := strings.HasPrefix(trimmedLine, "export ")
			if newExport {
				candidate := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "export "))
				idx := strings.IndexByte(candidate, '=')
				newExport = idx > 0 && isValidShellEnvName(strings.TrimSpace(candidate[:idx]))
			}
			if newExport {
				pendingKey = ""
				pendingQuote = 0
				pendingValue.Reset()
			} else if idx := findClosingQuote(rawLine, pendingQuote); idx >= 0 {
				pendingValue.WriteByte('\n')
				pendingValue.WriteString(rawLine[:idx])
				configValues[pendingKey] = pendingValue.String()
				pendingKey = ""
				pendingQuote = 0
				pendingValue.Reset()
				continue
			} else {
				pendingValue.WriteByte('\n')
				pendingValue.WriteString(rawLine)
				continue
			}
		}

		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		line := strings.TrimLeft(rawLine, " \t")
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimLeft(strings.TrimPrefix(line, "export "), " \t")
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}

		value := strings.TrimLeft(line[idx+1:], " \t")
		if value == "" {
			configValues[key] = ""
			continue
		}
		if value[0] != '\'' && value[0] != '"' {
			configValues[key] = strings.Trim(strings.TrimSpace(value), "\"'")
			continue
		}

		quote := value[0]
		if closeAt := findClosingQuote(value[1:], quote); closeAt >= 0 {
			configValues[key] = value[1 : closeAt+1]
			continue
		}

		// 引号在本行未闭合时保留真实换行，直到后续行出现同类型闭合引号。
		pendingKey = key
		pendingQuote = quote
		pendingValue.Reset()
		pendingValue.WriteString(value[1:])
	}

	// 数据库环境变量来自面板页面，同名时优先级高于 config.sh。
	for key, value := range configValues {
		if _, exists := envMap[key]; exists {
			continue
		}
		envMap[key] = value
	}
}
