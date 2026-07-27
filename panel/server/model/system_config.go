package model

import (
	"errors"
	"strings"
	"time"

	"daidai-panel/database"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type SystemConfig struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Key         string    `gorm:"size:64;uniqueIndex;not null" json:"key"`
	Value       string    `gorm:"type:text;default:''" json:"value"`
	Description string    `gorm:"size:256;default:''" json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var deprecatedSystemConfigKeys = map[string]bool{
	"command_timeout": true,
}

func (SystemConfig) TableName() string {
	return "system_configs"
}

func silentDB() *gorm.DB {
	return database.DB.Session(&gorm.Session{Logger: database.DB.Logger.LogMode(logger.Silent)})
}

func GetConfig(key string, defaultValue string) string {
	if defaultValue == "" {
		if def, exists := GetSystemConfigDefinition(key); exists {
			defaultValue = def.DefaultValue
		}
	}

	var cfg SystemConfig
	if err := silentDB().Where("`key` = ?", key).First(&cfg).Error; err != nil {
		return defaultValue
	}
	if cfg.Value == "" {
		return defaultValue
	}
	return cfg.Value
}

func GetConfigInt(key string, defaultValue int) int {
	val := GetConfig(key, "")
	if val == "" {
		return defaultValue
	}
	var result int
	for _, c := range val {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			return defaultValue
		}
	}
	return result
}

func GetConfigBool(key string, defaultValue bool) bool {
	val := GetConfig(key, "")
	if val == "" {
		return defaultValue
	}

	parsed, ok := parseBoolString(val)
	if !ok {
		return defaultValue
	}
	return parsed
}

func SetConfig(key, value string) error {
	if deprecatedSystemConfigKeys[key] {
		return removeDeprecatedSystemConfigs(key)
	}

	normalized, err := NormalizeSystemConfigValue(key, value)
	if err != nil {
		return err
	}

	var cfg SystemConfig
	if err := silentDB().Where("`key` = ?", key).First(&cfg).Error; err != nil {
		cfg = SystemConfig{Key: key, Value: normalized}
		if def, exists := GetSystemConfigDefinition(key); exists {
			cfg.Description = def.Description
		}
		return database.DB.Create(&cfg).Error
	}

	updates := map[string]interface{}{"value": normalized}
	if def, exists := GetSystemConfigDefinition(key); exists && cfg.Description != def.Description {
		updates["description"] = def.Description
	}

	return database.DB.Model(&cfg).Updates(updates).Error
}

func InitDefaultConfigs() {
	db := silentDB()
	for _, def := range SystemConfigDefinitions() {
		var existing SystemConfig
		if err := db.Where("`key` = ?", def.Key).First(&existing).Error; err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
			database.DB.Create(&SystemConfig{
				Key:         def.Key,
				Value:       def.DefaultValue,
				Description: def.Description,
			})
			continue
		}

		normalizedValue := existing.Value
		if normalizedValue == "" {
			normalizedValue = def.DefaultValue
		} else if normalized, err := NormalizeSystemConfigValue(def.Key, existing.Value); err == nil {
			normalizedValue = normalized
		} else {
			normalizedValue = def.DefaultValue
		}

		updates := map[string]interface{}{}
		if strings.TrimSpace(existing.Description) != def.Description {
			updates["description"] = def.Description
		}
		if def.Key == "max_log_content_size" && strings.TrimSpace(existing.Value) == "102400" {
			normalizedValue = def.DefaultValue
		}
		if normalizedValue != existing.Value {
			updates["value"] = normalizedValue
		}
		if len(updates) > 0 {
			database.DB.Model(&existing).Updates(updates)
		}
	}

	removeDeprecatedSystemConfigs("command_timeout")
}

func removeDeprecatedSystemConfigs(keys ...string) error {
	for _, key := range keys {
		if err := database.DB.Where("`key` = ?", key).Delete(&SystemConfig{}).Error; err != nil {
			return err
		}
	}
	return nil
}
