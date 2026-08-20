package handler_test

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// isolatePythonProbePath 把进程 PATH 整体替换成一个空的临时目录，并返回这个目录。
//
// service.DefaultPythonVersion() 会真的 exec `python3.X --version` 做版本回退探测，
// 所以只要系统 PATH 还可见，创建任务拿到的默认 Python 版本就跟着宿主机走：
// GitHub ubuntu runner 自带 python3.12，纯净构建容器里一个 python 都没有。
// 整体替换之后，PATH 上有哪些 Python 完全由用例自己写死。
//
// 注：service 包里有同名的等价实现（writeFakeExecutable 是 service 包的测试辅助，
// handler 包用不了），两边各自维护一份，改动时注意同步语义。
func isolatePythonProbePath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	pathValue := dir
	if runtime.GOOS == "windows" {
		// Windows 上假解释器是 .cmd，CreateProcess 需要能找到 cmd.exe 才能拉起它。
		// System32 里没有任何 python / python3 / py，补上它不会让宿主机解释器重新可见。
		if systemRoot := strings.TrimSpace(os.Getenv("SystemRoot")); systemRoot != "" {
			pathValue += string(os.PathListSeparator) + filepath.Join(systemRoot, "System32")
		}
	}
	t.Setenv("PATH", pathValue)
	return dir
}

// writeFakePythonInterpreter 写一个假 Python 解释器，
// 按真实格式打印 "Python X.Y.Z"，供 `<binary> --version` 探测识别。
func writeFakePythonInterpreter(t *testing.T, dir, binary, version string) {
	t.Helper()

	path := filepath.Join(dir, binary)
	content := "#!/bin/sh\necho Python " + version + "\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		content = "@echo off\r\necho Python " + version + "\r\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake python interpreter: %v", err)
	}
}

func TestCreateTaskDefaultsTimeoutToZero(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-create-default-timeout", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"long running task","command":"echo ok","task_type":"manual"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task data object, got %#v", payload["data"])
	}
	if got := data["timeout"]; got != float64(0) {
		t.Fatalf("expected response timeout 0, got %#v", got)
	}

	var task model.Task
	if err := database.DB.First(&task, uint(data["id"].(float64))).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.Timeout != 0 {
		t.Fatalf("expected stored timeout 0, got %d", task.Timeout)
	}
}

func TestCreateTaskUsesConfiguredDefaultPythonVersionWhenOmitted(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("PATH", t.TempDir())

	// 请求里省略 python_version 时，handler 会落到 service.DefaultPythonVersion()，
	// 那里会做"请求版本探测不到才回退到已装版本"的真实 exec 探测。
	// 原来这个用例没造任何假 python，纯靠宿主机恰好没装 python3.12 才拿到 3.11；
	// runner 上自带 3.12 时配置的 3.11 会被回退掉，响应变成 3.12。
	// 现在把 PATH 收敛到只有自造的 3.10 / 3.11 / 3.12：
	//   - 配置的 3.11 探测得到 → 不回退 → 响应必须是 3.11；
	//   - 回退不变量若被破坏（例如无条件回退到默认 3.12），环境里恰好也有 3.12 可回退，本用例立刻红。
	fakePythonDir := isolatePythonProbePath(t)
	for _, version := range []string{"3.10", "3.11", "3.12"} {
		writeFakePythonInterpreter(t, fakePythonDir, "python"+version, version+".4")
	}

	if err := model.SetConfig("python_default_version", "3.11"); err != nil {
		t.Fatalf("set default python version: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-create-default-python", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"default python task","command":"task test.py","task_type":"manual"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task data object, got %#v", payload["data"])
	}
	if got := data["python_version"]; got != "3.11" {
		t.Fatalf("expected response python_version 3.11, got %#v", got)
	}

	var task model.Task
	if err := database.DB.First(&task, uint(data["id"].(float64))).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.PythonVersion != "3.11" {
		t.Fatalf("expected stored python_version 3.11, got %q", task.PythonVersion)
	}
}

func TestCreateTaskRejectsUnsupportedSingleRuntimePythonVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-create-unsupported-python", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 单版本镜像只允许创建当前镜像支持的小版本任务，避免历史 3.10/3.11 环境被清理后继续误选。
	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"unsupported python task","command":"task test.py","task_type":"manual","python_version":"3.10"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateTaskPersistsNotifyOnAbortSwitch(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-create-notify-on-abort", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"abort notify task","command":"echo ok","task_type":"manual","notify_on_abort":true}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected task data object, got %#v", payload["data"])
	}
	if got, ok := data["notify_on_abort"].(bool); !ok || !got {
		t.Fatalf("expected response notify_on_abort=true, got %#v", data["notify_on_abort"])
	}

	var task model.Task
	if err := database.DB.First(&task, uint(data["id"].(float64))).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if !task.NotifyOnAbort {
		t.Fatalf("expected stored notify_on_abort=true")
	}
}
