package router

import (
	"crypto/sha256"
	"crypto/subtle"

	"daidai-panel/handler"
	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
)

type ManagementSecurity struct {
	LocalToken string
	Host       string
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
		providedTokenDigest := sha256.Sum256([]byte(c.GetHeader("X-Daidai-Local-Token")))
		if subtle.ConstantTimeCompare(providedTokenDigest[:], expectedTokenDigest[:]) != 1 {
			c.AbortWithStatus(401)
			return
		}
		if c.Request.Host != security.Host {
			c.AbortWithStatus(403)
			return
		}
		if c.GetHeader("Origin") != expectedOrigin {
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
