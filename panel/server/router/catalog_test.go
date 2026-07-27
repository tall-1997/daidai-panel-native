package router

import (
	"strings"
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
	if !mobileKeys["PUT /api/v1/tasks/:id/run"] {
		t.Error("mobile profile is missing capability-covered task execution")
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
		if contract.MobileStatus == "planned" {
			t.Errorf("route %s remains planned in Milestone 1", key)
		}
		wantTestCase := "TestMobileRouteContract/" + routeSubtestKey(contract.Method, contract.Path)
		if contract.TestCase != wantTestCase {
			t.Errorf("route %s testCase=%q want=%q", key, contract.TestCase, wantTestCase)
		}
	}
	for key := range serverKeys {
		if !contractKeys[key] {
			t.Errorf("server route %s is absent from the contract", key)
		}
	}
}

func TestRouteMetadataCoversCanonicalRoutesAndExplicitExceptions(t *testing.T) {
	wantPublic := map[string]bool{
		"POST /api/open-api/token":    true,
		"POST /api/v1/open-api/token": true,
	}
	wantStreams := map[string]bool{
		"GET /api/deps/:id/log-stream":              true,
		"GET /api/v1/deps/:id/log-stream":           true,
		"GET /api/subscriptions/:id/pull-stream":    true,
		"GET /api/v1/subscriptions/:id/pull-stream": true,
		"POST /api/android-runtime/install":         true,
		"POST /api/v1/android-runtime/install":      true,
	}

	for _, route := range CanonicalServerRoutes() {
		metadata, ok := MetadataForRoute(route.Method, route.Path)
		if !ok {
			t.Errorf("route %s has no explicit metadata source", routeKey(route))
			continue
		}
		key := routeKey(route)
		if wantPublic[key] && metadata.AuthContract != "public" {
			t.Errorf("route %s auth=%q want=public", key, metadata.AuthContract)
		}
		if wantStreams[key] && metadata.StreamContract != "sse" {
			t.Errorf("route %s stream=%q want=sse", key, metadata.StreamContract)
		}
	}
}

func TestRouteMetadataListsOnlyRealSSEHandlers(t *testing.T) {
	want := map[string]bool{
		"GET /api/deps/:id/log-stream":              true,
		"GET /api/v1/deps/:id/log-stream":           true,
		"GET /api/logs/:id/stream":                  true,
		"GET /api/v1/logs/:id/stream":               true,
		"GET /api/subscriptions/:id/pull-stream":    true,
		"GET /api/v1/subscriptions/:id/pull-stream": true,
		"POST /api/android-runtime/install":         true,
		"POST /api/v1/android-runtime/install":      true,
	}
	got := make(map[string]bool)
	for _, route := range CanonicalServerRoutes() {
		metadata, ok := MetadataForRoute(route.Method, route.Path)
		if ok && metadata.StreamContract == "sse" {
			got[routeKey(route)] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("SSE routes=%v want=%v", got, want)
	}
	for key := range want {
		if !got[key] {
			t.Errorf("missing SSE route %s", key)
		}
	}
}

func TestCanonicalServerRoutesUnionsNamedFixtures(t *testing.T) {
	base := CanonicalServerRoutesForFixtures([]ServerRouteFixture{{
		Name:     "base",
		Register: func(engine *gin.Engine) { engine.GET("/base", func(*gin.Context) {}) },
	}})
	union := CanonicalServerRoutesForFixtures([]ServerRouteFixture{
		{Name: "base", Register: func(engine *gin.Engine) { engine.GET("/base", func(*gin.Context) {}) }},
		{Name: "optional", Register: func(engine *gin.Engine) { engine.POST("/optional", func(*gin.Context) {}) }},
	})
	if len(base) != 1 || len(union) != 2 || !routeKeys(union)["POST /optional"] {
		t.Fatalf("fixture union base=%v union=%v", routeKeys(base), routeKeys(union))
	}
}

func TestBuildContractDocumentCarriesSourceAndFixtureMetadata(t *testing.T) {
	document := BuildContractDocument(CanonicalServerRoutes(), CanonicalMobileRoutes())
	if !strings.HasPrefix(document.SourceVersion, "0e33d022") {
		t.Fatalf("sourceVersion=%q", document.SourceVersion)
	}
	if len(document.Fixtures) == 0 || document.Fixtures[0] != "all-enabled-default" {
		t.Fatalf("fixtures=%v", document.Fixtures)
	}
	if len(document.Routes) != 423 {
		t.Fatalf("routes=%d want=423", len(document.Routes))
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
