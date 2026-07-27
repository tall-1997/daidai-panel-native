package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestBuildManagedRuntimeEnvMapDoesNotWritePythonPreCheckEnv 守卫：
// Python 预检自动安装链路已移除，不应再向任务环境写入这些已废弃的 env 键。
// 若将来有人把预检加回来，这个测试会立刻失败，提醒同时把 pysmx 漏判问题重新评估。
func TestBuildManagedRuntimeEnvMapDoesNotWritePythonPreCheckEnv(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	envMap, err := BuildManagedRuntimeEnvMap(root, root, nil, time.Hour)
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}

	for _, key := range []string{"DD_AUTO_INSTALL_DEPS", "DD_PY_AUTO_INSTALL_ALIASES"} {
		if got, exists := envMap[key]; exists {
			t.Fatalf("expected %s to be absent, got %q", key, got)
		}
	}
}

func TestBuildManagedRuntimeEnvMapUsesRequestedPythonVersion(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if envMap["DAIDAI_PYTHON_VERSION"] != "3.10" {
		t.Fatalf("expected DAIDAI_PYTHON_VERSION=3.10, got %q", envMap["DAIDAI_PYTHON_VERSION"])
	}
	expectedVenvBin := resolveManagedVenvBin(ManagedPythonVenvDir("3.10"))
	if !strings.Contains(envMap["PATH"], expectedVenvBin) {
		t.Fatalf("expected PATH to contain python 3.10 venv bin %q, got %q", expectedVenvBin, envMap["PATH"])
	}
}

func TestNodePreloadKeepsGithubEnvReadableButHiddenFromStringify(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "env.json")
	// 回归 hex-ci/smzdm_script：它会用 JSON.stringify(process.env) 检测 GITHUB，
	// 但脚本显式读取 process.env.GITHUB_ACTIONS 时仍应该拿到真实值。
	envJSON := `{"GITHUB_ACTIONS":"1","SMZDM_COOKIE":"cookie"}`
	if err := os.WriteFile(envFile, []byte(envJSON), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	preloadFile, err := writeNodePreloadScript(tempDir, envFile, map[string]string{})
	if err != nil {
		t.Fatalf("write node preload: %v", err)
	}

	scriptFile := filepath.Join(tempDir, "target.js")
	script := `
if (JSON.stringify(process.env).indexOf("GITHUB") > -1) process.exit(7);
if (process.env.GITHUB_ACTIONS !== "1") process.exit(8);
if (process.env.SMZDM_COOKIE !== "cookie") process.exit(9);
console.log("ok");
`
	if err := os.WriteFile(scriptFile, []byte(script), 0o600); err != nil {
		t.Fatalf("write target script: %v", err)
	}

	cmd := exec.Command(nodeBin, "--require", preloadFile, scriptFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node script failed: %v, output=%s", err, string(out))
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("expected ok output, got %q", string(out))
	}
}

func TestManagedPythonVenvDirUsesFlatVersionedPaths(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")

	if got := ManagedPythonVenvDir("3.12"); got != filepath.Join(dataDir, "deps", "python", "3.12") {
		t.Fatalf("expected flat 3.12 venv path, got %q", got)
	}
	if got := ManagedPythonVenvDir("3.10"); got != filepath.Join(dataDir, "deps", "python", "3.10") {
		t.Fatalf("expected flat 3.10 venv path, got %q", got)
	}
}

func TestWarmManagedPythonVenvWarmsAllSupportedVersions(t *testing.T) {
	testutil.SetupTestEnv(t)

	var warmed []string
	original := warmManagedPythonVenvForVersionFunc
	originalCleanup := cleanupBrokenManagedPythonVenvsFunc
	warmManagedPythonVenvForVersionFunc = func(version string) {
		warmed = append(warmed, version)
	}
	cleanupBrokenManagedPythonVenvsFunc = func() {}
	t.Cleanup(func() {
		warmManagedPythonVenvForVersionFunc = original
		cleanupBrokenManagedPythonVenvsFunc = originalCleanup
	})

	if err := model.SetConfig("python_default_version", "3.11"); err != nil {
		t.Fatalf("set default python version: %v", err)
	}
	for _, version := range []string{"3.10", "3.12"} {
		if err := os.MkdirAll(resolveManagedVenvBin(ManagedPythonVenvDir(version)), 0o755); err != nil {
			t.Fatalf("mkdir versioned venv %s: %v", version, err)
		}
	}

	WarmManagedPythonVenv()

	want := []string{"3.11", "3.10", "3.12"}
	if len(warmed) != len(want) {
		t.Fatalf("expected warmed versions %v, got %v", want, warmed)
	}
	for idx, version := range want {
		if warmed[idx] != version {
			t.Fatalf("expected warmed versions %v, got %v", want, warmed)
		}
	}
}

func TestSupportedPythonVersionsHonorsSingleRuntimeVariant(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.11")

	versions := SupportedPythonVersions()
	if len(versions) != 1 || versions[0] != "3.11" {
		t.Fatalf("expected only Python 3.11 in single runtime image, got %v", versions)
	}
	if got := DefaultPythonVersion(); got != "3.11" {
		t.Fatalf("expected default python version to follow single image version 3.11, got %q", got)
	}

	// 单版本镜像的运行时接口也只能暴露当前小版本，前端任务表单会直接使用这个列表生成选项。
	infos := PythonRuntimeInfos()
	if len(infos) != 1 || infos[0].Version != "3.11" {
		t.Fatalf("expected runtime infos only for Python 3.11 in single image, got %+v", infos)
	}
}

func TestCleanupManagedPythonArtifactsOnStartupRemovesUnsupportedSingleRuntimeDirs(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	for _, version := range []string{"3.10", "3.11", "3.12"} {
		if err := os.MkdirAll(filepath.Join(dataDir, "deps", "python", version, "bin"), 0o755); err != nil {
			t.Fatalf("mkdir python %s dir: %v", version, err)
		}
	}

	CleanupManagedPythonArtifactsOnStartup()

	for _, version := range []string{"3.10", "3.11"} {
		if _, err := os.Stat(filepath.Join(dataDir, "deps", "python", version)); !os.IsNotExist(err) {
			t.Fatalf("expected unsupported python %s dir to be removed, stat err=%v", version, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "deps", "python", "3.12")); err != nil {
		t.Fatalf("expected default python 3.12 dir to be kept: %v", err)
	}
}

func TestCleanupManagedPythonArtifactsOnStartupKeepsAllRuntimeDirs(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "all")

	for _, version := range []string{"3.10", "3.11", "3.12"} {
		if err := os.MkdirAll(filepath.Join(dataDir, "deps", "python", version, "bin"), 0o755); err != nil {
			t.Fatalf("mkdir python %s dir: %v", version, err)
		}
	}

	CleanupManagedPythonArtifactsOnStartup()

	for _, version := range []string{"3.10", "3.11", "3.12"} {
		if _, err := os.Stat(filepath.Join(dataDir, "deps", "python", version)); err != nil {
			t.Fatalf("expected python %s dir to be kept in all image: %v", version, err)
		}
	}
}

func TestApplySinglePythonRuntimePolicyOnStartupResetsDefaultAndTasks(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	if err := model.SetConfig("python_default_version", "3.10"); err != nil {
		t.Fatalf("set old default python version: %v", err)
	}
	task := model.Task{
		Name:           "old python task",
		Command:        "task.py",
		PythonVersion:  "3.10",
		CronExpression: "0 0 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatalf("create old python task: %v", err)
	}

	ApplySinglePythonRuntimePolicyOnStartup()

	if got := model.GetRegisteredConfig("python_default_version"); got != "3.12" {
		t.Fatalf("expected default python version reset to 3.12, got %q", got)
	}
	var reloaded model.Task
	if err := database.DB.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if reloaded.PythonVersion != "3.12" {
		t.Fatalf("expected task python version reset to 3.12, got %q", reloaded.PythonVersion)
	}
}

func TestWarmManagedPythonVenvCleansBrokenBackups(t *testing.T) {
	testutil.SetupTestEnv(t)

	brokenDir := ManagedPythonVenvDir("3.12") + ".broken-20260609121849"
	if err := os.MkdirAll(filepath.Join(brokenDir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir broken venv dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "bin", "python3.12"), []byte("stub"), 0o644); err != nil {
		t.Fatalf("write broken venv stub: %v", err)
	}

	original := warmManagedPythonVenvForVersionFunc
	warmManagedPythonVenvForVersionFunc = func(version string) {}
	t.Cleanup(func() {
		warmManagedPythonVenvForVersionFunc = original
	})

	WarmManagedPythonVenv()

	if _, err := os.Stat(brokenDir); !os.IsNotExist(err) {
		t.Fatalf("expected broken venv backup to be cleaned, stat err=%v", err)
	}
}

func TestCleanupManagedPythonArtifactsOnStartupDoesNotWarmAnyVersion(t *testing.T) {
	testutil.SetupTestEnv(t)

	called := false
	originalWarm := warmManagedPythonVenvForVersionFunc
	warmManagedPythonVenvForVersionFunc = func(version string) {
		called = true
	}
	t.Cleanup(func() {
		warmManagedPythonVenvForVersionFunc = originalWarm
	})

	CleanupManagedPythonArtifactsOnStartup()

	if called {
		t.Fatal("expected startup cleanup to avoid eager python venv warm-up")
	}
}

func TestBuildManagedPythonPathPrioritizesWorkDirAndScriptsDir(t *testing.T) {
	got := buildManagedPythonPath(
		filepath.Clean("/custom/pythonpath"),
		filepath.Clean("/work/scripts/subdir"),
		filepath.Clean("/work/scripts"),
		filepath.Clean("/deps/python/venv/lib/python3.11/site-packages"),
	)

	parts := strings.Split(got, string(os.PathListSeparator))
	want := []string{
		filepath.Clean("/work/scripts/subdir"),
		filepath.Clean("/work/scripts"),
		filepath.Clean("/custom/pythonpath"),
		filepath.Clean("/deps/python/venv/lib/python3.11/site-packages"),
	}

	if len(parts) != len(want) {
		t.Fatalf("unexpected python path parts: got=%v want=%v", parts, want)
	}
	for idx, expected := range want {
		if parts[idx] != expected {
			t.Fatalf("python path order mismatch at %d: got=%q want=%q (all=%v)", idx, parts[idx], expected, parts)
		}
	}
}

func TestMigrateLegacyManagedPythonVenvUsesDetectedVersion(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")
	legacyDir := filepath.Join(dataDir, "deps", "python", "venv")
	writeFakeExecutable(t, resolveManagedVenvBin(legacyDir), "python", []string{"echo 3.11"})
	writeFakeExecutable(t, resolveManagedVenvBin(legacyDir), "pip3", []string{"echo pip 24.0 from test"})

	version := MigrateLegacyManagedPythonVenv()
	if version != "3.11" {
		t.Fatalf("expected legacy venv to be detected as 3.11, got %q", version)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "deps", "python", "3.11")); err != nil {
		t.Fatalf("expected legacy venv to move to 3.11: %v", err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("expected legacy venv path to be removed, err=%v", err)
	}
}

func TestMigrateLegacyManagedPythonVenvFlattensVersionedNestedVenv(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")
	nestedDir := filepath.Join(dataDir, "deps", "python", "3.10", "venv")
	writeFakeExecutable(t, resolveManagedVenvBin(nestedDir), "python", []string{"echo 3.10"})
	writeFakeExecutable(t, resolveManagedVenvBin(nestedDir), "pip3", []string{"echo pip 24.0 from test"})

	MigrateLegacyManagedPythonVenv()

	flatDir := filepath.Join(dataDir, "deps", "python", "3.10")
	if _, err := os.Stat(resolveManagedVenvBin(flatDir)); err != nil {
		t.Fatalf("expected nested 3.10 venv to be flattened: %v", err)
	}
	if _, err := os.Stat(nestedDir); !os.IsNotExist(err) {
		t.Fatalf("expected nested venv path to be removed, err=%v", err)
	}
}

func TestFindVenvSitePackagesSupportsWindowsLayout(t *testing.T) {
	venvDir := filepath.Join(t.TempDir(), "venv")
	sitePackages := filepath.Join(venvDir, "Lib", "site-packages")
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatalf("mkdir site-packages: %v", err)
	}

	if got := findVenvSitePackages(venvDir); got != sitePackages {
		t.Fatalf("expected windows site-packages path %q, got %q", sitePackages, got)
	}
}

func TestResolveManagedVenvBinUsesExistingScriptsDir(t *testing.T) {
	venvDir := filepath.Join(t.TempDir(), "venv")
	scriptsDir := filepath.Join(venvDir, "Scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	if got := resolveManagedVenvBin(venvDir); got != scriptsDir {
		t.Fatalf("expected Scripts dir %q, got %q", scriptsDir, got)
	}
}

func TestManagedPythonVenvHealthyRejectsBrokenPip(t *testing.T) {
	venvDir := filepath.Join(t.TempDir(), "venv")
	venvBin := resolveManagedVenvBin(venvDir)
	writeFakeExecutable(t, venvBin, "python", []string{"exit 0"})
	writeFakeExecutable(t, venvBin, "pip3", []string{"echo ModuleNotFoundError: No module named 'pip' 1>&2", "exit 1"})

	if managedPythonVenvHealthy(venvDir) {
		t.Fatal("expected venv with broken pip module to be unhealthy")
	}
}

func TestManagedPythonVenvHealthyAcceptsWorkingPip(t *testing.T) {
	venvDir := filepath.Join(t.TempDir(), "venv")
	venvBin := resolveManagedVenvBin(venvDir)
	writeFakeExecutable(t, venvBin, "python", []string{"exit 0"})
	writeFakeExecutable(t, venvBin, "pip3", []string{"echo pip 24.0 from test", "exit 0"})

	if !managedPythonVenvHealthy(venvDir) {
		t.Fatal("expected venv with working pip --version to be healthy")
	}
}

func writeFakeExecutable(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake executable dir: %v", err)
	}

	path := filepath.Join(dir, name)
	content := "#!/bin/sh\n" + strings.Join(lines, "\n") + "\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		content = "@echo off\r\n" + strings.Join(lines, "\r\n") + "\r\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	return path
}
func TestResolveManagedBinaryPrefersRealWindowsPythonInstallOverWindowsAppsProxy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only resolution behavior")
	}

	root := t.TempDir()
	windowsAppsDir := filepath.Join(root, "WindowsApps")
	realPythonDir := filepath.Join(root, "Programs", "Python", "Python314")
	if err := os.MkdirAll(windowsAppsDir, 0o755); err != nil {
		t.Fatalf("mkdir windows apps dir: %v", err)
	}
	if err := os.MkdirAll(realPythonDir, 0o755); err != nil {
		t.Fatalf("mkdir real python dir: %v", err)
	}

	windowsAppsPython := filepath.Join(windowsAppsDir, "python.exe")
	realPython := filepath.Join(realPythonDir, "python.exe")
	for _, path := range []string{windowsAppsPython, realPython} {
		if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
			t.Fatalf("write stub binary %s: %v", path, err)
		}
	}

	got, err := resolveManagedBinary("python", []string{realPythonDir}, []string{windowsAppsDir})
	if err != nil {
		t.Fatalf("resolve managed binary: %v", err)
	}
	if got != realPython {
		t.Fatalf("expected real python %q, got %q", realPython, got)
	}
}

// TestPythonBootstrapHasNoPreCheckAutoInstall 守卫：
// Python bootstrap 必须保持"纯跑脚本"语义，不做任何基于 importlib.find_spec 或
// AST 扫 import 的预检自动安装。历史上这套预检曾导致 pysmx 等已装好的包被反复
// 判定缺失并循环触发 pip install（v2.0.7 两次尝试修 find_spec 均未根治）。
// 真实缺失的依赖由 Go 侧 task_executor.detectAndInstallDeps 兜底处理，
// 它在脚本真实抛出 ModuleNotFoundError 时再 pip install + 自动重跑，更精准。
func TestPythonBootstrapHasNoPreCheckAutoInstall(t *testing.T) {
	forbidden := []struct {
		name string
		text string
	}{
		{"AST import scan", "_dd_scan_imports"},
		{"find_spec pre-check", "find_spec"},
		{"importlib.metadata fallback", "packages_distributions"},
		{"disk scan fallback", "_dd_module_available_on_disk"},
		{"pip install subprocess", "_dd_install_package"},
		{"auto install switch", "DD_AUTO_INSTALL_DEPS"},
		{"alias env", "DD_PY_AUTO_INSTALL_ALIASES"},
		{"missing dep banner", "检测到缺失依赖"},
	}
	for _, m := range forbidden {
		if strings.Contains(pythonEnvBootstrap, m.text) {
			t.Fatalf("pythonEnvBootstrap must not contain %s marker %q (预检链路已移除，改由 Go 侧后置兜底)", m.name, m.text)
		}
	}
}

func TestDefaultPythonVersionFallsBackToActiveSystemPythonOnMagiskRuntime(t *testing.T) {
	testutil.SetupTestEnv(t)

	t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+os.Getenv("PATH"))

	fakeDir := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	// 新版回退探测走 `<binary> --version`（discoverSystemPythonForVersion），
	// 假 python3 需按真实 `--version` 输出格式（"Python 3.11.4"）响应才能被识别。
	writeFakeExecutable(t, fakeDir, "python3", []string{"echo Python 3.11.4"})

	if got := DefaultPythonVersion(); got != "3.11" {
		t.Fatalf("expected Magisk runtime default python version to follow active python3=3.11, got %q", got)
	}
}

func TestResolvePythonVersionFromEnvFallsBackToActiveSystemPythonOnMagiskRuntime(t *testing.T) {
	testutil.SetupTestEnv(t)

	t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+os.Getenv("PATH"))

	fakeDir := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	// 同上：假 python3 按真实 `--version` 输出格式响应，供新版探测识别。
	writeFakeExecutable(t, fakeDir, "python3", []string{"echo Python 3.11.4"})

	envMap := map[string]string{
		"DAIDAI_PYTHON_VERSION": "3.12",
	}
	if got := ResolvePythonVersionFromEnv(envMap); got != "3.11" {
		t.Fatalf("expected Magisk runtime python version to fall back from 3.12 to active python3=3.11, got %q", got)
	}
}
