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

// depend_on（青龙 `ql repo` 的第 4 个位置参数 dependence）在本次改造前是纯备注字段，
// 全链路一次都没读过。青龙那边它是功能性的：命中的文件会被拉到脚本目录、但不注册成定时任务。
// 本文件覆盖改造后的三条语义：
//  1. 依赖规则并入 sparse-checkout 的「包含侧」——命中的文件会落盘；
//  2. 仅命中依赖、没命中白名单的文件不进任务候选；
//  3. 白名单为空时依赖规则不改变任何行为（白名单为空 = 全部文件都算命中白名单）。
//
// 复用 subscription_ql_filter_test.go 里用户报告的那条真实指令常量：
//
//	ql repo <url> "jd_|jx_|jddj_" "backUp" "^jd[^_]|USER|JD|function|sendNotify|utils"

func TestSplitSubscriptionDependencyPatterns(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantPat   []string
		wantNotes []string
	}{
		{
			name:      "用户真实指令的 dependence 参数",
			raw:       qlRepoDependOn,
			wantPat:   []string{"^jd[^_]", "USER", "JD", "function", "sendNotify", "utils"},
			wantNotes: nil,
		},
		{
			name:    "逗号分隔",
			raw:     "sendNotify,utils",
			wantPat: []string{"sendNotify", "utils"},
		},
		{
			name:    "路径片段",
			raw:     "utils/|JS_USER_AGENTS",
			wantPat: []string{"utils/", "JS_USER_AGENTS"},
		},
		{
			name:    "空值",
			raw:     "",
			wantPat: nil,
		},

		// 兼容性：存量数据里的中文备注必须被识别成「文字备注」，不参与文件检出。
		{
			name:      "纯中文备注",
			raw:       "依赖青龙的通知库",
			wantNotes: []string{"依赖青龙的通知库"},
		},
		{
			name:      "中文逗号在段内",
			raw:       "迁移自青龙，需要 sendNotify",
			wantNotes: []string{"迁移自青龙，需要 sendNotify"},
		},
		{
			// 英文逗号会被当分隔符拆开，前半段是中文备注、后半段恰好是个合法文件名片段。
			// 这种「半备注半规则」按段各自判定，不整体丢弃。
			name:      "中文备注 + 英文逗号 + 文件名片段",
			raw:       "迁移自青龙,sendNotify",
			wantPat:   []string{"sendNotify"},
			wantNotes: []string{"迁移自青龙"},
		},
		{
			name:      "含空格的英文说明",
			raw:       "needs sendNotify lib",
			wantNotes: []string{"needs sendNotify lib"},
		},
		{
			name:      "超长片段按备注跳过",
			raw:       strings.Repeat("a", subscriptionDependencyPatternMaxLen+1),
			wantNotes: []string{strings.Repeat("a", subscriptionDependencyPatternMaxLen+1)},
		},
		{
			name:    "刚好到长度上限仍算规则",
			raw:     strings.Repeat("a", subscriptionDependencyPatternMaxLen),
			wantPat: []string{strings.Repeat("a", subscriptionDependencyPatternMaxLen)},
		},

		// 通配符跳过：若把整个仓库都算成依赖，等于一个定时任务都建不出来。
		{
			name: "通配符视为未配置",
			raw:  "*",
		},
		{
			name:    "通配符与普通模式混用",
			raw:     "*|sendNotify",
			wantPat: []string{"sendNotify"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPat, gotNotes := splitSubscriptionDependencyPatterns(tc.raw)
			if !reflect.DeepEqual(gotPat, tc.wantPat) {
				t.Errorf("patterns = %#v, want %#v", gotPat, tc.wantPat)
			}
			if !reflect.DeepEqual(gotNotes, tc.wantNotes) {
				t.Errorf("notes = %#v, want %#v", gotNotes, tc.wantNotes)
			}
		})
	}
}

// 兼容性核心：改造前 depend_on 是纯备注，存量用户很可能写了中文说明。
// 这类内容不能凭空多检出一批文件。
func TestSubscriptionDependencyNotesDoNotAffectCheckout(t *testing.T) {
	sub := &model.Subscription{
		Whitelist: "jd_",
		DependOn:  "依赖 utils 库，迁移自青龙",
	}

	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	want := []string{"**/*jd_*", "**/*jd_*/**"}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("中文备注不应产生检出规则, got %#v want %#v", patterns, want)
	}
	// 静默跳过是这个项目最难查的失败形态，必须留下日志。
	if len(warnings) != 1 || !strings.Contains(warnings[0], "文字备注") {
		t.Fatalf("应给出「已按文字备注跳过」的提示, got %#v", warnings)
	}
	if matchesSubscriptionDependency(sub, "utils/helper.js") {
		t.Error("中文备注不应命中任何文件")
	}
}

func TestBuildSparseCheckoutPatternsIncludesDependencySide(t *testing.T) {
	t.Run("白名单 + 依赖两侧的包含模式同时下发", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_", DependOn: "sendNotify|utils"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{
			"**/*jd_*", "**/*jd_*/**",
			"**/*sendNotify*", "**/*sendNotify*/**",
			"**/*utils*", "**/*utils*/**",
		}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})

	t.Run("指定子目录 + 依赖", func(t *testing.T) {
		// 主脚本在子目录里，辅助库常常在仓库根，所以依赖规则与子目录是「或」关系。
		// 子目录填的是明确路径，不做 `**/*x*` 包装，也就没有递归配对规则。
		sub := &model.Subscription{SubPath: "scripts/daily", DependOn: "sendNotify"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"scripts/daily", "**/*sendNotify*", "**/*sendNotify*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})

	t.Run("黑名单对依赖同样生效", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_", Blacklist: "backUp", DependOn: "sendNotify"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{
			"**/*jd_*", "**/*jd_*/**",
			"**/*sendNotify*", "**/*sendNotify*/**",
			"!**/*backUp*", "!**/*backUp*/**",
		}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})

	t.Run("依赖含元字符逐条跳过并告警", func(t *testing.T) {
		// 与白名单不同：漏一条依赖只会「少落一个辅助文件」，退化到改造前的行为，
		// 方向安全，所以逐条跳过而不是整体退回完整检出。
		sub := &model.Subscription{Whitelist: "jd_", DependOn: "^jd[^_]|sendNotify"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"**/*jd_*", "**/*jd_*/**", "**/*sendNotify*", "**/*sendNotify*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		joined := strings.Join(warnings, "\n")
		if !strings.Contains(joined, "^jd[^_]") {
			t.Fatalf("被跳过的依赖模式必须点名, got %#v", warnings)
		}
	})
}

// 降级路径不能被依赖规则破坏：包含侧一旦退回「完整检出」，patterns 必须保持为空，
// 否则 sparse-checkout 会被依赖模式重新激活成「只检出依赖文件」，白名单文件全丢。
func TestBuildSparseCheckoutPatternsDependencyDoesNotBreakFullCheckoutFallback(t *testing.T) {
	t.Run("白名单含元字符退回完整检出", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "^jd[^_]", DependOn: "sendNotify|utils"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("包含侧已退回完整检出，不应因依赖规则重新生成规则, got %#v", patterns)
		}
		if !strings.Contains(strings.Join(warnings, "\n"), "依赖规则无需额外生效") {
			t.Errorf("应说明依赖规则本次无需生效, got %#v", warnings)
		}
	})

	t.Run("子目录含元字符退回完整检出", func(t *testing.T) {
		sub := &model.Subscription{SubPath: "scripts/day[0-9]", DependOn: "sendNotify"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		// 只剩黑名单兜底逻辑；这里没有黑名单，所以应该是空规则。
		if len(patterns) != 0 {
			t.Fatalf("不安全子目录已退回完整检出, got %#v", patterns)
		}
	})

	t.Run("白名单为空时依赖规则不生成任何规则", func(t *testing.T) {
		// 白名单为空 = 全部文件都算命中白名单，本来就检出全部文件。
		// 此时若追加依赖模式，会把「全量检出」缩成「只有依赖文件」。
		sub := &model.Subscription{DependOn: "sendNotify|utils"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("白名单为空时依赖规则不应激活 sparse-checkout, got %#v", patterns)
		}
		if !strings.Contains(strings.Join(warnings, "\n"), "依赖规则无需额外生效") {
			t.Errorf("应说明依赖规则本次无需生效, got %#v", warnings)
		}
	})
}

func TestIsSubscriptionDependencyOnlyFile(t *testing.T) {
	sub := &model.Subscription{
		Whitelist: qlRepoWhitelist,
		Blacklist: qlRepoBlacklist,
		DependOn:  qlRepoDependOn,
	}

	cases := []struct {
		path string
		want bool
	}{
		// 仅命中依赖 → 是依赖文件，不建任务
		{"sendNotify.js", true},
		{"utils/date.js", true},
		{"JS_USER_AGENTS.js", true},
		// 命中白名单（哪怕也命中依赖）→ 按白名单算，照常建任务
		{"jd_bean_change.js", false},
		{"jx_sign.js", false},
		// 两边都没命中 → 不是依赖文件（本来也不建任务）
		{"README.md", false},
		{"other_task.js", false},
	}

	for _, tc := range cases {
		if got := isSubscriptionDependencyOnlyFile(sub, tc.path); got != tc.want {
			t.Errorf("isSubscriptionDependencyOnlyFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// 白名单为空时，matchesSubscriptionWhitelist 对所有文件恒 true，
// 于是没有任何文件会被判成「仅命中依赖」——依赖规则完全不改变建任务的结果。
func TestSubscriptionDependencyIsNoOpWhenWhitelistEmpty(t *testing.T) {
	for _, whitelist := range []string{"", "   ", "*", "*|*"} {
		sub := &model.Subscription{Whitelist: whitelist, DependOn: qlRepoDependOn}
		for _, path := range []string{"sendNotify.js", "utils/date.js", "jd_bean_change.js", "other_task.js"} {
			if isSubscriptionDependencyOnlyFile(sub, path) {
				t.Errorf("whitelist=%q: %q 不应被判成仅命中依赖", whitelist, path)
			}
			if !matchesSubscriptionFilters(sub, path) {
				t.Errorf("whitelist=%q: %q 应照常通过筛选", whitelist, path)
			}
		}
	}
}

// 端到端：造一个 jdpro 风格的目录（主脚本 + 辅助库），用用户那条真实指令跑同步，
// 断言辅助库文件不会被建成定时任务，并且日志里点名了它们。
func TestSubscriptionDependencyOnlyFilesAreCheckedOutButNotScheduled(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "jdpro_depend"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "utils"), 0o755); err != nil {
		t.Fatalf("create dirs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "backUp"), 0o755); err != nil {
		t.Fatalf("create backUp dir: %v", err)
	}

	files := map[string]string{
		// 命中白名单 → 检出 + 建任务
		"jd_bean_change.js": "/**\n * cron 1 0 * * * jd_bean_change.js, tag:京东资产变动\n */\nconst $ = new Env('京东资产');\n",
		"jx_sign.js":        "const $ = new Env('京喜签到');\n",
		"jddj_bean.js":      "const $ = new Env('京东到家');\n",
		// 仅命中依赖 → 检出但不建任务。刻意带 cron 头，验证「依赖优先于 cron 头」：
		// 不排除的话它会因为有合法 cron 而被建成任务。
		"sendNotify.js":     "//cron: 5 5 * * *\nconst $ = new Env('通知库');\n",
		"utils/date.js":     "//cron: 6 6 * * *\nmodule.exports = {};\n",
		"JS_USER_AGENTS.js": "//cron: 7 7 * * *\nmodule.exports = [];\n",
		// 两边都没命中 → 不建任务
		"other_task.js": "//cron: 30 8 * * *\nconst $ = new Env('无关脚本');\n",
		"README.md":     "readme\n",
		// 命中黑名单 → 不建任务
		"backUp/jd_old.js": "//cron: 40 8 * * *\nconst $ = new Env('旧备份');\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(scriptsRoot, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	sub := &model.Subscription{
		Name:        "jdpro-depend",
		Type:        model.SubTypeGitRepo,
		URL:         "https://github.com/6dylan6/jdpro.git",
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

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)

	byBase := map[string]model.Task{}
	for _, task := range tasks {
		byBase[filepath.Base(task.Command)] = task
	}
	for _, want := range []string{"jd_bean_change.js", "jx_sign.js", "jddj_bean.js"} {
		if _, ok := byBase[want]; !ok {
			t.Errorf("白名单文件 %s 应建任务\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"sendNotify.js", "date.js", "JS_USER_AGENTS.js", "other_task.js", "jd_old.js"} {
		if _, ok := byBase[unwanted]; ok {
			t.Errorf("%s 不应建任务（依赖文件/黑名单/白名单未命中）\n%s", unwanted, joined)
		}
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d\n%s", len(tasks), joined)
	}

	// 静默失配最难查：依赖命中了哪些文件、这些文件不会建任务，必须写进日志。
	if !strings.Contains(joined, "[依赖文件]") {
		t.Fatalf("日志应有 [依赖文件] 段落\n%s", joined)
	}
	if !strings.Contains(joined, "不会建成定时任务") {
		t.Fatalf("日志应说明依赖文件不会建成定时任务\n%s", joined)
	}
	for _, name := range []string{"sendNotify.js", "utils/date.js", "JS_USER_AGENTS.js"} {
		if !strings.Contains(joined, name) {
			t.Errorf("日志应点名依赖文件 %s\n%s", name, joined)
		}
	}
}

// 回归守卫：兜底 #2（白名单全落空 → 忽略过滤规则）不能把依赖文件变成定时任务。
// 依赖规则改成功能性之后，辅助库文件才开始落盘；如果它们还留在候选集合里，
// 白名单一填错，sendNotify.js / utils/*.js 会被兜底逻辑一次性全建成任务。
func TestSubscriptionDependencyFilesStayOutOfWhitelistFallback(t *testing.T) {
	testutil.SetupTestEnv(t)

	saveDir := "depend_fallback"
	scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "utils"), 0o755); err != nil {
		t.Fatalf("create dirs: %v", err)
	}
	os.WriteFile(filepath.Join(scriptsRoot, "sendNotify.js"), []byte("//cron: 5 5 * * *\nmodule.exports={};\n"), 0o644)
	os.WriteFile(filepath.Join(scriptsRoot, "utils", "date.js"), []byte("//cron: 6 6 * * *\nmodule.exports={};\n"), 0o644)

	sub := &model.Subscription{
		Name: "depend-fallback", Type: model.SubTypeGitRepo,
		URL: "https://github.com/u/r.git", SaveDir: saveDir,
		// 白名单填了个仓库里根本不存在的片段 → 触发兜底 #2
		Whitelist:   "definitely_not_here",
		DependOn:    "sendNotify|utils",
		AutoAddTask: true, Enabled: true,
	}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	if len(tasks) != 0 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("依赖文件被兜底逻辑建成了任务, got %d 个任务\n%s", len(tasks), strings.Join(logs, "\n"))
	}
	// 这种「只有依赖命中、白名单一个没命中」的误配要单独点名，否则用户只看到任务列表空。
	if !strings.Contains(strings.Join(logs, "\n"), "请确认主脚本的文件名片段是否填在「白名单」里") {
		t.Errorf("应提示白名单与依赖规则填反了\n%s", strings.Join(logs, "\n"))
	}
}

// 回归守卫：白名单为空时，依赖规则一个任务都不能少建。
// 用「同一份文件、两个只差 depend_on 的订阅」对拍候选集合，比对拍任务表更直接，
// 也不受任务去重/复用逻辑干扰。
func TestSyncSubscriptionTasksDependencyNoOpWhenWhitelistEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 三个文件名刻意都命中依赖规则（USER / JD / function）；白名单为空时
	// matchesSubscriptionWhitelist 恒 true，它们一个都不能被判成「仅命中依赖」。
	files := map[string]string{
		"JS_USER_AGENTS.js": "//cron: 1 1 * * *\nconst $=new Env('a');\n",
		"JD_helper.js":      "//cron: 2 2 * * *\nconst $=new Env('b');\n",
		"function_box.js":   "//cron: 3 3 * * *\nconst $=new Env('c');\n",
	}

	newSub := func(saveDir, dependOn string) *model.Subscription {
		scriptsRoot := filepath.Join(config.C.Data.ScriptsDir, saveDir)
		if err := os.MkdirAll(scriptsRoot, 0o755); err != nil {
			t.Fatalf("create %s: %v", saveDir, err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(scriptsRoot, name), []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		return &model.Subscription{
			Name: "depend-noop", Type: model.SubTypeGitRepo,
			URL: "https://github.com/u/r.git", SaveDir: saveDir,
			Whitelist: "", DependOn: dependOn,
			AutoAddTask: true, Enabled: true,
		}
	}

	options := subscriptionTaskSyncOptions{
		autoAdd:     true,
		defaultCron: FallbackSubscriptionCron,
		allowedExts: map[string]bool{".js": true},
	}

	baseline, baselineDeps := collectSubscriptionTaskCandidates(newSub("noop_without", ""), options)
	if len(baseline) != len(files) {
		t.Fatalf("基线：白名单为空应产出 %d 个候选, got %d", len(files), len(baseline))
	}
	if len(baselineDeps) != 0 {
		t.Fatalf("未配置依赖规则时不应有依赖文件, got %#v", baselineDeps)
	}

	withDepend, deps := collectSubscriptionTaskCandidates(newSub("noop_with", qlRepoDependOn), options)
	if len(deps) != 0 {
		t.Fatalf("白名单为空时不应有任何文件被判成「仅命中依赖」, got %#v", deps)
	}
	if len(withDepend) != len(baseline) {
		t.Fatalf("白名单为空时 depend_on 改变了候选数量: baseline=%d got=%d", len(baseline), len(withDepend))
	}
	// 逐条对拍：命令与 cron 都必须完全一致（只有 saveDir 不同）。
	baselineByBase := map[string]string{}
	for command, candidate := range baseline {
		baselineByBase[filepath.Base(command)] = candidate.CronExpression
	}
	for command, candidate := range withDepend {
		base := filepath.Base(command)
		want, ok := baselineByBase[base]
		if !ok {
			t.Errorf("多出候选 %s", base)
			continue
		}
		if want != candidate.CronExpression {
			t.Errorf("%s cron 不一致: baseline=%q got=%q", base, want, candidate.CronExpression)
		}
	}
}

// 真机 git 验证：用户那条真实指令下，仅命中依赖的辅助文件必须真的被检出到工作区，
// 而白名单/依赖都没命中的文件不能落盘。这是「拉了库但任务一跑就缺依赖」的直接修复点。
func TestPullGitRepoWithCallbackChecksOutDependencyFiles(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	for _, dir := range []string{"utils", "backUp"} {
		if err := os.MkdirAll(filepath.Join(worktreeDir, dir), 0o755); err != nil {
			t.Fatalf("create %s dir: %v", dir, err)
		}
	}
	repoFiles := map[string]string{
		"jd_bean_change.js": "console.log('jd')\n",
		"jx_sign.js":        "console.log('jx')\n",
		"jddj_bean.js":      "console.log('jddj')\n",
		"sendNotify.js":     "module.exports = {}\n",
		"utils/date.js":     "module.exports = {}\n",
		"JS_USER_AGENTS.js": "module.exports = []\n",
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
		SaveDir:   "jdpro-depend-repo",
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
	// 白名单文件照常检出（不能被依赖规则挤掉）
	for _, name := range []string{"jd_bean_change.js", "jx_sign.js", "jddj_bean.js"} {
		if _, statErr := os.Stat(filepath.Join(destDir, name)); statErr != nil {
			t.Errorf("白名单文件 %s 应被检出: %v", name, statErr)
		}
	}
	// 核心断言：改造前这两个辅助文件不匹配白名单 → 不落盘 → 主脚本一跑就缺依赖。
	for _, name := range []string{"sendNotify.js", "JS_USER_AGENTS.js"} {
		if _, statErr := os.Stat(filepath.Join(destDir, name)); statErr != nil {
			t.Errorf("依赖文件 %s 应被检出: %v", name, statErr)
		}
	}
	// 两边都没命中的仍然不落盘，sparse-checkout 的收敛效果不能丢
	for _, name := range []string{"other_task.js", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(destDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s 不应被检出, stat err=%v", name, statErr)
		}
	}
	// 下面两条以前只能「记录不断言」，因为 gitignore 的 `*` 不跨 `/`；
	// 现在每个片段都成对下发了 `**/*x*/**`，可以真正断言了。
	//   utils/date.js —— 依赖片段 `utils` 现在同时下发 `**/*utils*` 与 `**/*utils*/**`，
	//     目录里的文件必须真的落盘，否则主脚本 require('./utils/xxx') 一跑就报错。
	if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash("utils/date.js"))); statErr != nil {
		t.Errorf("依赖片段命中目录时，目录里的 utils/date.js 也应被检出: %v", statErr)
	}
	//   backUp/jd_old.js —— `!**/*backUp*/**` 现在能压住正向的 `**/*jd_*`（最后匹配者胜出）。
	if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash("backUp/jd_old.js"))); !os.IsNotExist(statErr) {
		t.Errorf("黑名单目录里的 backUp/jd_old.js 不应被检出, stat err=%v", statErr)
	}

	// 检出之后立刻跑一遍任务同步：辅助文件在盘上，但不能变成定时任务。
	sub.AutoAddTask = true
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	InitSchedulerV2()
	defer ShutdownSchedulerV2()

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	byBase := map[string]bool{}
	for _, task := range tasks {
		byBase[filepath.Base(task.Command)] = true
	}
	for _, want := range []string{"jd_bean_change.js", "jx_sign.js", "jddj_bean.js"} {
		if !byBase[want] {
			t.Errorf("白名单文件 %s 应建任务\n%s", want, strings.Join(logs, "\n"))
		}
	}
	for _, unwanted := range []string{"sendNotify.js", "date.js", "JS_USER_AGENTS.js"} {
		if byBase[unwanted] {
			t.Errorf("依赖文件 %s 不应建任务\n%s", unwanted, strings.Join(logs, "\n"))
		}
	}
}
