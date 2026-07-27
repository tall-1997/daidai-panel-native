package service

import (
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 复现用户报告的 jdpro 仓库 "扫描 0 个候选文件" 失败：
// saveDir = "京东"（中文），仓库根目录有 65 个 .js + 6 个 .py。
// 期望：扫描出 71 个候选，全部被建任务（带 cron 头的用脚本 cron，没有的用兜底）。
func TestSyncSubscriptionTasksHandlesChineseSaveDirWithJdproLayout(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "京东"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create chinese save dir: %v", err)
	}

	// 模拟 jdpro 仓库 4 个真实脚本头部样本
	scripts := map[string]string{
		"jd_bean_change.js": `/**
 * cron 1 0 * * * jd_bean_change.js, tag:京东资产变动
 */
const $ = new Env('京东资产');
`,
		"jd_CheckCK.js": `/**
 * cron "6 6 6 6 *" jd_CheckCK.js, tag:京东CK检测
 */
const $ = new Env('京东CK检测');
`,
		// 没 cron 头的业务脚本——必须用兜底建任务
		"jd_no_cron.js": `const $ = new Env('某脚本');
`,
		// 通知辅助脚本——必须跳过
		"sendNotify.js": `module.exports = { sendNotify: () => {} };
`,
		// 中文文件名 + 没 cron 头
		"京东签到.js": `const $ = new Env('京东签到');
`,
	}
	for name, body := range scripts {
		path := filepath.Join(scriptsRoot, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	sub := &model.Subscription{
		Name:        "京东",
		Type:        model.SubTypeGitRepo,
		URL:         "https://github.com/6dylan6/jdpro.git",
		SaveDir:     saveDir,
		AutoAddTask: true,
		Enabled:     true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	for _, l := range logs {
		t.Logf("  %s", l)
	}

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)

	// 期望 4 个任务：jd_bean_change / jd_CheckCK（带 cron） + jd_no_cron / 京东签到（兜底）
	// sendNotify.js 应该被跳过
	if len(tasks) != 4 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("expected 4 tasks (helpers skipped), got %d", len(tasks))
	}

	hasHelperTask := false
	for _, task := range tasks {
		if filepath.Base(task.Command) == "sendNotify.js" {
			hasHelperTask = true
		}
	}
	if hasHelperTask {
		t.Errorf("sendNotify.js should NOT be created as task")
	}
}

// 用户填了白名单但完全没匹配到任何文件 → 自动忽略白名单 fallback。
// 之前会"git 拉成功但任务列表 0"——v2.2.10 hotfix 后必须 fallback 建任务。
func TestSyncSubscriptionTasksFallsBackWhenWhitelistFiltersEverythingOut(t *testing.T) {
	testutil.SetupTestEnv(t)
	saveDir := "filter_test"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)
	os.WriteFile(filepath.Join(scriptsRoot, "biz.js"),
		[]byte("/**\n * cron 5 8 * * * biz.js\n */\nconst $ = new Env('biz');\n"), 0o644)

	sub := &model.Subscription{
		Name: "filter", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
		// 用户填了一个完全不匹配的白名单 pattern
		Whitelist: "this_pattern_matches_no_file",
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("whitelist completely filters out → should fallback to ignore filter, expected 1 task, got %d", len(tasks))
	}
}

func TestSyncSubscriptionTasksBlacklistMatchesDirectoryPath(t *testing.T) {
	testutil.SetupTestEnv(t)
	saveDir := "path_filter"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	backupDir := filepath.Join(scriptsRoot, "Backup")
	os.MkdirAll(backupDir, 0o755)

	os.WriteFile(filepath.Join(scriptsRoot, "daily.js"),
		[]byte("//cron: 15 12 * * *\nconst $ = new Env('daily');\n"), 0o644)
	os.WriteFile(filepath.Join(backupDir, "backup_task.js"),
		[]byte("//cron: 20 12 * * *\nconst $ = new Env('backup');\n"), 0o644)

	sub := &model.Subscription{
		Name: "path_filter", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
		Blacklist: "Backup",
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 1 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("expected only the root script task after Backup blacklist, got %d", len(tasks))
	}
	if filepath.Base(tasks[0].Command) != "daily.js" {
		t.Fatalf("blacklisted Backup script should not be created, got command %q", tasks[0].Command)
	}
	if tasks[0].CronExpression != "15 12 * * *" {
		t.Fatalf("expected //cron expression to be preserved, got %q", tasks[0].CronExpression)
	}
}

func TestSyncSubscriptionTasksBlacklistCanFilterEverythingWithoutFallback(t *testing.T) {
	testutil.SetupTestEnv(t)
	saveDir := "blacklist_all"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	backupDir := filepath.Join(scriptsRoot, "Backup")
	os.MkdirAll(backupDir, 0o755)
	os.WriteFile(filepath.Join(backupDir, "backup_task.js"),
		[]byte("//cron: 20 12 * * *\nconst $ = new Env('backup');\n"), 0o644)

	sub := &model.Subscription{
		Name: "blacklist_all", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
		Blacklist: "Backup",
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 0 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("blacklist-only filter should not fallback and recreate excluded scripts, got %d tasks", len(tasks))
	}
}

// 用户在白名单填了 `*` 通配符（最常见的"我要全部"误用）→ 视为不过滤。
func TestSyncSubscriptionTasksTreatsWildcardWhitelistAsNoFilter(t *testing.T) {
	testutil.SetupTestEnv(t)
	saveDir := "wildcard"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)
	os.WriteFile(filepath.Join(scriptsRoot, "a.js"),
		[]byte("/**\n * cron 1 1 * * * a.js\n */\nconst $ = new Env('a');\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "b.js"),
		[]byte("/**\n * cron 2 2 * * * b.js\n */\nconst $ = new Env('b');\n"), 0o644)

	for _, whitelist := range []string{"*", "**", "*.*", "全部"} {
		t.Run("whitelist="+whitelist, func(t *testing.T) {
			// 每个子测试用独立 sub，避免状态污染
			sub := &model.Subscription{
				Name: "w_" + whitelist, Type: model.SubTypeGitRepo,
				URL: "https://github.com/u/r.git", SaveDir: saveDir,
				AutoAddTask: true, Enabled: true, Whitelist: whitelist,
			}
			database.DB.Create(sub)
			InitSchedulerV2()
			syncSubscriptionTasks(sub, func(string) {})
			var tasks []model.Task
			queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
			if len(tasks) != 2 {
				t.Errorf("wildcard %q should be treated as no filter, expected 2 tasks, got %d", whitelist, len(tasks))
			}
			ShutdownSchedulerV2()
		})
	}
}
