package model

import (
	"strings"
	"time"
)

const (
	SubTypeSingleFile = "single-file"
	SubTypeGitRepo    = "git-repo"
	SubAuthTypeSSH    = "ssh"
	SubAuthTypeToken  = "token"
)

type Subscription struct {
	ID                      uint       `gorm:"primarykey" json:"id"`
	Name                    string     `gorm:"size:128;not null" json:"name"`
	Type                    string     `gorm:"size:32;not null;default:'git-repo'" json:"type"`
	URL                     string     `gorm:"size:512;not null" json:"url"`
	Branch                  string     `gorm:"size:128;default:''" json:"branch"`
	Schedule                string     `gorm:"size:64;default:''" json:"schedule"`
	Whitelist               string     `gorm:"size:512;default:''" json:"whitelist"`
	Blacklist               string     `gorm:"size:512;default:''" json:"blacklist"`
	DependOn                string     `gorm:"size:512;default:''" json:"depend_on"`
	PreScript               string     `gorm:"type:text;default:''" json:"pre_script"`
	HookScript              string     `gorm:"type:text;default:''" json:"hook_script"`
	AutoAddTask             bool       `gorm:"default:false" json:"auto_add_task"`
	AutoDelTask             bool       `gorm:"default:false" json:"auto_del_task"`
	Enabled                 bool       `gorm:"default:true" json:"enabled"`
	Status                  int        `gorm:"default:0" json:"status"`
	LastPullAt              *time.Time `json:"last_pull_at"`
	SaveDir                 string     `gorm:"size:512;default:''" json:"save_dir"`
	SSHKeyID                *uint      `json:"ssh_key_id"`
	AuthType                string     `gorm:"size:16;default:''" json:"auth_type"`
	AuthUsername            string     `gorm:"size:128;default:''" json:"auth_username"`
	AuthToken               string     `gorm:"type:text;default:''" json:"-"`
	AuthTokenSecretProvider string     `gorm:"size:64;default:''" json:"-"`
	AuthTokenSecretCipher   []byte     `gorm:"type:blob" json:"-"`
	SSHKeySecretProvider    string     `gorm:"size:64;default:''" json:"-"`
	SSHKeySecretCipher      []byte     `gorm:"type:blob" json:"-"`
	CACertPath              string     `gorm:"size:512;default:''" json:"ca_cert_path"`
	SubPath                 string     `gorm:"size:512;default:''" json:"sub_path"`
	Alias                   string     `gorm:"size:128;default:''" json:"alias"`
	ForceOverwrite          *bool      `gorm:"default:true" json:"force_overwrite"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}

func (s *Subscription) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":                 s.ID,
		"name":               s.Name,
		"type":               s.Type,
		"url":                s.URL,
		"branch":             s.Branch,
		"schedule":           s.Schedule,
		"whitelist":          s.Whitelist,
		"blacklist":          s.Blacklist,
		"depend_on":          s.DependOn,
		"pre_script":         s.PreScript,
		"hook_script":        s.HookScript,
		"auto_add_task":      s.AutoAddTask,
		"auto_del_task":      s.AutoDelTask,
		"enabled":            s.Enabled,
		"status":             s.Status,
		"last_pull_at":       s.LastPullAt,
		"sub_path":           s.SubPath,
		"save_dir":           s.SaveDir,
		"ssh_key_id":         s.SSHKeyID,
		"auth_type":          s.EffectiveAuthType(),
		"auth_username":      s.AuthUsername,
		"has_auth_token":     s.HasAuthToken(),
		"has_ssh_key_secret": s.HasSSHKeySecret(),
		"ca_cert_path":       s.CACertPath,
		"alias":              s.Alias,
		"force_overwrite":    s.ForceOverwrite == nil || *s.ForceOverwrite,
		"created_at":         s.CreatedAt,
		"updated_at":         s.UpdatedAt,
	}
}

func NormalizeSubscriptionAuthType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case SubAuthTypeSSH:
		return SubAuthTypeSSH
	case SubAuthTypeToken:
		return SubAuthTypeToken
	default:
		return ""
	}
}

func (s *Subscription) HasAuthToken() bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.AuthToken) != "" || s.HasAuthTokenSecret()
}

func (s *Subscription) HasAuthTokenSecret() bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.AuthTokenSecretProvider) != "" && len(s.AuthTokenSecretCipher) > 0
}

func (s *Subscription) HasSSHKeySecret() bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.SSHKeySecretProvider) != "" && len(s.SSHKeySecretCipher) > 0
}

func (s *Subscription) EffectiveAuthType() string {
	if s == nil {
		return ""
	}
	if normalized := NormalizeSubscriptionAuthType(s.AuthType); normalized != "" {
		return normalized
	}
	if s.HasAuthToken() || s.HasAuthTokenSecret() {
		return SubAuthTypeToken
	}
	if (s.SSHKeyID != nil && *s.SSHKeyID > 0) || s.HasSSHKeySecret() {
		return SubAuthTypeSSH
	}
	return ""
}

type SubLog struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	SubscriptionID uint      `gorm:"index;not null" json:"subscription_id"`
	OperationID    string    `gorm:"size:64;index;default:''" json:"operation_id"`
	Status         int       `gorm:"default:0" json:"status"`
	Content        string    `gorm:"type:text" json:"content"`
	LogCursor      int64     `gorm:"default:0" json:"log_cursor"`
	Duration       float64   `gorm:"default:0" json:"duration"`
	CreatedAt      time.Time `json:"created_at"`

	Subscription *Subscription `gorm:"foreignKey:SubscriptionID" json:"-"`
}

func (SubLog) TableName() string {
	return "sub_logs"
}

func (l *SubLog) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":              l.ID,
		"subscription_id": l.SubscriptionID,
		"operation_id":    l.OperationID,
		"status":          l.Status,
		"content":         l.Content,
		"log_cursor":      l.LogCursor,
		"duration":        l.Duration,
		"created_at":      l.CreatedAt,
	}
	if l.Subscription != nil {
		result["subscription_name"] = l.Subscription.Name
	}
	return result
}
