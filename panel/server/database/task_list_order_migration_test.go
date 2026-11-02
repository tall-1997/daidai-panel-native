package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsTaskListOrderToLegacyDatabase 验证老库补 list_order 列后，
// 存量任务一律落成 0 —— 也就是「升级后任务列表顺序逐字节不变」这条验收。
//
// 这条用例的全部价值都在默认值上：list_order 在默认排序里排在 sort_order 之前，
// 只要它不是 0，升级后所有老任务的列表顺序都会被这一列重排，
// 而编译、go vet、其余所有测试都照样全绿，用户只会看到「我的任务顺序怎么全乱了」。
// 契约见 .trellis/spec/backend/database-guidelines.md：新增列必须有一条迁移测试锁住默认值。
func TestEnsureColumnsAddsTaskListOrderToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 刻意建成非 0：DropColumn 会把这一列连同取值一起丢掉，补列后读到 0 才能证明
	// 这个 0 来自列定义本身的默认值，而不是「这行原本就是 0」。
	legacyTask := &model.Task{
		Name:           "历史任务",
		Command:        "task demo.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
		ListOrder:      70,
	}
	if err := database.DB.Create(legacyTask).Error; err != nil {
		t.Fatalf("create task before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.Task{}, "ListOrder"); err != nil {
		t.Fatalf("drop list_order to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.Task{}, "ListOrder") {
		t.Fatal("expected simulated legacy database to have no list_order column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.Task{}, "ListOrder") {
		t.Fatal("expected EnsureColumns to add list_order")
	}

	// 刻意用 Raw SELECT 读原始存储值而不是走 GORM 模型：列表排序是数据库直接按这一列排的，
	// 模型侧的零值兜底在那条链路上帮不上忙。
	var storedValue int
	if err := database.DB.Raw("SELECT list_order FROM tasks WHERE id = ?", legacyTask.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read migrated list_order: %v", err)
	}
	if storedValue != 0 {
		t.Fatalf("存量任务补列后必须落成 0（列表顺序与升级前一致），got %d", storedValue)
	}

	// 幂等：EnsureColumns 每次启动都会跑，第二次不该把用户拖出来的顺序改回去。
	if err := database.DB.Model(&model.Task{}).Where("id = ?", legacyTask.ID).
		Update("list_order", 30).Error; err != nil {
		t.Fatalf("update list_order before idempotency check: %v", err)
	}
	database.EnsureColumns()
	if err := database.DB.Raw("SELECT list_order FROM tasks WHERE id = ?", legacyTask.ID).Scan(&storedValue).Error; err != nil {
		t.Fatalf("read list_order after second EnsureColumns: %v", err)
	}
	if storedValue != 30 {
		t.Fatalf("第二次 EnsureColumns 不应改动已有取值，want 30 got %d", storedValue)
	}
}
