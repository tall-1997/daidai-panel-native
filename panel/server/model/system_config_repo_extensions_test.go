package model_test

import (
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func setRawSystemConfigValue(t *testing.T, key, value string) {
	t.Helper()
	if err := database.DB.Model(&model.SystemConfig{}).Where("`key` = ?", key).Update("value", value).Error; err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

func readSystemConfigValue(t *testing.T, key string) string {
	t.Helper()
	var cfg model.SystemConfig
	if err := database.DB.Where("`key` = ?", key).First(&cfg).Error; err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return cfg.Value
}

// 新装实例必须直接拿到含 mjs 的默认值。
func TestRepoFileExtensionsDefaultCoversMjs(t *testing.T) {
	testutil.SetupTestEnv(t)

	if !strings.Contains(model.DefaultRepoFileExtensions, "mjs") {
		t.Fatalf("default repo_file_extensions must contain mjs, got %q", model.DefaultRepoFileExtensions)
	}
	if got := model.GetRegisteredConfig("repo_file_extensions"); !strings.Contains(got, "mjs") {
		t.Fatalf("fresh install should get mjs in repo_file_extensions, got %q", got)
	}
}

// 存量实例：库里存的还是旧默认值（说明用户没改过）→ 升级时抬到新默认。
func TestInitDefaultConfigsUpgradesUntouchedRepoFileExtensions(t *testing.T) {
	testutil.SetupTestEnv(t)

	setRawSystemConfigValue(t, "repo_file_extensions", model.LegacyRepoFileExtensions)
	model.InitDefaultConfigs()

	if got := readSystemConfigValue(t, "repo_file_extensions"); got != model.DefaultRepoFileExtensions {
		t.Fatalf("legacy default should be upgraded to %q, got %q", model.DefaultRepoFileExtensions, got)
	}
}

// 用户自定义过的值一律不动 —— 包括「故意只留 py」这种把 js 都去掉的配置。
func TestInitDefaultConfigsKeepsCustomRepoFileExtensions(t *testing.T) {
	testutil.SetupTestEnv(t)

	setRawSystemConfigValue(t, "repo_file_extensions", "py")
	model.InitDefaultConfigs()

	if got := readSystemConfigValue(t, "repo_file_extensions"); got != "py" {
		t.Fatalf("custom repo_file_extensions must be preserved, got %q", got)
	}
}
