package router

import (
	"fmt"
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

func CanonicalServerRoutes() []gin.RouteInfo {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine)
	return canonicalRoutes(engine.Routes())
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
		contracts = append(contracts, RouteContract{
			Method:            route.Method,
			Path:              route.Path,
			Module:            module,
			MobileStatus:      status,
			AndroidEquivalent: androidEquivalent,
			AuthContract:      routeAuthContract(route.Path),
			StreamContract:    routeStreamContract(route.Path),
			TestCase:          fmt.Sprintf("route:%s %s", route.Method, route.Path),
		})
	}
	return contracts
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

func routeAuthContract(routePath string) string {
	for _, publicPath := range []string{
		"/auth/check-init", "/auth/init", "/auth/login", "/auth/refresh",
		"/auth/captcha-config", "/auth/avatar/:id", "/health", "/version",
		"/system/public-version", "/system/panel-settings", "/robots.txt",
	} {
		if strings.HasSuffix(routePath, publicPath) {
			return "public"
		}
	}
	return "jwt"
}

func routeStreamContract(routePath string) string {
	if strings.HasSuffix(routePath, "/stream") || strings.HasSuffix(routePath, "/live-logs") {
		return "sse"
	}
	return "none"
}
