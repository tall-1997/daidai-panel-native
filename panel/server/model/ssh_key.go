package model

import (
	"strings"
	"time"
)

type SSHKey struct {
	ID                       uint      `gorm:"primarykey" json:"id"`
	Name                     string    `gorm:"size:128;not null" json:"name"`
	PrivateKey               string    `gorm:"type:text;default:''" json:"-"`
	PrivateKeySecretProvider string    `gorm:"size:64;default:''" json:"-"`
	PrivateKeySecretCipher   []byte    `gorm:"type:blob" json:"-"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func (SSHKey) TableName() string {
	return "ssh_keys"
}

func (k *SSHKey) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":              k.ID,
		"name":            k.Name,
		"has_private_key": k.HasPrivateKey(),
		"created_at":      k.CreatedAt,
		"updated_at":      k.UpdatedAt,
	}
}

func (k *SSHKey) ToDictWithKey() map[string]interface{} {
	result := k.ToDict()
	if k.HasPrivateKey() {
		result["private_key"] = "******"
	}
	return result
}

func (k *SSHKey) HasPrivateKey() bool {
	if k == nil {
		return false
	}
	return strings.TrimSpace(k.PrivateKey) != "" || k.HasPrivateKeySecret()
}

func (k *SSHKey) HasPrivateKeySecret() bool {
	if k == nil {
		return false
	}
	return strings.TrimSpace(k.PrivateKeySecretProvider) != "" && len(k.PrivateKeySecretCipher) > 0
}
