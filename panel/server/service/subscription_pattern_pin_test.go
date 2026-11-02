package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 本文件把「普通片段」配置在 #129（白名单 / 黑名单 / 依赖规则支持正则）改造前的行为逐项钉住：
//   - 下发给 git sparse-checkout 的规则（逐条、逐字节）；
//   - 告警的条数与类别（`[提示]` / `[警告]` / `[依赖规则]`，正文允许改写）；
//   - Go 侧六个判定对一组固定路径的结果；
//   - 在一个固定脚本目录上扫描出来的任务候选与「仅依赖」文件（含兜底 #2）。
//
// golden（subscription_pattern_pin_golden_test.go）是在改造前的实现（HEAD 52a8320）上跑出来的，
// 改造后必须完全一致：#129 只允许「含正则触发字符（^ $ ( ) [ ] { } ? \ 或 .* .+）的片段」改变行为，
// 普通片段一个字节都不许动。唯一的例外是 subpath-depend 的 managed / candidates：那是有意修正的旧缺陷
// （子目录外的依赖文件被建成定时任务），见 golden 里该条的注释。
//
// 注意两类「看起来像正则、但必须钉死」的配置也在这里：
//   - 指定子目录（sub_path）不是正则字段，含 `[` 的子目录保持原来的「退回完整检出」；
//   - 单独的 `+`、`.`、glob 式 `*`（c++、a.b、jd_*.js、*.js）不是正则触发字符。

type subscriptionPinConfig struct {
	name string
	sub  model.Subscription
}

type subscriptionPinResult struct {
	patterns                []string
	warningTags             []string
	hasNonWildcardWhitelist bool
	whitelist               []string
	blacklistExcluded       []string
	filters                 []string
	depend                  []string
	dependOnly              []string
	managed                 []string
	candidates              []string
	scanDependOnly          []string
}

// qlRepoPlainDependOn 是 qlRepoDependOn 去掉 `^jd[^_]` 之后剩下的普通片段：
// 改造后这五段的匹配结果必须与改造前逐条一致。
const qlRepoPlainDependOn = "USER|JD|function|sendNotify|utils"

var subscriptionPinConfigs = []subscriptionPinConfig{
	{"ql-plain", model.Subscription{Whitelist: qlRepoWhitelist, Blacklist: qlRepoBlacklist, DependOn: qlRepoPlainDependOn}},
	{"ql-whitelist-blacklist", model.Subscription{Whitelist: qlRepoWhitelist, Blacklist: qlRepoBlacklist}},
	{"comma", model.Subscription{Whitelist: "jd_,jx_,jddj_", Blacklist: "backUp"}},
	{"spaces-and-empty-segments", model.Subscription{Whitelist: " jd_ , jx_ ||", Blacklist: "jd_old||,Archive"}},
	{"glob-star", model.Subscription{Whitelist: "jd_*.js"}},
	{"glob-ext", model.Subscription{Whitelist: "*.js"}},
	{"single-keep", model.Subscription{Whitelist: "keep_task"}},
	{"blacklist-only", model.Subscription{Blacklist: "backUp|Archive|.github"}},
	{"subpath-depend", model.Subscription{SubPath: "scripts/daily", DependOn: "sendNotify"}},
	{"subpath-multi-whitelist", model.Subscription{SubPath: "scripts/daily|tools", Whitelist: "jd_"}},
	{"subpath-trailing-slash", model.Subscription{SubPath: "scripts/daily/", Whitelist: "keep"}},
	{"subpath-risky", model.Subscription{SubPath: "scripts/day[0-9]", Blacklist: "backUp"}},
	{"subpath-risky-depend", model.Subscription{SubPath: "scripts/day[0-9]", DependOn: "sendNotify"}},
	{"whitelist-depend-overlap", model.Subscription{Whitelist: "utils", DependOn: "utils|sendNotify"}},
	{"wildcard-star", model.Subscription{Whitelist: "*"}},
	{"wildcard-all", model.Subscription{Whitelist: "all", Blacklist: "*"}},
	{"wildcard-mixed", model.Subscription{Whitelist: "*|jd_"}},
	{"wildcard-dot-star", model.Subscription{Whitelist: ".*", DependOn: "*.*|sendNotify"}},
	{"depend-note", model.Subscription{Whitelist: "jd_", DependOn: "依赖 utils 库，迁移自青龙"}},
	{"depend-half-note", model.Subscription{Whitelist: "jd_", DependOn: "迁移自青龙,sendNotify"}},
	{"depend-long-note", model.Subscription{Whitelist: "jd_", DependOn: strings.Repeat("a", subscriptionDependencyPatternMaxLen+1)}},
	{"normalization-prefix", model.Subscription{Whitelist: "./jd_|/jx_", Blacklist: "./backUp", DependOn: "./utils/"}},
	{"normalization-dot-slash-star", model.Subscription{Whitelist: "./*"}},
	{"plain-plus-dot", model.Subscription{Whitelist: "c++|a.b|x+y|jd+", Blacklist: "node_modules"}},
	{"depend-wildcard", model.Subscription{Whitelist: "jd_", DependOn: "*|sendNotify"}},
	{"full-checkout-subpath", model.Subscription{FullCheckout: true, SubPath: "qinglong/DefaultTasks", Whitelist: "bili_task_", Blacklist: "backUp"}},
	{"full-checkout-subpath-only", model.Subscription{FullCheckout: true, SubPath: "qinglong/DefaultTasks"}},
	{"full-checkout-bare", model.Subscription{FullCheckout: true, DependOn: "sendNotify"}},
	{"depend-only", model.Subscription{DependOn: "sendNotify|utils"}},
	{"whitelist-blacklist-depend", model.Subscription{Whitelist: "jd_", Blacklist: "backUp", DependOn: "sendNotify"}},
	{"whitelist-dir-path", model.Subscription{Whitelist: "scripts/daily", Blacklist: "scripts/other"}},
	{"whitelist-equals-blacklist", model.Subscription{Whitelist: "jd_", Blacklist: "jd_"}},
	{"whitelist-case-sensitive", model.Subscription{Whitelist: "JD_"}},
	{"whitelist-no-match", model.Subscription{Whitelist: "definitely_not_here", DependOn: "sendNotify|utils"}},
	{"whitelist-no-match-blacklist", model.Subscription{Whitelist: "definitely_not_here", Blacklist: "backUp"}},
	{"empty", model.Subscription{}},
}

// subscriptionPinPaths 用正斜杠写；判定函数内部会 ToSlash，Windows 与 Linux 结果一致。
var subscriptionPinPaths = []string{
	"jd_bean_change.js",
	"jx_sign.js",
	"jddj_bean.js",
	"jdCookie.js",
	"sendNotify.js",
	"utils/date.js",
	"utils/nested/http.js",
	"JS_USER_AGENTS.js",
	"JD_helper.js",
	"JD_UPPER.JS",
	"function_box.js",
	"other_task.js",
	"README.md",
	"backUp/jd_old.js",
	"backUp/nested/jd_older.js",
	"jd_group/helper.js",
	"jd_group/backUp/old.js",
	"scripts/daily/keep.js",
	"scripts/daily/keep_task.py",
	"scripts/other/skip.js",
	"scripts/jdCookie.js",
	"tools/build.sh",
	"qinglong/DefaultTasks/bili_task_manga.sh",
	"qinglong/DefaultTasksExtra/x.sh",
	".github/workflows/c.yml",
	"Archive/b.js",
	"node_modules/x.js",
	"c++/a.js",
	"a.b.js",
	"x+y.js",
	"jd+x.js",
	"keep_task.py",
}

var subscriptionPinExts = map[string]bool{".js": true, ".sh": true, ".py": true}

const subscriptionPinSaveDir = "pin-repo"

func subscriptionWarningTag(w string) string {
	if strings.HasPrefix(w, "[") {
		if end := strings.Index(w, "]"); end > 0 {
			return w[:end+1]
		}
	}
	return w
}

// writeSubscriptionPinFixture 在脚本目录下铺一份与 subscriptionPinPaths 相同的仓库布局。
// 每个脚本都带 cron 头：这样「辅助脚本没有 cron 就不建任务」那条规则不会干扰结果，
// 候选集合完全由白 / 黑名单、依赖规则、子目录护栏和兜底 #2 决定。
func writeSubscriptionPinFixture(t *testing.T) {
	t.Helper()
	root := filepath.Join(config.C.Data.ScriptsDir, subscriptionPinSaveDir)
	for _, rel := range subscriptionPinPaths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create dir for %s: %v", rel, err)
		}
		body := "readme\n"
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".js":
			body = "//cron: 1 1 * * *\nconsole.log('x')\n"
		case ".sh", ".py":
			body = "# cron: 1 1 * * *\necho x\n"
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func evaluateSubscriptionPinConfig(sub model.Subscription) subscriptionPinResult {
	var r subscriptionPinResult
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(&sub)
	r.patterns = patterns
	for _, w := range warnings {
		r.warningTags = append(r.warningTags, subscriptionWarningTag(w))
	}
	r.hasNonWildcardWhitelist = hasNonWildcardSubscriptionFilter(sub.Whitelist)
	for _, p := range subscriptionPinPaths {
		if matchesSubscriptionWhitelist(&sub, p) {
			r.whitelist = append(r.whitelist, p)
		}
		if !checkBlacklist(&sub, p) {
			r.blacklistExcluded = append(r.blacklistExcluded, p)
		}
		if matchesSubscriptionFilters(&sub, p) {
			r.filters = append(r.filters, p)
		}
		if matchesSubscriptionDependency(&sub, p) {
			r.depend = append(r.depend, p)
		}
		if isSubscriptionDependencyOnlyFile(&sub, p) {
			r.dependOnly = append(r.dependOnly, p)
		}
		if shouldManageSubscriptionFile(&sub, p, subscriptionPinExts) {
			r.managed = append(r.managed, p)
		}
	}

	sub.SaveDir = subscriptionPinSaveDir
	// 默认规则留空（出厂口径）：fixture 里每个脚本都带 cron 头，候选集合不受影响。
	candidates, deps := collectSubscriptionTaskCandidates(&sub, subscriptionTaskSyncOptions{
		autoAdd:     true,
		allowedExts: subscriptionPinExts,
	})
	for command := range candidates {
		r.candidates = append(r.candidates, filepath.ToSlash(command))
	}
	sort.Strings(r.candidates)
	for _, dep := range deps {
		r.scanDependOnly = append(r.scanDependOnly, filepath.ToSlash(dep))
	}
	sort.Strings(r.scanDependOnly)
	return r
}

func assertSubscriptionPinSlice(t *testing.T, name, field string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if len(got) != len(want) {
		t.Errorf("[%s] %s = %#v, want %#v", name, field, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%s] %s = %#v, want %#v", name, field, got, want)
			return
		}
	}
}

func TestSubscriptionPlainFilterBehaviorPinned(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeSubscriptionPinFixture(t)

	if len(subscriptionPinGolden) != len(subscriptionPinConfigs) {
		t.Fatalf("golden 条数 %d 与配置条数 %d 不一致", len(subscriptionPinGolden), len(subscriptionPinConfigs))
	}
	for _, c := range subscriptionPinConfigs {
		want, ok := subscriptionPinGolden[c.name]
		if !ok {
			t.Fatalf("配置 %q 缺少 golden", c.name)
		}
		got := evaluateSubscriptionPinConfig(c.sub)
		assertSubscriptionPinSlice(t, c.name, "patterns", got.patterns, want.patterns)
		assertSubscriptionPinSlice(t, c.name, "warningTags", got.warningTags, want.warningTags)
		if got.hasNonWildcardWhitelist != want.hasNonWildcardWhitelist {
			t.Errorf("[%s] hasNonWildcardWhitelist = %v, want %v", c.name, got.hasNonWildcardWhitelist, want.hasNonWildcardWhitelist)
		}
		assertSubscriptionPinSlice(t, c.name, "whitelist", got.whitelist, want.whitelist)
		assertSubscriptionPinSlice(t, c.name, "blacklistExcluded", got.blacklistExcluded, want.blacklistExcluded)
		assertSubscriptionPinSlice(t, c.name, "filters", got.filters, want.filters)
		assertSubscriptionPinSlice(t, c.name, "depend", got.depend, want.depend)
		assertSubscriptionPinSlice(t, c.name, "dependOnly", got.dependOnly, want.dependOnly)
		assertSubscriptionPinSlice(t, c.name, "managed", got.managed, want.managed)
		assertSubscriptionPinSlice(t, c.name, "candidates", got.candidates, want.candidates)
		assertSubscriptionPinSlice(t, c.name, "scanDependOnly", got.scanDependOnly, want.scanDependOnly)
	}
}
