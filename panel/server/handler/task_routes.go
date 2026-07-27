package handler

import (
	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
)

func (h *TaskHandler) RegisterRoutes(r *gin.RouterGroup) {
	h.registerRoutes(r, true)
}

func (h *TaskHandler) RegisterManagementRoutes(r *gin.RouterGroup) {
	r.PUT("/tasks/batch", func(c *gin.Context) { c.Status(404) })
	tasks := r.Group("/tasks", middleware.JWTAuth(), middleware.OpenAPIAccess("tasks"))
	{
		tasks.GET("", middleware.RequireRole("viewer"), h.List)
		tasks.GET("/:id/stats", middleware.RequireRole("viewer"), h.Stats)
		tasks.GET("/export", middleware.RequireRole("viewer"), h.Export)
		tasks.POST("/cron/parse", middleware.RequireRole("viewer"), h.CronParse)
		tasks.GET("/cron/templates", middleware.RequireRole("viewer"), h.CronTemplates)

		tasks.POST("", middleware.RequireRole("operator"), h.Create)
		tasks.PUT("/:id", middleware.RequireRole("operator"), h.Update)
		tasks.DELETE("/:id", middleware.RequireRole("operator"), h.Delete)
		tasks.POST("/:id/copy", middleware.RequireRole("operator"), h.Copy)

		tasks.GET("/views", middleware.RequireRole("viewer"), h.ListViews)
		tasks.POST("/views", middleware.RequireRole("operator"), h.CreateView)
		tasks.PUT("/views/reorder", middleware.RequireRole("operator"), h.ReorderViews)
		tasks.PUT("/views/:viewId", middleware.RequireRole("operator"), h.UpdateView)
		tasks.DELETE("/views/:viewId", middleware.RequireRole("operator"), h.DeleteView)
	}
}

func (h *TaskHandler) registerRoutes(r *gin.RouterGroup, executionRoutes bool) {
	tasks := r.Group("/tasks", middleware.JWTAuth(), middleware.OpenAPIAccess("tasks"))
	{
		tasks.GET("", middleware.RequireRole("viewer"), h.List)
		tasks.GET("/notification-channels", middleware.RequireRole("viewer"), h.NotificationChannels)
		tasks.GET("/:id/latest-log", middleware.RequireRole("viewer"), h.LatestLog)
		tasks.GET("/:id/live-logs", middleware.RequireRole("viewer"), h.LiveLogs)
		tasks.GET("/:id/log-files", middleware.RequireRole("viewer"), h.LogFiles)
		tasks.GET("/:id/log-files/:filename", middleware.RequireRole("viewer"), h.LogFileContent)
		tasks.GET("/:id/log-files/:filename/download", middleware.RequireRole("viewer"), h.DownloadLogFile)
		tasks.GET("/:id/stats", middleware.RequireRole("viewer"), h.Stats)
		tasks.GET("/export", middleware.RequireRole("viewer"), h.Export)
		tasks.POST("/cron/parse", middleware.RequireRole("viewer"), h.CronParse)
		tasks.GET("/cron/templates", middleware.RequireRole("viewer"), h.CronTemplates)

		tasks.POST("", middleware.RequireRole("operator"), h.Create)
		tasks.PUT("/:id", middleware.RequireRole("operator"), h.Update)
		tasks.DELETE("/:id", middleware.RequireRole("operator"), h.Delete)
		if executionRoutes {
			tasks.PUT("/:id/run", middleware.RequireRole("operator"), h.Run)
			tasks.PUT("/:id/stop", middleware.RequireRole("operator"), h.Stop)
			tasks.POST("/batch/run", middleware.RequireRole("operator"), h.BatchRun)
		}
		tasks.PUT("/:id/enable", middleware.RequireRole("operator"), h.Enable)
		tasks.PUT("/:id/disable", middleware.RequireRole("operator"), h.Disable)
		tasks.PUT("/:id/pin", middleware.RequireRole("operator"), h.Pin)
		tasks.PUT("/:id/unpin", middleware.RequireRole("operator"), h.Unpin)
		tasks.POST("/:id/copy", middleware.RequireRole("operator"), h.Copy)
		tasks.DELETE("/:id/log-files/:filename", middleware.RequireRole("operator"), h.DeleteLogFile)
		tasks.PUT("/batch", middleware.RequireRole("operator"), h.Batch)
		tasks.PUT("/batch/enable", middleware.RequireRole("operator"), h.BatchEnable)
		tasks.PUT("/batch/disable", middleware.RequireRole("operator"), h.BatchDisable)
		tasks.DELETE("/batch/delete", middleware.RequireRole("operator"), h.BatchDelete)
		tasks.PUT("/batch/add-labels", middleware.RequireRole("operator"), h.BatchAddLabels)
		tasks.DELETE("/clean-logs", middleware.RequireRole("operator"), h.CleanLogs)
		tasks.POST("/import", middleware.RequireRole("operator"), h.Import)

		tasks.GET("/views", middleware.RequireRole("viewer"), h.ListViews)
		tasks.POST("/views", middleware.RequireRole("operator"), h.CreateView)
		tasks.PUT("/views/reorder", middleware.RequireRole("operator"), h.ReorderViews)
		tasks.PUT("/views/:viewId", middleware.RequireRole("operator"), h.UpdateView)
		tasks.DELETE("/views/:viewId", middleware.RequireRole("operator"), h.DeleteView)
	}
}
