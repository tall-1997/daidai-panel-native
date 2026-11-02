package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// #129：订阅白名单 / 黑名单 / 依赖规则支持正则（逐片段自动识别）。
// 普通片段「一个字节都不变」由 subscription_pattern_pin_test.go 钉着；本文件只管新语义：
// 拆分器、片段分类、正则匹配口径、非法正则（保存时与拉取时）、sparse 档 1、
// 放宽检出不扩大建任务的集合，以及两条真实 git 用例。

func TestSplitSubscriptionPatternSegmentsRespectsGroupsAndEscapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"圆括号里的竖线不拆", "(jd|jx)_|sendNotify", []string{"(jd|jx)_", "sendNotify"}},
		{"花括号里的逗号不拆", `^jd_\w{1,3}\.js,utils`, []string{`^jd_\w{1,3}\.js`, "utils"}},
		{"方括号里的分隔符不拆", "[,|]x|y", []string{"[,|]x", "y"}},
		{"紧跟左方括号的右方括号是字面量", "[]|]x|y", []string{"[]|]x", "y"}},
		{"取反类里紧跟的右方括号是字面量", "[^]|]x|y", []string{"[^]|]x", "y"}},
		{"具名字符类里的右方括号不结束外层", "[[:alpha:]|_]x|y", []string{"[[:alpha:]|_]x", "y"}},
		{"转义的竖线不拆", `a\|b|c`, []string{`a\|b`, "c"}},
		{"转义的逗号不拆", `a\,b,c`, []string{`a\,b`, "c"}},
		{"嵌套括号", "((jd|jx)_(a|b))|z", []string{"((jd|jx)_(a|b))", "z"}},
		{"用户真实依赖参数", qlRepoDependOn, []string{"^jd[^_]", "USER", "JD", "function", "sendNotify", "utils"}},
		// 没配对的括号当普通字符照常拆：这类片段本来就编译不过，拆开后只坏它自己那一段。
		{"没闭合的方括号", "^jd[|USER", []string{"^jd[", "USER"}},
		{"没闭合的圆括号", "(jd|USER", []string{"(jd", "USER"}},
		{"落单的右括号", "jd)|x", []string{"jd)", "x"}},
		{"末尾的反斜杠", `jd\`, []string{`jd\`}},
		{"不去重、保留顺序", "a|b|a", []string{"a", "b", "a"}},
		{"空段丢弃并去掉空白", " (a|b) ,, c |", []string{"(a|b)", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSubscriptionPatternSegments(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitSubscriptionPatternSegments(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// 不含 ( [ { \ 的输入，新拆分器必须与改造前按每个 `,` `|` 拆的结果逐字节一致。
func TestSplitSubscriptionFilterPatternsPlainInputUnchanged(t *testing.T) {
	inputs := []string{
		qlRepoWhitelist, qlRepoBlacklist, qlRepoPlainDependOn,
		"jd_,jx_,jddj_", " jd_ , jx_ ", "jd_|jx_,jddj_", "jd_||jx_", "|jd_|", ",jd_,", "jd_| |jx_", "|,|", "", "   ",
		"jd_,jd_|jd_", "*", "c++|a.b|x+y", "jd_*.js", "*.js|.github", "a)b|c]d|e}f",
		"依赖 utils 库，迁移自青龙,sendNotify",
		// 含正则触发字符、但不含 ( [ { \ ：拆法同样不变
		"$HOME|^x|jd?|a.*b",
	}
	for _, raw := range inputs {
		want := splitSubscriptionPlainPatterns(raw)
		got := splitSubscriptionFilterPatterns(raw)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("raw %q: got %#v, want %#v", raw, got, want)
		}
	}
}

func TestIsSubscriptionRegexFragment(t *testing.T) {
	for _, fragment := range []string{"^jd[^_]", "jd_$", "(a)", "a{2}", "jd?", `utils\.js`, "jd.*", "jd.+", "[0-9]", "(?i)readme"} {
		if !isSubscriptionRegexFragment(fragment) {
			t.Errorf("%q 应按正则识别", fragment)
		}
	}
	// 单独的 + . * 不是触发字符：这些存量写法必须保持子串包含。
	for _, fragment := range []string{"jd_", "c++", "a.b", "jd_*.js", "*.js", ".github", "x+y", "jd*", "sendNotify", "utils/", "backUp"} {
		if isSubscriptionRegexFragment(fragment) {
			t.Errorf("%q 不应按正则识别", fragment)
		}
	}
}

// 正则片段匹配仓库相对路径（正斜杠）、不锚定，与青龙 `egrep` 同口径；白名单、黑名单、依赖规则共用一套。
func TestSubscriptionRegexFragmentsMatchRepoRelativePath(t *testing.T) {
	sub := &model.Subscription{Whitelist: `^jd[^_]|Cook(ie)?\.js$|(?i)^readme`}
	cases := []struct {
		path string
		want bool
	}{
		{"jdCookie.js", true},
		{"jdPublic.js", true},
		// 第三个字符是 _，^jd[^_] 不命中，其余两段也不命中
		{"jd_bean_change.js", false},
		// 不锚定：Cook(ie)?\.js$ 命中子目录里的文件
		{"scripts/jdCookie.js", true},
		{"scripts/jd_x.js", false},
		// (?i) 忽略大小写；^ 锚在仓库相对路径开头
		{"README.md", true},
		{"docs/readme.md", false},
		// Windows 风格的路径先转成正斜杠再匹配
		{filepath.FromSlash("scripts/jdCookie.js"), true},
	}
	for _, tc := range cases {
		if got := matchesSubscriptionWhitelist(sub, tc.path); got != tc.want {
			t.Errorf("matchesSubscriptionWhitelist(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}

	blacklist := &model.Subscription{Blacklist: "back[Uu]p|.github"}
	for _, path := range []string{"backUp/a.js", "backup/b.js", "x/.github/c.yml"} {
		if checkBlacklist(blacklist, path) {
			t.Errorf("黑名单应排除 %q", path)
		}
	}
	// 正则大小写敏感（与青龙一致）：Backup 不含 back
	if !checkBlacklist(blacklist, "Backup/a.js") {
		t.Error("Backup/a.js 不应被 back[Uu]p 排除")
	}

	depend := &model.Subscription{DependOn: "^jd[^_]"}
	if !matchesSubscriptionDependency(depend, "jdCookie.js") || matchesSubscriptionDependency(depend, "utils/jdCookie.js") {
		t.Error("依赖规则的 ^jd[^_] 应只命中仓库根下的 jdCookie.js")
	}
}

// 正则片段只 TrimSpace，不走 normalizeSubscriptionFilterTarget：Windows 上 ToSlash 会把 `\.` 改写成 `/.`，
// `utils\.js`（字面量的点）就变成了「utils 目录下任意一个字符再跟 js」；去掉 `./` 也会改写原意。
func TestSubscriptionRegexFragmentKeepsBackslashVerbatim(t *testing.T) {
	set := compileSubscriptionPatternSet(`utils\.js`, subscriptionPatternWhitelist)
	if got := set.regexFragments(); !reflect.DeepEqual(got, []string{`utils\.js`}) {
		t.Fatalf("正则片段应保留原文, got %#v", got)
	}
	if !set.whitelistMatches("lib/utils.js") {
		t.Error(`utils\.js 应命中 lib/utils.js`)
	}
	if set.whitelistMatches("utils/ajs") {
		t.Error(`utils\.js 不应命中 utils/ajs（说明反斜杠被改写成了 /）`)
	}

	// 普通片段照旧规整（去掉 ./），正则片段保留原文
	patterns, _ := splitSubscriptionDependencyPatterns(`./^jd[^_]|./utils/`)
	if !reflect.DeepEqual(patterns, []string{"./^jd[^_]", "utils/"}) {
		t.Fatalf("patterns = %#v", patterns)
	}
}

// 通配写法与依赖规则的文字备注判定照旧先于分类执行。
func TestSubscriptionWildcardAndNoteChecksRunBeforeRegexClassification(t *testing.T) {
	patterns, notes := splitSubscriptionDependencyPatterns("^京东|^jd [a-z]|^jd[^_]|.*|*.*")
	if !reflect.DeepEqual(patterns, []string{"^jd[^_]"}) {
		t.Errorf("patterns = %#v", patterns)
	}
	if !reflect.DeepEqual(notes, []string{"^京东", "^jd [a-z]"}) {
		t.Errorf("notes = %#v", notes)
	}
	// `.*` 含 `.*` 组合，但它首先是「全部」的通配写法：白名单填它等于不过滤。
	if !matchesSubscriptionWhitelist(&model.Subscription{Whitelist: ".*"}, "anything/at/all.js") {
		t.Error("白名单 .* 应视为通配、全部命中")
	}
}

func TestSubscriptionInvalidRegexFragments(t *testing.T) {
	t.Run("保存时校验点名字段、第几段、RE2 报错与转义提示", func(t *testing.T) {
		err := ValidateSubscriptionFilterField(SubscriptionFilterFieldDependOn, "sendNotify|^jd[")
		if err == nil {
			t.Fatal("非法正则应返回错误")
		}
		for _, keyword := range []string{"依赖规则", "第 2 段", "`^jd[`", "missing closing ]", `要写字面量请用 \ 转义`} {
			if !strings.Contains(err.Error(), keyword) {
				t.Errorf("错误文案应包含 %q, got %q", keyword, err.Error())
			}
		}
	})

	t.Run("合法正则、普通片段、通配写法、依赖备注都放行", func(t *testing.T) {
		if err := ValidateSubscriptionFilterFields("^jd[^_]|jd_|*", "back[Uu]p|.github", "依赖 [x 库|sendNotify|(?i)^utils"); err != nil {
			t.Fatalf("不该报错: %v", err)
		}
		if err := ValidateSubscriptionFilterField("sub_path", "scripts/day[0-9"); err != nil {
			t.Fatalf("指定子目录不是正则字段，不该校验: %v", err)
		}
	})

	t.Run("三个字段依次校验，报第一处", func(t *testing.T) {
		err := ValidateSubscriptionFilterFields("jd_", "(backUp", "^jd[")
		if err == nil || !strings.Contains(err.Error(), "黑名单第 1 段") {
			t.Fatalf("应先报黑名单第 1 段, got %v", err)
		}
	})

	t.Run("拉取时逐条跳过并告警，同字段其余片段照常生效", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_|^jd["}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"**/*jd_*", "**/*jd_*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "[警告]") || !strings.Contains(warnings[0], "白名单第 2 段 `^jd[`") {
			t.Fatalf("应对非法片段打 [警告] 并点名, got %#v", warnings)
		}
		if !matchesSubscriptionWhitelist(sub, "jd_x.js") || matchesSubscriptionWhitelist(sub, "jdCookie.js") {
			t.Error("jd_ 照常生效，非法片段一个文件都不命中")
		}
	})

	t.Run("黑名单、依赖规则的非法片段逐条跳过", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_", Blacklist: "back[Uu|Archive", DependOn: "(sendNotify|utils"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{
			"**/*jd_*", "**/*jd_*/**",
			"**/*utils*", "**/*utils*/**",
			"!**/*Archive*", "!**/*Archive*/**",
		}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		joined := strings.Join(warnings, "\n")
		for _, keyword := range []string{"依赖规则第 1 段 `(sendNotify`", "黑名单第 1 段 `back[Uu`"} {
			if !strings.Contains(joined, keyword) {
				t.Errorf("告警应包含 %q, got %#v", keyword, warnings)
			}
		}
		if !checkBlacklist(sub, "backUp/x.js") || checkBlacklist(sub, "Archive/x.js") {
			t.Error("非法黑名单片段不排除任何文件，Archive 照常排除")
		}
		if matchesSubscriptionDependency(sub, "sendNotify.js") || !matchesSubscriptionDependency(sub, "utils/date.js") {
			t.Error("非法依赖片段不命中任何文件，utils 照常命中")
		}
	})

	t.Run("完整检出也点名非法片段", func(t *testing.T) {
		sub := &model.Subscription{FullCheckout: true, Whitelist: "^jd["}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("完整检出不下发规则, got %#v", patterns)
		}
		if len(warnings) != 2 || !strings.Contains(warnings[0], "整个仓库") || !strings.Contains(warnings[1], "`^jd[`") {
			t.Fatalf("应先打完整检出提示、再点名非法片段, got %#v", warnings)
		}
	})

	t.Run("白名单一段有效的都不剩：维持整仓 + 兜底 #2", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		writeSubscriptionPinFixture(t)

		sub := model.Subscription{Whitelist: "^jd["}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(&sub)
		if len(patterns) != 0 {
			t.Fatalf("白名单没有可用片段时应检出完整仓库, got %#v", patterns)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "没有可用的片段") {
			t.Fatalf("应说明白名单已经没有可用片段, got %#v", warnings)
		}
		// 与改造前（把 ^jd[ 当字面量、一个都匹配不到）同样的结果：兜底 #2 忽略白 / 黑名单，全部建任务。
		got := evaluateSubscriptionPinConfig(sub)
		assertSubscriptionPinSlice(t, "invalid-only-whitelist", "candidates", got.candidates, subscriptionPinGolden["empty"].candidates)
	})
}

func TestBuildSparseCheckoutPatternsRegexFragmentsUseFullCheckout(t *testing.T) {
	t.Run("白名单正则 + 黑名单普通：整仓 + 排除规则", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "^jd[^_]", Blacklist: "backUp"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"*", "!**/*backUp*", "!**/*backUp*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "白名单") || !strings.Contains(warnings[0], "正则") {
			t.Fatalf("应说明白名单的正则片段让本次检出完整仓库, got %#v", warnings)
		}
	})

	t.Run("依赖正则 + 指定子目录：整仓，告警说明建任务仍限在子目录", func(t *testing.T) {
		sub := &model.Subscription{SubPath: "scripts/daily", DependOn: "^jd[^_]|sendNotify"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("依赖含正则片段时应放宽成完整检出, got %#v", patterns)
		}
		joined := strings.Join(warnings, "\n")
		for _, keyword := range []string{"^jd[^_]", "只给子目录里的脚本建定时任务"} {
			if !strings.Contains(joined, keyword) {
				t.Errorf("告警应包含 %q, got %#v", keyword, warnings)
			}
		}
	})

	t.Run("包含侧本来就整仓时依赖正则不重复放宽", func(t *testing.T) {
		sub := &model.Subscription{DependOn: "^jd[^_]|sendNotify"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("白名单为空本来就整仓, got %#v", patterns)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "依赖规则无需额外生效") {
			t.Fatalf("应沿用「依赖规则无需额外生效」的提示, got %#v", warnings)
		}
	})

	t.Run("只有黑名单正则：不下发任何规则，由 Go 侧排除", func(t *testing.T) {
		sub := &model.Subscription{Blacklist: "back[Uu]p"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("黑名单正则表达不成排除规则, got %#v", patterns)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "back[Uu]p") || !strings.Contains(warnings[0], "排除在定时任务之外") {
			t.Fatalf("应说明黑名单正则由面板排除, got %#v", warnings)
		}
		if checkBlacklist(sub, "backUp/jd_old.js") {
			t.Error("Go 侧应按正则排除 backUp/jd_old.js")
		}
	})

	t.Run("白名单普通 + 依赖正则 + 黑名单普通与正则混合", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_", Blacklist: "backUp|^Archive/", DependOn: "^jd[^_]"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"*", "!**/*backUp*", "!**/*backUp*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		if len(warnings) != 2 || !strings.Contains(warnings[0], "依赖规则") || !strings.Contains(warnings[1], "^Archive/") {
			t.Fatalf("应依次说明依赖正则与黑名单正则, got %#v", warnings)
		}
	})
}

// 依赖规则的正则片段把检出放宽成整仓，只为了多落几个依赖文件，不能顺带扩大建任务的集合。
// 脚本目录用 pin 用例那份完整布局（相当于整仓都在盘上）。
func TestSubscriptionDependRegexWideningDoesNotGrowTaskSet(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeSubscriptionPinFixture(t)

	t.Run("子目录 + 依赖正则：只给子目录里的脚本建任务", func(t *testing.T) {
		sub := model.Subscription{SubPath: "scripts/daily", DependOn: "^jd[^_]"}
		got := evaluateSubscriptionPinConfig(sub)
		want := []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}
		assertSubscriptionPinSlice(t, "subpath-depend-regex", "candidates", got.candidates, want)
		// 日志里「扫描 X 个候选文件」走同一个匹配器，口径必须与候选一致。
		scriptsDir := filepath.Join(config.C.Data.ScriptsDir, subscriptionPinSaveDir)
		if n := countSubscriptionScriptFiles(scriptsDir, subscriptionPinExts, &sub); n != len(want) {
			t.Errorf("countSubscriptionScriptFiles = %d, want %d", n, len(want))
		}
	})

	t.Run("白名单普通 + 依赖正则：白名单一个没命中时不兜底", func(t *testing.T) {
		got := evaluateSubscriptionPinConfig(model.Subscription{Whitelist: "definitely_not_here", DependOn: "^jd[^_]"})
		if len(got.candidates) != 0 {
			t.Errorf("放宽检出后不能靠兜底 #2 把整仓脚本都建成任务, got %#v", got.candidates)
		}
		assertSubscriptionPinSlice(t, "whitelist-miss-depend-regex", "scanDependOnly", got.scanDependOnly,
			[]string{"jd+x.js", "jdCookie.js", "jddj_bean.js"})
	})

	t.Run("子目录 + 依赖正则 + 白名单没命中：兜底只在子目录里生效", func(t *testing.T) {
		got := evaluateSubscriptionPinConfig(model.Subscription{SubPath: "scripts/daily", Whitelist: "definitely_not_here", DependOn: "^jd[^_]"})
		want := []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}
		assertSubscriptionPinSlice(t, "subpath-whitelist-miss-depend-regex", "candidates", got.candidates, want)
	})

	// 指定子目录护栏必须与 git 解释同一条 sparse 规则的口径一致（末尾 `/`、glob、不带 `/` 的名字在任意层级匹配），
	// 否则放宽成整仓之后建任务的范围比改造前落盘的范围还小。复查实测：这三种写法改造前都能建出
	// scripts/daily 里的任务，放宽之后一个都不建。
	for _, tc := range []struct {
		subPath string
		want    []string
	}{
		{"scripts/daily/", []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}},
		{"scripts/*", []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py", "task pin-repo/scripts/jdCookie.js", "task pin-repo/scripts/other/skip.js"}},
		{"daily", []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}},
		{"**/daily/", []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}},
	} {
		t.Run("子目录写成 "+tc.subPath+" + 依赖正则：护栏与 sparse 同口径", func(t *testing.T) {
			got := evaluateSubscriptionPinConfig(model.Subscription{SubPath: tc.subPath, DependOn: "^jd[^_]"})
			assertSubscriptionPinSlice(t, "subpath-spelling-depend-regex", "candidates", got.candidates, tc.want)
		})
	}

	t.Run("白名单自己的正则片段让本次整仓检出时不兜底", func(t *testing.T) {
		// 改造前 `^nothing_here$` 被当成 gitignore 规则下发，一个文件都检不出来、一个任务都不建；
		// 改造后整仓在盘上，正则没命中就兜底会把整个仓库的脚本都建成任务。
		got := evaluateSubscriptionPinConfig(model.Subscription{Whitelist: "^nothing_here$"})
		if len(got.candidates) != 0 {
			t.Errorf("白名单的正则片段一个没命中时不能靠兜底 #2 把整仓脚本都建成任务, got %#v", got.candidates)
		}
	})

	t.Run("完整检出开关打开时白名单正则没命中：兜底 #2 照旧", func(t *testing.T) {
		// 完整检出下整仓本来就在盘上（改造前也一样），兜底与改造前一致。
		got := evaluateSubscriptionPinConfig(model.Subscription{FullCheckout: true, Whitelist: "^nothing_here$"})
		assertSubscriptionPinSlice(t, "full-checkout-whitelist-regex-miss", "candidates", got.candidates, subscriptionPinGolden["empty"].candidates)
	})
}

// 完整检出的子目录护栏与依赖正则放宽共用 subscriptionSubPathCovers：末尾带 `/`、glob 写法不能让护栏把子目录里的脚本也挡掉。
func TestSubscriptionSubPathScopeFollowsSparseRules(t *testing.T) {
	exts := map[string]bool{".sh": true, ".js": true}
	cases := []struct {
		subPath string
		in      []string
		out     []string
	}{
		{"qinglong/DefaultTasks/", []string{"qinglong/DefaultTasks/bili_task_manga.sh", "qinglong/DefaultTasks/sub/x.sh"}, []string{"qinglong/DefaultTasksExtra/x.sh", "tools/build.sh", "qinglong/x.sh"}},
		{"qinglong/*", []string{"qinglong/DefaultTasks/bili_task_manga.sh", "qinglong/x.sh"}, []string{"tools/build.sh", "qinglongX/x.sh"}},
		{"qinglong/**/bili", []string{"qinglong/bili/a.sh", "qinglong/a/b/bili/c.sh"}, []string{"qinglong/bilibili/a.sh", "bili/a.sh"}},
		// 不带 `/` 的名字在任意层级匹配（sparse 规则本来就这么解释）；同前缀的兄弟目录不命中
		{"DefaultTasks", []string{"qinglong/DefaultTasks/bili_task_manga.sh", "DefaultTasks/a.sh"}, []string{"qinglong/DefaultTasksExtra/x.sh", "tools/build.sh"}},
		// 末尾带 `/` 只匹配目录：同名的文件不算
		{"tools/build.sh/", nil, []string{"tools/build.sh"}},
		{"tools/build.sh", []string{"tools/build.sh"}, []string{"tools/build.shx", "tools/other.sh"}},
	}
	for _, tc := range cases {
		sub := &model.Subscription{Type: model.SubTypeGitRepo, SubPath: tc.subPath, FullCheckout: true}
		for _, p := range tc.in {
			if !shouldManageSubscriptionFile(sub, p, exts) {
				t.Errorf("sub_path=%q: %q 在子目录范围内，应照常建任务", tc.subPath, p)
			}
		}
		for _, p := range tc.out {
			if shouldManageSubscriptionFile(sub, p, exts) {
				t.Errorf("sub_path=%q: %q 不在子目录范围内，不该建任务", tc.subPath, p)
			}
		}
	}
}

// 正则片段真的决定建哪些任务（扫描层面，不只是单文件判定）：这两条正是发布说明里要点名的可见变化。
func TestSubscriptionRegexFiltersDecideTaskCandidates(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeSubscriptionPinFixture(t)

	t.Run("只填正则的白名单只建命中的脚本", func(t *testing.T) {
		// 改造前 ^jd[^_] 一个都匹配不到，靠兜底 #2 把全部脚本都建成任务。
		got := evaluateSubscriptionPinConfig(model.Subscription{Whitelist: "^jd[^_]"})
		assertSubscriptionPinSlice(t, "whitelist-regex-only", "candidates", got.candidates,
			[]string{"task pin-repo/jd+x.js", "task pin-repo/jdCookie.js", "task pin-repo/jddj_bean.js"})
	})

	t.Run("黑名单的正则片段真的排除", func(t *testing.T) {
		// 改造前 back[Uu]p 被当成字面量，一个都挡不住：backUp/ 与 jd_group/backUp/ 下的脚本照样建任务。
		got := evaluateSubscriptionPinConfig(model.Subscription{Whitelist: "jd_", Blacklist: "back[Uu]p"})
		assertSubscriptionPinSlice(t, "blacklist-regex", "candidates", got.candidates,
			[]string{"task pin-repo/jd_bean_change.js", "task pin-repo/jd_group/helper.js"})
	})
}

func newSubscriptionRegexRemote(t *testing.T, root string, files map[string]string) string {
	t.Helper()

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)
	for name, body := range files {
		full := filepath.Join(worktreeDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create dir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runGit(t, worktreeDir, "add", ".")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")
	return remoteDir
}

func syncedSubscriptionTaskBases(t *testing.T, sub *model.Subscription) ([]string, string) {
	t.Helper()
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	InitSchedulerV2()
	t.Cleanup(ShutdownSchedulerV2)

	var logs []string
	syncSubscriptionTasks(sub, func(s string) { logs = append(logs, s) })

	var tasks []model.Task
	queryTasksByLabel(subscriptionTaskLabel(sub.ID)).Find(&tasks)
	bases := make([]string, 0, len(tasks))
	for _, task := range tasks {
		bases = append(bases, filepath.Base(filepath.FromSlash(task.Command)))
	}
	sort.Strings(bases)
	return bases, strings.Join(logs, "\n")
}

// 真机 git：issue #129 的原始场景。用户那条青龙指令的依赖规则里有 `^jd[^_]`，改造前它被当成
// gitignore 元字符跳过，jdCookie.js 不落盘，主脚本 require('./jdCookie') 一跑就报找不到模块。
func TestPullGitRepoWithCallbackRegexDependencyChecksOutJdCookie(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := newSubscriptionRegexRemote(t, root, map[string]string{
		"jd_bean_change.js": "//cron: 1 1 * * *\nconsole.log('jd')\n",
		"jx_sign.js":        "//cron: 2 2 * * *\nconsole.log('jx')\n",
		// 正主。刻意带 cron 头：依赖文件哪怕有合法 cron 也不能被建成任务。
		"jdCookie.js":      "//cron: 3 3 * * *\nmodule.exports = {}\n",
		"sendNotify.js":    "module.exports = {}\n",
		"utils/date.js":    "module.exports = {}\n",
		"other_task.js":    "//cron: 4 4 * * *\nconsole.log('other')\n",
		"README.md":        "readme\n",
		"backUp/jd_old.js": "//cron: 5 5 * * *\nconsole.log('old')\n",
	})

	sub := &model.Subscription{
		Name:        "jdpro-regex",
		Type:        model.SubTypeGitRepo,
		URL:         remoteDir,
		Branch:      "main",
		SaveDir:     "jdpro-regex-repo",
		Whitelist:   qlRepoWhitelist,
		Blacklist:   qlRepoBlacklist,
		DependOn:    qlRepoDependOn,
		AutoAddTask: true,
		Enabled:     true,
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)

	assertCheckout := func(stage string, logs []string) {
		t.Helper()
		for _, name := range []string{"jdCookie.js", "jd_bean_change.js", "jx_sign.js", "sendNotify.js", "utils/date.js"} {
			if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); statErr != nil {
				t.Errorf("[%s] %s 应被检出: %v", stage, name, statErr)
			}
		}
		// 档 1 的代价：整仓检出，白名单 / 依赖都没命中的文件也会落盘（建任务的集合不变，见下面的同步）。
		for _, name := range []string{"other_task.js", "README.md"} {
			if _, statErr := os.Stat(filepath.Join(destDir, name)); statErr != nil {
				t.Errorf("[%s] 整仓检出时 %s 也应落盘: %v", stage, name, statErr)
			}
		}
		// 黑名单的排除规则照旧下发。
		if _, statErr := os.Stat(filepath.Join(destDir, "backUp", "jd_old.js")); !os.IsNotExist(statErr) {
			t.Errorf("[%s] 黑名单目录里的 backUp/jd_old.js 不应被检出, stat err=%v", stage, statErr)
		}
		joined := strings.Join(logs, "\n")
		if !strings.Contains(joined, "^jd[^_]") || !strings.Contains(joined, "完整仓库") {
			t.Errorf("[%s] 拉取日志应说明依赖规则的正则片段让本次检出完整仓库\n%s", stage, joined)
		}
	}

	var firstLogs []string
	if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(s string) { firstLogs = append(firstLogs, s) }); err != nil {
		t.Fatalf("first pull: %v\n%s", err, output)
	}
	assertCheckout("首次 clone", firstLogs)

	// 第二次拉取走「已有仓库」那条路径：fetch → applySparseCheckout → reset --hard FETCH_HEAD。
	var secondLogs []string
	if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(s string) { secondLogs = append(secondLogs, s) }); err != nil {
		t.Fatalf("second pull: %v\n%s", err, output)
	}
	assertCheckout("已有仓库", secondLogs)

	bases, joined := syncedSubscriptionTaskBases(t, sub)
	if !reflect.DeepEqual(bases, []string{"jd_bean_change.js", "jx_sign.js"}) {
		t.Fatalf("只有白名单命中的脚本应建任务, got %#v\n%s", bases, joined)
	}
	if !strings.Contains(joined, "[依赖文件]") || !strings.Contains(joined, "jdCookie.js") {
		t.Errorf("日志应点名依赖文件 jdCookie.js\n%s", joined)
	}
}

// 真机 git：指定子目录 + 依赖正则。整仓落盘让 jdCookie.js 能被 require，但只有子目录里的脚本建任务。
// 子目录的几种常见写法（末尾带 `/`、glob、不带 `/` 的目录名）改造前经 sparse 都能检出并建出 keep.js 的任务，
// 放宽成整仓之后护栏必须照样认得它们（复查时实测过：护栏只认精确前缀时这三种写法一个任务都不建）。
func TestPullGitRepoWithCallbackRegexDependencyUnderSubPathKeepsTaskScope(t *testing.T) {
	for _, subPath := range []string{"scripts/daily", "scripts/daily/", "scripts/*", "daily"} {
		t.Run(subPath, func(t *testing.T) {
			root := testutil.SetupTestEnv(t)
			remoteDir := newSubscriptionRegexRemote(t, root, map[string]string{
				"scripts/daily/keep.js": "//cron: 1 1 * * *\nconsole.log('keep')\n",
				"jdCookie.js":           "//cron: 2 2 * * *\nmodule.exports = {}\n",
				"tools/build.js":        "//cron: 3 3 * * *\nconsole.log('build')\n",
			})

			sub := &model.Subscription{
				Name:        "subpath-regex",
				Type:        model.SubTypeGitRepo,
				URL:         remoteDir,
				Branch:      "main",
				SaveDir:     "subpath-regex-repo",
				SubPath:     subPath,
				DependOn:    "^jd[^_]",
				AutoAddTask: true,
				Enabled:     true,
			}
			authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
			if err != nil {
				t.Fatalf("build git auth config: %v", err)
			}

			var logs []string
			if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(s string) { logs = append(logs, s) }); err != nil {
				t.Fatalf("pull: %v\n%s", err, output)
			}
			destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
			for _, name := range []string{"jdCookie.js", "scripts/daily/keep.js", "tools/build.js"} {
				if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); statErr != nil {
					t.Errorf("整仓检出时 %s 应落盘: %v", name, statErr)
				}
			}
			if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "只给子目录里的脚本建定时任务") {
				t.Errorf("拉取日志应说明建任务仍限在子目录\n%s", joined)
			}

			bases, joined := syncedSubscriptionTaskBases(t, sub)
			if !reflect.DeepEqual(bases, []string{"keep.js"}) {
				t.Fatalf("只有子目录里的脚本应建任务, got %#v\n%s", bases, joined)
			}
		})
	}
}
