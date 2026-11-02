package model_test

import (
	"testing"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 新装实例的日志底色必须是空串 —— 空串在前端才会走「跟随明暗主题」那条分支，
// 一旦这里被改回具体颜色，浅色面板下的日志区又会变回固定黑底。
func TestLogBackgroundColorDefaultIsEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)

	if got := model.GetRegisteredConfig("log_background_color"); got != "" {
		t.Fatalf("fresh install should leave log_background_color empty, got %q", got)
	}
}

// 存量实例：库里存的还是 v1.8.0 ~ v2.2.3 的旧默认值（说明用户没改过）→ 升级时抬到新默认（空串）。
func TestInitDefaultConfigsUpgradesUntouchedLogBackgroundColor(t *testing.T) {
	testutil.SetupTestEnv(t)

	setRawSystemConfigValue(t, "log_background_color", model.LegacyLogBackgroundColor)
	model.InitDefaultConfigs()

	if got := readSystemConfigValue(t, "log_background_color"); got != "" {
		t.Fatalf("legacy log_background_color should be cleared, got %q", got)
	}
}

// 用户自己设过的颜色一律不动 —— 这是「保留用户设置」的契约，不是可选项。
func TestInitDefaultConfigsKeepsCustomLogBackgroundColor(t *testing.T) {
	testutil.SetupTestEnv(t)

	setRawSystemConfigValue(t, "log_background_color", "#ffffff")
	model.InitDefaultConfigs()

	if got := readSystemConfigValue(t, "log_background_color"); got != "#ffffff" {
		t.Fatalf("custom log_background_color must be preserved, got %q", got)
	}
}

// 迁移必须幂等：连跑两次 InitDefaultConfigs 结果一致（第二次时库值已是空串，不再命中迁移分支）。
func TestInitDefaultConfigsLogBackgroundColorMigrationIsIdempotent(t *testing.T) {
	testutil.SetupTestEnv(t)

	setRawSystemConfigValue(t, "log_background_color", model.LegacyLogBackgroundColor)
	model.InitDefaultConfigs()
	first := readSystemConfigValue(t, "log_background_color")
	model.InitDefaultConfigs()
	second := readSystemConfigValue(t, "log_background_color")

	if first != second {
		t.Fatalf("migration is not idempotent: first=%q second=%q", first, second)
	}
	if second != "" {
		t.Fatalf("log_background_color should stay empty after re-run, got %q", second)
	}
}
