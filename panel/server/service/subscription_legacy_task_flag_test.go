package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	_ "github.com/glebarez/sqlite"
)

// TestLoadQingLongSubscriptionsWritesOnlyTaskSyncMode 守住青龙导入侧的不变量：
// 只写三态 auto_add_task_mode / auto_del_task_mode，旧布尔列恒为 false。
//
// 旧写法两列一起写，于是导入进来的订阅带着 legacy=1；用户之后把它改成「跟随全局设置」，
// 下次重启就会被启动回填提回「强制开启」——设置静默消失。mode 已经带够信息，legacy 不必再写。
func TestLoadQingLongSubscriptionsWritesOnlyTaskSyncMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "database.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open qinglong fixture db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE Subscriptions (
		id INTEGER PRIMARY KEY,
		name TEXT,
		type TEXT,
		url TEXT,
		branch TEXT,
		schedule TEXT,
		whitelist TEXT,
		blacklist TEXT,
		dependences TEXT,
		"autoAddCron" INTEGER,
		"autoDelCron" INTEGER,
		is_disabled INTEGER,
		alias TEXT
	)`); err != nil {
		t.Fatalf("create qinglong Subscriptions table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO Subscriptions
		(id, name, type, url, branch, schedule, whitelist, blacklist, dependences, "autoAddCron", "autoDelCron", is_disabled, alias)
		VALUES
		(1, '要建任务的订阅', 'public-repo', 'https://github.com/example/ql-add.git', 'main', '0 0 * * *', '', '', '', 1, 0, 0, 'ql-add'),
		(2, '跟随全局的订阅', 'public-repo', 'https://github.com/example/ql-inherit.git', 'main', '0 0 * * *', '', '', '', 0, 0, 0, 'ql-inherit')`); err != nil {
		t.Fatalf("insert qinglong subscriptions: %v", err)
	}

	subs, err := loadQingLongSubscriptions(db)
	if err != nil {
		t.Fatalf("load qinglong subscriptions: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	// autoAddCron=1 → 强制开启；autoDelCron=0 → 跟随本面板全局默认（**不能**翻成 disabled）。
	if subs[0].AutoAddTaskMode != model.SubTaskSyncEnabled {
		t.Fatalf("expected auto_add_task_mode %q, got %q", model.SubTaskSyncEnabled, subs[0].AutoAddTaskMode)
	}
	if subs[0].AutoDelTaskMode != model.SubTaskSyncInherit {
		t.Fatalf("expected auto_del_task_mode %q, got %q", model.SubTaskSyncInherit, subs[0].AutoDelTaskMode)
	}
	if subs[1].AutoAddTaskMode != model.SubTaskSyncInherit || subs[1].AutoDelTaskMode != model.SubTaskSyncInherit {
		t.Fatalf("expected both modes %q for autoAddCron=0 row, got add=%q del=%q",
			model.SubTaskSyncInherit, subs[1].AutoAddTaskMode, subs[1].AutoDelTaskMode)
	}
	for _, sub := range subs {
		if sub.AutoAddTask || sub.AutoDelTask {
			t.Fatalf("expected legacy flags to stay false for %q, got auto_add_task=%v auto_del_task=%v",
				sub.Name, sub.AutoAddTask, sub.AutoDelTask)
		}
	}
}

// TestRestoreSubscriptionsTranslatesLegacyTaskSyncFlags 守住还原侧的同一条不变量：
//   - 老备份（v3.2.6 之前）只有 auto_add_task=1、没有三态 → 落库时翻译成 enabled，源列清成 false，
//     这样还原完当场就有正确语义，也不给启动回填留下新的 legacy=1 行；
//   - 新备份里的三态是准的 → 一个字都不能被 legacy 顶掉，尤其是显式的 inherit。
func TestRestoreSubscriptionsTranslatesLegacyTaskSyncFlags(t *testing.T) {
	testutil.SetupTestEnv(t)

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		Selection: BackupSelection{Subscriptions: true},
		Data: BackupPayload{
			Subscriptions: []BackupSubscription{
				{
					// 老备份形态：没有三态列，只有旧布尔。
					Subscription: model.Subscription{
						ID:          1,
						Name:        "老备份订阅",
						Type:        model.SubTypeGitRepo,
						URL:         "https://github.com/example/legacy-backup.git",
						Enabled:     true,
						AutoAddTask: true,
						AutoDelTask: true,
					},
				},
				{
					// 新备份形态：三态是权威，legacy 只是历史脏数据，不许覆盖三态。
					Subscription: model.Subscription{
						ID:              2,
						Name:            "新备份订阅",
						Type:            model.SubTypeGitRepo,
						URL:             "https://github.com/example/modern-backup.git",
						Enabled:         true,
						AutoAddTask:     true,
						AutoDelTask:     true,
						AutoAddTaskMode: model.SubTaskSyncDisabled,
						AutoDelTaskMode: model.SubTaskSyncInherit,
					},
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	cases := []struct {
		name    string
		wantAdd string
		wantDel string
	}{
		{"老备份订阅", model.SubTaskSyncEnabled, model.SubTaskSyncEnabled},
		{"新备份订阅", model.SubTaskSyncDisabled, model.SubTaskSyncInherit},
	}
	for _, tc := range cases {
		var restored model.Subscription
		if err := database.DB.Where("name = ?", tc.name).First(&restored).Error; err != nil {
			t.Fatalf("load restored subscription %q: %v", tc.name, err)
		}
		if restored.AutoAddTaskMode != tc.wantAdd {
			t.Fatalf("[%s] expected auto_add_task_mode %q, got %q", tc.name, tc.wantAdd, restored.AutoAddTaskMode)
		}
		if restored.AutoDelTaskMode != tc.wantDel {
			t.Fatalf("[%s] expected auto_del_task_mode %q, got %q", tc.name, tc.wantDel, restored.AutoDelTaskMode)
		}
		if restored.AutoAddTask || restored.AutoDelTask {
			t.Fatalf("[%s] expected legacy flags cleared to false, got auto_add_task=%v auto_del_task=%v",
				tc.name, restored.AutoAddTask, restored.AutoDelTask)
		}
	}
}
