package database_test

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsAddsUserPreferenceListToLegacyDatabase 验证老库补 user_preferences.list 列后，
// 存量行一律落成空串 ——「一个键都没存过」，升级后任务页 / 环境变量页的每页条数与视图栏显隐逐字节不变
// （前端只在服务端没有某个键时才把本机老值迁上来，落成别的值就会反过来冲掉本机那份）。
//
// 同时锁住两件更隐蔽的事：
//   - editor 列在补列前后逐字节不变：补列不能顺手动到已有的编辑器偏好；
//   - 列是 NOT NULL、默认值为空串：回退到不认这一列的老版本时，老代码插进来的新行靠 DEFAULT 落空串，
//     再升级回来时 GORM 把它扫进 string 不会因为 NULL 报错。
//
// 契约见 .trellis/spec/backend/database-guidelines.md：新增列必须有一条迁移测试锁住默认值。
func TestEnsureColumnsAddsUserPreferenceListToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	const legacyEditor = `{"word_wrap":"off","minimap":true,"indent_guides":true,"whitespace":"all","indent_width":"4"}`

	// list 刻意建成非空：DropColumn 会把这一列连同取值一起丢掉，补列后读到空串才能证明
	// 这个空串来自列定义本身的默认值，而不是「这行原本就是空串」。
	legacy := &model.UserPreference{
		UserID: 1,
		Editor: legacyEditor,
		List:   `{"tasks_page_size":50}`,
	}
	if err := database.DB.Create(legacy).Error; err != nil {
		t.Fatalf("create user preference before legacy migration: %v", err)
	}
	if err := database.DB.Migrator().DropColumn(&model.UserPreference{}, "List"); err != nil {
		t.Fatalf("drop list to simulate legacy database: %v", err)
	}
	if database.DB.Migrator().HasColumn(&model.UserPreference{}, "List") {
		t.Fatal("expected simulated legacy database to have no list column")
	}

	database.EnsureColumns()
	if !database.DB.Migrator().HasColumn(&model.UserPreference{}, "List") {
		t.Fatal("expected EnsureColumns to add user_preferences.list")
	}

	// 刻意用 Raw SELECT 读原始存储值而不是走 GORM 模型：handler 对空串、脏值都有回落，
	// 走模型读出来「看着没问题」证明不了库里存的到底是什么。
	// Scan 进 string 本身也是一道断言：列里是 NULL 时这一步就会报错。
	type rawColumns struct {
		Editor string
		List   string
	}
	var stored rawColumns
	if err := database.DB.Raw("SELECT editor, list FROM user_preferences WHERE id = ?", legacy.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("read migrated user preference: %v", err)
	}
	if stored.List != "" {
		t.Fatalf("存量行补列后 list 必须落空串（等于从没存过），got %q", stored.List)
	}
	if stored.Editor != legacyEditor {
		t.Fatalf("补 list 列不能动 editor 列\nwant %q\ngot  %q", legacyEditor, stored.Editor)
	}

	// 回退场景：不认 list 列的老代码插入新行时不会带这一列，只能靠 DEFAULT '' 兜住。
	now := time.Now()
	if err := database.DB.Exec(
		"INSERT INTO user_preferences (user_id, editor, created_at, updated_at) VALUES (?, ?, ?, ?)",
		2, "", now, now,
	).Error; err != nil {
		t.Fatalf("insert row without list column like a legacy binary: %v", err)
	}
	var insertedList string
	if err := database.DB.Raw("SELECT list FROM user_preferences WHERE user_id = ?", 2).Scan(&insertedList).Error; err != nil {
		t.Fatalf("read list of a row inserted without the column: %v", err)
	}
	if insertedList != "" {
		t.Fatalf("不带 list 列插入的行必须落 DEFAULT ''，got %q", insertedList)
	}

	// 幂等：EnsureColumns 每次启动都会跑，第二次不该把用户存过的列表偏好改回去。
	const savedList = `{"envs_page_size":"all"}`
	if err := database.DB.Model(&model.UserPreference{}).Where("id = ?", legacy.ID).
		Update("list", savedList).Error; err != nil {
		t.Fatalf("update list before idempotency check: %v", err)
	}
	database.EnsureColumns()
	if err := database.DB.Raw("SELECT editor, list FROM user_preferences WHERE id = ?", legacy.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("read user preference after second EnsureColumns: %v", err)
	}
	if stored.List != savedList {
		t.Fatalf("第二次 EnsureColumns 不应改动已有取值，want %q got %q", savedList, stored.List)
	}
	if stored.Editor != legacyEditor {
		t.Fatalf("第二次 EnsureColumns 不应改动 editor 列，got %q", stored.Editor)
	}
}
