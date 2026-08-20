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

// 用户复现路径：建订阅 → 拉取生成任务 → 手改任务 cron → 再拉一次 → cron 被打回订阅源自带时间。
// 这些用例直接调 syncSubscriptionTasks 断言数据库，锁住「带锁任务不被覆盖 / 不被删除」这条不变量，
// 同时把「未加锁任务行为完全不变」也一起锁住——否则修复很容易顺手把正常的订阅跟随也关掉。

// setupLockTestSubscription 铺一个带 cron 头的脚本目录并建订阅，返回订阅与脚本命令。
func setupLockTestSubscription(t *testing.T, saveDir, scriptName, scriptCron string) (*model.Subscription, string) {
	t.Helper()

	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
		t.Fatalf("create scripts root: %v", err)
	}
	body := "/**\n * cron " + scriptCron + " " + scriptName + "\n */\nconst $ = new Env('lock');\n"
	if err := os.WriteFile(filepath.Join(scriptsRoot, scriptName), []byte(body), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	sub := &model.Subscription{
		Name:        saveDir,
		Type:        model.SubTypeGitRepo,
		URL:         "https://github.com/u/" + saveDir + ".git",
		SaveDir:     saveDir,
		AutoAddTask: true,
		AutoDelTask: true,
		Enabled:     true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return sub, "task " + filepath.Join(saveDir, scriptName)
}

func findTaskByCommand(t *testing.T, command string) *model.Task {
	t.Helper()
	var task model.Task
	if err := database.DB.Where("command = ?", command).First(&task).Error; err != nil {
		t.Fatalf("task %q not found: %v", command, err)
	}
	return &task
}

func containsLogLine(logs []string, keyword string) bool {
	for _, line := range logs {
		if strings.Contains(line, keyword) {
			return true
		}
	}
	return false
}

// 手改 cron + 加锁 → 再同步 → cron 保持用户值，且日志给出明确提示。
func TestSyncSubscriptionTasksKeepsManualCronWhenLocked(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	sub, command := setupLockTestSubscription(t, "locked_cron_repo", "biz.js", "9 5 * * *")
	syncSubscriptionTasks(sub, func(string) {})

	task := findTaskByCommand(t, command)
	if task.CronExpression != "9 5 * * *" {
		t.Fatalf("首次同步应使用订阅源 cron，got %q", task.CronExpression)
	}

	// 模拟用户在面板改 cron：handler 会同时把 subscription_locked 置真
	if err := database.DB.Model(task).Updates(map[string]interface{}{
		"cron_expression":     "30 7 * * *",
		"subscription_locked": true,
	}).Error; err != nil {
		t.Fatalf("模拟用户手改 cron: %v", err)
	}

	var logs []string
	syncSubscriptionTasks(sub, func(line string) { logs = append(logs, line) })

	after := findTaskByCommand(t, command)
	if after.CronExpression != "30 7 * * *" {
		t.Fatalf("带锁任务的 cron 被订阅同步覆盖了：want %q got %q", "30 7 * * *", after.CronExpression)
	}
	if !after.SubscriptionLocked {
		t.Fatalf("同步不应清掉 subscription_locked")
	}
	if !containsLogLine(logs, "保留手动定时") {
		t.Fatalf("同步日志缺少「保留手动定时」提示，logs=%v", logs)
	}
}

// 手改任务名 + 加锁 → 再同步 → 名称保持用户值。
func TestSyncSubscriptionTasksKeepsManualNameWhenLocked(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	sub, command := setupLockTestSubscription(t, "locked_name_repo", "biz.js", "9 5 * * *")
	syncSubscriptionTasks(sub, func(string) {})

	task := findTaskByCommand(t, command)
	if err := database.DB.Model(task).Updates(map[string]interface{}{
		"name":                "我自己改的名字",
		"subscription_locked": true,
	}).Error; err != nil {
		t.Fatalf("模拟用户手改任务名: %v", err)
	}

	syncSubscriptionTasks(sub, func(string) {})

	after := findTaskByCommand(t, command)
	if after.Name != "我自己改的名字" {
		t.Fatalf("带锁任务的名称被订阅同步覆盖了：got %q", after.Name)
	}
}

// 候选集里已经没有这条 command 的带锁任务：必须连任务带历史日志一起保留。
func TestSyncSubscriptionTasksKeepsLockedTaskMissingFromCandidates(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	sub, _ := setupLockTestSubscription(t, "locked_delete_repo", "biz.js", "9 5 * * *")
	label := subscriptionTaskLabel(sub.ID)

	// 订阅源里没有 gone.js，autoDelete 会命中它
	orphan := model.Task{
		Name:               "上游已删除的脚本",
		Command:            "task " + filepath.Join("locked_delete_repo", "gone.js"),
		CronExpression:     "0 3 * * *",
		TaskType:           model.TaskTypeCron,
		Status:             model.TaskStatusEnabled,
		SubscriptionLocked: true,
	}
	orphan.SetLabelsFromSlice([]string{label})
	if err := database.DB.Select("*").Create(&orphan).Error; err != nil {
		t.Fatalf("create orphan task: %v", err)
	}
	if err := database.DB.Create(&model.TaskLog{TaskID: orphan.ID, Content: "历史日志"}).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	var logs []string
	syncSubscriptionTasks(sub, func(line string) { logs = append(logs, line) })

	var count int64
	database.DB.Model(&model.Task{}).Where("id = ?", orphan.ID).Count(&count)
	if count != 1 {
		t.Fatalf("带锁任务被自动删除了")
	}
	var logCount int64
	database.DB.Model(&model.TaskLog{}).Where("task_id = ?", orphan.ID).Count(&logCount)
	if logCount != 1 {
		t.Fatalf("带锁任务的历史日志被删了，剩 %d 条", logCount)
	}
	if !containsLogLine(logs, "已加锁") {
		t.Fatalf("同步日志缺少保留提示，logs=%v", logs)
	}
}

// 回归护栏：未加锁任务的行为必须与改动前完全一致——
// 订阅源改了时间仍然跟随；不在候选集里仍然连日志一起被删。
func TestSyncSubscriptionTasksUnlockedBehaviourUnchanged(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	sub, command := setupLockTestSubscription(t, "unlocked_repo", "biz.js", "9 5 * * *")
	label := subscriptionTaskLabel(sub.ID)
	syncSubscriptionTasks(sub, func(string) {})

	// ① 用户改了 cron 但没加锁（存量数据场景）→ 订阅源的值仍然覆盖回来
	task := findTaskByCommand(t, command)
	if err := database.DB.Model(task).Update("cron_expression", "30 7 * * *").Error; err != nil {
		t.Fatalf("改 cron: %v", err)
	}

	// ② 不在候选集里的未加锁任务 → 连历史日志一起被删
	orphan := model.Task{
		Name:           "上游已删除的脚本",
		Command:        "task " + filepath.Join("unlocked_repo", "gone.js"),
		CronExpression: "0 3 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	orphan.SetLabelsFromSlice([]string{label})
	if err := database.DB.Select("*").Create(&orphan).Error; err != nil {
		t.Fatalf("create orphan task: %v", err)
	}
	if err := database.DB.Create(&model.TaskLog{TaskID: orphan.ID, Content: "历史日志"}).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	syncSubscriptionTasks(sub, func(string) {})

	after := findTaskByCommand(t, command)
	if after.CronExpression != "9 5 * * *" {
		t.Fatalf("未加锁任务应继续跟随订阅源 cron：want %q got %q", "9 5 * * *", after.CronExpression)
	}
	var count int64
	database.DB.Model(&model.Task{}).Where("id = ?", orphan.ID).Count(&count)
	if count != 0 {
		t.Fatalf("未加锁的失效任务应被自动删除")
	}
	var logCount int64
	database.DB.Model(&model.TaskLog{}).Where("task_id = ?", orphan.ID).Count(&logCount)
	if logCount != 0 {
		t.Fatalf("未加锁失效任务的日志应一并删除，剩 %d 条", logCount)
	}
}

// adopt 分支接管的是用户自建任务，接管时必须直接加锁，否则下一次同步立刻覆盖用户自己排的时间。
func TestSyncSubscriptionTasksAdoptLocksExistingTask(t *testing.T) {
	testutil.SetupTestEnv(t)
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	sub, command := setupLockTestSubscription(t, "adopt_repo", "biz.js", "9 5 * * *")

	// 用户先自己建了一条同 command 的任务（没有订阅标签）
	own := model.Task{
		Name:           "我自己建的",
		Command:        command,
		CronExpression: "45 21 * * *",
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Select("*").Create(&own).Error; err != nil {
		t.Fatalf("create own task: %v", err)
	}

	syncSubscriptionTasks(sub, func(string) {})

	adopted := findTaskByCommand(t, command)
	if !adopted.SubscriptionLocked {
		t.Fatalf("adopt 接管的用户自建任务应直接加锁")
	}

	// 再同步一次，用户自己排的名称与时间都不能被订阅源覆盖
	syncSubscriptionTasks(sub, func(string) {})
	after := findTaskByCommand(t, command)
	if after.CronExpression != "45 21 * * *" || after.Name != "我自己建的" {
		t.Fatalf("adopt 后的任务被覆盖了：name=%q cron=%q", after.Name, after.CronExpression)
	}
}
