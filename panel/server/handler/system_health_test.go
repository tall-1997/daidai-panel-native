package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"strings"

	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func newSystemHealthTestRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	NewSystemHandler().RegisterRoutes(api)
	return engine
}

func decodeSystemHealthSnapshot(t *testing.T, rec *httptest.ResponseRecorder) systemHealthSnapshot {
	t.Helper()

	var snapshot systemHealthSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode health snapshot: %v", err)
	}
	return snapshot
}

func TestRunHealthCheckPersistsSnapshot(t *testing.T) {
	testutil.SetupTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	previousClient := systemHealthCheckHTTPClient
	previousURL := systemHealthCheckURL
	systemHealthCheckHTTPClient = upstream.Client()
	systemHealthCheckURL = upstream.URL
	t.Cleanup(func() {
		systemHealthCheckHTTPClient = previousClient
		systemHealthCheckURL = previousURL
	})

	user := testutil.MustCreateUser(t, "system-health-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newSystemHealthTestRouter()

	initialReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/health-check", nil)
	initialReq.Header.Set("Authorization", "Bearer "+token)
	initialRec := httptest.NewRecorder()
	engine.ServeHTTP(initialRec, initialReq)

	if initialRec.Code != http.StatusOK {
		t.Fatalf("expected initial GET to return 200, got %d, body=%s", initialRec.Code, initialRec.Body.String())
	}

	initialSnapshot := decodeSystemHealthSnapshot(t, initialRec)
	if initialSnapshot.LastCheckedAt != "" {
		t.Fatalf("expected no last_checked_at before running health check, got %q", initialSnapshot.LastCheckedAt)
	}
	if len(initialSnapshot.Items) != 0 {
		t.Fatalf("expected no persisted health items before running health check, got %#v", initialSnapshot.Items)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/health-check", nil)
	postReq.Header.Set("Authorization", "Bearer "+token)
	postRec := httptest.NewRecorder()
	engine.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("expected POST to return 200, got %d, body=%s", postRec.Code, postRec.Body.String())
	}

	postSnapshot := decodeSystemHealthSnapshot(t, postRec)
	if postSnapshot.LastCheckedAt == "" {
		t.Fatal("expected last_checked_at to be persisted after running health check")
	}
	if len(postSnapshot.Items) == 0 {
		t.Fatal("expected health items after running health check")
	}

	expectedNames := map[string]bool{
		"database":  false,
		"memory":    false,
		"scheduler": false,
		"network":   false,
	}
	for _, item := range postSnapshot.Items {
		if _, exists := expectedNames[item.Name]; exists {
			expectedNames[item.Name] = true
		}
	}
	for name, seen := range expectedNames {
		if !seen {
			t.Fatalf("expected persisted health snapshot to include %s, got %#v", name, postSnapshot.Items)
		}
	}

	if got := model.GetConfig(systemHealthLastCheckedAtKey, ""); got != postSnapshot.LastCheckedAt {
		t.Fatalf("expected persisted last_checked_at %q, got %q", postSnapshot.LastCheckedAt, got)
	}
	if got := model.GetConfig(systemHealthLastResultKey, ""); got == "" {
		t.Fatal("expected persisted health result JSON to be stored")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/health-check", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRec := httptest.NewRecorder()
	engine.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET after POST to return 200, got %d, body=%s", getRec.Code, getRec.Body.String())
	}

	getSnapshot := decodeSystemHealthSnapshot(t, getRec)
	if getSnapshot.LastCheckedAt != postSnapshot.LastCheckedAt {
		t.Fatalf("expected GET last_checked_at %q, got %q", postSnapshot.LastCheckedAt, getSnapshot.LastCheckedAt)
	}
	if !reflect.DeepEqual(getSnapshot.Items, postSnapshot.Items) {
		t.Fatalf("expected GET to return persisted items %#v, got %#v", postSnapshot.Items, getSnapshot.Items)
	}
}

func TestRunHealthCheckUsesUnavailableWarningWhenMemoryTotalMissing(t *testing.T) {
	testutil.SetupTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	previousClient := systemHealthCheckHTTPClient
	previousURL := systemHealthCheckURL
	previousGetResourceInfo := systemHealthGetResourceInfo
	systemHealthCheckHTTPClient = upstream.Client()
	systemHealthCheckURL = upstream.URL
	systemHealthGetResourceInfo = func() service.ResourceInfo {
		return service.ResourceInfo{
			MemoryTotal: 0,
			MemoryUsage: 0,
		}
	}
	t.Cleanup(func() {
		systemHealthCheckHTTPClient = previousClient
		systemHealthCheckURL = previousURL
		systemHealthGetResourceInfo = previousGetResourceInfo
	})

	user := testutil.MustCreateUser(t, "system-health-missing-memory", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newSystemHealthTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/health-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected POST to return 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	snapshot := decodeSystemHealthSnapshot(t, rec)
	var memoryItem *systemHealthCheckItem
	for i := range snapshot.Items {
		if snapshot.Items[i].Name == "memory" {
			memoryItem = &snapshot.Items[i]
			break
		}
	}
	if memoryItem == nil {
		t.Fatalf("expected memory health item, got %#v", snapshot.Items)
	}
	if memoryItem.Status != "warning" {
		t.Fatalf("expected memory status warning when memory_total is missing, got %#v", memoryItem)
	}
	if !strings.Contains(memoryItem.Message, "资源采集不可用") {
		t.Fatalf("expected memory warning message to explain unavailable resource collection, got %#v", memoryItem)
	}
}
