package model

import (
	"time"
)

type Platform struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Label     string    `gorm:"size:128" json:"label"`
	Icon      string    `gorm:"size:256" json:"icon"`
	CreatedAt time.Time `json:"created_at"`
}

func (Platform) TableName() string {
	return "platforms"
}

func (p *Platform) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         p.ID,
		"name":       p.Name,
		"label":      p.Label,
		"icon":       p.Icon,
		"created_at": p.CreatedAt,
	}
}

type PlatformToken struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	PlatformID uint       `gorm:"index;not null" json:"platform_id"`
	Name       string     `gorm:"size:128;not null" json:"name"`
	Token      string     `gorm:"type:text;not null" json:"-"`
	Remarks    string     `gorm:"size:256" json:"remarks"`
	Enabled    bool       `gorm:"default:true" json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	Platform *Platform `gorm:"foreignKey:PlatformID" json:"-"`
}

func (PlatformToken) TableName() string {
	return "platform_tokens"
}

func (t *PlatformToken) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":          t.ID,
		"platform_id": t.PlatformID,
		"name":        t.Name,
		"remarks":     t.Remarks,
		"enabled":     t.Enabled,
		"expires_at":  t.ExpiresAt,
		"created_at":  t.CreatedAt,
		"updated_at":  t.UpdatedAt,
	}
	if t.Platform != nil {
		result["platform_name"] = t.Platform.Label
	}
	return result
}

func (t *PlatformToken) ToDictWithToken() map[string]interface{} {
	result := t.ToDict()
	result["token"] = t.Token
	return result
}

type PlatformTokenLog struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	TokenID   uint      `gorm:"index" json:"token_id"`
	Action    string    `gorm:"size:64" json:"action"`
	Detail    string    `gorm:"size:512" json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (PlatformTokenLog) TableName() string {
	return "platform_token_logs"
}
