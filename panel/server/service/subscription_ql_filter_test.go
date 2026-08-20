package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 用户报告的真实指令（青龙一键识别后拉库，脚本全没检出、任务一个没建）：
//
//	ql repo https://js.googo.win/https://github.com/6dylan6/jdpro.git \
//	    "jd_|jx_|jddj_" "backUp" "^jd[^_]|USER|JD|function|sendNotify|utils"
//
// 青龙的第 2/3/4 个位置参数是 `grep -E` 模式，用 `|` 分隔。
// 旧实现只按 `,` 拆分 → 整串被当成一个模式 → sparse-checkout 检出 0 个文件、
// 白名单 strings.Contains 恒 false，全链路静默失败。
const (
	qlRepoWhitelist = "jd_|jx_|jddj_"
	qlRepoBlacklist = "backUp"
	qlRepoDependOn  = "^jd[^_]|USER|JD|function|sendNotify|utils"
)

func TestSplitSubscriptionFilterPatternsSupportsPipeAndComma(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		// 用户报告的真实配置
		{"ql 白名单三段", qlRepoWhitelist, []string{"jd_", "jx_", "jddj_"}},
		{"ql 黑名单单段", qlRepoBlacklist, []string{"backUp"}},
		{"ql 依赖参数", qlRepoDependOn, []string{"^jd[^_]", "USER", "JD", "function", "sendNotify", "utils"}},

		// 兼容性：`,` 分隔的老配置行为必须不变
		{"逗号分隔", "jd_,jx_,jddj_", []string{"jd_", "jx_", "jddj_"}},
		{"逗号带空格", " jd_ , jx_ ", []string{"jd_", "jx_"}},
		{"单个模式", "keep_task", []string{"keep_task"}},
		{"带路径的子目录", "scripts/daily", []string{"scripts/daily"}},

		// 混用
		{"逗号与竖线混用", "jd_|jx_,jddj_", []string{"jd_", "jx_", "jddj_"}},

		// 空段必须丢弃（空模式会让 Contains 恒 false / sparse 规则跑偏）
		{"连续竖线", "jd_||jx_", []string{"jd_", "jx_"}},
		{"连续逗号", "jd_,,jx_", []string{"jd_", "jx_"}},
		{"首尾竖线", "|jd_|", []string{"jd_"}},
		{"首尾逗号", ",jd_,", []string{"jd_"}},
		{"纯空白段", "jd_| |jx_", []string{"jd_", "jx_"}},
		{"只有分隔符", "|,|", nil},
		{"空字符串", "", nil},
		{"纯空白", "   ", nil},

		// 去重
		{"重复模式去重", "jd_,jd_|jd_", []string{"jd_"}},

		// 通配符原样返回，由 isWildcardFilterPattern 判定
		{"通配符", "*", []string{"*"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSubscriptionFilterPatterns(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitSubscriptionFilterPatterns(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
			for _, p := range got {
				if strings.TrimSpace(p) == "" {
					t.Fatalf("splitSubscriptionFilterPatterns(%q) 产出了空模式: %#v", tc.raw, got)
				}
			}
		})
	}
}

// isWildcardFilterPattern 的 case 列表在拆分后仍然要正确工作：
// 用户在青龙命令里写 `*` 或 `*|*` 都应视为"不过滤"。
func TestIsWildcardFilterPatternAfterPipeSplit(t *testing.T) {
	for _, raw := range []string{"*", "**", "*.*", ".*", "/", "all", "any", "全部", "*|*", "*,*", "ALL"} {
		patterns := splitSubscriptionFilterPatterns(raw)
		if len(patterns) == 0 {
			t.Fatalf("raw %q 拆分后为空", raw)
		}
		for _, p := range patterns {
			if !isWildcardFilterPattern(p) {
				t.Fatalf("raw %q 拆出的 %q 应被判定为通配符", raw, p)
			}
		}
	}

	// 反例：普通过滤词不能被误判成通配符
	for _, raw := range []string{"jd_", "jx_", "jddj_", "backUp", "scripts/daily", "^jd[^_]"} {
		for _, p := range splitSubscriptionFilterPatterns(raw) {
			if isWildcardFilterPattern(p) {
				t.Fatalf("%q 不应被判定为通配符", p)
			}
		}
	}
}

func TestBuildSparseCheckoutPatternsForQLRepoCommand(t *testing.T) {
	sub := &model.Subscription{
		Name:      "jdpro",
		Type:      model.SubTypeGitRepo,
		URL:       "https://js.googo.win/https://github.com/6dylan6/jdpro.git",
		SaveDir:   "6dylan6_jdpro",
		Whitelist: qlRepoWhitelist,
		Blacklist: qlRepoBlacklist,
		DependOn:  qlRepoDependOn,
	}

	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	// 白名单三条 + 依赖规则里 5 条安全模式（`^jd[^_]` 含元字符被单独跳过）+ 黑名单排除，
	// 每个片段成对下发「条目本身」与「条目下全部内容」两条规则：
	// gitignore 的 `*` 不跨 `/`，只发 `**/*utils*` 拉不到 utils/date.js。
	want := []string{
		"**/*jd_*", "**/*jd_*/**",
		"**/*jx_*", "**/*jx_*/**",
		"**/*jddj_*", "**/*jddj_*/**",
		"**/*USER*", "**/*USER*/**",
		"**/*JD*", "**/*JD*/**",
		"**/*function*", "**/*function*/**",
		"**/*sendNotify*", "**/*sendNotify*/**",
		"**/*utils*", "**/*utils*/**",
		"!**/*backUp*", "!**/*backUp*/**",
	}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
	}
	// 依赖规则并入检出范围这件事必须可见，含元字符被跳过的那条也必须可见。
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings (依赖并入 + 元字符跳过), got %#v", warnings)
	}
	if !strings.Contains(warnings[0], "sendNotify") || !strings.Contains(warnings[0], "不会建成定时任务") {
		t.Errorf("第一条应说明依赖规则已并入检出、且不建任务, got %q", warnings[0])
	}
	if !strings.Contains(warnings[1], "^jd[^_]") {
		t.Errorf("第二条应点名被跳过的依赖模式, got %q", warnings[1])
	}
	for _, p := range patterns {
		if strings.Contains(p, "|") {
			t.Fatalf("sparse pattern %q 仍含 `|`，gitignore 语法里它不是「或」，会静默失配", p)
		}
	}
}

// `,` 分隔的老配置生成的 sparse 规则必须与竖线版完全一致。
func TestBuildSparseCheckoutPatternsCommaSeparatedUnchanged(t *testing.T) {
	pipeSub := &model.Subscription{Whitelist: "jd_|jx_|jddj_", Blacklist: "backUp"}
	commaSub := &model.Subscription{Whitelist: "jd_,jx_,jddj_", Blacklist: "backUp"}

	pipePatterns, _ := buildSubscriptionSparseCheckoutPatterns(pipeSub)
	commaPatterns, _ := buildSubscriptionSparseCheckoutPatterns(commaSub)
	if !reflect.DeepEqual(pipePatterns, commaPatterns) {
		t.Fatalf("竖线与逗号应等价: pipe=%#v comma=%#v", pipePatterns, commaPatterns)
	}
}

// 白名单含 gitignore 元字符（`[` `]` `?` `\`）时，只跳过那一条会让它对应的文件
// 静默检不出来。约定：整体放弃包含侧限制、检出完整仓库，并打出可见告警。
func TestBuildSparseCheckoutPatternsFallsBackOnUnsafeWhitelist(t *testing.T) {
	// 无黑名单 → 直接返回空规则（等价于关闭 sparse-checkout）
	sub := &model.Subscription{Whitelist: "^jd[^_]|USER"}
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	if len(patterns) != 0 {
		t.Fatalf("含元字符的白名单应退回完整检出, got %#v", patterns)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "^jd[^_]") {
		t.Fatalf("应给出含具体模式的可见告警, got %#v", warnings)
	}

	// 有黑名单 → 包含全部 + 保留排除规则（排除规则同样成对，否则 backUp/jd_old.js
	// 会重新命中 `*` 而落盘）
	sub = &model.Subscription{Whitelist: "^jd[^_]|USER", Blacklist: "backUp"}
	patterns, warnings = buildSubscriptionSparseCheckoutPatterns(sub)
	want := []string{"*", "!**/*backUp*", "!**/*backUp*/**"}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %#v", warnings)
	}

	// 混合安全/不安全 → 仍整体退回（白名单是「或」语义，不能只保留安全的那半边）
	sub = &model.Subscription{Whitelist: "jd_|^jd[^_]"}
	patterns, warnings = buildSubscriptionSparseCheckoutPatterns(sub)
	if len(patterns) != 0 {
		t.Fatalf("混合白名单应整体退回完整检出, got %#v", patterns)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %#v", warnings)
	}
}

// 黑名单是「排除」语义：跳过不安全的排除规则只会多落盘，方向安全，逐条跳过 + 告警。
func TestBuildSparseCheckoutPatternsSkipsUnsafeBlacklist(t *testing.T) {
	sub := &model.Subscription{Whitelist: "jd_", Blacklist: "back[Uu]p|Archive"}
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	want := []string{"**/*jd_*", "**/*jd_*/**", "!**/*Archive*", "!**/*Archive*/**"}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "back[Uu]p") {
		t.Fatalf("应对被跳过的黑名单给出可见告警, got %#v", warnings)
	}
}

func TestBuildSparseCheckoutPatternsFallsBackOnUnsafeSubPath(t *testing.T) {
	sub := &model.Subscription{SubPath: "scripts/day[0-9]", Blacklist: "backUp"}
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	want := []string{"*", "!**/*backUp*", "!**/*backUp*/**"}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "day[0-9]") {
		t.Fatalf("应对不安全子目录给出可见告警, got %#v", warnings)
	}
}

func TestMatchesSubscriptionFiltersForQLRepoCommand(t *testing.T) {
	sub := &model.Subscription{
		Whitelist: qlRepoWhitelist,
		Blacklist: qlRepoBlacklist,
		DependOn:  qlRepoDependOn,
	}

	cases := []struct {
		path string
		want bool
	}{
		{"jd_bean_change.js", true},
		{"jd_CheckCK.js", true},
		{"jx_sign.js", true},
		{"jddj_bean.js", true},
		{"scripts/jd_fruit.js", true},

		// 黑名单命中（旧实现里 checkBlacklist 自己写了一份 Split(",")，同样不认 `|`）
		{"backUp/jd_xxx.js", false},
		// 黑名单目录里的白名单文件同样排除
		{"backUp/jx_old.js", false},
		// 白名单未命中
		{"README.md", false},
		// 依赖规则（ql 的 $4）只决定「检不检出」，不决定「建不建任务」：
		// 辅助文件会落盘，但白名单没命中就不会变成定时任务。
		// 见 TestSubscriptionDependencyOnlyFilesAreCheckedOutButNotScheduled。
		{"utils/sendNotify.js", false},
	}

	for _, tc := range cases {
		if got := matchesSubscriptionFilters(sub, tc.path); got != tc.want {
			t.Errorf("matchesSubscriptionFilters(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// 黑名单单独走 checkBlacklist（它以前自己写了一份 strings.Split(",")，不认 `|`）。
func TestCheckBlacklistSupportsPipeSeparator(t *testing.T) {
	sub := &model.Subscription{Blacklist: "backUp|Archive|.github"}
	for _, path := range []string{"backUp/a.js", "Archive/b.js", ".github/workflows/c.yml"} {
		if checkBlacklist(sub, path) {
			t.Errorf("checkBlacklist(%q) = true, want false", path)
		}
	}
	if !checkBlacklist(sub, "jd_bean_change.js") {
		t.Error("未命中黑名单的文件应放行")
	}

	// 逗号老配置不变
	commaSub := &model.Subscription{Blacklist: "backUp,Archive"}
	if checkBlacklist(commaSub, "Archive/b.js") {
		t.Error("逗号分隔的黑名单回归失败")
	}
}

// 端到端：造一个 jdpro 风格的脚本目录，用用户那条真实指令的白/黑名单跑同步，
// 断言能扫描出候选并建出定时任务。
func TestSyncSubscriptionTasksHandlesQLPipeSeparatedFilters(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "6dylan6_jdpro"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	backupDir := filepath.Join(scriptsRoot, "backUp")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create dirs: %v", err)
	}

	files := map[string]string{
		"jd_bean_change.js": "/**\n * cron 1 0 * * * jd_bean_change.js, tag:京东资产变动\n */\nconst $ = new Env('京东资产');\n",
		"jx_sign.js":        "const $ = new Env('京喜签到');\n",
		"jddj_bean.js":      "const $ = new Env('京东到家');\n",
		"other_task.js":     "//cron: 30 8 * * *\nconst $ = new Env('无关脚本');\n",
		"README.md":         "readme\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(scriptsRoot, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(backupDir, "jd_old.js"),
		[]byte("//cron: 40 8 * * *\nconst $ = new Env('旧备份');\n"), 0o644); err != nil {
		t.Fatalf("write backup script: %v", err)
	}

	sub := &model.Subscription{
		Name:        "jdpro",
		Type:        model.SubTypeGitRepo,
		URL:         "https://js.googo.win/https://github.com/6dylan6/jdpro.git",
		SaveDir:     saveDir,
		Whitelist:   qlRepoWhitelist,
		Blacklist:   qlRepoBlacklist,
		DependOn:    qlRepoDependOn,
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

	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, "共扫描 0 个候选文件") {
		t.Fatalf("白名单竖线分隔失效，仍然扫描 0 个候选文件:\n%s", joined)
	}

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 3 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("expected 3 tasks (jd_/jx_/jddj_), got %d\n%s", len(tasks), joined)
	}

	byBase := map[string]model.Task{}
	for _, task := range tasks {
		byBase[filepath.Base(task.Command)] = task
	}
	for _, want := range []string{"jd_bean_change.js", "jx_sign.js", "jddj_bean.js"} {
		if _, ok := byBase[want]; !ok {
			t.Errorf("白名单文件 %s 没有建任务", want)
		}
	}
	for _, unwanted := range []string{"jd_old.js", "other_task.js", "README.md"} {
		if _, ok := byBase[unwanted]; ok {
			t.Errorf("%s 不应建任务", unwanted)
		}
	}
	if got := byBase["jd_bean_change.js"].CronExpression; got != "1 0 * * *" {
		t.Errorf("脚本头 cron 应被保留, got %q", got)
	}
	if got := byBase["jx_sign.js"].CronExpression; got != FallbackSubscriptionCron {
		t.Errorf("无 cron 头的脚本应用兜底 cron, got %q", got)
	}
}

// 回归：`,` 分隔的老配置行为不变。
func TestSyncSubscriptionTasksCommaSeparatedFiltersUnchanged(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "comma_repo"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	backupDir := filepath.Join(scriptsRoot, "backUp")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create dirs: %v", err)
	}
	os.WriteFile(filepath.Join(scriptsRoot, "jd_a.js"), []byte("//cron: 1 1 * * *\nconst $ = new Env('a');\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "jx_b.js"), []byte("//cron: 2 2 * * *\nconst $ = new Env('b');\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "zz_c.js"), []byte("//cron: 3 3 * * *\nconst $ = new Env('c');\n"), 0o644)
	os.WriteFile(filepath.Join(backupDir, "jd_old.js"), []byte("//cron: 4 4 * * *\nconst $ = new Env('old');\n"), 0o644)

	sub := &model.Subscription{
		Name: "comma", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		Whitelist: "jd_,jx_", Blacklist: "backUp",
		AutoAddTask: true, Enabled: true,
	}
	database.DB.Create(sub)

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	syncSubscriptionTasks(sub, func(string) {})

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 2 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("逗号分隔老配置回归失败, expected 2 tasks, got %d", len(tasks))
	}
}

// 真机 git 验证：竖线分隔的白名单必须真的能把三类文件都检出到工作区。
// 这是最贴近用户现场的一层——旧实现在这里 sparse-checkout 会静默检出 0 个文件。
func TestPullGitRepoWithCallbackPipeSeparatedWhitelistChecksOutAllMatches(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	if err := os.MkdirAll(filepath.Join(worktreeDir, "backUp"), 0o755); err != nil {
		t.Fatalf("create backUp dir: %v", err)
	}
	repoFiles := map[string]string{
		"jd_bean_change.js": "console.log('jd')\n",
		"jx_sign.js":        "console.log('jx')\n",
		"jddj_bean.js":      "console.log('jddj')\n",
		"other_task.js":     "console.log('other')\n",
		"README.md":         "readme\n",
		"backUp/jd_old.js":  "console.log('old')\n",
	}
	for name, body := range repoFiles {
		if err := os.WriteFile(filepath.Join(worktreeDir, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runGit(t, worktreeDir, "add", ".")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:      "jdpro",
		Type:      model.SubTypeGitRepo,
		URL:       remoteDir,
		Branch:    "main",
		SaveDir:   "jdpro-repo",
		Whitelist: qlRepoWhitelist,
		Blacklist: qlRepoBlacklist,
		DependOn:  qlRepoDependOn,
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}

	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull repo: %v\n%s", err, output)
	}

	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
	// 核心断言：三条白名单子模式各自命中，旧实现在这里是「一个都检不出来」。
	for _, name := range []string{"jd_bean_change.js", "jx_sign.js", "jddj_bean.js"} {
		if _, statErr := os.Stat(filepath.Join(destDir, name)); statErr != nil {
			t.Errorf("白名单文件 %s 应被检出: %v", name, statErr)
		}
	}
	for _, name := range []string{"other_task.js", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(destDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s 不应被检出, stat err=%v", name, statErr)
		}
	}
	// 黑名单目录里的文件：以前这条只能「记录不断言」——`!**/*backUp*` 在 gitignore 语法下
	// 只匹配到 backUp 这个目录条目本身，子文件 backUp/jd_old.js 会重新命中正向的
	// `**/*jd_*` 而落盘（`*` 不跨 `/`，最后匹配者胜出）。
	// 现在黑名单同样成对下发 `!**/*backUp*/**`，可以真正断言了。
	if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash("backUp/jd_old.js"))); !os.IsNotExist(statErr) {
		t.Errorf("黑名单目录里的 backUp/jd_old.js 不应被检出, stat err=%v", statErr)
	}
}
