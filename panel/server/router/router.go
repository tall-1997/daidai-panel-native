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
	registerFullHandlers(engine)

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
