package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"daidai-panel/model"
)

// 本机版（Android）通知配置加密与脱敏：
//   - SealNotificationConfig 把渠道 config 整体密封进 SecretStore，库里只留密文；
//   - RedactNotificationConfig 对外响应把密钥字段替换成 ********；
//   - loadNotificationConfig 发送时按需解封。
// 上游 v3.4.0 没有这层，合并后在此补齐；渠道发送统一走 loadNotificationConfig。

type sealedNotificationConfig struct {
	Provider string `json:"provider"`
	Cipher   string `json:"cipher"`
}

// SealNotificationConfig 把通知渠道配置加密成可直接落库的 JSON 字符串。
func SealNotificationConfig(ctx context.Context, name, rawConfig string) (string, error) {
	if strings.TrimSpace(rawConfig) == "" {
		rawConfig = "{}"
	}
	if !json.Valid([]byte(rawConfig)) {
		return "", fmt.Errorf("通知配置不是有效 JSON")
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(ctx, "notification:"+name, []byte(rawConfig))
	if err != nil {
		return "", err
	}
	payload := sealedNotificationConfig{
		Provider: sealed.Provider,
		Cipher:   base64.StdEncoding.EncodeToString(sealed.Cipher),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// RedactNotificationConfig 返回对外可见的渠道配置：密封配置只报 sealed 标记，
// 明文配置把密钥类字段替换成 ********（与 APP 回写占位符的保护配对）。
func RedactNotificationConfig(rawConfig string) string {
	if isSealedNotificationConfig(rawConfig) {
		return `{"sealed":true}`
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(rawConfig), &cfg); err != nil {
		return `{"redacted":true}`
	}
	for _, key := range []string{"token", "secret", "password", "smtp_pass", "app_token", "authorization", "api_key", "key", "bearer_token"} {
		if _, ok := cfg[key]; ok {
			cfg[key] = "********"
		}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return `{"redacted":true}`
	}
	return string(encoded)
}

func isSealedNotificationConfig(raw string) bool {
	var payload sealedNotificationConfig
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false
	}
	return strings.TrimSpace(payload.Provider) != "" && strings.TrimSpace(payload.Cipher) != ""
}

func openSealedNotificationConfig(key, raw string) ([]byte, error) {
	var payload sealedNotificationConfig
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Cipher)
	if err != nil {
		return nil, err
	}
	opened, err := RuntimeSecretStoreInstance().Open(context.Background(), key, SealedValue{Provider: payload.Provider, Cipher: ciphertext})
	if err != nil {
		return nil, err
	}
	return opened, nil
}

// loadNotificationConfig 读取渠道配置：密封配置先解封，再按字符串键值解析。
func loadNotificationConfig(ch model.NotifyChannel) (map[string]string, error) {
	configText := strings.TrimSpace(ch.Config)
	if configText == "" {
		configText = "{}"
	}
	if isSealedNotificationConfig(configText) {
		opened, err := openSealedNotificationConfig("notification:"+ch.Name, configText)
		if err != nil {
			return nil, err
		}
		configText = string(opened)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(configText), &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}