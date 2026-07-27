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

// 真实场景回归：拿 QLScriptPublic 风格的 4 个脚本头部样本，铺到一个 saveDir 下，
// 直接调 syncSubscriptionTasks，断言任务**真的**被创建到数据库里。
// 这是 v2.2.10 之前缺的端到端测试——cron 解析测过、collectCandidates 测过，
// 但 sync 整条链路从来没在测试里跑通。用户反馈"自动建任务从来没成功过"，
// 这个测试如果失败就指明真正的失败点。
func TestSyncSubscriptionTasksEndToEndCreatesRealTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "smallfawn_QLScriptPublic"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create scripts root: %v", err)
	}

	// 4 个真实 QLScriptPublic 仓库的脚本头部样本
	scripts := map[string]string{
		"qtx.js": `/**
 * 青碳行
 * cron 9 5 * * *  qtx.js
 */
const $ = new Env("青碳行");
`,
		"fenxiang.js": `/**
 * cron 31 11 * * *
 */
const $ = new Env("粉象生活App");
`,
		"jetta.js": `/*
0 6 * * * https://raw.githubusercontent.com/liuqi6968/-/main/jetta.js
*/
const $ = new Env("捷达 APP签到");
`,
		"jlld.js": `/**
 * cron: 1 11 * * *
 */
const $ = new Env("吉利雷达");
`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(scriptsRoot, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	sub := &model.Subscription{
		Name:        "QLScriptPublic",
		Type:        model.SubTypeGitRepo,
		URL:         "https://github.com/smallfawn/QLScriptPublic.git",
		SaveDir:     saveDir,
		AutoAddTask: true,
		Enabled:     true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// 必须有 schedulerV2，否则 AddJob 段会 panic
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	emit := func(line string) { logs = append(logs, line) }

	syncSubscriptionTasks(sub, emit)

	t.Logf("captured %d log lines:", len(logs))
	for _, l := range logs {
		t.Logf("  %s", l)
	}

	// 真正的断言：至少为这 4 个有 cron 头的脚本建了 4 个任务
	label := subscriptionTaskLabel(sub.ID)
	var tasks []model.Task
	if err := queryTasksByLabel(label).Find(&tasks).Error; err != nil {
		t.Fatalf("query tasks by label: %v", err)
	}

	if len(tasks) != 4 {
		t.Errorf("expected 4 tasks created, got %d", len(tasks))
		for _, task := range tasks {
			t.Logf("  task: name=%q cmd=%q cron=%q", task.Name, task.Command, task.CronExpression)
		}
	}

	// 每个脚本都应有对应任务
	wantCommands := map[string]string{
		"task " + filepath.Join(saveDir, "qtx.js"):      "9 5 * * *",
		"task " + filepath.Join(saveDir, "fenxiang.js"): "31 11 * * *",
		"task " + filepath.Join(saveDir, "jetta.js"):    "0 6 * * *",
		"task " + filepath.Join(saveDir, "jlld.js"):     "1 11 * * *",
	}
	gotByCommand := make(map[string]string, len(tasks))
	for _, task := range tasks {
		gotByCommand[strings.TrimSpace(task.Command)] = task.CronExpression
	}
	for command, wantCron := range wantCommands {
		gotCron, ok := gotByCommand[command]
		if !ok {
			t.Errorf("missing task for command %q (期望 cron %q)", command, wantCron)
			continue
		}
		if gotCron != wantCron {
			t.Errorf("task %q cron mismatch: want %q got %q", command, wantCron, gotCron)
		}
	}
}

// 用户实际环境最可能的破坏场景：repo_file_extensions 系统配置被改坏（空字符串）。
// v2.2.10 必须能保证仍能识别 .js / .py 等核心后缀。
func TestSyncSubscriptionTasksSurvivesEmptyRepoFileExtensions(t *testing.T) {
	testutil.SetupTestEnv(t)
	_ = model.SetConfig("repo_file_extensions", "")

	saveDir := "user_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)
	os.WriteFile(filepath.Join(scriptsRoot, "demo.js"),
		[]byte("/**\n * cron 5 8 * * * demo.js\n */\nconst $ = new Env('Demo');\n"), 0o644)

	sub := &model.Subscription{
		Name: "user_repo", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task even with empty repo_file_extensions config, got %d", len(tasks))
	}
}

// 用户可能没显式开 sub.AutoAddTask，但系统默认 auto_add_cron 应该 true。
// 这条路径在 isConfigEnabled 里——确保即使数据库没插入这条配置，也 fallback 到 true。
func TestSyncSubscriptionTasksUsesSystemDefaultWhenSubFlagOff(t *testing.T) {
	testutil.SetupTestEnv(t)
	// 注意：故意不调用 model.SetConfig，让 auto_add_cron 走默认 true 路径

	saveDir := "default_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)
	os.WriteFile(filepath.Join(scriptsRoot, "x.js"),
		[]byte("/**\n * cron 1 2 * * * x.js\n */\nconst $ = new Env('x');\n"), 0o644)

	sub := &model.Subscription{
		Name: "default", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: false, // 故意关掉
		Enabled:     true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 1 {
		t.Logf("logs: %v", logs)
		t.Fatalf("expected 1 task via system default auto_add_cron=true, got %d", len(tasks))
	}
}

// 用户的"git 拉了但任务列表空"场景：脚本头没写 cron 注释。
// v2.2.10 起必须用 default_cron_rule 兜底建任务（每天 0 点），
// 同时通知辅助脚本 sendNotify.js / notify.py 不应被误建。
func TestSyncSubscriptionTasksFallsBackToDefaultCronForBusinessScripts(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "no_cron_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)

	// 业务脚本：没 cron 头，必须用 default_cron_rule 兜底建任务
	os.WriteFile(filepath.Join(scriptsRoot, "biz.js"),
		[]byte("const $ = new Env('业务');\n// 这个脚本作者忘了写 cron 头\nconsole.log('biz');\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "another.py"),
		[]byte("# 业务脚本\nimport os\nprint('another')\n"), 0o644)

	// 通知辅助脚本：即使没 cron 头也**不**应被建任务
	os.WriteFile(filepath.Join(scriptsRoot, "sendNotify.js"),
		[]byte("// 通知 helper\nmodule.exports = { sendNotify: () => {} };\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "notify.py"),
		[]byte("# notify helper\ndef send(): pass\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "sendNofity.js"), // 真实拼写错误样本
		[]byte("// 拼错的通知 helper\n"), 0o644)

	sub := &model.Subscription{
		Name: "no_cron", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)

	// 期望：biz.js + another.py 用 default cron 建任务（2 个），辅助脚本不建
	if len(tasks) != 2 {
		t.Logf("logs: %v", logs)
		for _, task := range tasks {
			t.Logf("  task: name=%q cmd=%q cron=%q", task.Name, task.Command, task.CronExpression)
		}
		t.Fatalf("expected 2 tasks (biz.js + another.py, helpers skipped), got %d", len(tasks))
	}

	for _, task := range tasks {
		if !strings.Contains(task.Command, "biz.js") && !strings.Contains(task.Command, "another.py") {
			t.Errorf("unexpected task created: %s (cron: %s)", task.Command, task.CronExpression)
		}
		// 必须是 FallbackSubscriptionCron 兜底（每天 0 点）
		if task.CronExpression != FallbackSubscriptionCron {
			t.Errorf("expected fallback cron %q, got %q for task %s",
				FallbackSubscriptionCron, task.CronExpression, task.Command)
		}
	}
}

// 脚本头明确写了 cron 时，用脚本里的；不能被 default cron 覆盖。
func TestSyncSubscriptionTasksScriptCronTakesPriorityOverDefault(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "mixed"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	os.MkdirAll(scriptsRoot, 0o755)

	os.WriteFile(filepath.Join(scriptsRoot, "with_cron.js"),
		[]byte("/**\n * cron 7 8 * * * with_cron.js\n */\nconst $ = new Env('have cron');\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "no_cron.js"),
		[]byte("const $ = new Env('no cron');\n"), 0o644)

	sub := &model.Subscription{
		Name: "mixed", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		AutoAddTask: true, Enabled: true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	cronByCmd := map[string]string{}
	for _, task := range tasks {
		cronByCmd[task.Command] = task.CronExpression
	}
	if cron := cronByCmd["task "+filepath.Join(saveDir, "with_cron.js")]; cron != "7 8 * * *" {
		t.Errorf("script with explicit cron should keep its cron, got %q", cron)
	}
	if cron := cronByCmd["task "+filepath.Join(saveDir, "no_cron.js")]; cron != FallbackSubscriptionCron {
		t.Errorf("script without cron should use fallback %q, got %q", FallbackSubscriptionCron, cron)
	}
}

// 真实失败场景：sub.SaveDir 未设置，应该按 URL 派生 saveDir。
func TestSyncSubscriptionTasksDerivesSaveDirFromURL(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitDefaultSystemConfigsForTest := model.InitDefaultConfigs
	InitDefaultSystemConfigsForTest()

	// URL 派生：smallfawn/QLScriptPublic.git → QLScriptPublic
	derivedSaveDir := "QLScriptPublic"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, derivedSaveDir)
	os.MkdirAll(scriptsRoot, 0o755)
	os.WriteFile(filepath.Join(scriptsRoot, "x.py"),
		[]byte("'''\n3 4 * * * x.py\n'''\nprint('hi')\n"), 0o644)

	sub := &model.Subscription{
		Name: "QL", Type: model.SubTypeGitRepo,
		URL:         "https://github.com/smallfawn/QLScriptPublic.git",
		SaveDir:     "", // 故意空，让代码 fallback 到 URL 派生
		AutoAddTask: true, Enabled: true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 1 {
		t.Logf("logs: %v", logs)
		t.Fatalf("expected 1 task with derived saveDir from URL, got %d", len(tasks))
	}
	if !strings.Contains(tasks[0].Command, derivedSaveDir) {
		t.Errorf("task command should reference derived saveDir %q, got %q", derivedSaveDir, tasks[0].Command)
	}
}

