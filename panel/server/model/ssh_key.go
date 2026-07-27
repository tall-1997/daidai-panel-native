package model

import (
	"time"
)

type SSHKey struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	PrivateKey string    `gorm:"type:text;not null" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (SSHKey) TableName() string {
	return "ssh_keys"
}

func (k *SSHKey) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         k.ID,
		"name":       k.Name,
		"created_at": k.CreatedAt,
		"updated_at": k.UpdatedAt,
	}
}

func (k *SSHKey) ToDictWithKey() map[string]interface{} {
	result := k.ToDict()
	result["private_key"] = k.PrivateKey
	return result
}
