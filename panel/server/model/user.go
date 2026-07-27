package model

import (
	"time"
)

type User struct {
	ID          uint       `gorm:"primarykey" json:"id"`
	Username    string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Password    string     `gorm:"size:256;not null" json:"-"`
	Role        string     `gorm:"size:16;default:admin" json:"role"`
	Enabled     bool       `gorm:"default:true" json:"enabled"`
	AvatarURL   string     `gorm:"size:512;default:''" json:"avatar_url"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (User) TableName() string {
	return "users"
}

func (u *User) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":         u.ID,
		"username":   u.Username,
		"role":       u.Role,
		"enabled":    u.Enabled,
		"avatar_url": u.AvatarURL,
		"created_at": u.CreatedAt,
		"updated_at": u.UpdatedAt,
	}
	if u.LastLoginAt != nil {
		result["last_login_at"] = u.LastLoginAt
	} else {
		result["last_login_at"] = nil
	}
	return result
}

type TokenBlocklist struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	JTI       string    `gorm:"size:36;uniqueIndex" json:"jti"`
	TokenType string    `gorm:"size:16" json:"token_type"`
	UserID    *uint     `json:"user_id"`
	RevokedAt time.Time `json:"revoked_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (TokenBlocklist) TableName() string {
	return "token_blocklist"
}
