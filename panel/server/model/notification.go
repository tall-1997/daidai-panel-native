package model

import (
	"time"
)

type NotifyChannel struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	Name           string     `gorm:"size:128;not null" json:"name"`
	Type           string     `gorm:"size:32;not null" json:"type"`
	Config         string     `gorm:"type:text;default:'{}'" json:"-"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	TodaySendCount int        `gorm:"default:0" json:"-"`
	TodaySendDate  string     `gorm:"size:10;default:''" json:"-"`
	LastTestAt     *time.Time `json:"last_test_at"`
	LastTestStatus string     `gorm:"size:16;default:''" json:"last_test_status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (NotifyChannel) TableName() string {
	return "notify_channels"
}

func (n *NotifyChannel) ToDict() map[string]interface{} {
	todaySendCount := 0
	if n.TodaySendDate == time.Now().Format("2006-01-02") {
		todaySendCount = n.TodaySendCount
	}

	return map[string]interface{}{
		"id":               n.ID,
		"name":             n.Name,
		"type":             n.Type,
		"config":           n.Config,
		"enabled":          n.Enabled,
		"today_send_count": todaySendCount,
		"last_test_at":     n.LastTestAt,
		"last_test_status": n.LastTestStatus,
		"created_at":       n.CreatedAt,
		"updated_at":       n.UpdatedAt,
	}
}
