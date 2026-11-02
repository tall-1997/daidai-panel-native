package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsSubscriptionOverwriteModeToLegacyDatabase 验证老库补列后，
// 存量订阅全部落成 inherit —— 也就是「升级后拉取行为与升级前完全一致」这条验收。
//
// 这一列若落成空串或 NULL，NormalizeSubscriptionOverwriteMode 也会兜回 inherit，
// 但库里存的默认值仍然必须是 inherit：备份 / 导出 / 直接读库的那几条链路不走 Normalize。
// 另外这里刻意不复用 force_overwrite 改 nullable —— 存量行那一列全是 1，
// 复用会把所有老订阅解读成「强制覆盖」，等于替把全局关掉的用户偷偷切回覆盖模式。
func TestEnsureColumnsAddsSubscriptionOverwriteModeToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacySub := &model.Subscription{
		Name:          "历史订阅",
		Type:          model.SubTypeGitRepo,
		URL:           "https://github.com/example/legacy.git",
		Enabled:       true,
		OverwriteMode: model.SubOverwriteForce,
	}
	if err := database.DB.Create(legacySub).Error; err != nil {
		t.Fatalf("create subscription before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.Subscription{}, "OverwriteMode"); err != nil {
		t.Fatalf("drop overwrite_mode to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.Subscription{}, "OverwriteMode") {
		t.Fatal("expected simulated legacy database to have no overwrite_mode column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.Subscription{}, "OverwriteMode") {
		t.Fatal("expected EnsureColumns to add overwrite_mode")
	}

	var storedValue string
	if err := database.DB.Raw("SELECT overwrite_mode FROM subscriptions WHERE id = ?", legacySub.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read migrated overwrite_mode: %v", err)
	}
	if storedValue != model.SubOverwriteInherit {
		t.Fatalf("expected migrated legacy subscription default %q, got %q", model.SubOverwriteInherit, storedValue)
	}

	// 幂等：EnsureColumns 每次启动都会跑，第二次不该把已有取值改掉。
	if err := database.DB.Model(&model.Subscription{}).Where("id = ?", legacySub.ID).
		Update("overwrite_mode", model.SubOverwritePreserve).Error; err != nil {
		t.Fatalf("update overwrite_mode before idempotency check: %v", err)
	}
	database.EnsureColumns()
	if err := database.DB.Raw("SELECT overwrite_mode FROM subscriptions WHERE id = ?", legacySub.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read overwrite_mode after second EnsureColumns: %v", err)
	}
	if storedValue != model.SubOverwritePreserve {
		t.Fatalf("expected second EnsureColumns to keep %q, got %q", model.SubOverwritePreserve, storedValue)
	}
}
