package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestCanonicalSSEHandlerContracts(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	t.Setenv("DAIDAI_ANDROID_RUNTIME_BIN_DIR", root+"/android-runtimes")

	user := testutil.MustCreateUser(t, "sse-contract-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	task := model.Task{Name: "sse-contract", Command: "true", CronExpression: "", TaskType: model.TaskTypeManual, Status: model.TaskStatusDisabled}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	dependency := model.Dependency{Type: model.DepTypeNodeJS, Name: "sse-contract", Status: model.DepStatusInstalled, Log: "installed"}
	if err := database.DB.Create(&dependency).Error; err != nil {
		t.Fatal(err)
	}

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(download.Close)

	engine := gin.New()
	Setup(engine)
	for _, prefix := range []string{"/api", "/api/v1"} {
		cases := []struct {
			method      string
			routePath   string
			requestPath string
			body        string
		}{
			{method: http.MethodGet, routePath: prefix + "/logs/:id/stream", requestPath: prefix + "/logs/" + strconv.FormatUint(uint64(task.ID), 10) + "/stream"},
			{method: http.MethodGet, routePath: prefix + "/deps/:id/log-stream", requestPath: prefix + "/deps/" + strconv.FormatUint(uint64(dependency.ID), 10) + "/log-stream"},
			{method: http.MethodGet, routePath: prefix + "/subscriptions/:id/pull-stream", requestPath: prefix + "/subscriptions/1/pull-stream"},
			{method: http.MethodPost, routePath: prefix + "/android-runtime/install", requestPath: prefix + "/android-runtime/install", body: `{"name":"node","url":"` + download.URL + `"}`},
		}
		for _, tc := range cases {
			testID := routeSubtestKey(tc.method, tc.routePath)
			t.Run(testID, func(t *testing.T) {
				unauthorized := httptest.NewRequest(tc.method, tc.requestPath, bytes.NewBufferString(tc.body))
				if tc.body != "" {
					unauthorized.Header.Set("Content-Type", "application/json")
				}
				unauthorizedRec := httptest.NewRecorder()
				engine.ServeHTTP(unauthorizedRec, unauthorized)
				if unauthorizedRec.Code != http.StatusUnauthorized {
					t.Fatalf("unauthorized status=%d want=401 body=%s", unauthorizedRec.Code, unauthorizedRec.Body.String())
				}

				req := httptest.NewRequest(tc.method, tc.requestPath, bytes.NewBufferString(tc.body))
				req.Header.Set("Authorization", "Bearer "+token)
				if tc.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
					t.Fatalf("Content-Type=%q body=%s", contentType, rec.Body.String())
				}
				if body := rec.Body.String(); !strings.Contains(body, "data:") && !strings.Contains(body, "event: done") {
					t.Fatalf("stream has no event or terminal semantics: %q", body)
				}

				contract := contractByKey(t, tc.method+" "+tc.routePath)
				wantTestCase := "TestCanonicalSSEHandlerContracts/" + testID
				if contract.TestCase != wantTestCase {
					t.Fatalf("testCase=%q want=%q", contract.TestCase, wantTestCase)
				}
				if strings.HasSuffix(tc.routePath, "/android-runtime/install") {
					assertAndroidInstallMobileCapabilityGate(t, token, tc.method, tc.routePath, contract)
				}
			})
		}
	}
}

func assertAndroidInstallMobileCapabilityGate(t *testing.T, token, method, routePath string, contract RouteContract) {
	t.Helper()
	if contract.MobileStatus != "android_equivalent" || contract.AndroidEquivalent != "capability:"+CapabilityRuntimeMutation {
		t.Fatalf("android install mobile contract=%+v", contract)
	}
	engine := gin.New()
	SetupMobileFull(engine, ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}, NewMobilePlatform(CapabilitySnapshot{Version: 1}))
	req := httptest.NewRequest(method, "http://127.0.0.1"+concreteRoutePath(routePath), bytes.NewBufferString(`{"name":"node"}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Origin", "http://127.0.0.1")
	req.Header.Set("X-Daidai-Local-Token", "token")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("mobile capability status=%d want=409 body=%s", rec.Code, rec.Body.String())
	}
}

func contractByKey(t *testing.T, key string) RouteContract {
	t.Helper()
	for _, contract := range BuildRouteContracts(CanonicalServerRoutes(), CanonicalMobileRoutes()) {
		if contract.Method+" "+contract.Path == key {
			return contract
		}
	}
	t.Fatalf("contract %s not found", key)
	return RouteContract{}
}
