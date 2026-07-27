package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const upstreamSourceVersion = "0e33d022beb1ed16e68d4de022fef86f9834f2df"

type RouteMetadata struct {
	AuthContract    string
	StreamContract  string
	HandlerContract string
}

type CapabilityRoute struct {
	Method     string
	Path       string
	Capability string
}

var publicRouteOverrides = routeMetadataOverrides([]string{
	"GET /auth/check-init",
	"POST /auth/init",
	"POST /auth/login",
	"POST /auth/refresh",
	"GET /auth/captcha-config",
	"GET /auth/avatar/:id",
	"POST /open-api/token",
	"GET /system/public-version",
	"GET /system/panel-settings",
	"GET /sponsors",
}, RouteMetadata{AuthContract: "public", StreamContract: "none"})

var streamRouteOverrides = routeMetadataOverrides([]string{
	"GET /logs/:id/stream",
	"GET /deps/:id/log-stream",
	"GET /subscriptions/:id/pull-stream",
	"POST /android-runtime/install",
}, RouteMetadata{AuthContract: "jwt", StreamContract: "sse", HandlerContract: "event-stream"})

var capabilityRouteDefinitions = []CapabilityRoute{
	{http.MethodPut, "/tasks/:id/run", CapabilityTaskExecution},
	{http.MethodPut, "/tasks/:id/stop", CapabilityTaskExecution},
	{http.MethodPost, "/tasks/batch/run", CapabilityTaskExecution},
	{http.MethodPost, "/scripts/run", CapabilityScriptExecution},
	{http.MethodPost, "/scripts/run-code", CapabilityScriptExecution},
	{http.MethodGet, "/scripts/run/:id/logs", CapabilityScriptExecution},
	{http.MethodPut, "/scripts/run/:id/stop", CapabilityScriptExecution},
	{http.MethodDelete, "/scripts/run/:id", CapabilityScriptExecution},
	{http.MethodPost, "/scripts/format", CapabilityScriptExecution},
	{http.MethodPost, "/deps", CapabilityDependencyMutation},
	{http.MethodPost, "/deps/batch-reinstall", CapabilityDependencyMutation},
	{http.MethodPost, "/deps/batch-delete", CapabilityDependencyMutation},
	{http.MethodDelete, "/deps/:id", CapabilityDependencyMutation},
	{http.MethodPut, "/deps/:id/cancel", CapabilityDependencyMutation},
	{http.MethodPut, "/deps/:id/reinstall", CapabilityDependencyMutation},
	{http.MethodPut, "/deps/python-runtime-default", CapabilityDependencyMutation},
	{http.MethodPut, "/deps/mirrors", CapabilityDependencyMutation},
	{http.MethodPut, "/subscriptions/:id/pull", CapabilitySubscriptionPull},
	{http.MethodPut, "/subscriptions/:id/pull/stop", CapabilitySubscriptionPull},
	{http.MethodGet, "/subscriptions/:id/pull-stream", CapabilitySubscriptionPull},
	{http.MethodPost, "/system/update", CapabilitySystemUpdate},
	{http.MethodPost, "/system/restart", CapabilitySystemRestart},
	{http.MethodPost, "/system/backup", CapabilityBackupMutation},
	{http.MethodPost, "/system/backup/upload", CapabilityBackupMutation},
	{http.MethodPost, "/system/restore", CapabilityBackupMutation},
	{http.MethodDelete, "/system/backup", CapabilityBackupMutation},
	{http.MethodPost, "/android-runtime/install", CapabilityRuntimeMutation},
	{http.MethodPost, "/android-runtime/uninstall", CapabilityRuntimeMutation},
	{http.MethodPost, "/notifications/send", CapabilityNotificationDispatch},
	{http.MethodPost, "/notifications/:id/test", CapabilityNotificationDispatch},
}

func MetadataForRoute(method, routePath string) (RouteMetadata, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	routePath = normalizeRoutePath(routePath)
	if routePath == "/robots.txt" || routePath == "/api/health" || routePath == "/api/version" ||
		routePath == "/api/v1/health" || routePath == "/api/v1/version" {
		return RouteMetadata{AuthContract: "public", StreamContract: "none"}, true
	}

	relativePath, ok := apiRelativePath(routePath)
	if !ok || !isExplicitGenericRouteGroup(relativePath) {
		return RouteMetadata{}, false
	}
	key := method + " " + relativePath
	if metadata, exists := streamRouteOverrides[key]; exists {
		return metadata, true
	}
	if metadata, exists := publicRouteOverrides[key]; exists {
		return metadata, true
	}
	return RouteMetadata{AuthContract: "jwt", StreamContract: "none"}, true
}

func MobileCapabilityRoutes() []CapabilityRoute {
	routes := make([]CapabilityRoute, 0, len(capabilityRouteDefinitions)*2)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, route := range capabilityRouteDefinitions {
			route.Path = prefix + route.Path
			routes = append(routes, route)
		}
	}
	return routes
}

func mobileRouteCapability(method, routePath string) string {
	key := strings.ToUpper(strings.TrimSpace(method)) + " " + normalizeRoutePath(routePath)
	for _, route := range MobileCapabilityRoutes() {
		if route.Method+" "+route.Path == key {
			return route.Capability
		}
	}
	return ""
}

func routeMetadataOverrides(routes []string, metadata RouteMetadata) map[string]RouteMetadata {
	overrides := make(map[string]RouteMetadata, len(routes))
	for _, route := range routes {
		overrides[route] = metadata
	}
	return overrides
}

func apiRelativePath(routePath string) (string, bool) {
	for _, prefix := range []string{"/api/v1", "/api"} {
		if routePath == prefix {
			return "/", true
		}
		if strings.HasPrefix(routePath, prefix+"/") {
			return strings.TrimPrefix(routePath, prefix), true
		}
	}
	return "", false
}

func isExplicitGenericRouteGroup(relativePath string) bool {
	for _, group := range []string{
		"/auth", "/tasks", "/logs", "/scripts", "/envs", "/subscriptions",
		"/notifications", "/ssh-keys", "/users", "/security", "/system",
		"/open-api", "/deps", "/configs", "/platform-tokens", "/sponsors",
		"/android-runtime",
	} {
		if relativePath == group || strings.HasPrefix(relativePath, group+"/") {
			return true
		}
	}
	return false
}

func routeSubtestKey(method, routePath string) string {
	replacer := strings.NewReplacer("/", "_", ":", "", "*", "")
	return strings.ToLower(method) + replacer.Replace(normalizeRoutePath(routePath))
}

func concreteRoutePath(routePath string) string {
	parts := strings.Split(routePath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

type ServerRouteFixture struct {
	Name     string
	Register func(*gin.Engine)
}
