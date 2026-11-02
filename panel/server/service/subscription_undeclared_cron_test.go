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

// #134：订阅里没声明 cron 的脚本。默认 Cron 规则留空时不建任务，已有任务一律不动，自动删除不能把它们判成失效。
// 「留空不建 / 填了按规则建」的主流程见 subscription_sync_integration_test.go 的 TestSyncSubscriptionTasksUndeclaredCronFollowsDefaultRule。

// ucWriteScript 按原样写一个脚本（name 用正斜杠）。
func ucWriteScript(t *testing.T, saveDir, name, body string) {
	t.Helper()
	full := filepath.Join(config.C.Data.ScriptsDir, saveDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ucHintLine 找「未建定时任务」那行提示。只认「未建定时任务」这半句，措辞再改也不会让「不该提示」的断言静默失效。
func ucHintLine(logs []string) string {
	for _, line := range logs {
		if strings.Contains(line, "未建定时任务") {
			return line
		}
	}
	return ""
}

// ucSetDirtyDefaultRule 绕过注册表校验，把一个 cron.Parse 不认的值直接写进库（直接改库、恢复旧备份就是这样）。
func ucSetDirtyDefaultRule(t *testing.T, value string) {
	t.Helper()
	result := database.DB.Model(&model.SystemConfig{}).Where("`key` = ?", "default_cron_rule").Update("value", value)
	if result.Error != nil {
		t.Fatalf("write dirty value: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		if err := database.DB.Create(&model.SystemConfig{Key: "default_cron_rule", Value: value}).Error; err != nil {
			t.Fatalf("create dirty value: %v", err)
		}
	}
}

// 默认规则只保存用户配置的合法值：留空（出厂默认）就是空，不再换成每天 0 点；
// 绕过注册表直接写进库里的脏值同样当作没配。
func TestGetSubscriptionTaskSyncOptionsHasNoMidnightFallback(t *testing.T) {
	testutil.SetupTestEnv(t)
	sub := &model.Subscription{Name: "opts", Type: model.SubTypeGitRepo, URL: "https://github.com/u/r.git"}

	if got := getSubscriptionTaskSyncOptions(sub).defaultCron; got != "" {
		t.Fatalf("empty default_cron_rule must stay empty, got %q", got)
	}

	if err := model.SetConfig("default_cron_rule", "  15 3 * * *  "); err != nil {
		t.Fatalf("set default_cron_rule: %v", err)
	}
	if got := getSubscriptionTaskSyncOptions(sub).defaultCron; got != "15 3 * * *" {
		t.Fatalf("configured rule must be used as is, got %q", got)
	}

	if err := database.DB.Model(&model.SystemConfig{}).Where("`key` = ?", "default_cron_rule").
		Update("value", "garbage value").Error; err != nil {
		t.Fatalf("write dirty value: %v", err)
	}
	if got := getSubscriptionTaskSyncOptions(sub); got.defaultCron != "" || got.ignoredDefaultCron != "garbage value" {
		t.Fatalf("an invalid stored rule must be treated as not configured and kept for the warning, got %q / %q",
			got.defaultCron, got.ignoredDefaultCron)
	}
}

// 提示里最多列 5 个文件名，其余用「等」带过；关掉了自动添加时不提示（本来就不建，提示「去填默认规则」没有意义）。
func TestSyncSubscriptionTasksUndeclaredCronHint(t *testing.T) {
	t.Run("lists_at_most_five", func(t *testing.T) {
		steEnv(t)
		saveDir := "uc_many"
		for _, name := range []string{"a1.js", "a2.js", "a3.js", "a4.js", "a5.js", "a6.js", "lib/a7.py"} {
			ucWriteScript(t, saveDir, name, "console.log('no cron')\n")
		}
		sub := steSubscription(t, saveDir, true, nil)

		logs := steSync(sub)
		want := "[提示] 7 个脚本没有识别到 cron 声明，未建定时任务（如 a1.js、a2.js、a3.js、a4.js、a5.js 等）。需要的话在订阅设置填写「默认 Cron 规则」，或手动为脚本建任务"
		if got := ucHintLine(logs); got != want {
			t.Fatalf("hint mismatch\nwant %q\n got %q\n%s", want, got, strings.Join(logs, "\n"))
		}
		if n := len(steManaged(sub)); n != 0 {
			t.Fatalf("no task may be built, got %s", steDump(steManaged(sub)))
		}
	})

	t.Run("silent_when_auto_add_off", func(t *testing.T) {
		steEnv(t)
		saveDir := "uc_add_off"
		ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")
		sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
		sub.AutoAddTaskMode = model.SubTaskSyncDisabled

		logs := steSync(sub)
		if got := ucHintLine(logs); got != "" {
			t.Fatalf("auto add is off, no hint expected, got %q", got)
		}
	})
}

// #134 最要紧的一条：开着自动删除时，升级前兜底建出来的任务（文件还在、只是没声明 cron）必须保留——
// 原样命令的、用户改过定时和参数的都算，历史日志不动，也不再为它们新建。
// 真正从订阅里消失的脚本（上游删了、被黑名单排除）照旧按开关连日志删。
func TestSyncSubscriptionTasksKeepsExistingTasksOfUndeclaredCronScripts(t *testing.T) {
	type fixture struct {
		sub                  *model.Subscription
		saveDir              string
		cfg, edited, removed *model.Task
	}
	setup := func(t *testing.T, saveDir string, withDeclared bool) fixture {
		t.Helper()
		steEnv(t)
		scripts := map[string]string{}
		if withDeclared {
			scripts["keep.js"] = "1 1 * * *"
		}
		sub := steSubscription(t, saveDir, true, scripts)
		ucWriteScript(t, saveDir, "config.py", "# 卡密配置\nCARD = 'x'\n")
		ucWriteScript(t, saveDir, "biz.js", "const $ = new Env('业务');\n")
		label := subscriptionTaskLabel(sub.ID)
		f := fixture{sub: sub, saveDir: saveDir}
		f.cfg = steNewTask(t, "config", steCmd(saveDir, "config.py"), "0 0 * * *", label)
		f.edited = steNewTask(t, "业务", steCmd(saveDir, "biz.js")+" desi JD_COOKIE 1-3", "30 7 * * *", label)
		// gone.js 已经从订阅里消失：盘上没有这个文件。
		f.removed = steNewTask(t, "gone", steCmd(saveDir, "gone.js"), "0 0 * * *", label)
		for _, task := range []*model.Task{f.cfg, f.edited, f.removed} {
			steAddLog(t, task.ID)
		}
		return f
	}
	assertKept := func(t *testing.T, logs []string, tasks ...*model.Task) {
		t.Helper()
		for _, task := range tasks {
			steAssertUnchanged(t, *task, logs)
			if n := steLogCount(task.ID); n != 1 {
				t.Fatalf("logs of %s changed, got %d\n%s", task.Name, n, strings.Join(logs, "\n"))
			}
		}
	}
	assertDeleted := func(t *testing.T, logs []string, task *model.Task) {
		t.Helper()
		if _, exists := steReload(task.ID); exists || steLogCount(task.ID) != 0 {
			t.Fatalf("%s should be deleted with its logs\n%s", task.Name, strings.Join(logs, "\n"))
		}
	}

	t.Run("with_declared_scripts", func(t *testing.T) {
		f := setup(t, "uc_keep", true)

		logs := steSync(f.sub)
		assertKept(t, logs, f.cfg, f.edited)
		assertDeleted(t, logs, f.removed)
		keep := findTaskByCommand(t, steCmd(f.saveDir, "keep.js"))
		if tasks := steManaged(f.sub); len(tasks) != 3 {
			t.Fatalf("want config / edited biz / keep.js, got %s\n%s", steDump(tasks), strings.Join(logs, "\n"))
		}
		if steCountLines(logs, "[自动添加任务]") != 1 || steCountLines(logs, "[自动删除任务]") != 1 {
			t.Fatalf("want exactly one add (keep.js) and one delete (gone.js)\n%s", strings.Join(logs, "\n"))
		}
		// 两个脚本都已经有任务了，不算「未建任务」，不提示。
		if got := ucHintLine(logs); got != "" {
			t.Fatalf("scripts that already have tasks must not be listed, got %q", got)
		}

		logs = steSync(f.sub)
		assertKept(t, logs, f.cfg, f.edited)
		steAssertUnchanged(t, *keep, logs)
		if !containsLogLine(logs, "[同步完成] 本次未对定时任务做任何变更") {
			t.Fatalf("second round should be a no-op\n%s", strings.Join(logs, "\n"))
		}

		// 之后用户填了默认规则：两个脚本进了候选，但已经有任务（按脚本认出），不新建、不改。
		if err := model.SetConfig("default_cron_rule", "15 3 * * *"); err != nil {
			t.Fatalf("set default_cron_rule: %v", err)
		}
		logs = steSync(f.sub)
		assertKept(t, logs, f.cfg, f.edited)
		steAssertUnchanged(t, *keep, logs)
		steAssertNoAddDelete(t, logs)
		if n := len(steManaged(f.sub)); n != 3 {
			t.Fatalf("want 3 tasks after setting a default rule, got %s", steDump(steManaged(f.sub)))
		}
	})

	// 订阅里一个声明了 cron 的脚本都没有：候选为空，但扫描确实读到了受管脚本，不能当成「检出为空」熔断——
	// 改动前这些文件都是兜底候选，自动删除照常进行，这里保持一致。
	t.Run("only_undeclared_scripts", func(t *testing.T) {
		f := setup(t, "uc_only", false)

		logs := steSync(f.sub)
		assertKept(t, logs, f.cfg, f.edited)
		assertDeleted(t, logs, f.removed)
		if containsLogLine(logs, "[跳过自动删除]") {
			t.Fatalf("undeclared scripts were scanned, the empty-candidates breaker must not trip\n%s", strings.Join(logs, "\n"))
		}
		if n := len(steManaged(f.sub)); n != 2 {
			t.Fatalf("want config / edited biz, got %s", steDump(steManaged(f.sub)))
		}
	})

	// 文件还在盘上、但被黑名单排除：不再是订阅在管的脚本，照删。
	t.Run("blacklisted_undeclared_script_is_deleted", func(t *testing.T) {
		f := setup(t, "uc_black", true)
		f.sub.Blacklist = "config.py"

		logs := steSync(f.sub)
		assertDeleted(t, logs, f.cfg)
		assertDeleted(t, logs, f.removed)
		assertKept(t, logs, f.edited)
	})

	// 自动删除关着：谁都不删，没声明 cron 的也不建。
	t.Run("auto_delete_off", func(t *testing.T) {
		f := setup(t, "uc_del_off", true)
		f.sub.AutoDelTaskMode = model.SubTaskSyncDisabled

		logs := steSync(f.sub)
		assertKept(t, logs, f.cfg, f.edited, f.removed)
		if n := len(steManaged(f.sub)); n != 4 {
			t.Fatalf("want the three existing tasks plus keep.js, got %s", steDump(steManaged(f.sub)))
		}
	})
}

// ucAssertTask：任务还在，名称、定时、命令、状态都没变，标签是 wantLabels。
func ucAssertTask(t *testing.T, before model.Task, wantLabels string, logs []string) {
	t.Helper()
	got, ok := steReload(before.ID)
	if !ok {
		t.Fatalf("task %d (%s) was deleted\n%s", before.ID, before.Name, strings.Join(logs, "\n"))
	}
	if got.Name != before.Name || got.CronExpression != before.CronExpression || got.Command != before.Command ||
		got.Status != before.Status || got.Labels != wantLabels {
		t.Fatalf("task %d:\n want name=%q cron=%q cmd=%q labels=%q\n  got name=%q cron=%q cmd=%q labels=%q\n%s",
			before.ID, before.Name, before.CronExpression, before.Command, wantLabels,
			got.Name, got.CronExpression, got.Command, got.Labels, strings.Join(logs, "\n"))
	}
}

// Wave 2 复查：默认规则留空时，没识别到 cron 声明的脚本不建任务，但已有的同脚本任务照旧只加标签接管（与 v3.2.8 一致）——
// 没有本订阅标签的（青龙导入、手建）、带着已删订阅悬空标签的（删除重建订阅）都算。不接管的话它们不归订阅管，
// 上游删了脚本也不会被自动删除；提示还会叫用户给已经有任务的脚本再建一条。
func TestSyncSubscriptionTasksAdoptsUnmanagedTaskOfUndeclaredCronScript(t *testing.T) {
	steEnv(t)
	saveDir := "uc_adopt"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	ucWriteScript(t, saveDir, "cfg.py", "X = 1\n")
	ucWriteScript(t, saveDir, "user.js", "console.log('no cron')\n")
	label := subscriptionTaskLabel(sub.ID)
	dangling := steNewTask(t, "cfg", steCmd(saveDir, "cfg.py"), "0 0 * * *", subscriptionTaskLabel(9999))
	imported := steNewTask(t, "user", steCmd(saveDir, "user.js")+" desi JD_COOKIE", "30 8 * * *", "青龙导入")
	for _, task := range []*model.Task{dangling, imported} {
		steAddLog(t, task.ID)
	}

	logs := steSync(sub)
	ucAssertTask(t, *dangling, subscriptionTaskLabel(9999)+","+label, logs)
	ucAssertTask(t, *imported, "青龙导入,"+label, logs)
	if steCountLines(logs, "[关联已有任务]") != 2 || !containsLogLine(logs, "[关联已有任务] cfg") ||
		!containsLogLine(logs, "[关联已有任务] user") || !containsLogLine(logs, "[共关联 2 个已有任务]") {
		t.Fatalf("both tasks should be adopted, one [关联已有任务] line each\n%s", strings.Join(logs, "\n"))
	}
	if steCountLines(logs, "[自动添加任务]") != 1 {
		t.Fatalf("only keep.js may be built\n%s", strings.Join(logs, "\n"))
	}
	if got := ucHintLine(logs); got != "" {
		t.Fatalf("scripts that already have tasks must not be listed, got %q", got)
	}

	logs = steSync(sub)
	if !containsLogLine(logs, "[同步完成] 本次未对定时任务做任何变更") || ucHintLine(logs) != "" {
		t.Fatalf("second round should be a silent no-op\n%s", strings.Join(logs, "\n"))
	}

	// 上游删了这两个脚本：接管过的任务照常连日志删掉。
	for _, name := range []string{"cfg.py", "user.js"} {
		if err := os.Remove(filepath.Join(config.C.Data.ScriptsDir, saveDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	logs = steSync(sub)
	for _, task := range []*model.Task{dangling, imported} {
		if _, exists := steReload(task.ID); exists || steLogCount(task.ID) != 0 {
			t.Fatalf("%s should be deleted with its logs after upstream removed the script\n%s", task.Name, strings.Join(logs, "\n"))
		}
	}
	if steCountLines(logs, "[自动删除任务]") != 2 {
		t.Fatalf("want two [自动删除任务] lines\n%s", strings.Join(logs, "\n"))
	}
}

// 照提示手动建了任务之后，下次拉取接管它，提示随之消失，之后不再有任何变更。
func TestSyncSubscriptionTasksUndeclaredHintClearsAfterHandMadeTask(t *testing.T) {
	steEnv(t)
	saveDir := "uc_handmade"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")

	logs := steSync(sub)
	if got := ucHintLine(logs); !strings.Contains(got, "（biz.js）") {
		t.Fatalf("first pull should list biz.js, got %q\n%s", got, strings.Join(logs, "\n"))
	}

	handMade := steNewTask(t, "我建的", steCmd(saveDir, "biz.js"), "5 5 * * *")
	logs = steSync(sub)
	ucAssertTask(t, *handMade, subscriptionTaskLabel(sub.ID), logs)
	if !containsLogLine(logs, "[关联已有任务] 我建的") {
		t.Fatalf("the hand-made task should be adopted\n%s", strings.Join(logs, "\n"))
	}
	if got := ucHintLine(logs); got != "" {
		t.Fatalf("the hint must clear once the script has a task, got %q", got)
	}

	logs = steSync(sub)
	if !containsLogLine(logs, "[同步完成] 本次未对定时任务做任何变更") || ucHintLine(logs) != "" {
		t.Fatalf("third round should be a silent no-op\n%s", strings.Join(logs, "\n"))
	}
}

// 自动添加关着：不接管（接管属于新增分支），也不提示，与 v3.2.8 一致。
func TestSyncSubscriptionTasksUndeclaredNoAdoptionWhenAutoAddOff(t *testing.T) {
	steEnv(t)
	saveDir := "uc_adopt_off"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	sub.AutoAddTaskMode = model.SubTaskSyncDisabled
	ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")
	userTask := steNewTask(t, "biz", steCmd(saveDir, "biz.js"), "0 0 * * *")

	logs := steSync(sub)
	steAssertUnchanged(t, *userTask, logs)
	if containsLogLine(logs, "[关联已有任务]") || ucHintLine(logs) != "" {
		t.Fatalf("auto add is off: no adoption and no hint expected\n%s", strings.Join(logs, "\n"))
	}
}

// 命令为空（或只有空白）的任务不跑任何脚本，未声明 cron 的接管路径绝不能取到它们。
func TestSyncSubscriptionTasksUndeclaredNeverAdoptsEmptyCommandTask(t *testing.T) {
	steEnv(t)
	saveDir := "uc_empty_cmd"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")
	empty := steNewTask(t, "空命令", "", "0 0 * * *")
	blank := steNewTask(t, "空白命令", "   ", "0 0 * * *")

	logs := steSync(sub)
	steAssertUnchanged(t, *empty, logs)
	steAssertUnchanged(t, *blank, logs)
	if containsLogLine(logs, "[关联已有任务]") {
		t.Fatalf("empty-command tasks must never be adopted\n%s", strings.Join(logs, "\n"))
	}
	if got := ucHintLine(logs); !strings.Contains(got, "（biz.js）") {
		t.Fatalf("biz.js still has no task and should be listed, got %q", got)
	}
}

// 本订阅已经有任务在跑这个脚本（这里改过参数）：同脚本的其他任务不接管，与候选脚本「已有任务就不动」同口径；也不提示。
func TestSyncSubscriptionTasksUndeclaredSkipsAdoptionWhenAlreadyManaged(t *testing.T) {
	steEnv(t)
	saveDir := "uc_dup"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")
	managed := steNewTask(t, "biz", steCmd(saveDir, "biz.js")+" now", "0 0 * * *", subscriptionTaskLabel(sub.ID))
	duplicate := steNewTask(t, "biz 副本", steCmd(saveDir, "biz.js"), "0 0 * * *")

	logs := steSync(sub)
	steAssertUnchanged(t, *managed, logs)
	steAssertUnchanged(t, *duplicate, logs)
	if containsLogLine(logs, "[关联已有任务]") || ucHintLine(logs) != "" {
		t.Fatalf("the script already has a task of this subscription: no adoption, no hint\n%s", strings.Join(logs, "\n"))
	}
}

// Wave 2 复查：文件名里有引号、或被空格隔开的 `--` 时命令切不开，求不出脚本键。升级前兜底建出来的原样命令（task <相对路径>）
// 要靠精确命令认出：开着自动删除也连日志保留（v3.2.8 按候选命令原文就是这么认的），没有本订阅标签的照常接管，也不提示。
func TestSyncSubscriptionTasksKeepsUndeclaredCronTasksWithOddFileNames(t *testing.T) {
	steEnv(t)
	saveDir := "uc_odd"
	sub := steSubscription(t, saveDir, true, map[string]string{"keep.js": "1 1 * * *"})
	label := subscriptionTaskLabel(sub.ID)
	var managed []*model.Task
	for _, name := range []string{"it's.js", "a -- b.js", "Tom's and Jerry's.py"} {
		ucWriteScript(t, saveDir, name, "console.log('no cron')\n")
		task := steNewTask(t, name, steCmd(saveDir, name), "0 0 * * *", label)
		steAddLog(t, task.ID)
		managed = append(managed, task)
	}
	ucWriteScript(t, saveDir, "it's mine.js", "console.log('no cron')\n")
	unlabelled := steNewTask(t, "it's mine", steCmd(saveDir, "it's mine.js"), "0 0 * * *")

	logs := steSync(sub)
	for _, task := range managed {
		steAssertUnchanged(t, *task, logs)
		if n := steLogCount(task.ID); n != 1 {
			t.Fatalf("logs of %s changed, got %d\n%s", task.Name, n, strings.Join(logs, "\n"))
		}
	}
	ucAssertTask(t, *unlabelled, label, logs)
	if containsLogLine(logs, "[自动删除任务]") || ucHintLine(logs) != "" {
		t.Fatalf("odd file names: nothing may be deleted and nothing listed as unbuilt\n%s", strings.Join(logs, "\n"))
	}
}

// Wave 2 复查：库里的默认 Cron 规则不合法（直接改库、恢复旧备份绕过了注册表校验）时照样当没配，但拉取日志要说出来，
// 提示也改成「修正」——设置里明明填着值，还叫人去「填写」只会让人摸不着头脑。
func TestSyncSubscriptionTasksWarnsAboutInvalidDefaultRule(t *testing.T) {
	steEnv(t)
	saveDir := "uc_dirty"
	sub := steSubscription(t, saveDir, false, map[string]string{"keep.js": "1 1 * * *"})
	ucWriteScript(t, saveDir, "biz.js", "console.log('no cron')\n")
	ucSetDirtyDefaultRule(t, "0 0 * * * * *")

	logs := steSync(sub)
	if !containsLogLine(logs, "[警告] 订阅设置里的默认 Cron 规则「0 0 * * * * *」无效，已忽略") {
		t.Fatalf("missing invalid default rule warning\n%s", strings.Join(logs, "\n"))
	}
	want := "[提示] 1 个脚本没有识别到 cron 声明，未建定时任务（biz.js）。需要的话在订阅设置修正「默认 Cron 规则」，或手动为脚本建任务"
	if got := ucHintLine(logs); got != want {
		t.Fatalf("hint mismatch\nwant %q\n got %q\n%s", want, got, strings.Join(logs, "\n"))
	}
	if n := len(steManaged(sub)); n != 1 {
		t.Fatalf("only keep.js may be built, got %s", steDump(steManaged(sub)))
	}

	// 自动添加关着时默认规则用不上，不警告。
	sub.AutoAddTaskMode = model.SubTaskSyncDisabled
	if logs = steSync(sub); containsLogLine(logs, "默认 Cron 规则「") {
		t.Fatalf("auto add is off, no warning expected\n%s", strings.Join(logs, "\n"))
	}

	// 留空不是脏值，不警告，提示照旧叫人填写。
	sub.AutoAddTaskMode = model.SubTaskSyncEnabled
	ucSetDirtyDefaultRule(t, "")
	logs = steSync(sub)
	if containsLogLine(logs, "默认 Cron 规则「") || !strings.Contains(ucHintLine(logs), "在订阅设置填写「默认 Cron 规则」") {
		t.Fatalf("empty rule: no warning, hint asks to fill the rule in\n%s", strings.Join(logs, "\n"))
	}
}
