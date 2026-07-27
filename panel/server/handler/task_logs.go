package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

func (h *TaskHandler) LatestLog(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var taskLog model.TaskLog
	if err := database.DB.Where("task_id = ?", taskID).Order("started_at DESC").First(&taskLog).Error; err != nil {
		response.NotFound(c, "暂无日志")
		return
	}

	result := taskLog.ToDict()
	if taskLog.Content != "" {
		decompressed, err := service.DecompressFromBase64(taskLog.Content)
		if err == nil {
			result["content"] = decompressed
		}
	} else if taskLog.LogPath != nil {
		content, err := service.ReadLogFile(*taskLog.LogPath, config.C.Data.LogDir)
		if err == nil {
			result["content"] = content
		}
	}

	response.Success(c, result)
}

func (h *TaskHandler) LiveLogs(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	database.DB.First(&task, taskID)

	done := task.Status != model.TaskStatusRunning

	var lines []string
	manager := service.GetTinyLogManager()
	tinyLog := manager.FindByTaskID(uint(taskID))
	if tinyLog != nil {
		data, _ := tinyLog.ReadLastLines(200)
		if len(data) > 0 {
			lines = strings.Split(string(data), "\n")
		}
	}

	if lines == nil {
		lines = []string{}
	}

	response.Success(c, gin.H{
		"logs":   lines,
		"done":   done,
		"status": task.Status,
	})
}

func (h *TaskHandler) LogFiles(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	files := service.ListLogFiles(uint(taskID), config.C.Data.LogDir)
	response.Success(c, files)
}

func (h *TaskHandler) LogFileContent(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	content, err := service.ReadLogFile(logPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}

	response.Success(c, gin.H{"filename": filename, "content": content})
}

func (h *TaskHandler) DeleteLogFile(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	if err := service.DeleteLogFile(logPath, config.C.Data.LogDir); err != nil {
		response.InternalError(c, "删除日志文件失败")
		return
	}
	response.Success(c, gin.H{"message": "日志文件已删除"})
}

func (h *TaskHandler) DownloadLogFile(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	content, err := service.ReadLogFile(logPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}

func (h *TaskHandler) CleanLogs(c *gin.Context) {
	defaultDays := model.GetRegisteredConfigInt("log_retention_days")
	daysStr := c.DefaultQuery("days", strconv.Itoa(defaultDays))
	days, _ := strconv.Atoi(daysStr)
	if days < 1 {
		days = defaultDays
	}

	count := service.CleanOldLogs(config.C.Data.LogDir, days)
	response.Success(c, gin.H{"message": fmt.Sprintf("已清理 %d 个日志文件（保留最近 %d 天）", count, days)})
}
