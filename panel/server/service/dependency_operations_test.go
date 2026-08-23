package service

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestEvaluateLinuxDependencyOnNonRootAndroidReturnsEquivalentCapability(t *testing.T) {
	details := evaluateDependencyCompatibilityForPlatform(model.DepTypeLinux, "curl", "", "android", 10123)
	if details.Supported() {
		t.Fatalf("expected Linux package installation to be unavailable in an Android app process: %+v", details)
	}
	if details.ReasonCode != DependencyReasonAndroidEquivalent || details.Capability != "android-equivalent" {
		t.Fatalf("expected stable Android equivalent capability, got %+v", details)
	}
	if len(details.Alternatives) == 0 || !strings.Contains(strings.ToLower(details.Message), "apt") {
		t.Fatalf("expected clear apt prohibition and alternatives, got %+v", details)
	}
}

func TestManagedDependencyTaskEnvironmentUsesPrivatePaths(t *testing.T) {
	testutil.SetupTestEnv(t)
	version := runtimePythonVersionForTest()
	sitePackages := ManagedPythonSitePackagesDir(version)
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatalf("create private site-packages: %v", err)
	}

	env, _ := BuildManagedRuntimeEnvMapForPythonVersion(t.TempDir(), config.C.Data.ScriptsDir, nil, 0, version)
	wantNodePath := filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules")
	if !pathListContains(env["NODE_PATH"], wantNodePath) {
		t.Fatalf("expected NODE_PATH to contain %q, got %q", wantNodePath, env["NODE_PATH"])
	}
	if !pathListContains(env["PYTHONPATH"], sitePackages) {
		t.Fatalf("expected PYTHONPATH to contain %q, got %q", sitePackages, env["PYTHONPATH"])
	}
}

func TestOfflinePureWheelInstallsListsAndUninstallsInPrivateDirectory(t *testing.T) {
	testutil.SetupTestEnv(t)
	version := runtimePythonVersionForTest()
	wheel := writePureWheelFixture(t, "local_fixture", "1.0.0")

	install, err := NewPipInstallCommandForPythonVersion(version, wheel)
	if err != nil {
		t.Fatalf("build private wheel install: %v", err)
	}
	install.Env = ManagedPythonDependencyEnv(SanitizePipEnv(os.Environ()), version)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install local wheel: %v\n%s", err, out)
	}
	if !DependencyInstalledForPythonVersion(model.DepTypePython, "local-fixture", version) {
		t.Fatal("expected local wheel to be listed from private site-packages")
	}
	if _, err := os.Stat(filepath.Join(ManagedPythonSitePackagesDir(version), "local_fixture", "__init__.py")); err != nil {
		t.Fatalf("expected package in app-private dependency directory: %v", err)
	}

	uninstall, err := NewPipUninstallCommandForPythonVersion(version, "local-fixture")
	if err != nil {
		t.Fatalf("build private wheel uninstall: %v", err)
	}
	uninstall.Env = ManagedPythonDependencyEnv(SanitizePipEnv(os.Environ()), version)
	if out, err := uninstall.CombinedOutput(); err != nil {
		t.Fatalf("uninstall local wheel: %v\n%s", err, out)
	}
	if DependencyInstalledForPythonVersion(model.DepTypePython, "local-fixture", version) {
		t.Fatal("expected local wheel to be absent after uninstall")
	}
}

func TestPipInstallAllowsNativeWheelsAndSourceDistributions(t *testing.T) {
	testutil.SetupTestEnv(t)
	version := runtimePythonVersionForTest()

	install, err := NewPipInstallCommandForPythonVersion(version, "native-package")
	if err != nil {
		t.Fatalf("build unrestricted pip install: %v", err)
	}
	for _, arg := range install.Args {
		if arg == "--only-binary=:all:" {
			t.Fatalf("pip install still blocks source distributions: %v", install.Args)
		}
	}
	if !containsArgPair(install.Args, "--target", ManagedPythonSitePackagesDir(version)) {
		t.Fatalf("pip install must retain the private target directory: %v", install.Args)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func TestOfflinePureNodeTarballInstallsListsAndUninstallsInPrivateDirectory(t *testing.T) {
	testutil.SetupTestEnv(t)
	tarball := writeNodeTarballFixture(t, "local-fixture", "1.0.0")

	install, err := NewNpmInstallCommand(tarball)
	if err != nil {
		t.Fatalf("build local npm install: %v", err)
	}
	EnforceNpmScriptPolicy(install)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install local tarball: %v\n%s", err, out)
	}
	if !DependencyInstalled(model.DepTypeNodeJS, "local-fixture") {
		t.Fatal("expected local npm package to be listed from private node_modules")
	}

	uninstall, err := NewNpmUninstallCommand("local-fixture", false)
	if err != nil {
		t.Fatalf("build local npm uninstall: %v", err)
	}
	EnforceNpmScriptPolicy(uninstall)
	if out, err := uninstall.CombinedOutput(); err != nil {
		t.Fatalf("uninstall local tarball: %v\n%s", err, out)
	}
	if DependencyInstalled(model.DepTypeNodeJS, "local-fixture") {
		t.Fatal("expected local npm package to be absent after uninstall")
	}
}

func runtimePythonVersionForTest() string {
	return runtimePythonVersion()
}

func runtimePythonVersion() string {
	out, _ := exec.Command("python3", "--version").CombinedOutput()
	parts := strings.Split(strings.TrimSpace(string(out)), ".")
	if len(parts) < 2 {
		return "3.11"
	}
	return strings.TrimPrefix(parts[0], "Python ") + "." + parts[1]
}

func pathListContains(value, target string) bool {
	for _, item := range filepath.SplitList(value) {
		if item == target {
			return true
		}
	}
	return false
}

func writePureWheelFixture(t *testing.T, name, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+"-"+version+"-py3-none-any.whl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create wheel: %v", err)
	}
	writer := zip.NewWriter(file)
	files := map[string]string{
		name + "/__init__.py":                        "VALUE = 'offline'\n",
		name + "-" + version + ".dist-info/METADATA": "Metadata-Version: 2.1\nName: " + name + "\nVersion: " + version + "\n",
		name + "-" + version + ".dist-info/WHEEL":    "Wheel-Version: 1.0\nGenerator: test\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
		name + "-" + version + ".dist-info/RECORD":   "",
	}
	for filename, content := range files {
		entry, createErr := writer.Create(filename)
		if createErr != nil {
			t.Fatalf("create wheel entry: %v", createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatalf("write wheel entry: %v", writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close wheel: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close wheel file: %v", err)
	}
	return path
}

func writeNodeTarballFixture(t *testing.T, name, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+"-"+version+".tgz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tarball: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	manifest, _ := json.Marshal(map[string]string{"name": name, "version": version, "main": "index.js"})
	files := map[string][]byte{
		"package/package.json": manifest,
		"package/index.js":     []byte("module.exports = 'offline';\n"),
	}
	for filename, content := range files {
		header := &tar.Header{Name: filename, Mode: 0o644, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tarball: %v", err)
	}
	return path
}

func TestEvaluateDependencyCompatibilityAllowsNativeAddonAttempt(t *testing.T) {
	details := EvaluateDependencyCompatibility(model.DepTypeNodeJS, "sharp", "")
	if !details.Supported() {
		t.Fatalf("expected native addon installation attempt to be supported: %+v", details)
	}
	if !details.Native || details.ReasonCode != "" {
		t.Fatalf("expected native metadata without a policy rejection: %+v", details)
	}
	if details.ABI != "android-arm64-bionic" || details.NpmScriptPolicy != "enabled" {
		t.Fatalf("expected stable ABI and npm script policy, got %+v", details)
	}
	if len(details.Alternatives) == 0 {
		t.Fatalf("expected alternatives in compatibility details")
	}
}

func TestEvaluateDependencyCompatibilityAcceptsSignedNativeAllowlist(t *testing.T) {
	details := EvaluateDependencyCompatibility(model.DepTypePython, "cryptography", "3.12")
	if !details.Supported() {
		t.Fatalf("expected signed native allowlist package to be supported: %+v", details)
	}
	if !details.Native || !details.SignedAllowlist {
		t.Fatalf("expected native signed allowlist markers: %+v", details)
	}
	if details.AllowlistSHA256 == "" || details.CompatibilityKey == "" {
		t.Fatalf("expected stable allowlist metadata: %+v", details)
	}
}

func TestEnforceNpmScriptPolicyEnablesInstallScripts(t *testing.T) {
	cmd := exec.Command("npm", "install", "left-pad")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"npm_config_ignore_scripts=false",
		"NPM_CONFIG_IGNORE_SCRIPTS=0",
		"Npm_Config_Ignore_Scripts=yes",
		"npm_config_ignore_scripts=false",
	}

	EnforceNpmScriptPolicy(cmd)
	EnforceNpmScriptPolicy(cmd)

	if stringSliceContains(cmd.Args, "--ignore-scripts") {
		t.Fatalf("dependency install must allow lifecycle scripts: %v", cmd.Args)
	}

	policyEntries := 0
	for _, entry := range cmd.Env {
		if entry == "npm_config_ignore_scripts=false" {
			policyEntries++
			continue
		}
		if key, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, "npm_config_ignore_scripts") {
			t.Fatalf("unexpected npm script policy variant remained in env: %q", entry)
		}
	}
	if policyEntries != 1 {
		t.Fatalf("expected one npm_config_ignore_scripts=false entry, got %d in %v", policyEntries, cmd.Env)
	}
}

func countString(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

func TestCheckDependencyQuota(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_DEPENDENCY_QUOTA_BYTES", "1")
	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("mkdir deps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "payload"), []byte("overflow"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := CheckDependencyQuota(); err == nil || !strings.Contains(err.Error(), DependencyReasonQuotaExceeded) {
		t.Fatalf("expected quota exceeded error, got %v", err)
	}
}

func TestCheckDependencyQuotaUsesProjectedUsage(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_DEPENDENCY_QUOTA_BYTES", "10")
	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("mkdir deps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "payload"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	if err := CheckDependencyQuotaWithProjection(4); err != nil {
		t.Fatalf("expected projected usage within quota, got %v", err)
	}
	if err := CheckDependencyQuotaWithProjection(6); err == nil || !strings.Contains(err.Error(), "projected 11 bytes") {
		t.Fatalf("expected projected quota error, got %v", err)
	}
}

func TestPrepareDependencyStagingRestoresNodeTargetOnFailure(t *testing.T) {
	testutil.SetupTestEnv(t)
	target := filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "left-pad")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"left-pad"}`), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	finish, state := PrepareDependencyStaging(model.DepTypeNodeJS, "left-pad", "")
	if state != "prepared" {
		t.Fatalf("expected prepared staging, got %s", state)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target to move aside during staging")
	}
	if msg := finish("failed"); !strings.Contains(msg, "restored") {
		t.Fatalf("expected restore message, got %s", msg)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("expected previous target restored: %v", err)
	}
}
