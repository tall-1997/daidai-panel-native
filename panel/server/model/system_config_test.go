package model_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestSetConfigNormalizesRegisteredValues(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := model.SetConfig("auto_install_deps", "0"); err != nil {
		t.Fatalf("set auto_install_deps: %v", err)
	}
	if got := model.GetRegisteredConfigBool("auto_install_deps"); got {
		t.Fatalf("expected auto_install_deps to be false after normalization")
	}

	var autoInstall model.SystemConfig
	if err := database.DB.Where("`key` = ?", "auto_install_deps").First(&autoInstall).Error; err != nil {
		t.Fatalf("query auto_install_deps: %v", err)
	}
	if autoInstall.Value != "false" {
		t.Fatalf("expected canonical bool value false, got %q", autoInstall.Value)
	}

	if err := model.SetConfig("captcha_fail_mode", " strict "); err != nil {
		t.Fatalf("set captcha_fail_mode: %v", err)
	}
	if got := model.GetRegisteredConfig("captcha_fail_mode"); got != "strict" {
		t.Fatalf("expected captcha_fail_mode strict, got %q", got)
	}

	if err := model.SetConfig("trusted_proxy_cidrs", "127.0.0.1, 203.0.113.10"); err != nil {
		t.Fatalf("set trusted_proxy_cidrs: %v", err)
	}
	if got := model.GetRegisteredConfig("trusted_proxy_cidrs"); got != "127.0.0.1/32\n203.0.113.10/32" {
		t.Fatalf("expected canonical trusted_proxy_cidrs, got %q", got)
	}

	if err := model.SetConfig("update_image_mirror", "https://docker.1ms.run/"); err != nil {
		t.Fatalf("set update_image_mirror: %v", err)
	}
	if got := model.GetRegisteredConfig("update_image_mirror"); got != "docker.1ms.run" {
		t.Fatalf("expected canonical update_image_mirror docker.1ms.run, got %q", got)
	}

	if err := model.SetConfig("binary_update_proxy", "gh-proxy.org"); err != nil {
		t.Fatalf("set binary_update_proxy: %v", err)
	}
	if got := model.GetRegisteredConfig("binary_update_proxy"); got != "https://gh-proxy.org/" {
		t.Fatalf("expected canonical binary_update_proxy https://gh-proxy.org/, got %q", got)
	}

	if err := model.SetConfig(model.PanelTimezoneConfigKey, " Asia/Tokyo "); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	if got := model.GetRegisteredConfig(model.PanelTimezoneConfigKey); got != "Asia/Tokyo" {
		t.Fatalf("expected timezone Asia/Tokyo, got %q", got)
	}

	if err := model.SetConfig("default_cron_rule", "invalid cron"); err == nil {
		t.Fatal("expected invalid default_cron_rule to be rejected")
	}
	if err := model.SetConfig("trusted_proxy_cidrs", "not-an-ip"); err == nil {
		t.Fatal("expected invalid trusted_proxy_cidrs to be rejected")
	}
	if err := model.SetConfig("update_image_mirror", "https://docker.1ms.run/proxy"); err == nil {
		t.Fatal("expected update_image_mirror with path to be rejected")
	}
	if err := model.SetConfig("binary_update_proxy", "https://gh-proxy.org/?url=x"); err == nil {
		t.Fatal("expected binary_update_proxy with query to be rejected")
	}
	if err := model.SetConfig(model.PanelTimezoneConfigKey, "Bad/Zone"); err == nil {
		t.Fatal("expected invalid timezone to be rejected")
	}
	if err := model.SetConfig(model.PanelTimezoneConfigKey, "Local"); err == nil {
		t.Fatal("expected Local timezone to be rejected")
	}
}

func TestRegisteredConfigUsesRegistryDefaults(t *testing.T) {
	testutil.SetupTestEnv(t)

	database.DB.Where("`key` = ?", "panel_title").Delete(&model.SystemConfig{})

	if got := model.GetRegisteredConfig("panel_title"); got != "呆呆面板" {
		t.Fatalf("expected registry default panel_title, got %q", got)
	}
	database.DB.Where("`key` = ?", model.PanelTimezoneConfigKey).Delete(&model.SystemConfig{})
	if got := model.GetRegisteredConfig(model.PanelTimezoneConfigKey); got != model.DefaultPanelTimezone {
		t.Fatalf("expected registry default timezone %q, got %q", model.DefaultPanelTimezone, got)
	}
	if got := model.GetRegisteredConfigBool("notify_on_login"); got {
		t.Fatalf("expected registry default notify_on_login to be false")
	}
}

func TestInitDefaultConfigsRemovesDeprecatedCommandTimeout(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.SystemConfig{
		Key:         "command_timeout",
		Value:       "86400",
		Description: "全局默认超时（秒）",
	}).Error; err != nil {
		t.Fatalf("create deprecated config: %v", err)
	}

	model.InitDefaultConfigs()

	var count int64
	if err := database.DB.Model(&model.SystemConfig{}).Where("`key` = ?", "command_timeout").Count(&count).Error; err != nil {
		t.Fatalf("count deprecated config: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deprecated command_timeout config to be removed, got count=%d", count)
	}
}

func TestSetConfigIgnoresDeprecatedCommandTimeout(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := model.SetConfig("command_timeout", "600"); err != nil {
		t.Fatalf("set deprecated command_timeout: %v", err)
	}

	var count int64
	if err := database.DB.Model(&model.SystemConfig{}).Where("`key` = ?", "command_timeout").Count(&count).Error; err != nil {
		t.Fatalf("count deprecated config: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deprecated command_timeout config to stay absent, got count=%d", count)
	}
}
