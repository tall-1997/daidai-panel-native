package handler

import (
	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
)

func (h *ScriptHandler) RegisterRoutes(r *gin.RouterGroup) {
	scripts := r.Group("/scripts", middleware.JWTAuth(), middleware.OpenAPIAccess("scripts"), middleware.RequireRole("operator"))
	{
		scripts.GET("", h.List)
		scripts.GET("/tree", h.Tree)
		scripts.GET("/content", h.GetContent)
		scripts.GET("/download", h.Download)
		scripts.PUT("/content", h.SaveContent)
		scripts.POST("/upload", h.Upload)
		scripts.DELETE("", h.Delete)
		scripts.POST("/directory", h.CreateDirectory)
		scripts.PUT("/rename", h.Rename)
		scripts.PUT("/move", h.Move)
		scripts.POST("/copy", h.Copy)
		scripts.DELETE("/batch", h.BatchDelete)
		scripts.GET("/versions", h.ListVersions)
		scripts.DELETE("/versions", h.ClearVersions)
		scripts.GET("/versions/:id", h.GetVersion)
		scripts.PUT("/versions/:id/rollback", h.Rollback)
		scripts.POST("/run", h.DebugRun)
		scripts.POST("/run-code", h.RunCode)
		scripts.GET("/run/:run_id/logs", h.DebugLogs)
		scripts.PUT("/run/:run_id/stop", h.DebugStop)
		scripts.DELETE("/run/:run_id", h.DebugClear)
		scripts.POST("/format", h.Format)
	}
}
