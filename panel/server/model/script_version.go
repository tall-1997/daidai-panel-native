package model

import (
	"time"
)

type ScriptVersion struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	ScriptPath string    `gorm:"size:512;index;not null" json:"script_path"`
	Content    string    `gorm:"type:text;not null" json:"-"`
	Version    int       `gorm:"default:1;not null" json:"version"`
	Message    string    `gorm:"size:256;default:''" json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ScriptVersion) TableName() string {
	return "script_versions"
}

func (v *ScriptVersion) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":             v.ID,
		"script_path":    v.ScriptPath,
		"version":        v.Version,
		"message":        v.Message,
		"content_length": len(v.Content),
		"created_at":     v.CreatedAt,
	}
}

func (v *ScriptVersion) ToDictWithContent() map[string]interface{} {
	result := v.ToDict()
	result["content"] = v.Content
	return result
}
