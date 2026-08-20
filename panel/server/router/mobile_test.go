package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestMobileRouteContract(t *testing.T) {
	testutil.SetupTestEnv(t)
	contracts := BuildContractDocument(CanonicalServerRoutes(), CanonicalMobileRoutes()).Routes
	mobileRoutes := routeKeys(CanonicalMobileRoutes())
	engine := gin.New()
	SetupMobileFull(engine, ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}, NewMobilePlatform(CapabilitySnapshot{Version: 1}))
	if len(contracts) != 437 {
		t.Fatalf("contracts=%d want=437", len(contracts))
	}
	for _, contract := range contracts {
		contract := contract
		const testPrefix = "TestMobileRouteContract/"
		if !strings.HasPrefix(contract.TestCase, testPrefix) {
			continue
		}
		t.Run(strings.TrimPrefix(contract.TestCase, testPrefix), func(t *testing.T) {
			if !mobileRoutes[contract.Method+" "+contract.Path] {
				t.Fatal("route is absent from the real mobile router")
			}
			metadata, ok := MetadataForRoute(contract.Method, contract.Path)
			if !ok {
				t.Fatal("route metadata is missing")
			}
			if metadata.AuthContract != contract.AuthContract || metadata.StreamContract != contract.StreamContract {
				t.Fatalf("metadata=%+v contract=%+v", metadata, contract)
			}
			if contract.StreamContract == "sse" && metadata.HandlerContract != "event-stream" {
				t.Fatalf("SSE route handlerContract=%q", metadata.HandlerContract)
			}

			requestPath := concreteRoutePath(contract.Path)
			withoutLocalToken := httptest.NewRequest(contract.Method, "http://127.0.0.1"+requestPath, nil)
			withoutLocalToken.Host = "127.0.0.1"
			withoutLocalToken.Header.Set("Origin", "http://127.0.0.1")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, withoutLocalToken)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("local security status=%d want=401", response.Code)
			}

			request := httptest.NewRequest(contract.Method, "http://127.0.0.1"+requestPath, nil)
			request.Host = "127.0.0.1"
			request.Header.Set("Origin", "http://127.0.0.1")
			request.Header.Set("X-Daidai-Local-Token", "token")
			response = httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if contract.AuthContract == "jwt" && response.Code != http.StatusUnauthorized {
				t.Fatalf("JWT contract status=%d want=401", response.Code)
			}
			if contract.AuthContract == "public" && response.Code == http.StatusUnauthorized &&
				bytes.Contains(response.Body.Bytes(), []byte("缺少授权令牌")) {
				t.Fatalf("public contract was stopped by JWT middleware: body=%s", response.Body.String())
			}
		})
	}
}

func TestSetupMobileFullRegistersEveryServerRoute(t *testing.T) {
	server := CanonicalServerRoutes()
	mobile := CanonicalMobileRoutes()
	if len(server) != 437 {
		t.Fatalf("canonical server routes=%d want=437", len(server))
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

func TestMobileCapabilityGateControlsDangerousHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.SetupTestEnv(t)
	accessToken := testutil.MustCreateAccessToken(t, "admin", "admin")
	for _, test := range []struct {
		name       string
		capability CapabilityState
		wantStatus int
		wantCalled int
		wantReason string
	}{
		{name: "enabled", capability: CapabilityState{State: CapabilityEnabled}, wantStatus: http.StatusNoContent, wantCalled: 1},
		{name: "disabled", capability: CapabilityState{State: CapabilityDisabled}, wantStatus: http.StatusConflict, wantReason: "CAPABILITY_DISABLED"},
		{name: "unsupported", capability: CapabilityState{State: "unsupported", ReasonCode: "PLATFORM_UNSUPPORTED"}, wantStatus: http.StatusConflict, wantReason: "PLATFORM_UNSUPPORTED"},
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
			request.Header.Set("Authorization", "Bearer "+accessToken)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if called != test.wantCalled {
				t.Fatalf("dangerous handler called %d times, want %d", called, test.wantCalled)
			}
			if test.capability.State == CapabilityEnabled {
				return
			}
			var payload struct {
				ErrorCode  string `json:"errorCode"`
				Capability string `json:"capability"`
				State      string `json:"state"`
				ReasonCode string `json:"reasonCode"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ErrorCode != "PLATFORM_CAPABILITY" || payload.Capability != CapabilityTaskExecution ||
				payload.State != test.capability.State || payload.ReasonCode != test.wantReason {
				t.Fatalf("unexpected capability response: %+v", payload)
			}
		})
	}
}

func TestMobileCapabilityRejectsInvalidJWTBeforeCapabilityDisclosure(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := gin.New()
	engine.Use(managementSecurityMiddleware(ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}))
	engine.Use(mobileCapabilityMiddleware(NewMobilePlatform(CapabilitySnapshot{Version: 1})))
	engine.PUT("/api/tasks/:id/run", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/tasks/1/run", nil)
	request.Host = "127.0.0.1"
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set("X-Daidai-Local-Token", "token")
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=401 body=%s", response.Code, response.Body.String())
	}
}

func TestEveryMobileCapabilityRouteHonorsGateState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.SetupTestEnv(t)
	accessToken := testutil.MustCreateAccessToken(t, "admin", "admin")
	capabilityRoutes := MobileCapabilityRoutes()
	if len(capabilityRoutes) != 60 {
		t.Fatalf("capability routes=%d want=60", len(capabilityRoutes))
	}
	for _, state := range []struct {
		name       string
		capability CapabilityState
		wantStatus int
		wantCalled int
	}{
		{name: "disabled", capability: CapabilityState{State: CapabilityDisabled}, wantStatus: http.StatusConflict},
		{name: "unsupported", capability: CapabilityState{State: "unsupported"}, wantStatus: http.StatusConflict},
		{name: "enabled", capability: CapabilityState{State: CapabilityEnabled}, wantStatus: http.StatusNoContent, wantCalled: 1},
	} {
		for _, route := range capabilityRoutes {
			route := route
			t.Run(state.name+"/"+routeSubtestKey(route.Method, route.Path), func(t *testing.T) {
				called := 0
				engine := gin.New()
				engine.Use(managementSecurityMiddleware(ManagementSecurity{LocalToken: "token", Host: "127.0.0.1"}))
				engine.Use(mobileCapabilityMiddleware(NewMobilePlatform(CapabilitySnapshot{
					Version: 1,
					Capabilities: map[string]CapabilityState{
						route.Capability: state.capability,
					},
				})))
				engine.Handle(route.Method, route.Path, func(c *gin.Context) {
					called++
					c.Status(http.StatusNoContent)
				})
				requestPath := concreteRoutePath(route.Path)
				request := httptest.NewRequest(route.Method, "http://127.0.0.1"+requestPath, nil)
				request.Host = "127.0.0.1"
				request.Header.Set("Origin", "http://127.0.0.1")
				request.Header.Set("X-Daidai-Local-Token", "token")
				request.Header.Set("Authorization", "Bearer "+accessToken)
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				if response.Code != state.wantStatus {
					t.Fatalf("status=%d want=%d body=%s", response.Code, state.wantStatus, response.Body.String())
				}
				if called != state.wantCalled {
					t.Fatalf("dangerous handler called %d times, want %d", called, state.wantCalled)
				}
				if state.capability.State == CapabilityEnabled {
					return
				}
				if !json.Valid(response.Body.Bytes()) {
					t.Fatalf("invalid JSON: %s", response.Body.String())
				}
				var payload struct {
					Capability string `json:"capability"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Capability != route.Capability {
					t.Fatalf("capability=%q want=%q", payload.Capability, route.Capability)
				}
			})
		}
	}
}
