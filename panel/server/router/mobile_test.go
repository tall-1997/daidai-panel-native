package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupMobileFullRegistersEveryServerRoute(t *testing.T) {
	server := CanonicalServerRoutes()
	mobile := CanonicalMobileRoutes()
	if len(server) != 423 {
		t.Fatalf("canonical server routes=%d want=423", len(server))
	}
	if diff := DiffRoutes(server, mobile); len(diff.MissingFromMobile) != 0 || len(diff.MissingFromServer) != 0 {
		t.Fatalf("mobile route diff is not empty: %+v", diff)
	}

	keys := routeKeys(mobile)
	for _, route := range []string{
		"GET /api/v1/auth/check-init",
		"GET /api/v1/tasks",
		"GET /api/v1/logs",
		"GET /api/v1/scripts/tree",
		"GET /api/v1/envs",
		"GET /api/v1/subscriptions",
		"GET /api/v1/notifications",
		"GET /api/v1/ssh-keys",
		"GET /api/v1/users",
		"GET /api/v1/security/sessions",
		"GET /api/v1/system/dashboard",
		"GET /api/v1/open-api/apps",
		"GET /api/v1/deps",
		"GET /api/v1/configs",
		"GET /api/v1/platform-tokens",
		"GET /api/v1/sponsors",
		"GET /api/v1/android-runtime/status",
	} {
		if !keys[route] {
			t.Errorf("mobile profile is missing representative route %s", route)
		}
	}
}

func TestMobileCapabilityDefaultsToDisabled(t *testing.T) {
	platform := NewMobilePlatform(CapabilitySnapshot{Version: 1})
	state := platform.Capability(CapabilityTaskExecution)
	if state.State != CapabilityDisabled || state.ReasonCode != "NOT_DECLARED" {
		t.Fatalf("undeclared capability=%+v", state)
	}
}

func TestMobileCapabilityRejectsBeforeDangerousHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		capability CapabilityState
		wantStatus int
	}{
		{name: "disabled", capability: CapabilityState{State: CapabilityDisabled}, wantStatus: http.StatusConflict},
		{name: "enabled without adapter", capability: CapabilityState{State: CapabilityEnabled}, wantStatus: http.StatusNotImplemented},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := 0
			oldRegister := registerMobileHandlers
			registerMobileHandlers = func(engine *gin.Engine) {
				engine.PUT("/api/tasks/:id/run", func(c *gin.Context) {
					called++
					c.Status(http.StatusNoContent)
				})
			}
			t.Cleanup(func() { registerMobileHandlers = oldRegister })

			platform := NewMobilePlatform(CapabilitySnapshot{
				Version: 1,
				Capabilities: map[string]CapabilityState{
					CapabilityTaskExecution: test.capability,
				},
			})
			engine := gin.New()
			SetupMobileFull(engine, ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}, platform)
			request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/tasks/1/run", nil)
			request.Host = "127.0.0.1"
			request.Header.Set("Origin", "http://127.0.0.1")
			request.Header.Set("X-Daidai-Local-Token", "token")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if called != 0 {
				t.Fatalf("dangerous handler called %d times", called)
			}
			var payload struct {
				ErrorCode  string `json:"errorCode"`
				Capability string `json:"capability"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ErrorCode != "PLATFORM_CAPABILITY" || payload.Capability != CapabilityTaskExecution {
				t.Fatalf("unexpected capability response: %+v", payload)
			}
		})
	}
}

func TestMobileDangerousRouteClassesReturnCapabilityResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetupMobileFull(engine, ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}, NewMobilePlatform(CapabilitySnapshot{Version: 1}))

	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/api/tasks/1/run"},
		{http.MethodPost, "/api/scripts/run"},
		{http.MethodPost, "/api/deps"},
		{http.MethodPut, "/api/subscriptions/1/pull"},
		{http.MethodPost, "/api/system/update"},
		{http.MethodPost, "/api/system/restart"},
		{http.MethodPost, "/api/system/restore"},
		{http.MethodPost, "/api/android-runtime/install"},
		{http.MethodPost, "/api/notifications/send"},
	} {
		request := httptest.NewRequest(test.method, "http://127.0.0.1"+test.path, nil)
		request.Host = "127.0.0.1"
		request.Header.Set("Origin", "http://127.0.0.1")
		request.Header.Set("X-Daidai-Local-Token", "token")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Errorf("%s %s status=%d want=409 body=%s", test.method, test.path, response.Code, response.Body.String())
		}
		if !json.Valid(response.Body.Bytes()) {
			t.Errorf("%s %s returned invalid JSON: %s", test.method, test.path, response.Body.String())
		}
	}
}
