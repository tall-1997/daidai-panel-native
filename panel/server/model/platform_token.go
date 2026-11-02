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
	ID uint `gorm:"primarykey" json:"id"`
	// 令牌唯一键是「平台 + 名称」的复合键，不是裸 name：
	// 令牌本来就按平台分组管理（List 支持 platform_id 过滤、删平台会连带删令牌），
	// 「京东」和「淘宝」下各有一条叫「主号」的令牌是完全正常的用法，卡裸 name 会误伤。
	// 老库里同一平台下的同名残留由 database.DeduplicateBeforeUniqueIndex() 在 AutoMigrate 之前改名让路。
	PlatformID uint   `gorm:"index;not null;uniqueIndex:idx_platform_tokens_platform_name,priority:1" json:"platform_id"`
	Name       string `gorm:"size:128;not null;uniqueIndex:idx_platform_tokens_platform_name,priority:2" json:"name"`
	Token      string `gorm:"type:text;not null" json:"-"`
	// --- 本机版（Android）扩展：Keystore 密封 ---
	TokenHash   string     `gorm:"size:128;default:''" json:"-"`
	Sealed      string     `gorm:"type:text;default:''" json:"-"`
	Service     string     `gorm:"size:64;default:''" json:"service"`
	ServiceUser string     `gorm:"size:128;default:''" json:"service_user"`
	Remarks     string     `gorm:"size:256" json:"remarks"`
	Enabled     bool       `gorm:"default:true" json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	Platform *Platform `gorm:"foreignKey:PlatformID" json:"-"`
}

func (PlatformToken) TableName() string {
	return "platform_tokens"
}

func (t *PlatformToken) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":           t.ID,
		"platform_id":  t.PlatformID,
		"name":         t.Name,
		"service":      t.Service,
		"service_user": t.ServiceUser,
		"remarks":      t.Remarks,
		"enabled":      t.Enabled,
		"sealed":       t.Sealed != "",
		"expires_at":   t.ExpiresAt,
		"created_at":   t.CreatedAt,
		"updated_at":   t.UpdatedAt,
	}
	if t.Platform != nil {
		result["platform_name"] = t.Platform.Label
	}
	return result
}

func (t *PlatformToken) ToDictWithToken() map[string]interface{} {
	result := t.ToDict()
	result["token"] = "********"
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
