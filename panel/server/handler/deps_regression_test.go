package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

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

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, depType, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
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

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, depType, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
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

	var stored []model.Dependency
	if err := database.DB.Where("type = ? AND name = ?", model.DepTypePython, "requests").Order("python_version ASC").Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 1 || stored[0].PythonVersion != "3.12" {
		t.Fatalf("expected dependency only for python 3.12 in single image, got %+v", stored)
	}
}
