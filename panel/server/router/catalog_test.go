package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouteContractCollectsCompleteAndMobileFixtures(t *testing.T) {
	server := CanonicalServerRoutes()
	mobile := CanonicalMobileRoutes()
	if len(server) == 0 {
		t.Fatal("complete Setup fixture returned no routes")
	}
	if len(mobile) == 0 {
		t.Fatal("mobile Setup fixture returned no routes")
	}

	serverKeys := routeKeys(server)
	for _, want := range []string{
		"GET /api/v1/auth/check-init",
		"PUT /api/v1/tasks/:id/run",
		"GET /api/v1/logs/:id/stream",
		"POST /api/v1/android-runtime/install",
	} {
		if !serverKeys[want] {
			t.Errorf("complete Setup fixture is missing %s", want)
		}
	}

	mobileKeys := routeKeys(mobile)
	if !mobileKeys["GET /api/v1/auth/check-init"] {
		t.Error("mobile Setup fixture is missing its registered auth route")
	}
	if mobileKeys["PUT /api/v1/tasks/:id/run"] {
		t.Error("current mobile Setup fixture unexpectedly advertises task execution")
	}
}

func TestRouteContractNormalizesAndDiffsBothDirections(t *testing.T) {
	server := []gin.RouteInfo{
		{Method: " get ", Path: "//api/v1/tasks/:taskID/"},
		{Method: "POST", Path: "/api/v1/tasks"},
		{Method: "GET", Path: "/api/v1/tasks/:id"},
	}
	mobile := []gin.RouteInfo{
		{Method: "GET", Path: "/api/v1/tasks/:id"},
		{Method: "delete", Path: "/api/v1/tasks/:id"},
	}

	diff := DiffRoutes(server, mobile)
	assertRouteKeys(t, diff.MissingFromMobile, []string{"POST /api/v1/tasks"})
	assertRouteKeys(t, diff.MissingFromServer, []string{"DELETE /api/v1/tasks/:id"})
}

func TestRouteContractClassifiesEveryServerRoute(t *testing.T) {
	server := CanonicalServerRoutes()
	mobile := CanonicalMobileRoutes()
	contracts := BuildRouteContracts(server, mobile)
	if len(contracts) != len(server) {
		t.Fatalf("got %d contracts for %d canonical server routes", len(contracts), len(server))
	}

	serverKeys := routeKeys(server)
	contractKeys := make(map[string]bool, len(contracts))
	for _, contract := range contracts {
		key := contract.Method + " " + contract.Path
		contractKeys[key] = true
		if contract.Module == "" || contract.MobileStatus == "" || contract.AndroidEquivalent == "" ||
			contract.AuthContract == "" || contract.StreamContract == "" || contract.TestCase == "" {
			t.Errorf("route %s has an incomplete classification: %+v", key, contract)
		}
		if contract.MobileStatus == "supported" && !routeKeys(mobile)[key] {
			t.Errorf("route %s is marked supported without a mobile fixture route", key)
		}
	}
	for key := range serverKeys {
		if !contractKeys[key] {
			t.Errorf("server route %s is absent from the contract", key)
		}
	}
}

func routeKeys(routes []gin.RouteInfo) map[string]bool {
	keys := make(map[string]bool, len(routes))
	for _, route := range routes {
		keys[route.Method+" "+route.Path] = true
	}
	return keys
}

func assertRouteKeys(t *testing.T, routes []gin.RouteInfo, want []string) {
	t.Helper()
	got := routeKeys(routes)
	if len(got) != len(want) {
		t.Fatalf("got routes %v, want %v", got, want)
	}
	for _, key := range want {
		if !got[key] {
			t.Errorf("missing route %s in %v", key, got)
		}
	}
}
