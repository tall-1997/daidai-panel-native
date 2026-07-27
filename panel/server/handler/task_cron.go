package handler

import (
	"time"

	panelcron "daidai-panel/pkg/cron"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *TaskHandler) CronParse(c *gin.Context) {
	var req struct {
		Expression string `json:"expression" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := panelcron.Parse(req.Expression)
	if !result.Valid {
		response.Success(c, gin.H{
			"is_valid": false,
			"error":    result.Error,
		})
		return
	}

	nextTimes := panelcron.NextRunTimes(req.Expression, 5)
	timeStrs := make([]string, len(nextTimes))
	for i, nextTime := range nextTimes {
		timeStrs[i] = nextTime.Format(time.RFC3339)
	}

	format := "标准格式 (5位)"
	if result.HasSecond {
		format = "扩展格式 (6位含秒)"
	}

	response.Success(c, gin.H{
		"is_valid":       true,
		"description":    result.Description,
		"next_run_times": timeStrs,
		"format":         format,
	})
}

func (h *TaskHandler) CronTemplates(c *gin.Context) {
	response.Success(c, panelcron.GetTemplates())
}
