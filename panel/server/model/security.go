package model

import (
	"time"
)

type LoginLog struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	UserID     uint      `gorm:"index" json:"user_id"`
	Username   string    `gorm:"size:64" json:"username"`
	IP         string    `gorm:"size:64" json:"ip"`
	ClientName string    `gorm:"size:255" json:"client_name"`
	UserAgent  string    `gorm:"size:512" json:"user_agent"`
	Method     string    `gorm:"size:32;default:'密码登录'" json:"method"`
	Status     int       `gorm:"default:0" json:"status"`
	Message    string    `gorm:"size:256" json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (LoginLog) TableName() string {
	return "login_logs"
}

func (l *LoginLog) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":          l.ID,
		"user_id":     l.UserID,
		"username":    l.Username,
		"ip":          l.IP,
		"client_name": l.ClientName,
		"user_agent":  l.UserAgent,
		"method":      l.Method,
		"status":      l.Status,
		"message":     l.Message,
		"created_at":  l.CreatedAt,
	}
}

type LoginAttempt struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	IP        string     `gorm:"size:64;index" json:"ip"`
	Username  string     `gorm:"size:64" json:"username"`
	Count     int        `gorm:"default:1" json:"count"`
	LockedAt  *time.Time `json:"locked_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (LoginAttempt) TableName() string {
	return "login_attempts"
}

type UserSession struct {
	ID               uint       `gorm:"primarykey" json:"id"`
	UserID           uint       `gorm:"index" json:"user_id"`
	Username         string     `gorm:"size:64" json:"username"`
	JTI              string     `gorm:"size:36;uniqueIndex" json:"jti"`
	RefreshJTI       string     `gorm:"size:36" json:"-"`
	ClientType       string     `gorm:"size:16;index;default:'web'" json:"client_type"`
	ClientName       string     `gorm:"size:255" json:"client_name"`
	IP               string     `gorm:"size:64" json:"ip"`
	UserAgent        string     `gorm:"size:512" json:"user_agent"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RefreshExpiresAt *time.Time `json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}

func (s *UserSession) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":          s.ID,
		"user_id":     s.UserID,
		"username":    s.Username,
		"client_type": s.ClientType,
		"client_name": s.ClientName,
		"ip":          s.IP,
		"user_agent":  s.UserAgent,
		"expires_at":  s.ExpiresAt,
		"created_at":  s.CreatedAt,
	}
}

type IPWhitelist struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	IP        string    `gorm:"size:64;uniqueIndex;not null" json:"ip"`
	Remarks   string    `gorm:"size:256" json:"remarks"`
	CreatedAt time.Time `json:"created_at"`
}

func (IPWhitelist) TableName() string {
	return "ip_whitelists"
}

func (w *IPWhitelist) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         w.ID,
		"ip":         w.IP,
		"remarks":    w.Remarks,
		"created_at": w.CreatedAt,
	}
}

type SecurityAudit struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UserID    *uint     `gorm:"index" json:"user_id"`
	Username  string    `gorm:"size:64" json:"username"`
	Action    string    `gorm:"size:64" json:"action"`
	Detail    string    `gorm:"size:512" json:"detail"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

func (SecurityAudit) TableName() string {
	return "security_audits"
}

func (a *SecurityAudit) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         a.ID,
		"user_id":    a.UserID,
		"username":   a.Username,
		"action":     a.Action,
		"detail":     a.Detail,
		"ip":         a.IP,
		"created_at": a.CreatedAt,
	}
}

type TwoFactorAuth struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UserID    uint      `gorm:"uniqueIndex;not null" json:"user_id"`
	Secret    string    `gorm:"size:64;not null" json:"-"`
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TwoFactorAuth) TableName() string {
	return "two_factor_auths"
}
