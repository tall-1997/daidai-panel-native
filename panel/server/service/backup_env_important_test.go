package service

import (
	"encoding/json"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// APP #16：备份 / 恢复带上环境变量的「重要」标记。备份里 true 才写这个键（omitempty），
// 老备份、青龙备份没有这个键，恢复出来一律不是重要变量。
// 单开文件是为了不和 #158 组改的 backup_restore_regression_test.go 抢同一个文件。
func TestBackupEnvVarImportantRoundTrip(t *testing.T) {
	testutil.SetupTestEnv(t)

	for _, env := range []model.EnvVar{
		{Name: "CFG_IMPORTANT", Value: "1", Enabled: true, Position: 1000, Important: true},
		{Name: "ACCOUNT", Value: "2", Enabled: true, Position: 2000},
	} {
		if err := database.DB.Create(&env).Error; err != nil {
			t.Fatalf("create env: %v", err)
		}
	}

	manifest, err := buildBackupManifest(BackupSelection{EnvVars: true})
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	text := string(raw)
	if strings.Count(text, `"important":true`) != 1 || strings.Contains(text, `"important":false`) {
		t.Fatalf("备份里应当只有重要变量带 important:true，普通变量不写这个键，实际 %s", text)
	}

	// 模拟写进 tgz 再读回：走一遍 JSON 往返后恢复（恢复会先清空 env_vars 再按备份重建）。
	var decoded BackupManifest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if err := restoreBackupManifest(decoded, t.TempDir()); err != nil {
		t.Fatalf("restore manifest: %v", err)
	}
	restored := map[string]bool{}
	var envs []model.EnvVar
	if err := database.DB.Find(&envs).Error; err != nil {
		t.Fatalf("list envs: %v", err)
	}
	for _, env := range envs {
		restored[env.Name] = env.Important
	}
	if len(restored) != 2 || !restored["CFG_IMPORTANT"] || restored["ACCOUNT"] {
		t.Fatalf("恢复后重要标记应当原样回来，实际 %v", restored)
	}

	// 老备份（v3.3.5 及以前）没有 important 键：恢复后一律不是重要变量。
	legacy := BackupManifest{
		Format: "daidai-panel-backup", Version: "0.4.0", Source: "daidai-panel",
		Selection: BackupSelection{EnvVars: true},
	}
	if err := json.Unmarshal([]byte(`{"env_vars":[{"name":"LEGACY_ENV","value":"x","position":1000}]}`), &legacy.Data); err != nil {
		t.Fatalf("decode legacy payload: %v", err)
	}
	if err := restoreBackupManifest(legacy, t.TempDir()); err != nil {
		t.Fatalf("restore legacy manifest: %v", err)
	}
	var legacyEnv model.EnvVar
	if err := database.DB.Where("name = ?", "LEGACY_ENV").First(&legacyEnv).Error; err != nil {
		t.Fatalf("load legacy env: %v", err)
	}
	if legacyEnv.Important {
		t.Fatalf("老备份恢复出来的变量不应是重要变量")
	}
}
