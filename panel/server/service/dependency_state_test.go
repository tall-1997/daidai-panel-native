package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestNormalizeNodeDependencyPackageName(t *testing.T) {
	tests := map[string]string{
		"chalk":                    "chalk",
		"chalk@4.1.2":              "chalk",
		"http-proxy-agent@7.0.0":   "http-proxy-agent",
		"@scope/pkg":               "@scope/pkg",
		"@scope/pkg@1.2.3":         "@scope/pkg",
		"@scope/pkg-beta@^2.0.0":   "@scope/pkg-beta",
		"@scope/pkg/subpath@1.2.3": "@scope/pkg/subpath",
	}

	for input, expected := range tests {
		if got := NormalizeNodeDependencyPackageName(input); got != expected {
			t.Fatalf("NormalizeNodeDependencyPackageName(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestDependencyInstalledNodeJSAcceptsVersionSpec(t *testing.T) {
	testutil.SetupTestEnv(t)

	for _, pkg := range []string{
		filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "http-proxy-agent"),
		filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "@scope", "pkg"),
	} {
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatalf("mkdir node dependency: %v", err)
		}
	}

	if !DependencyInstalledForPythonVersion(model.DepTypeNodeJS, "http-proxy-agent@7.0.0", "") {
		t.Fatal("expected versioned node dependency to be detected as installed")
	}
	if !DependencyInstalledForPythonVersion(model.DepTypeNodeJS, "@scope/pkg@1.2.3", "") {
		t.Fatal("expected scoped versioned node dependency to be detected as installed")
	}
}

func TestDependencyInstalledLinuxAcceptsDpkgQueryInstalledStatus(t *testing.T) {
	testutil.SetupTestEnv(t)

	dir := t.TempDir()
	dpkgQuery := filepath.Join(dir, "dpkg-query")
	script := "#!/bin/sh\nif [ \"$1\" = \"-W\" ]; then\n  printf 'install ok installed'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(dpkgQuery, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake dpkg-query: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+originalPath)

	if !DependencyInstalledForPythonVersion(model.DepTypeLinux, "curl", "") {
		t.Fatal("expected linux dependency to be detected as installed from dpkg-query")
	}
}

// 拿到托管 pip 时只问它一次（#156）：装着的、判缺的都是 1 次健康检查（2 个子进程）+ 1 次 pip show（#158 起解析托管 pip 不再查第二遍）。
// 以前判缺还要把 bin/pip、bin/pip3 与 NewPipCommandForPythonVersion 解析出的同一个 pip 再各问一遍，共 12 个。
func TestDependencyInstalledForPythonVersionAsksManagedPipOnce(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests"}, "")

	for _, tc := range []struct {
		name string
		want bool
	}{
		{name: "requests", want: true},
		{name: "notexist-pkg", want: false},
	} {
		if err := os.WriteFile(execLog, nil, 0o644); err != nil {
			t.Fatalf("reset exec log: %v", err)
		}
		if got := DependencyInstalledForPythonVersion(model.DepTypePython, tc.name, "3.12"); got != tc.want {
			t.Fatalf("%s: 判定结果应为 %v，实际 %v", tc.name, tc.want, got)
		}
		calls := readFakeVenvExecLog(t, execLog)
		if len(calls) != 3 || calls[2] != "pip3 show "+tc.name || strings.Count(strings.Join(calls, "\n"), " show ") != 1 {
			t.Fatalf("%s: 应只起 3 个子进程（1 次健康检查的 2 个 + 1 次托管 pip show），实际 %d 个：%v", tc.name, len(calls), calls)
		}
	}
}

// 解析托管 pip / python 时只做一遍健康检查（#158）：Ensure 返回 true 时刚查过，Resolve 不再自己查第二遍。
// 健康的 venv 上各只起 2 个子进程：python -c 核对版本、pip3 --version。
func TestResolveManagedBinariesCheckVenvHealthOnce(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests"}, "")
	venvBin := filepath.Join(ManagedPythonVenvDir("3.12"), "bin")

	for _, tc := range []struct {
		name    string
		resolve func(string) string
		want    string
	}{
		{name: "pip", resolve: ResolveManagedPipBinaryForPythonVersion, want: filepath.Join(venvBin, "pip3")},
		{name: "python", resolve: ResolveManagedPythonBinaryForPythonVersion, want: filepath.Join(venvBin, "python")},
	} {
		if err := os.WriteFile(execLog, nil, 0o644); err != nil {
			t.Fatalf("reset exec log: %v", err)
		}
		if got := tc.resolve("3.12"); got != tc.want {
			t.Fatalf("%s: 应解析到 %s，实际 %q", tc.name, tc.want, got)
		}
		calls := readFakeVenvExecLog(t, execLog)
		if len(calls) != 2 || !strings.HasPrefix(calls[0], "python -c ") || calls[1] != "pip3 --version" {
			t.Fatalf("%s: 只应做一遍健康检查（python -c + pip3 --version 两个子进程），实际 %d 个：%v", tc.name, len(calls), calls)
		}
	}
}

// 起 Python 任务时 Ensure 的健康检查已经核对过 venv 里解释器的版本，直接用它，不再进候选循环多起一次 python -c（#158）。
// PATH 收窄成空目录（isolatePythonProbePath），免得系统 Python 版本探测去碰宿主机上真实的 python。
func TestCreateManagedPythonCommandsReuseEnsureVersionCheck(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests"}, "")
	isolatePythonProbePath(t)
	venvPython := filepath.Join(ManagedPythonVenvDir("3.12"), "bin", "python")
	workDir := t.TempDir()

	for _, tc := range []struct {
		name  string
		build func() (*exec.Cmd, func(), error)
	}{
		{name: "script", build: func() (*exec.Cmd, func(), error) {
			envVars := map[string]string{"DAIDAI_PYTHON_VERSION": "3.12"}
			return createManagedPythonCommand(filepath.Join(workDir, "a.py"), nil, workDir, envVars, currentManagedRuntimePathsForPythonVersion("3.12"), "3.12")
		}},
		{name: "module", build: func() (*exec.Cmd, func(), error) {
			envVars := map[string]string{"DAIDAI_PYTHON_VERSION": "3.12"}
			return createManagedPythonModuleCommand("python3", "pip", []string{"--version"}, workDir, envVars)
		}},
	} {
		if err := os.WriteFile(execLog, nil, 0o644); err != nil {
			t.Fatalf("reset exec log: %v", err)
		}
		cmd, cleanup, err := tc.build()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		cleanup()
		if cmd.Path != venvPython {
			t.Fatalf("%s: 应直接用托管 venv 的 python（%s），实际 %q", tc.name, venvPython, cmd.Path)
		}
		calls := readFakeVenvExecLog(t, execLog)
		if len(calls) != 2 || strings.Count(strings.Join(calls, "\n"), "python -c ") != 1 {
			t.Fatalf("%s: 只应有健康检查的 2 个子进程、python -c 只起一次，实际 %d 个：%v", tc.name, len(calls), calls)
		}
	}
}

// writeFakeManagedVenv 在测试数据目录里按托管 venv 的布局（deps/python/3.12/bin）放 sh 脚本冒充 python、pip、pip3，
// 每被调用一次就往返回的日志文件里记一行（脚本名 + 参数），用来数子进程。只在类 Unix 上跑，Windows 上跳过（交叉编译到 Linux 跑）。
//
// pip / pip3 认三种用法：--version；show NAME（NAME 在 installed 里才输出 Name:，否则像真 pip 一样报 not found、退出码 1）；
// list（没带 --disable-pip-version-check 直接报错退出，往 stderr 打一行 ~xxx 残目录告警，stdout 只输出 JSON）。
// listMode 改 list 的行为：""正常；"garbage" 输出非 JSON；"hang" 带着一个攥住 stdout 的孙进程挂住。
func writeFakeManagedVenv(t *testing.T, installed []string, listMode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("用 sh 脚本冒充托管 venv 里的 python / pip，Windows 上跳过（交叉编译到 Linux 跑）")
	}
	// 固定成单版本 3.12，免得多版本探测去碰宿主机上真实的 python3.x。
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	venvBin := filepath.Join(ManagedPythonVenvDir("3.12"), "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatalf("mkdir fake venv bin: %v", err)
	}
	execLog := filepath.Join(t.TempDir(), "exec.log")

	rows := make([]map[string]string, 0, len(installed))
	for _, name := range installed {
		rows = append(rows, map[string]string{"name": name, "version": "1.0.0"})
	}
	listJSON, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal fake pip list output: %v", err)
	}
	listOutput := "echo '" + string(listJSON) + "'"
	switch listMode {
	case "garbage":
		listOutput = "echo 'this is not json'"
	case "hang":
		listOutput = "sleep 30 &\n  wait"
	}

	scripts := map[string]string{
		"python": "#!/bin/sh\necho \"python $*\" >> '" + execLog + "'\necho 3.12\n",
	}
	for _, pipName := range []string{"pip", "pip3"} {
		scripts[pipName] = "#!/bin/sh\n" +
			"echo \"" + pipName + " $*\" >> '" + execLog + "'\n" +
			"case \"$1\" in\n" +
			"--version) echo 'pip 24.0 from /fake/site-packages/pip (python 3.12)' ;;\n" +
			"show)\n" +
			"  case ' " + strings.Join(installed, " ") + " ' in\n" +
			"  *\" $2 \"*) echo \"Name: $2\" ;;\n" +
			"  *) echo \"WARNING: Package(s) not found: $2\" >&2; exit 1 ;;\n" +
			"  esac ;;\n" +
			"list)\n" +
			"  case \" $* \" in\n" +
			"  *' --disable-pip-version-check '*) ;;\n" +
			"  *) echo 'ERROR: pip list 没带 --disable-pip-version-check（pip <= 24.0 会联网自检）' >&2; exit 3 ;;\n" +
			"  esac\n" +
			"  echo 'WARNING: Ignoring invalid distribution ~equests (/fake/site-packages)' >&2\n" +
			"  " + listOutput + " ;;\n" +
			"*) exit 2 ;;\n" +
			"esac\n"
	}
	for name, content := range scripts {
		if err := os.WriteFile(filepath.Join(venvBin, name), []byte(content), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	return execLog
}

// isolatePythonProbePath 把进程 PATH 整体替换成一个空的临时目录，并返回这个目录。
// 与 handler 包同名 helper 对齐（上游 v3.4.0 定义在 service/runtime_exec_auto_install_test.go）；
// 合并后本地版该文件没有这段，补在依赖相关测试里共用。
func isolatePythonProbePath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	pathValue := dir
	if runtime.GOOS == "windows" {
		if systemRoot := strings.TrimSpace(os.Getenv("SystemRoot")); systemRoot != "" {
			pathValue += string(os.PathListSeparator) + filepath.Join(systemRoot, "System32")
		}
	}
	t.Setenv("PATH", pathValue)
	return dir
}
