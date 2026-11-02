package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestResolveSubscriptionTaskSyncModes 锁死「自动添加定时任务 / 自动删除失效任务」的三态优先级：
// 订阅显式选了 enabled / disabled 就以订阅为准，全局默认只在 inherit（含空串、脏值）时才生效。
//
// 这条替换的是升级前 `sub.AutoAddTask || isConfigEnabled("auto_add_cron", true)` 的 OR 语义。
// 注意 OR 语义下这些用例大多也是绿的（全局默认 true），唯独 disabled+全局 true 那一组会红 ——
// 那一组正是用户提的需求：「全局开着，但这一条订阅别建任务」。
func TestResolveSubscriptionTaskSyncModes(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		globalConfig string
		want         bool
	}{
		{"inherit 跟随全局开", model.SubTaskSyncInherit, "true", true},
		{"inherit 跟随全局关", model.SubTaskSyncInherit, "false", false},
		{"enabled 覆盖全局关", model.SubTaskSyncEnabled, "false", true},
		{"disabled 覆盖全局开", model.SubTaskSyncDisabled, "true", false},
		{"空串按 inherit 处理", "", "false", false},
		{"脏值按 inherit 处理", "not-a-mode", "true", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = testutil.SetupTestEnv(t)

			if err := model.SetConfig("auto_add_cron", tc.globalConfig); err != nil {
				t.Fatalf("set auto_add_cron: %v", err)
			}
			if err := model.SetConfig("auto_del_cron", tc.globalConfig); err != nil {
				t.Fatalf("set auto_del_cron: %v", err)
			}

			// 旧布尔列刻意反着设：验证它已经彻底不参与判定，只剩只读兼容。
			legacyFlag := !tc.want
			sub := &model.Subscription{
				Type:            model.SubTypeGitRepo,
				AutoAddTaskMode: tc.mode,
				AutoDelTaskMode: tc.mode,
				AutoAddTask:     legacyFlag,
				AutoDelTask:     legacyFlag,
			}

			if got := resolveSubscriptionAutoAddTask(sub); got != tc.want {
				t.Fatalf("auto_add_task_mode=%q 全局=%s 期望 %v，实际 %v", tc.mode, tc.globalConfig, tc.want, got)
			}
			if got := resolveSubscriptionAutoDelTask(sub); got != tc.want {
				t.Fatalf("auto_del_task_mode=%q 全局=%s 期望 %v，实际 %v", tc.mode, tc.globalConfig, tc.want, got)
			}

			// 同步选项是同一套判定的出口，一并锁住，防止哪天有人只改解析器忘了改这里。
			options := getSubscriptionTaskSyncOptions(sub)
			if options.autoAdd != tc.want || options.autoDelete != tc.want {
				t.Fatalf("getSubscriptionTaskSyncOptions 期望 add=%v del=%v，实际 add=%v del=%v",
					tc.want, tc.want, options.autoAdd, options.autoDelete)
			}

			// 解析器不能改写 sub —— 这个对象稍后还会被 database.DB.Model(sub).Updates(...) 用到。
			if sub.AutoAddTaskMode != tc.mode || sub.AutoDelTaskMode != tc.mode {
				t.Fatalf("expected resolve to leave task sync modes untouched, got add=%q del=%q",
					sub.AutoAddTaskMode, sub.AutoDelTaskMode)
			}
			if sub.AutoAddTask != legacyFlag || sub.AutoDelTask != legacyFlag {
				t.Fatalf("expected resolve to leave legacy flags untouched, got add=%v del=%v",
					sub.AutoAddTask, sub.AutoDelTask)
			}
		})
	}
}

// TestResolveSubscriptionTaskSyncModesWithNilSubscription 兜住 sub 为 nil 的分支：
// 回落全局默认，而不是 panic 或恒 false。
func TestResolveSubscriptionTaskSyncModesWithNilSubscription(t *testing.T) {
	_ = testutil.SetupTestEnv(t)

	if err := model.SetConfig("auto_add_cron", "false"); err != nil {
		t.Fatalf("set auto_add_cron: %v", err)
	}
	if err := model.SetConfig("auto_del_cron", "true"); err != nil {
		t.Fatalf("set auto_del_cron: %v", err)
	}

	if resolveSubscriptionAutoAddTask(nil) {
		t.Fatal("expected nil subscription to fall back to auto_add_cron=false")
	}
	if !resolveSubscriptionAutoDelTask(nil) {
		t.Fatal("expected nil subscription to fall back to auto_del_cron=true")
	}
}

// TestSyncSubscriptionTasksRespectsDisabledAutoAddMode 是这个需求本身的验收：
// 全局「自动添加定时任务」开着，但这条订阅选了强制关闭 —— 一个任务都不许建。
// 升级前的 OR 语义下这条必红（全局 true 就把订阅的关闭意图吃掉了）。
func TestSyncSubscriptionTasksRespectsDisabledAutoAddMode(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := model.SetConfig("auto_add_cron", "true"); err != nil {
		t.Fatalf("set auto_add_cron: %v", err)
	}

	saveDir := "task_sync_disabled_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create scripts root: %v", err)
	}
	script := "/**\n * cron 5 8 * * * demo.js\n */\nconst $ = new Env('Demo');\n"
	if err := os.WriteFile(filepath.Join(scriptsRoot, "demo.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}

	sub := &model.Subscription{
		Name:    "task-sync-disabled",
		Type:    model.SubTypeGitRepo,
		URL:     "https://github.com/example/task-sync-disabled.git",
		SaveDir: saveDir,
		Enabled: true,
		// 旧布尔列刻意开着：升级前这里是 OR，开着就一定会建任务。
		AutoAddTask:     true,
		AutoAddTaskMode: model.SubTaskSyncDisabled,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(line string) { logs = append(logs, line) })

	var tasks []model.Task
	if err := queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks).Error; err != nil {
		t.Fatalf("query tasks by label: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no task created when auto_add_task_mode=disabled, got %d", len(tasks))
	}

	// 策略来源必须写进日志，否则用户只看到「没建任务」而不知道是自己在订阅里关的。
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "强制关闭") {
		t.Fatalf("expected sync log to explain the forced-off strategy, got:\n%s", joined)
	}
}

// TestSyncSubscriptionTasksInheritModeFollowsDisabledGlobal 反向锁住 inherit：
// 订阅没单独设置时，全局关掉就真的不建任务（旧 OR 语义下 sub.AutoAddTask=true 会把它顶开）。
func TestSyncSubscriptionTasksInheritModeFollowsDisabledGlobal(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := model.SetConfig("auto_add_cron", "false"); err != nil {
		t.Fatalf("set auto_add_cron: %v", err)
	}
	if err := model.SetConfig("auto_del_cron", "false"); err != nil {
		t.Fatalf("set auto_del_cron: %v", err)
	}

	saveDir := "task_sync_inherit_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create scripts root: %v", err)
	}
	script := "/**\n * cron 5 8 * * * demo.js\n */\nconst $ = new Env('Demo');\n"
	if err := os.WriteFile(filepath.Join(scriptsRoot, "demo.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}

	sub := &model.Subscription{
		Name:            "task-sync-inherit",
		Type:            model.SubTypeGitRepo,
		URL:             "https://github.com/example/task-sync-inherit.git",
		SaveDir:         saveDir,
		Enabled:         true,
		AutoAddTaskMode: model.SubTaskSyncInherit,
		AutoDelTaskMode: model.SubTaskSyncInherit,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(line string) { logs = append(logs, line) })

	var tasks []model.Task
	if err := queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks).Error; err != nil {
		t.Fatalf("query tasks by label: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no task created when both global switches are off, got %d", len(tasks))
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "跳过自动同步任务") || !strings.Contains(joined, "跟随全局设置") {
		t.Fatalf("expected skip log to name the inherited strategy, got:\n%s", joined)
	}
}
