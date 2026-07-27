package router

import (
	"path"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type RouteContract struct {
	Method            string `json:"method"`
	Path              string `json:"path"`
	Module            string `json:"module"`
	MobileStatus      string `json:"mobileStatus"`
	AndroidEquivalent string `json:"androidEquivalent,omitempty"`
	AuthContract      string `json:"authContract"`
	StreamContract    string `json:"streamContract"`
	TestCase          string `json:"testCase"`
}

type RouteDiff struct {
	MissingFromMobile []gin.RouteInfo `json:"missingFromMobile"`
	MissingFromServer []gin.RouteInfo `json:"missingFromServer"`
}

type ContractDocument struct {
	SourceVersion string          `json:"sourceVersion"`
	Fixtures      []string        `json:"fixtures"`
	Routes        []RouteContract `json:"routes"`
}

func CanonicalServerRoutes() []gin.RouteInfo {
	return CanonicalServerRoutesForFixtures([]ServerRouteFixture{{Name: "all-enabled-default", Register: Setup}})
}

func CanonicalServerRoutesForFixtures(fixtures []ServerRouteFixture) []gin.RouteInfo {
	gin.SetMode(gin.TestMode)
	routes := make([]gin.RouteInfo, 0)
	for _, fixture := range fixtures {
		engine := gin.New()
		fixture.Register(engine)
		routes = append(routes, engine.Routes()...)
	}
	return canonicalRoutes(routes)
}

func CanonicalMobileRoutes() []gin.RouteInfo {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetupMobileFull(engine, ManagementSecurity{LocalToken: "route-contract", Host: "127.0.0.1"}, NewMobilePlatform(CapabilitySnapshot{Version: 1}))
	return canonicalRoutes(engine.Routes())
}

func DiffRoutes(server, mobile []gin.RouteInfo) RouteDiff {
	server = canonicalRoutes(server)
	mobile = canonicalRoutes(mobile)
	serverKeys := routeSet(server)
	mobileKeys := routeSet(mobile)

	diff := RouteDiff{}
	for _, route := range server {
		if _, ok := mobileKeys[routeKey(route)]; !ok {
			diff.MissingFromMobile = append(diff.MissingFromMobile, route)
		}
	}
	for _, route := range mobile {
		if _, ok := serverKeys[routeKey(route)]; !ok {
			diff.MissingFromServer = append(diff.MissingFromServer, route)
		}
	}
	return diff
}

func BuildRouteContracts(server, mobile []gin.RouteInfo) []RouteContract {
	server = canonicalRoutes(server)
	mobileKeys := routeSet(canonicalRoutes(mobile))
	contracts := make([]RouteContract, 0, len(server))
	for _, route := range server {
		module := routeModule(route.Path)
		status := "supported"
		androidEquivalent := "native:" + module
		if _, ok := mobileKeys[routeKey(route)]; ok {
			if capability := mobileRouteCapability(route.Method, route.Path); capability != "" {
				status = "android_equivalent"
				androidEquivalent = "capability:" + capability
			}
		} else {
			status = "planned"
			androidEquivalent = "planned:" + module
		}
		metadata, ok := MetadataForRoute(route.Method, route.Path)
		if !ok {
			panic("route metadata missing for " + routeKey(route))
		}
		descriptor := explicitRouteDescriptors[routeKey(route)]
		contracts = append(contracts, RouteContract{
			Method:            route.Method,
			Path:              route.Path,
			Module:            module,
			MobileStatus:      status,
			AndroidEquivalent: androidEquivalent,
			AuthContract:      metadata.AuthContract,
			StreamContract:    metadata.StreamContract,
			TestCase:          descriptor.TestCase,
		})
	}
	return contracts
}

func BuildContractDocument(server, mobile []gin.RouteInfo) ContractDocument {
	return ContractDocument{
		SourceVersion: upstreamSourceVersion,
		Fixtures:      []string{"all-enabled-default"},
		Routes:        BuildRouteContracts(server, mobile),
	}
}

func canonicalRoutes(routes []gin.RouteInfo) []gin.RouteInfo {
	byKey := make(map[string]gin.RouteInfo, len(routes))
	for _, route := range routes {
		route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
		route.Path = normalizeRoutePath(route.Path)
		route.Handler = ""
		byKey[routeKey(route)] = route
	}
	canonical := make([]gin.RouteInfo, 0, len(byKey))
	for _, route := range byKey {
		canonical = append(canonical, route)
	}
	sort.Slice(canonical, func(i, j int) bool {
		return routeKey(canonical[i]) < routeKey(canonical[j])
	})
	return canonical
}

func normalizeRoutePath(routePath string) string {
	routePath = path.Clean("/" + strings.TrimSpace(routePath))
	parts := strings.Split(routePath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = ":id"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "*path"
		}
	}
	return strings.Join(parts, "/")
}

func routeSet(routes []gin.RouteInfo) map[string]struct{} {
	set := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		set[routeKey(route)] = struct{}{}
	}
	return set
}

func routeKey(route gin.RouteInfo) string {
	return route.Method + " " + route.Path
}

func routeModule(routePath string) string {
	trimmed := strings.TrimPrefix(routePath, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) > 0 && parts[0] == "robots.txt" {
		return "metadata"
	}
	if len(parts) > 0 && parts[0] == "api" {
		parts = parts[1:]
	}
	if len(parts) > 0 && parts[0] == "v1" {
		parts = parts[1:]
	}
	if len(parts) == 0 || parts[0] == "" || parts[0] == "health" || parts[0] == "version" {
		return "metadata"
	}
	return parts[0]
}
