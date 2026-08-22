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

var extensionRouteDescriptors = map[string]RouteDescriptor{
	"GET /api/logs/:id/raw":                              {AuthContract: "public", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_logs_id_raw"},
	"GET /api/logs/:id/raw-ticket":                       {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_logs_id_raw-ticket"},
	"GET /api/tasks/:id/log-files/:id/raw":               {AuthContract: "public", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_tasks_id_log-files_id_raw"},
	"GET /api/tasks/:id/log-files/:id/raw-ticket":        {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_tasks_id_log-files_id_raw-ticket"},
	"GET /api/v1/logs/:id/raw":                           {AuthContract: "public", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_v1_logs_id_raw"},
	"GET /api/v1/logs/:id/raw-ticket":                    {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_v1_logs_id_raw-ticket"},
	"GET /api/v1/tasks/:id/log-files/:id/raw":            {AuthContract: "public", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_v1_tasks_id_log-files_id_raw"},
	"GET /api/v1/tasks/:id/log-files/:id/raw-ticket":     {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/get_api_v1_tasks_id_log-files_id_raw-ticket"},
	"POST /api/system/stop":                              {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/post_api_system_stop"},
	"POST /api/v1/system/stop":                           {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/post_api_v1_system_stop"},
	"PUT /api/envs/by-name":                              {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/put_api_envs_by-name"},
	"PUT /api/tasks/:id/restore-subscription-default":    {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/put_api_tasks_id_restore-subscription-default"},
	"PUT /api/v1/envs/by-name":                           {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/put_api_v1_envs_by-name"},
	"PUT /api/v1/tasks/:id/restore-subscription-default": {AuthContract: "jwt", StreamContract: "none", TestCase: "TestMobileRouteContract/put_api_v1_tasks_id_restore-subscription-default"},
}

var terminalRouteDescriptors = func() map[string]RouteDescriptor {
	descriptors := make(map[string]RouteDescriptor, 12)
	for _, prefix := range []string{"/api", "/api/v1"} {
		testPrefix := strings.ReplaceAll(strings.TrimPrefix(prefix, "/"), "/", "_")
		add := func(method, path, testName string) {
			descriptors[method+" "+prefix+path] = RouteDescriptor{
				AuthContract:   "jwt",
				StreamContract: "none",
				TestCase:       "TestMobileRouteContract/" + strings.ToLower(method) + "_" + testPrefix + "_" + testName,
			}
		}
		add(http.MethodPost, "/terminal/sessions", "terminal_sessions")
		add(http.MethodGet, "/terminal/sessions/:id", "terminal_sessions_id")
		add(http.MethodPost, "/terminal/sessions/:id/input", "terminal_sessions_id_input")
		add(http.MethodPut, "/terminal/sessions/:id/resize", "terminal_sessions_id_resize")
		add(http.MethodPut, "/terminal/sessions/:id/stop", "terminal_sessions_id_stop")
		add(http.MethodDelete, "/terminal/sessions/:id", "terminal_sessions_id")
	}
	return descriptors
}()

func descriptorForRoute(key string) (RouteDescriptor, bool) {
	descriptor, ok := explicitRouteDescriptors[key]
	if ok {
		return descriptor, true
	}
	descriptor, ok = extensionRouteDescriptors[key]
	if ok {
		return descriptor, true
	}
	descriptor, ok = terminalRouteDescriptors[key]
	return descriptor, ok
}

func MetadataForRoute(method, routePath string) (RouteMetadata, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	routePath = normalizeRoutePath(routePath)
	descriptor, ok := descriptorForRoute(method + " " + routePath)
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
	descriptors := make(map[string]RouteDescriptor, len(explicitRouteDescriptors)+len(extensionRouteDescriptors)+len(terminalRouteDescriptors))
	for key, descriptor := range explicitRouteDescriptors {
		descriptors[key] = descriptor
	}
	for key, descriptor := range extensionRouteDescriptors {
		descriptors[key] = descriptor
	}
	for key, descriptor := range terminalRouteDescriptors {
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
