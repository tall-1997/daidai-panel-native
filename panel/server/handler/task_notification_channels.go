package handler

import (
	"fmt"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type taskNotificationChannelInfo struct {
	ID      uint
	Name    string
	Type    string
	Enabled bool
}

func listTaskNotificationChannels() ([]taskNotificationChannelInfo, error) {
	var channels []model.NotifyChannel
	if err := database.DB.
		Order("enabled DESC, created_at DESC, id DESC").
		Find(&channels).Error; err != nil {
		return nil, err
	}

	items := make([]taskNotificationChannelInfo, 0, len(channels))
	for _, ch := range channels {
		items = append(items, taskNotificationChannelInfo{
			ID:      ch.ID,
			Name:    strings.TrimSpace(ch.Name),
			Type:    strings.TrimSpace(ch.Type),
			Enabled: ch.Enabled,
		})
	}
	return items, nil
}

func loadTaskNotificationChannelMap(tasks []model.Task) map[uint]taskNotificationChannelInfo {
	channelIDs := make(map[uint]struct{})
	for _, task := range tasks {
		if task.NotificationChannelID != nil && *task.NotificationChannelID > 0 {
			channelIDs[*task.NotificationChannelID] = struct{}{}
		}
	}

	if len(channelIDs) == 0 {
		return map[uint]taskNotificationChannelInfo{}
	}

	ids := make([]uint, 0, len(channelIDs))
	for id := range channelIDs {
		ids = append(ids, id)
	}

	var channels []model.NotifyChannel
	database.DB.Model(&model.NotifyChannel{}).Where("id IN ?", ids).Find(&channels)

	result := make(map[uint]taskNotificationChannelInfo, len(channels))
	for _, ch := range channels {
		result[ch.ID] = taskNotificationChannelInfo{
			ID:      ch.ID,
			Name:    strings.TrimSpace(ch.Name),
			Type:    strings.TrimSpace(ch.Type),
			Enabled: ch.Enabled,
		}
	}
	return result
}

func validateTaskNotificationChannelID(channelID *uint) error {
	if channelID == nil || *channelID == 0 {
		return nil
	}

	var count int64
	if err := database.DB.Model(&model.NotifyChannel{}).Where("id = ?", *channelID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("通知渠道不存在")
	}
	return nil
}

func normalizeTaskNotificationChannelIDValue(value interface{}) (*uint, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case float64:
		if v <= 0 {
			return nil, nil
		}
		id := uint(v)
		if err := validateTaskNotificationChannelID(&id); err != nil {
			return nil, err
		}
		return &id, nil
	case int:
		if v <= 0 {
			return nil, nil
		}
		id := uint(v)
		if err := validateTaskNotificationChannelID(&id); err != nil {
			return nil, err
		}
		return &id, nil
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return nil, nil
		}
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("通知渠道格式错误")
		}
		id := uint(parsed)
		if err := validateTaskNotificationChannelID(&id); err != nil {
			return nil, err
		}
		return &id, nil
	default:
		return nil, fmt.Errorf("通知渠道格式错误")
	}
}

func resolveImportedTaskNotificationChannel(taskData map[string]interface{}) (*uint, string, error) {
	if raw, exists := taskData["notification_channel_id"]; exists {
		channelID, err := normalizeTaskNotificationChannelIDValue(raw)
		if err == nil {
			return channelID, "", nil
		}
	}

	rawName, _ := taskData["notification_channel_name"].(string)
	name := strings.TrimSpace(rawName)
	if name == "" {
		return nil, "", nil
	}

	var channel model.NotifyChannel
	if err := database.DB.Where("name = ?", name).First(&channel).Error; err == nil {
		return &channel.ID, "", nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, "", err
	}

	return nil, fmt.Sprintf("通知渠道 %q 不存在，已按全部启用渠道导入", name), nil
}

func (h *TaskHandler) NotificationChannels(c *gin.Context) {
	channels, err := listTaskNotificationChannels()
	if err != nil {
		response.InternalError(c, "加载通知渠道失败")
		return
	}

	data := make([]map[string]interface{}, 0, len(channels))
	for _, ch := range channels {
		data = append(data, map[string]interface{}{
			"id":      ch.ID,
			"name":    ch.Name,
			"type":    ch.Type,
			"enabled": ch.Enabled,
		})
	}

	response.Success(c, gin.H{"data": data})
}
