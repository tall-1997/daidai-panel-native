package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func newDepsTestRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	NewDepsHandler().RegisterRoutes(api)
	return engine
}

func TestDependencyCreateReturnsCompatibilityDetailsForUnsupportedPackage(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeNodeJS,
		"names": []string{"sharp"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"reason_code":"NATIVE_NOT_ALLOWLISTED"`)) {
		t.Fatalf("expected stable compatibility reason details: %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"npm_script_policy":"ignore-scripts"`)) {
		t.Fatalf("expected npm script policy in compatibility details: %s", rec.Body.String())
	}
}

func TestDependencyCreatePersistsOperationAndCompatibilityDetails(t *testing.T) {
	testutil.SetupTestEnv(t)

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	done := make(chan uint, 1)
	dependencyInstallRunner = func(id uint, depType, name string) {
		done <- id
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeNodeJS,
		"names": []string{"left-pad"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var depID uint
	select {
	case depID = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dependency runner")
	}
	var dep model.Dependency
	if err := database.DB.First(&dep, depID).Error; err != nil {
		t.Fatalf("load dependency: %v", err)
	}
	if dep.OperationID == "" || dep.CompatibilityDetails == "" {
		t.Fatalf("expected operation and compatibility metadata, got %+v", dep)
	}
	var op model.Operation
	if err := database.DB.First(&op, "id = ?", dep.OperationID).Error; err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if op.Kind != model.OperationKindDependency || op.State != model.OperationStatePending {
		t.Fatalf("unexpected operation state: %+v", op)
	}
}

func TestDependencyCreateRejectsProjectedQuotaBeforeStartingInstall(t *testing.T) {
	testutil.SetupTestEnv(t)

	originalRunner := dependencyInstallRunner
	originalProjectedUsage := dependencyProjectedUsageBytes
	defer func() {
		dependencyInstallRunner = originalRunner
		dependencyProjectedUsageBytes = originalProjectedUsage
	}()
	started := false
	dependencyInstallRunner = func(id uint, depType, name string) {
		started = true
	}
	dependencyProjectedUsageBytes = func(depType, name, pythonVersion string) int64 {
		return 2 * 1024 * 1024 * 1024
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeNodeJS,
		"names": []string{"left-pad"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if started {
		t.Fatal("dependency install should not start after projected quota rejection")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"reason_code":"DEPENDENCY_QUOTA_EXCEEDED"`)) {
		t.Fatalf("expected quota compatibility details, got %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"projected_bytes"`)) {
		t.Fatalf("expected projected quota usage, got %s", rec.Body.String())
	}
}

func TestDependencyRunRestoresStagingWhenCommandStartFails(t *testing.T) {
	testutil.SetupTestEnv(t)

	target := filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "left-pad")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"left-pad"}`), 0o644); err != nil {
		t.Fatalf("write target package: %v", err)
	}
	dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "left-pad", Status: model.DepStatusInstalling}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	dep.OperationID = createDependencyOperation(dep.ID)

	runCmdWithSSE(exec.Command("/definitely/missing/dependency-installer"), dep.ID, model.DepStatusInstalled, false)

	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("expected previous target restored after start failure: %v", err)
	}
	var updated model.Dependency
	if err := database.DB.First(&updated, dep.ID).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	if updated.Status != model.DepStatusFailed || !strings.Contains(updated.Log, "previous target restored") {
		t.Fatalf("expected failed dependency with staging restore log, got %+v", updated)
	}
	assertDependencyOperationTerminal(t, dep.OperationID, model.OperationStateFailed, "DEPENDENCY_START_FAILED")
}

func TestDependencyCommandPreparationFailureTerminatesOperation(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := os.WriteFile(filepath.Join(config.C.Data.Dir, "deps"), []byte("blocks directory creation"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "local-fixture", Status: model.DepStatusInstalling}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	dep.OperationID = createDependencyOperation(dep.ID)

	installDependency(dep.ID, dep.Type, dep.Name)

	assertDependencyOperationTerminal(t, dep.OperationID, model.OperationStateFailed, "DEPENDENCY_PREPARE_FAILED")
}

func TestDependencyDeleteCreatesUninstallOperation(t *testing.T) {
	testutil.SetupTestEnv(t)
	dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "missing-local-fixture", Status: model.DepStatusInstalled}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/deps/%d", dep.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var operation model.Operation
		if err := database.DB.Where("kind = ?", model.OperationKindDependency).Order("created_at DESC").First(&operation).Error; err == nil && model.IsOperationTerminalState(operation.State) {
			if operation.State != model.OperationStateSuccess || operation.EndedAt == nil {
				t.Fatalf("expected successful uninstall operation, got %+v", operation)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for uninstall operation terminal state")
}

func TestDependencyNetworkAndNoSpaceFailuresHaveSpecificTerminalCodes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		message   string
		errorCode string
	}{
		{name: "network", message: "Temporary failure resolving registry.example", errorCode: "DEPENDENCY_NETWORK_FAILED"},
		{name: "space", message: "No space left on device", errorCode: "DEPENDENCY_NO_SPACE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "local-fixture", Status: model.DepStatusInstalling}
			if err := database.DB.Create(&dep).Error; err != nil {
				t.Fatalf("create dependency: %v", err)
			}
			dep.OperationID = createDependencyOperation(dep.ID)
			cmd := exec.Command("sh", "-c", "printf '%s\\n' \"$FAILURE\"; exit 1")
			cmd.Env = append(os.Environ(), "FAILURE="+tc.message)

			runCmdWithSSE(cmd, dep.ID, model.DepStatusInstalled, false)

			assertDependencyOperationTerminal(t, dep.OperationID, model.OperationStateFailed, tc.errorCode)
		})
	}
}

func assertDependencyOperationTerminal(t *testing.T, operationID, state, errorCode string) {
	t.Helper()
	var operation model.Operation
	if err := database.DB.First(&operation, "id = ?", operationID).Error; err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if operation.State != state || operation.ErrorCode != errorCode || operation.EndedAt == nil {
		var dependency model.Dependency
		_ = database.DB.Where("operation_id = ?", operationID).First(&dependency).Error
		t.Fatalf("expected terminal operation %s/%s, got %+v; dependency log: %q", state, errorCode, operation, dependency.Log)
	}
}

func performDepsJSONRequest(engine *gin.Engine, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestBatchReinstallRunsSequentially(t *testing.T) {
	testutil.SetupTestEnv(t)

	deps := []model.Dependency{
		{Name: "requests", Type: model.DepTypePython, Status: model.DepStatusFailed},
		{Name: "httpx", Type: model.DepTypePython, Status: model.DepStatusCancelled},
	}
	for i := range deps {
		if err := database.DB.Create(&deps[i]).Error; err != nil {
			t.Fatalf("create dep %d: %v", i, err)
		}
	}

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()

	var (
		mu    sync.Mutex
		order []uint
		done  = make(chan struct{})
	)
	dependencyInstallRunner = func(id uint, depType, name string) {
		mu.Lock()
		order = append(order, id)
		count := len(order)
		mu.Unlock()

		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusInstalled,
			"log":    "[测试] 已重装完成",
		})

		if count == len(deps) {
			close(done)
		}
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/batch-reinstall", map[string]any{
		"ids": []uint{deps[0].ID, deps[1].ID},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sequential batch reinstall")
	}

	mu.Lock()
	gotOrder := append([]uint(nil), order...)
	mu.Unlock()
	wantOrder := []uint{deps[0].ID, deps[1].ID}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("expected install order %v, got %v", wantOrder, gotOrder)
	}
}

func TestBuildDependencyExportLinesUsesExpectedFormat(t *testing.T) {
	deps := []model.Dependency{
		{Name: "requests"},
		{Name: "httpx"},
		{Name: "pendulum"},
	}

	lines := buildDependencyExportLinesFromVersions(deps, map[string]string{
		"requests": "2.32.3",
		"httpx":    "0.28.1",
	})

	want := []string{
		"requests==>2.32.3",
		"httpx==>0.28.1",
		"pendulum==>未知版本",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("expected export lines %v, got %v", want, lines)
	}
}

func TestPythonDependencyCreateInstallsAllPythonVersions(t *testing.T) {
	testutil.SetupTestEnv(t)
	installed := make(chan struct{}, 3)

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, depType, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
		installed <- struct{}{}
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypePython,
		"names": []string{"requests"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	for range 3 {
		<-installed
	}

	var stored []model.Dependency
	if err := database.DB.Where("type = ? AND name = ?", model.DepTypePython, "requests").Order("python_version ASC").Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("expected dependencies for three python versions, got %+v", stored)
	}
	var versions []string
	for _, dep := range stored {
		versions = append(versions, dep.PythonVersion)
	}
	if !slices.Equal(versions, []string{"3.10", "3.11", "3.12"}) {
		t.Fatalf("expected python versions 3.10/3.11/3.12, got %v", versions)
	}

	list310 := httptest.NewRecorder()
	req310 := httptest.NewRequest(http.MethodGet, "/api/v1/deps?type=python&python_version=3.10", nil)
	req310.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(list310, req310)
	if list310.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", list310.Code, list310.Body.String())
	}
	if !bytes.Contains(list310.Body.Bytes(), []byte(`"python_version":"3.10"`)) {
		t.Fatalf("expected 3.10 dependency in list response: %s", list310.Body.String())
	}

	list311 := httptest.NewRecorder()
	req311 := httptest.NewRequest(http.MethodGet, "/api/v1/deps?type=python&python_version=3.11", nil)
	req311.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(list311, req311)
	if list311.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", list311.Code, list311.Body.String())
	}
	if !bytes.Contains(list311.Body.Bytes(), []byte(`"python_version":"3.11"`)) {
		t.Fatalf("expected 3.11 dependency in list response: %s", list311.Body.String())
	}
}

func TestPythonDependencyCreateInstallsOnlySingleRuntimeVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")
	installed := make(chan struct{}, 1)

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, depType, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
		installed <- struct{}{}
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypePython,
		"names": []string{"requests"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	<-installed

	var stored []model.Dependency
	if err := database.DB.Where("type = ? AND name = ?", model.DepTypePython, "requests").Order("python_version ASC").Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 1 || stored[0].PythonVersion != "3.12" {
		t.Fatalf("expected dependency only for python 3.12 in single image, got %+v", stored)
	}
}

func TestPythonDependencyCreateHonorsExplicitVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	installed := make(chan struct{}, 1)
	originalRunner := dependencyInstallRunner
	defer func() { dependencyInstallRunner = originalRunner }()
	dependencyInstallRunner = func(id uint, depType, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
		installed <- struct{}{}
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type": model.DepTypePython, "names": []string{"requests"}, "python_version": "3.11",
	}, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	<-installed

	var stored []model.Dependency
	if err := database.DB.Where("type = ? AND name = ?", model.DepTypePython, "requests").Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 1 || stored[0].PythonVersion != "3.11" {
		t.Fatalf("expected only python 3.11 dependency, got %+v", stored)
	}
}

func TestPythonDependencyCreateRejectsUnsupportedExplicitVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type": model.DepTypePython, "names": []string{"requests"}, "python_version": "3.9",
	}, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var count int64
	if err := database.DB.Model(&model.Dependency{}).Count(&count).Error; err != nil {
		t.Fatalf("count dependencies: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no dependency records, got %d", count)
	}
}
