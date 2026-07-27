package router

import (
	"net/http"
	"strings"

	"daidai-panel/handler"
	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
)

const (
	CapabilityDisabled = "disabled"
	CapabilityEnabled  = "enabled"

	CapabilityTaskExecution        = "task_execution"
	CapabilityScriptExecution      = "script_execution"
	CapabilityDependencyMutation   = "dependency_mutation"
	CapabilitySubscriptionPull     = "subscription_pull"
	CapabilitySystemUpdate         = "system_update"
	CapabilitySystemRestart        = "system_restart"
	CapabilityBackupMutation       = "backup_mutation"
	CapabilityRuntimeMutation      = "runtime_mutation"
	CapabilityNotificationDispatch = "notification_dispatch"
)

type CapabilityState struct {
	State      string `json:"state"`
	ReasonCode string `json:"reasonCode,omitempty"`
	AdapterID  string `json:"adapterId,omitempty"`
}

type CapabilitySnapshot struct {
	Version      int                        `json:"version"`
	Capabilities map[string]CapabilityState `json:"capabilities"`
}

type MobilePlatform interface {
	Capability(string) CapabilityState
}

type immutableMobilePlatform struct {
	capabilities map[string]CapabilityState
}

func NewMobilePlatform(snapshot CapabilitySnapshot) MobilePlatform {
	capabilities := make(map[string]CapabilityState, len(snapshot.Capabilities))
	for id, state := range snapshot.Capabilities {
		capabilities[id] = state
	}
	return immutableMobilePlatform{capabilities: capabilities}
}

func (platform immutableMobilePlatform) Capability(id string) CapabilityState {
	state, ok := platform.capabilities[id]
	if !ok || state.State == "" {
		return CapabilityState{State: CapabilityDisabled, ReasonCode: "NOT_DECLARED"}
	}
	return state
}

var registerMobileHandlers = registerFullHandlers

func SetupMobileFull(engine *gin.Engine, security ManagementSecurity, platform MobilePlatform) {
	if platform == nil {
		platform = NewMobilePlatform(CapabilitySnapshot{Version: 1})
	}
	engine.Use(managementSecurityMiddleware(security))
	engine.Use(mobileCapabilityMiddleware(platform))
	engine.Use(middleware.CORS())
	engine.Use(middleware.SecurityHeaders())
	registerMobileHandlers(engine)
	registerMetadataRoutes(engine)
}

func registerFullHandlers(engine *gin.Engine) {
	authHandler := handler.NewAuthHandler()
	taskHandler := handler.NewTaskHandler()
	logHandler := handler.NewLogHandler()
	scriptHandler := handler.NewScriptHandler()
	envHandler := handler.NewEnvHandler()
	subHandler := handler.NewSubscriptionHandler()
	notifyHandler := handler.NewNotificationHandler()
	sshKeyHandler := handler.NewSSHKeyHandler()
	userHandler := handler.NewUserHandler()
	securityHandler := handler.NewSecurityHandler()
	systemHandler := handler.NewSystemHandler()
	openAPIHandler := handler.NewOpenAPIHandler()
	depsHandler := handler.NewDepsHandler()
	configHandler := handler.NewConfigHandler()
	platformTokenHandler := handler.NewPlatformTokenHandler()
	sponsorHandler := handler.NewSponsorHandler()
	androidRuntimeHandler := handler.NewAndroidRuntimeHandler()

	for _, prefix := range []string{"/api/v1", "/api"} {
		group := engine.Group(prefix)
		authHandler.RegisterRoutes(group)
		taskHandler.RegisterRoutes(group)
		logHandler.RegisterRoutes(group)
		scriptHandler.RegisterRoutes(group)
		envHandler.RegisterRoutes(group)
		subHandler.RegisterRoutes(group)
		notifyHandler.RegisterRoutes(group)
		sshKeyHandler.RegisterRoutes(group)
		userHandler.RegisterRoutes(group)
		securityHandler.RegisterRoutes(group)
		systemHandler.RegisterRoutes(group)
		openAPIHandler.RegisterRoutes(group)
		depsHandler.RegisterRoutes(group)
		configHandler.RegisterRoutes(group)
		platformTokenHandler.RegisterRoutes(group)
		sponsorHandler.RegisterRoutes(group)
		androidRuntimeHandler.RegisterRoutes(group)
	}
}

func mobileCapabilityMiddleware(platform MobilePlatform) gin.HandlerFunc {
	return func(c *gin.Context) {
		capability := mobileRouteCapability(c.Request.Method, c.FullPath())
		if capability == "" {
			c.Next()
			return
		}

		state := platform.Capability(capability)
		status := http.StatusConflict
		reasonCode := state.ReasonCode
		if reasonCode == "" {
			reasonCode = "CAPABILITY_DISABLED"
		}
		if state.State == CapabilityEnabled {
			status = http.StatusNotImplemented
			reasonCode = "ADAPTER_UNAVAILABLE"
		}
		c.AbortWithStatusJSON(status, gin.H{
			"errorCode":  "PLATFORM_CAPABILITY",
			"capability": capability,
			"state":      state.State,
			"reasonCode": reasonCode,
		})
	}
}

func mobileRouteCapability(method, routePath string) string {
	path := strings.TrimPrefix(routePath, "/api/v1")
	path = strings.TrimPrefix(path, "/api")
	switch {
	case strings.HasPrefix(path, "/tasks/") && (strings.HasSuffix(path, "/run") || strings.HasSuffix(path, "/stop")),
		path == "/tasks/batch/run":
		return CapabilityTaskExecution
	case strings.HasPrefix(path, "/scripts/run"), path == "/scripts/format":
		return CapabilityScriptExecution
	case strings.HasPrefix(path, "/deps") && method != http.MethodGet:
		return CapabilityDependencyMutation
	case strings.HasPrefix(path, "/subscriptions/") && strings.Contains(path, "/pull"):
		return CapabilitySubscriptionPull
	case path == "/system/update":
		return CapabilitySystemUpdate
	case path == "/system/restart":
		return CapabilitySystemRestart
	case path == "/system/restore", path == "/system/backup", path == "/system/backup/upload":
		return CapabilityBackupMutation
	case strings.HasPrefix(path, "/android-runtime/") && method != http.MethodGet:
		return CapabilityRuntimeMutation
	case path == "/notifications/send", strings.HasSuffix(path, "/test") && strings.HasPrefix(path, "/notifications/"):
		return CapabilityNotificationDispatch
	default:
		return ""
	}
}
