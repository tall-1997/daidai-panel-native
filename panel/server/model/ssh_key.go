package model

import (
	"strings"
	"time"
)

type SSHKey struct {
	ID uint `gorm:"primarykey" json:"id"`
	// 密钥名在语义上唯一：订阅选密钥时只认名字，同名两条用户无从分辨选中的是哪一把。
	// 老库里的同名残留由 database.DeduplicateBeforeUniqueIndex() 在 AutoMigrate 之前改名让路。
	Name       string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	PrivateKey string    `gorm:"type:text;not null" json:"-"`
	// --- 本机版（Android）扩展：Keystore 密封的私钥 ---
	PrivateKeySecretProvider string    `gorm:"size:64;default:''" json:"-"`
	PrivateKeySecretCipher   []byte    `gorm:"type:blob" json:"-"`
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
		// 本机版（Android）扩展：前端按它决定是否显示「删除密钥」等操作。
		"has_private_key": k.HasPrivateKey(),
		"created_at": k.CreatedAt,
		"updated_at": k.UpdatedAt,
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
