package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const upstreamSourceVersion = "9a7c27e2a03ed30d3ac632adf58fec60009e601c"

type RouteMetadata struct {
	AuthContract    string
	StreamContract  string
	HandlerContract string
}

type RouteDescriptor struct {
	AuthContract   string
	StreamContract string
	TestCase       string
}

type CapabilityRoute struct {
	Method     string
	Path       string
	Capability string
}

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
	descriptor, ok := explicitRouteDescriptors[method+" "+routePath]
	if !ok {
		return RouteMetadata{}, false
	}
	metadata := RouteMetadata{
		AuthContract:   descriptor.AuthContract,
		StreamContract: descriptor.StreamContract,
	}
	if descriptor.StreamContract == "sse" {
		metadata.HandlerContract = "event-stream"
	}
	return metadata, true
}

func RouteDescriptors() map[string]RouteDescriptor {
	descriptors := make(map[string]RouteDescriptor, len(explicitRouteDescriptors))
	for key, descriptor := range explicitRouteDescriptors {
		descriptors[key] = descriptor
	}
	return descriptors
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
