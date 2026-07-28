package model

import (
	"time"
)

type OpenApp struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	AppKey    string    `gorm:"size:64;uniqueIndex;not null" json:"app_key"`
	AppSecret string    `gorm:"size:128;not null" json:"-"`
	Scopes    string    `gorm:"size:512;default:''" json:"scopes"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	RateLimit int       `gorm:"default:0" json:"rate_limit"`
	CallCount int64     `gorm:"default:0" json:"call_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (OpenApp) TableName() string {
	return "open_apps"
}

func (a *OpenApp) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         a.ID,
		"name":       a.Name,
		"app_key":    a.AppKey,
		"scopes":     a.Scopes,
		"enabled":    a.Enabled,
		"rate_limit": a.RateLimit,
		"call_count": a.CallCount,
		"created_at": a.CreatedAt,
		"updated_at": a.UpdatedAt,
	}
}

func (a *OpenApp) ToDictWithSecret() map[string]interface{} {
	result := a.ToDict()
	result["app_secret"] = a.AppSecret
	return result
}

type ApiCallLog struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	AppID     uint      `gorm:"index" json:"app_id"`
	AppName   string    `gorm:"size:128" json:"app_name"`
	Endpoint  string    `gorm:"size:256" json:"endpoint"`
	Method    string    `gorm:"size:16" json:"method"`
	Status    int       `gorm:"default:200" json:"status"`
	Duration  float64   `gorm:"default:0" json:"duration"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

func (ApiCallLog) TableName() string {
	return "api_call_logs"
}

func (l *ApiCallLog) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         l.ID,
		"app_id":     l.AppID,
		"app_name":   l.AppName,
		"endpoint":   l.Endpoint,
		"method":     l.Method,
		"status":     l.Status,
		"duration":   l.Duration,
		"ip":         l.IP,
		"created_at": l.CreatedAt,
	}
}
