package middleware

import (
	"net/http"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"

	"github.com/gin-gonic/gin"
)

func isAppToken(username, role string) bool {
	return strings.HasPrefix(username, "app:") || strings.HasPrefix(role, "app:")
}

func appScopeAllowed(scopeList, required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return false
	}

	scopeList = strings.TrimSpace(scopeList)
	if scopeList == "" {
		return false
	}

	for _, item := range strings.Split(scopeList, ",") {
		item = strings.TrimSpace(item)
		if item == "*" || item == required {
			return true
		}
	}
	return false
}

func loadOpenAppByUsername(username string) (*model.OpenApp, error) {
	appKey := strings.TrimPrefix(username, "app:")
	var app model.OpenApp
	if err := database.DB.Where("app_key = ?", appKey).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func RequireUserToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.GetString("username")
		role := c.GetString("role")
		if isAppToken(username, role) {
			c.JSON(http.StatusForbidden, gin.H{"error": "应用令牌无权访问此接口"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func OpenAPIAccess(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("app_scope_authorized", false)

		username := c.GetString("username")
		role := c.GetString("role")
		if !isAppToken(username, role) {
			c.Next()
			return
		}

		app, err := loadOpenAppByUsername(username)
		if err != nil || !app.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"error": "应用不存在或已被禁用"})
			c.Abort()
			return
		}

		if !appScopeAllowed(app.Scopes, scope) {
			c.JSON(http.StatusForbidden, gin.H{"error": "应用无权访问此资源"})
			c.Abort()
			return
		}

		c.Set("app_scope_authorized", true)

		if app.RateLimit > 0 {
			since := time.Now().Add(-time.Hour)
			var count int64
			database.DB.Model(&model.ApiCallLog{}).Where("app_id = ? AND created_at >= ?", app.ID, since).Count(&count)
			if count >= int64(app.RateLimit) {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "应用调用频率超限"})
				c.Abort()
				return
			}
		}

		start := time.Now()
		c.Next()

		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path
		}

		database.DB.Create(&model.ApiCallLog{
			AppID:    app.ID,
			AppName:  app.Name,
			Endpoint: endpoint,
			Method:   c.Request.Method,
			Status:   c.Writer.Status(),
			Duration: float64(time.Since(start).Milliseconds()),
			IP:       ResolveClientIP(c),
		})
	}
}
