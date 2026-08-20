package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsNotifyChannelPushScopeToLegacyDatabase 验证老库补列后，
// 存量渠道全部落成 default —— 也就是「升级后行为与升级前完全一致」这条验收。
//
// 这一列若落成空串或 NULL，广播过滤条件写成 `= 'default'` 时会让存量渠道整批失联；
// 现在的过滤条件是 `COALESCE(push_scope,'') <> 'bound'`，两边都兜住了，但补列的默认值
// 仍然必须是 default，否则备份 / 序列化侧读出来的语义会开始漂移。
func TestEnsureColumnsAddsNotifyChannelPushScopeToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacyChannel := &model.NotifyChannel{
		Name:      "历史渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/webhook"}`,
		PushScope: model.NotifyPushScopeBound,
		Enabled:   true,
	}
	if err := database.DB.Create(legacyChannel).Error; err != nil {
		t.Fatalf("create notify channel before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.NotifyChannel{}, "PushScope"); err != nil {
		t.Fatalf("drop push_scope to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.NotifyChannel{}, "PushScope") {
		t.Fatal("expected simulated legacy database to have no push_scope column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.NotifyChannel{}, "PushScope") {
		t.Fatal("expected EnsureColumns to add push_scope")
	}

	var storedValue string
	if err := database.DB.Raw("SELECT push_scope FROM notify_channels WHERE id = ?", legacyChannel.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read migrated push_scope: %v", err)
	}
	if storedValue != model.NotifyPushScopeDefault {
		t.Fatalf("expected migrated legacy channel default %q, got %q", model.NotifyPushScopeDefault, storedValue)
	}
}
