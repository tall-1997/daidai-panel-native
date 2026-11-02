package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsSubscriptionFullCheckoutToLegacyDatabase 验证老库补 full_checkout 列后，
// 存量订阅一律落成 0 —— 也就是「升级后仍走 sparse-checkout，拉取行为与升级前完全一致」这条验收。
//
// 这条用例的全部价值都在默认值上：SQLType 若被写成 `DEFAULT 1`，编译、go vet、其余所有测试
// 都照样全绿，唯独所有老订阅的下一次拉取会静默变成整仓检出（大仓库能直接把磁盘拉满），
// 而用户在表单里看到的开关还是关着的。契约见 .trellis/spec/backend/database-guidelines.md：
// 新增列必须有一条迁移测试锁住默认值。
func TestEnsureColumnsAddsSubscriptionFullCheckoutToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 刻意建成 true：DropColumn 会把这一列连同取值一起丢掉，补列后读到 0 才能证明
	// 这个 0 来自列定义本身的默认值，而不是「这行原本就是 false」。
	legacySub := &model.Subscription{
		Name:         "历史订阅",
		Type:         model.SubTypeGitRepo,
		URL:          "https://github.com/example/legacy.git",
		Enabled:      true,
		FullCheckout: true,
	}
	if err := database.DB.Create(legacySub).Error; err != nil {
		t.Fatalf("create subscription before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.Subscription{}, "FullCheckout"); err != nil {
		t.Fatalf("drop full_checkout to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.Subscription{}, "FullCheckout") {
		t.Fatal("expected simulated legacy database to have no full_checkout column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.Subscription{}, "FullCheckout") {
		t.Fatal("expected EnsureColumns to add full_checkout")
	}

	// 刻意用 Raw SELECT 读原始存储值而不是走 GORM 模型：备份 / 导出 / 直接读库那几条链路
	// 拿到的就是这个值，模型侧的零值兜底在那里帮不上忙。
	var storedValue int
	if err := database.DB.Raw("SELECT full_checkout FROM subscriptions WHERE id = ?", legacySub.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read migrated full_checkout: %v", err)
	}
	if storedValue != 0 {
		t.Fatalf("存量订阅补列后必须落成 0（继续走 sparse-checkout），got %d", storedValue)
	}

	// 幂等：EnsureColumns 每次启动都会跑，第二次不该把用户已经打开的开关改回去。
	if err := database.DB.Model(&model.Subscription{}).Where("id = ?", legacySub.ID).
		Update("full_checkout", true).Error; err != nil {
		t.Fatalf("update full_checkout before idempotency check: %v", err)
	}
	database.EnsureColumns()
	if err := database.DB.Raw("SELECT full_checkout FROM subscriptions WHERE id = ?", legacySub.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read full_checkout after second EnsureColumns: %v", err)
	}
	if storedValue != 1 {
		t.Fatalf("第二次 EnsureColumns 不应改动已有取值，want 1 got %d", storedValue)
	}
}
