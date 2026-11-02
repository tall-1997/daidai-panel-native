package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsSubscriptionTaskSyncModeToLegacyDatabase 验证老库补列后，
// 存量订阅全部落成 inherit —— 也就是「升级后建任务/删任务的行为与升级前完全一致」这条验收。
//
// 这一列若落成空串或 NULL，NormalizeSubscriptionTaskSyncMode 也会兜回 inherit，
// 但库里存的默认值仍然必须是 inherit：备份 / 导出 / 直接读库的那几条链路不走 Normalize。
func TestEnsureColumnsAddsSubscriptionTaskSyncModeToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacySub := &model.Subscription{
		Name:            "历史订阅",
		Type:            model.SubTypeGitRepo,
		URL:             "https://github.com/example/legacy-task-sync.git",
		Enabled:         true,
		AutoAddTaskMode: model.SubTaskSyncEnabled,
		AutoDelTaskMode: model.SubTaskSyncDisabled,
	}
	if err := database.DB.Create(legacySub).Error; err != nil {
		t.Fatalf("create subscription before legacy migration: %v", err)
	}
	dropSubscriptionTaskSyncModeColumns(t)

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.Subscription{}, "AutoAddTaskMode") {
		t.Fatal("expected EnsureColumns to add auto_add_task_mode")
	}
	if !database.DB.Migrator().HasColumn(&model.Subscription{}, "AutoDelTaskMode") {
		t.Fatal("expected EnsureColumns to add auto_del_task_mode")
	}

	// 旧布尔列是 0（没走「识别 ql 命令」那条路径），所以不该被回填，必须停在 inherit。
	if got := readSubscriptionColumn(t, legacySub.ID, "auto_add_task_mode"); got != model.SubTaskSyncInherit {
		t.Fatalf("expected migrated legacy auto_add_task_mode %q, got %q", model.SubTaskSyncInherit, got)
	}
	if got := readSubscriptionColumn(t, legacySub.ID, "auto_del_task_mode"); got != model.SubTaskSyncInherit {
		t.Fatalf("expected migrated legacy auto_del_task_mode %q, got %q", model.SubTaskSyncInherit, got)
	}
}

// TestEnsureColumnsKeepsSubscriptionTaskSyncModeIdempotent 守住幂等：
// EnsureColumns 每次启动都会跑，第二次不该把用户已经选好的强制关闭改回去。
func TestEnsureColumnsKeepsSubscriptionTaskSyncModeIdempotent(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		Name:    "幂等订阅",
		Type:    model.SubTypeGitRepo,
		URL:     "https://github.com/example/idempotent-task-sync.git",
		Enabled: true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	dropSubscriptionTaskSyncModeColumns(t)
	database.EnsureColumns()

	if err := database.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"auto_add_task_mode": model.SubTaskSyncDisabled,
			"auto_del_task_mode": model.SubTaskSyncDisabled,
		}).Error; err != nil {
		t.Fatalf("update task sync mode before idempotency check: %v", err)
	}

	database.EnsureColumns()
	if got := readSubscriptionColumn(t, sub.ID, "auto_add_task_mode"); got != model.SubTaskSyncDisabled {
		t.Fatalf("expected second EnsureColumns to keep auto_add_task_mode %q, got %q", model.SubTaskSyncDisabled, got)
	}
	if got := readSubscriptionColumn(t, sub.ID, "auto_del_task_mode"); got != model.SubTaskSyncDisabled {
		t.Fatalf("expected second EnsureColumns to keep auto_del_task_mode %q, got %q", model.SubTaskSyncDisabled, got)
	}
}

// TestEnsureColumnsBackfillsLegacySubscriptionTaskSyncFlags 守住这次迁移最关键的一半：
// 旧语义是 OR（订阅列开 **或** 全局开就算开），所以 auto_add_task=1 的订阅今天是恒为「开」的。
// 光补列落 inherit 会让这批订阅改成「跟随全局」，把全局关掉的用户升级后突然就不建任务了。
//
// 同时锁死「必须成对」：源列要被清 0。少了清 0，用户之后把 mode 改回 inherit，
// 下次启动又会被源列的 1 重新提成 enabled —— 改不动还查不出原因。
func TestEnsureColumnsBackfillsLegacySubscriptionTaskSyncFlags(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		Name:        "识别 ql 命令建的订阅",
		Type:        model.SubTypeGitRepo,
		URL:         "https://github.com/example/legacy-flags.git",
		Enabled:     true,
		AutoAddTask: true,
		AutoDelTask: true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription with legacy flags: %v", err)
	}
	dropSubscriptionTaskSyncModeColumns(t)

	database.EnsureColumns()
	if got := readSubscriptionColumn(t, sub.ID, "auto_add_task_mode"); got != model.SubTaskSyncEnabled {
		t.Fatalf("expected legacy auto_add_task=1 backfilled to %q, got %q", model.SubTaskSyncEnabled, got)
	}
	if got := readSubscriptionColumn(t, sub.ID, "auto_del_task_mode"); got != model.SubTaskSyncEnabled {
		t.Fatalf("expected legacy auto_del_task=1 backfilled to %q, got %q", model.SubTaskSyncEnabled, got)
	}
	if got := readSubscriptionFlag(t, sub.ID, "auto_add_task"); got != 0 {
		t.Fatalf("expected legacy auto_add_task cleared to 0, got %d", got)
	}
	if got := readSubscriptionFlag(t, sub.ID, "auto_del_task"); got != 0 {
		t.Fatalf("expected legacy auto_del_task cleared to 0, got %d", got)
	}

	// 用户把三态改回 inherit（想让这条订阅重新跟随全局），下一次启动绝不能再被提回 enabled。
	if err := database.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"auto_add_task_mode": model.SubTaskSyncInherit,
			"auto_del_task_mode": model.SubTaskSyncInherit,
		}).Error; err != nil {
		t.Fatalf("reset task sync mode to inherit: %v", err)
	}
	database.EnsureColumns()
	if got := readSubscriptionColumn(t, sub.ID, "auto_add_task_mode"); got != model.SubTaskSyncInherit {
		t.Fatalf("expected auto_add_task_mode to stay %q after second EnsureColumns, got %q", model.SubTaskSyncInherit, got)
	}
	if got := readSubscriptionColumn(t, sub.ID, "auto_del_task_mode"); got != model.SubTaskSyncInherit {
		t.Fatalf("expected auto_del_task_mode to stay %q after second EnsureColumns, got %q", model.SubTaskSyncInherit, got)
	}
}

// TestEnsureColumnsPromotesLegacyFlagWhenModeIsInherit 把「legacy=1 且 mode='inherit'」
// 这个组合的处理方式钉死成设计内行为：回填**确实**会把它提成 enabled。
//
// 上面那条幂等用例只覆盖了「源列已被清 0 之后改回 inherit」，看不到这个窗口。
// 这个组合曾经是一条真实的丢设置路径：导入青龙备份写出 legacy=1，用户在网页里改成
// 「跟随全局设置」时旧前端又把 legacy=true 原样回传，于是下次重启被提回「强制开启」。
// 修法不是改回填（从 v3.2.5 直升上来的库里就有 legacy=1 的存量行，回填仍然必须跑），
// 而是把写入侧堵死：Create / 青龙导入 / 备份还原都把旧布尔翻译成 mode、源列恒写 false，
// Update 白名单里也删掉了这两个键。所以现在**已经没有写入路径能造出这个组合**，
// 回填看到的 legacy=1 只可能来自升级前的存量数据，天然只跑一次。
func TestEnsureColumnsPromotesLegacyFlagWhenModeIsInherit(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		Name:    "升级前的存量订阅",
		Type:    model.SubTypeGitRepo,
		URL:     "https://github.com/example/legacy-inherit.git",
		Enabled: true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	// 手工造出「源列还是 1、三态已经是 inherit」的组合——这正是 v3.2.5 直升上来的存量行形态。
	if err := database.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"auto_add_task":      true,
			"auto_del_task":      true,
			"auto_add_task_mode": model.SubTaskSyncInherit,
			"auto_del_task_mode": model.SubTaskSyncInherit,
		}).Error; err != nil {
		t.Fatalf("craft legacy flag + inherit mode combination: %v", err)
	}

	database.EnsureColumns()

	if got := readSubscriptionColumn(t, sub.ID, "auto_add_task_mode"); got != model.SubTaskSyncEnabled {
		t.Fatalf("expected legacy auto_add_task=1 + inherit promoted to %q, got %q", model.SubTaskSyncEnabled, got)
	}
	if got := readSubscriptionColumn(t, sub.ID, "auto_del_task_mode"); got != model.SubTaskSyncEnabled {
		t.Fatalf("expected legacy auto_del_task=1 + inherit promoted to %q, got %q", model.SubTaskSyncEnabled, got)
	}
	// 提升之后源列必须被清 0，否则用户再改回 inherit 又会被提一次。
	if got := readSubscriptionFlag(t, sub.ID, "auto_add_task"); got != 0 {
		t.Fatalf("expected legacy auto_add_task cleared to 0, got %d", got)
	}
	if got := readSubscriptionFlag(t, sub.ID, "auto_del_task"); got != 0 {
		t.Fatalf("expected legacy auto_del_task cleared to 0, got %d", got)
	}
}

// dropSubscriptionTaskSyncModeColumns 把两列删掉，模拟 v3.2.5 及更早的老库。
func dropSubscriptionTaskSyncModeColumns(t *testing.T) {
	t.Helper()
	for _, field := range []string{"AutoAddTaskMode", "AutoDelTaskMode"} {
		if err := database.DB.Migrator().DropColumn(&model.Subscription{}, field); err != nil {
			t.Fatalf("drop %s to simulate legacy database: %v", field, err)
		}
		if database.DB.Migrator().HasColumn(&model.Subscription{}, field) {
			t.Fatalf("expected simulated legacy database to have no %s column", field)
		}
	}
}

func readSubscriptionColumn(t *testing.T, id uint, column string) string {
	t.Helper()
	var value string
	if err := database.DB.Raw("SELECT "+column+" FROM subscriptions WHERE id = ?", id).Scan(&value).Error; err != nil {
		t.Fatalf("read subscriptions.%s: %v", column, err)
	}
	return value
}

func readSubscriptionFlag(t *testing.T, id uint, column string) int {
	t.Helper()
	var value int
	if err := database.DB.Raw("SELECT "+column+" FROM subscriptions WHERE id = ?", id).Scan(&value).Error; err != nil {
		t.Fatalf("read subscriptions.%s: %v", column, err)
	}
	return value
}
