package router

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"daidai-panel/handler"
	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
)

// ManagementSecurity 本机版（Android）扩展：管理端口（loopback）安全配置。
type ManagementSecurity struct {
	LocalToken string
	Host       string
	Browser    *LoopbackBrowserAccess
}

func Setup(engine *gin.Engine) {
	engine.Use(middleware.CORS())
	engine.Use(middleware.SecurityHeaders())

	v1 := engine.Group("/api/v1")
	legacy := engine.Group("/api")

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
	consoleHandler := handler.NewConsoleHandler()
	openAPIHandler := handler.NewOpenAPIHandler()
	depsHandler := handler.NewDepsHandler()
	configHandler := handler.NewConfigHandler()
	platformTokenHandler := handler.NewPlatformTokenHandler()
	sponsorHandler := handler.NewSponsorHandler()
	androidRuntimeHandler := handler.NewAndroidRuntimeHandler()

	authHandler.RegisterRoutes(v1)
	authHandler.RegisterRoutes(legacy)

	taskHandler.RegisterRoutes(v1)
	taskHandler.RegisterRoutes(legacy)

	logHandler.RegisterRoutes(v1)
	logHandler.RegisterRoutes(legacy)

	scriptHandler.RegisterRoutes(v1)
	scriptHandler.RegisterRoutes(legacy)

	envHandler.RegisterRoutes(v1)
	envHandler.RegisterRoutes(legacy)

	subHandler.RegisterRoutes(v1)
	subHandler.RegisterRoutes(legacy)

	notifyHandler.RegisterRoutes(v1)
	notifyHandler.RegisterRoutes(legacy)

	sshKeyHandler.RegisterRoutes(v1)
	sshKeyHandler.RegisterRoutes(legacy)

	userHandler.RegisterRoutes(v1)
	userHandler.RegisterRoutes(legacy)

	securityHandler.RegisterRoutes(v1)
	securityHandler.RegisterRoutes(legacy)

	systemHandler.RegisterRoutes(v1)
	systemHandler.RegisterRoutes(legacy)

	consoleHandler.RegisterRoutes(v1)
	consoleHandler.RegisterRoutes(legacy)

	openAPIHandler.RegisterRoutes(v1)
	openAPIHandler.RegisterRoutes(legacy)

	depsHandler.RegisterRoutes(v1)
	depsHandler.RegisterRoutes(legacy)

	configHandler.RegisterRoutes(v1)
	configHandler.RegisterRoutes(legacy)

	platformTokenHandler.RegisterRoutes(v1)
	platformTokenHandler.RegisterRoutes(legacy)

	sponsorHandler.RegisterRoutes(v1)
	sponsorHandler.RegisterRoutes(legacy)

	androidRuntimeHandler.RegisterRoutes(v1)
	androidRuntimeHandler.RegisterRoutes(legacy)

	// 内置 MCP 服务（issue #128）。它的工具调用会在进程内回放到上面这些 /api/v1 接口，
	// 所以要把 engine 本身交给它；回放发生在请求期，届时所有路由（含静态前端）都已注册完。
	mcpHandler := handler.NewMCPHandler(engine)
	mcpHandler.RegisterRoutes(v1)
	mcpHandler.RegisterRoutes(legacy)

	// 企业微信拉起任务的回调（issue #145）。和 MCP 是同一个形态：路由公开、鉴权在 handler 内，
	// 触发执行时在进程内回放到上面那些 /api/v1 接口，所以同样要把 engine 本身交给它。
	wecomCallbackHandler := handler.NewWecomCallbackHandler(engine)
	wecomCallbackHandler.RegisterRoutes(v1)
	wecomCallbackHandler.RegisterRoutes(legacy)

	engine.GET("/robots.txt", func(c *gin.Context) {
		c.Data(200, "text/plain; charset=utf-8", []byte("User-agent: *\nDisallow: /\n"))
	})

	engine.GET("/api/v1/version", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"version":     handler.Version,
			"api_version": "v1",
			"framework":   "gin",
		})
	})
	engine.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	engine.GET("/api/version", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"version":     handler.Version,
			"api_version": "v1",
			"framework":   "gin",
		})
	})
	engine.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
}

// --- 本机版（Android）扩展：管理路由与元数据路由 ---

func SetupManagement(engine *gin.Engine, security ManagementSecurity) {
	engine.Use(managementSecurityMiddleware(security))
	engine.Use(middleware.CORS())
	engine.Use(middleware.SecurityHeaders())

	authHandler := handler.NewManagementAuthHandler()
	taskHandler := handler.NewManagementTaskHandler()
	for _, prefix := range []string{"/api/v1", "/api"} {
		group := engine.Group(prefix)
		authHandler.RegisterManagementRoutes(group)
		taskHandler.RegisterManagementRoutes(group)
	}

	registerMetadataRoutes(engine)
}

func managementSecurityMiddleware(security ManagementSecurity) gin.HandlerFunc {
	expectedTokenDigest := sha256.Sum256([]byte(security.LocalToken))
	expectedOrigin := "http://" + security.Host
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/local-ui" || strings.HasPrefix(c.Request.URL.Path, "/local-ui/") {
			c.Next()
			return
		}
		browserSession := security.Browser != nil && security.Browser.HasSession(c.Request)
		providedTokenDigest := sha256.Sum256([]byte(c.GetHeader("X-Daidai-Local-Token")))
		if !browserSession && subtle.ConstantTimeCompare(providedTokenDigest[:], expectedTokenDigest[:]) != 1 {
			c.AbortWithStatus(401)
			return
		}
		if c.Request.Host != security.Host {
			c.AbortWithStatus(403)
			return
		}
		origin := c.GetHeader("Origin")
		if browserSession {
			if origin != "" && origin != expectedOrigin {
				c.AbortWithStatus(403)
				return
			}
		} else if origin != expectedOrigin {
			c.AbortWithStatus(403)
			return
		}
		c.Next()
	}
}

func registerMetadataRoutes(engine *gin.Engine) {
	engine.GET("/robots.txt", func(c *gin.Context) {
		c.Data(200, "text/plain; charset=utf-8", []byte("User-agent: *\nDisallow: /\n"))
	})
	for _, prefix := range []string{"/api/v1", "/api"} {
		engine.GET(prefix+"/version", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"version":     handler.Version,
				"api_version": "v1",
				"framework":   "gin",
			})
		})
		engine.GET(prefix+"/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
	}
}
