package handler_test

import (
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

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
