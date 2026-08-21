package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type NotificationHandler struct{}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{}
}

func updateNotificationTestState(channelID uint, status string) {
	if channelID == 0 {
		return
	}

	if err := database.DB.Model(&model.NotifyChannel{}).
		Where("id = ?", channelID).
		Updates(map[string]interface{}{
			"last_test_at":     time.Now(),
			"last_test_status": status,
		}).Error; err != nil {
		log.Printf("update notification test state failed: %v", err)
	}
}

func notificationChannelResponse(ch model.NotifyChannel) map[string]interface{} {
	item := ch.ToDict()
	item["config"] = service.RedactNotificationConfig(ch.Config)
	return item
}

func (h *NotificationHandler) List(c *gin.Context) {
	var channels []model.NotifyChannel
	database.DB.Order("created_at DESC").Find(&channels)

	data := make([]map[string]interface{}, len(channels))
	for i, ch := range channels {
		data[i] = notificationChannelResponse(ch)
	}

	response.Success(c, gin.H{"data": data})
}

func (h *NotificationHandler) Create(c *gin.Context) {
	var req struct {
		Name      string `json:"name" binding:"required"`
		Type      string `json:"type" binding:"required"`
		Config    string `json:"config"`
		PushScope string `json:"push_scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}
	req.Type = normalizeNotificationChannelType(req.Type)
	if _, exists := model.GetNotifyChannelDefinition(req.Type); !exists {
		response.BadRequest(c, "不支持的通知渠道类型")
		return
	}

	normalizedConfig, err := model.NormalizeNotifyChannelConfig(req.Config)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	pushScope, ok := model.NormalizeNotifyPushScope(req.PushScope)
	if !ok {
		response.BadRequest(c, "推送范围只能是 default（默认推送）或 bound（绑定推送）")
		return
	}

	ch := model.NotifyChannel{
		Name:      req.Name,
		Type:      req.Type,
		Config:    normalizedConfig,
		PushScope: pushScope,
		Enabled:   true,
	}

	if err := database.DB.Create(&ch).Error; err != nil {
		response.InternalError(c, "创建通知渠道失败")
		return
	}

	response.Created(c, gin.H{"message": "创建成功", "data": notificationChannelResponse(ch)})
}

func (h *NotificationHandler) Update(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	allowed := map[string]bool{"name": true, "type": true, "config": true, "push_scope": true}
	updates := make(map[string]interface{})
	for k, v := range req {
		if !allowed[k] {
			continue
		}
		if k == "push_scope" {
			if v == nil {
				continue
			}
			raw, ok := v.(string)
			if !ok {
				response.BadRequest(c, "推送范围必须是字符串")
				return
			}
			scope, valid := model.NormalizeNotifyPushScope(raw)
			if !valid {
				response.BadRequest(c, "推送范围只能是 default（默认推送）或 bound（绑定推送）")
				return
			}
			updates[k] = scope
			continue
		}
		if k == "config" {
			rawConfig, ok := v.(string)
			if !ok {
				response.BadRequest(c, "通知渠道配置必须是 JSON 字符串")
				return
			}
			if strings.TrimSpace(rawConfig) == "********" {
				continue
			}
			normalizedConfig, err := model.NormalizeNotifyChannelConfig(rawConfig)
			if err != nil {
				response.BadRequest(c, err.Error())
				return
			}
			mergedConfig, err := preserveRedactedNotificationFields(ch.Config, normalizedConfig)
			if err != nil {
				response.BadRequest(c, err.Error())
				return
			}
			updates[k] = mergedConfig
			continue
		}
		if k == "type" {
			rawType, ok := v.(string)
			if !ok {
				response.BadRequest(c, "通知渠道类型必须是字符串")
				return
			}
			rawType = normalizeNotificationChannelType(rawType)
			if _, exists := model.GetNotifyChannelDefinition(rawType); !exists {
				response.BadRequest(c, "不支持的通知渠道类型")
				return
			}
			updates[k] = rawType
			continue
		}
		updates[k] = v
	}

	if len(updates) > 0 {
		database.DB.Model(&ch).Updates(updates)
	}

	database.DB.First(&ch, chID)
	response.Success(c, gin.H{"message": "更新成功", "data": notificationChannelResponse(ch)})
}

func normalizeNotificationChannelType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "pludplus" {
		return "pushplus"
	}
	return normalized
}

func preserveRedactedNotificationFields(existingConfig, incomingConfig string) (string, error) {
	var existing map[string]interface{}
	var incoming map[string]interface{}
	if err := json.Unmarshal([]byte(existingConfig), &existing); err != nil {
		return "", fmt.Errorf("现有通知渠道配置无效")
	}
	if err := json.Unmarshal([]byte(incomingConfig), &incoming); err != nil {
		return "", fmt.Errorf("通知渠道配置无效")
	}
	for key, value := range incoming {
		if text, ok := value.(string); ok && text == "********" {
			if previous, exists := existing[key]; exists {
				incoming[key] = previous
			} else {
				delete(incoming, key)
			}
		}
	}
	encoded, err := json.Marshal(incoming)
	if err != nil {
		return "", fmt.Errorf("通知渠道配置编码失败")
	}
	return string(encoded), nil
}

func (h *NotificationHandler) Delete(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	transaction := database.DB.Begin()
	if transaction.Error != nil {
		response.InternalError(c, "删除通知渠道失败")
		return
	}
	if err := transaction.Model(&model.Task{}).Where("notification_channel_id = ?", chID).Update("notification_channel_id", nil).Error; err != nil {
		transaction.Rollback()
		response.InternalError(c, "清理任务通知渠道绑定失败")
		return
	}
	if err := transaction.Where("id = ?", chID).Delete(&model.NotifyChannel{}).Error; err != nil {
		transaction.Rollback()
		response.InternalError(c, "删除通知渠道失败")
		return
	}
	if err := transaction.Commit().Error; err != nil {
		response.InternalError(c, "提交通知渠道删除失败")
		return
	}
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *NotificationHandler) Enable(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}
	database.DB.Model(&ch).Update("enabled", true)
	ch.Enabled = true
	response.Success(c, gin.H{"message": "已启用", "data": notificationChannelResponse(ch)})
}

func (h *NotificationHandler) Disable(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}
	database.DB.Model(&ch).Update("enabled", false)
	ch.Enabled = false
	response.Success(c, gin.H{"message": "已禁用", "data": notificationChannelResponse(ch)})
}

func (h *NotificationHandler) Test(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}

	err := service.SendNotificationToChannel(&ch, "呆呆面板测试通知", "这是一条测试通知消息，如果您收到此消息，说明通知渠道配置正确。")
	if err != nil {
		updateNotificationTestState(ch.ID, "failed")
		response.BadRequest(c, "发送失败: "+err.Error())
		return
	}

	updateNotificationTestState(ch.ID, "success")
	response.Success(c, gin.H{"message": "测试通知发送成功"})
}

func (h *NotificationHandler) Send(c *gin.Context) {
	var req struct {
		Title      string                 `json:"title" binding:"required"`
		Content    string                 `json:"content" binding:"required"`
		ChannelID  *uint                  `json:"channel_id"`
		ChannelIDs []uint                 `json:"channel_ids"`
		Context    map[string]interface{} `json:"context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	targeted := req.ChannelID != nil || len(req.ChannelIDs) > 0
	channelIDs := make([]uint, 0, len(req.ChannelIDs)+1)
	if req.ChannelID != nil && *req.ChannelID > 0 {
		channelIDs = append(channelIDs, *req.ChannelID)
	}
	for _, id := range req.ChannelIDs {
		if id > 0 {
			channelIDs = append(channelIDs, id)
		}
	}
	if targeted && len(channelIDs) == 0 {
		response.BadRequest(c, "通知渠道 ID 无效：channel_id / channel_ids 必须是大于 0 的渠道 ID")
		return
	}

	context := make(map[string]string, len(req.Context))
	for key, value := range req.Context {
		context[key] = fmt.Sprint(value)
	}

	result, err := service.SendNotificationSyncWithOptions(req.Title, req.Content, service.NotificationDispatchOptions{
		ChannelIDs: channelIDs,
		Context:    context,
	})
	if err != nil {
		response.BadRequest(c, "发送失败: "+err.Error())
		return
	}

	if result.SentCount == 0 && result.FailedCount > 0 {
		response.BadRequest(c, "发送失败: "+strings.Join(result.Errors, "; "))
		return
	}

	message := fmt.Sprintf("通知发送完成，成功 %d 个渠道", result.SentCount)
	if result.FailedCount > 0 {
		message = fmt.Sprintf("%s，失败 %d 个渠道", message, result.FailedCount)
	}

	response.Success(c, gin.H{
		"message": message,
		"data": gin.H{
			"sent_count":     result.SentCount,
			"failed_count":   result.FailedCount,
			"channel_names":  result.ChannelNames,
			"errors":         result.Errors,
			"requested_ids":  channelIDs,
			"used_all":       len(channelIDs) == 0,
			"content_length": len([]rune(req.Content)),
		},
	})
}

func (h *NotificationHandler) Types(c *gin.Context) {
	response.Success(c, gin.H{"data": model.NotifyChannelDefinitions()})
}

func (h *NotificationHandler) RegisterRoutes(r *gin.RouterGroup) {
	notifySend := r.Group("/notifications", middleware.JWTAuth(), middleware.OpenAPIAccess("notifications"))
	{
		notifySend.POST("/send", middleware.RequireRole("operator"), h.Send)
	}

	notify := r.Group("/notifications", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireAdmin())
	{
		notify.GET("", h.List)
		notify.POST("", h.Create)
		notify.PUT("/:id", h.Update)
		notify.DELETE("/:id", h.Delete)
		notify.PUT("/:id/enable", h.Enable)
		notify.PUT("/:id/disable", h.Disable)
		notify.POST("/:id/test", h.Test)
		notify.GET("/types", h.Types)
	}
}
