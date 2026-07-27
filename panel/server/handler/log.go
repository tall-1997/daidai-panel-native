package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type LogHandler struct{}

func NewLogHandler() *LogHandler {
	return &LogHandler{}
}

func (h *LogHandler) List(c *gin.Context) {
	taskIDStr := c.Query("task_id")
	statusStr := c.Query("status")
	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := database.DB.Model(&model.TaskLog{}).
		Joins("LEFT JOIN tasks ON tasks.id = task_logs.task_id")

	if taskIDStr != "" {
		taskID, _ := strconv.ParseUint(taskIDStr, 10, 32)
		query = query.Where("task_logs.task_id = ?", taskID)
	}
	if statusStr != "" {
		status, err := strconv.Atoi(statusStr)
		if err == nil {
			query = query.Where("task_logs.status = ?", status)
		}
	}
	if keyword != "" {
		query = query.Where("tasks.name LIKE ?", "%"+keyword+"%")
	}

	var total int64
	query.Count(&total)

	var logs []model.TaskLog
	query.Select("task_logs.*").
		Preload("Task").
		Order("task_logs.started_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	data := make([]map[string]interface{}, len(logs))
	for i, l := range logs {
		data[i] = l.ToDict()
	}

	response.Paginated(c, data, total, page, pageSize)
}

func (h *LogHandler) Stream(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, _ := strconv.ParseUint(taskIDStr, 10, 32)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	mgr := service.GetTinyLogManager()
	tl := mgr.FindByTaskID(uint(taskID))

	if tl != nil {
		history, _ := tl.ReadAll()
		if len(history) > 0 {
			writeSSEData(c.Writer, string(history))
			c.Writer.Flush()
		}

		sub := tl.Subscribe()
		defer tl.Unsubscribe(sub)

		ctx := c.Request.Context()
		for {
			select {
			case data, ok := <-sub:
				if !ok {
					fmt.Fprintf(c.Writer, "event: done\ndata: finished\n\n")
					c.Writer.Flush()
					return
				}
				writeSSEData(c.Writer, string(data))
				c.Writer.Flush()
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				// 静默 30s 不代表任务结束（慢接口/长计算/下载/sleep 都会无输出）。
				// 这里发一条 SSE keepalive 注释心跳并继续保持连接（不 return）：
				//   - 维持长连接，避免反代/网关空闲超时主动断开；
				//   - 不再断开重连重发整段历史，消除安静任务的周期性全量重渲染卡顿；
				//   - 任务真正结束时 sub 通道会关闭，自然走上面已有的 done:finished 分支。
				// 前端 sse.ts dispatchEventSegment 对 ":" 开头的行直接 continue，注释心跳是无副作用 no-op。
				fmt.Fprintf(c.Writer, ": keepalive\n\n")
				c.Writer.Flush()
			}
		}
	}

	var task model.Task
	database.DB.First(&task, taskID)
	if task.Status != model.TaskStatusRunning {
		time.Sleep(1500 * time.Millisecond)
		tl = mgr.FindByTaskID(uint(taskID))
		if tl != nil {
			history, _ := tl.ReadAll()
			if len(history) > 0 {
				writeSSEData(c.Writer, string(history))
				c.Writer.Flush()
			}
		}
		// 启动竞态：打开日志窗时任务可能刚入队、runTask 尚未置运行中/未建 TinyLog。
		// sleep 后重查真实状态，若已开始运行 → reconnect（前端重连后进入 tl != nil 流式分支），否则才判完成。
		var t model.Task
		database.DB.Select("status").First(&t, taskID)
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", streamDoneEventForStatus(t.Status))
		c.Writer.Flush()
	} else {
		idleCount := 0
		c.Stream(func(w io.Writer) bool {
			tl = mgr.FindByTaskID(uint(taskID))
			if tl != nil {
				history, _ := tl.ReadAll()
				if len(history) > 0 {
					writeSSEData(w, string(history))
					c.Writer.Flush()
				}
				fmt.Fprintf(w, "event: done\ndata: reconnect\n\n")
				c.Writer.Flush()
				return false
			}

			idleCount++
			if idleCount >= 120 {
				// 等满约 60s（120 * 500ms）TinyLog 始终未出现：该任务没有可流式的实时日志
				// （典型为 conc / SuppressLiveOutput 抑制输出的运行任务）。此处直接发 finished，
				// 不再用 streamDoneEventForStatus —— 否则 status==running 会回 reconnect，
				// 而前端重连后仍是 tl==nil，60s 后又超时，形成无可续流的无限重连风暴。
				fmt.Fprintf(w, "event: done\ndata: finished\n\n")
				c.Writer.Flush()
				return false
			}

			time.Sleep(500 * time.Millisecond)
			return true
		})
	}
}

func writeSSEData(w io.Writer, data string) {
	// SSE 分帧只需要保证每个 data: 行本身不跨物理换行。
	// 这里保留裸 \r，避免终端进度条的覆盖刷新语义在传输层被抹掉。
	data = strings.ReplaceAll(data, "\r\n", "\n")
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// streamDoneEventForStatus 根据任务真实状态决定 SSE done 事件的 data 值。
// 任务仍在运行 → "reconnect"，让前端 LogViewer 无缝重连续流，避免误判"已完成"；
// 其它状态（已结束/排队/禁用等）→ "finished"，前端正常置为"已完成"。
func streamDoneEventForStatus(status float64) string {
	if status == model.TaskStatusRunning {
		return "reconnect"
	}
	return "finished"
}

func (h *LogHandler) Detail(c *gin.Context) {
	logID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var taskLog model.TaskLog
	if err := database.DB.Preload("Task").First(&taskLog, logID).Error; err != nil {
		response.NotFound(c, "日志不存在")
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

func (h *LogHandler) Delete(c *gin.Context) {
	logID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的日志ID")
		return
	}
	result := database.DB.Where("id = ?", logID).Delete(&model.TaskLog{})
	if result.RowsAffected == 0 {
		response.NotFound(c, "日志不存在")
		return
	}
	response.Success(c, gin.H{"message": "日志已删除"})
}

func (h *LogHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Where("id IN ?", req.IDs).Delete(&model.TaskLog{})
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已删除 %d 条日志", result.RowsAffected),
	})
}

func (h *LogHandler) Clean(c *gin.Context) {
	defaultDays := model.GetRegisteredConfigInt("log_retention_days")
	daysStr := c.DefaultQuery("days", strconv.Itoa(defaultDays))
	days, _ := strconv.Atoi(daysStr)
	if days < 1 {
		days = defaultDays
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	result := database.DB.Where("started_at < ?", cutoff).Delete(&model.TaskLog{})
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已清理 %d 条日志（保留最近 %d 天）", result.RowsAffected, days),
	})
}

func (h *LogHandler) RegisterRoutes(r *gin.RouterGroup) {
	logs := r.Group("/logs")
	{
		logs.GET("", middleware.JWTAuth(), middleware.OpenAPIAccess("logs"), middleware.RequireRole("viewer"), h.List)
		logs.DELETE("/batch", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.BatchDelete)
		logs.POST("/batch-delete", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.BatchDelete)
		logs.DELETE("/clean", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.Clean)
		logs.GET("/:id/stream", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("viewer"), h.Stream)
		logs.GET("/:id", middleware.JWTAuth(), middleware.OpenAPIAccess("logs"), middleware.RequireRole("viewer"), h.Detail)
		logs.DELETE("/:id", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.Delete)
	}
}
