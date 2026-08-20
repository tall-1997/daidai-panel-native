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

// 本文件覆盖「片段命中目录时，目录里的文件也要被 sparse-checkout 检出/排除」这条语义。
//
// 背景：sparse-checkout 用 gitignore 语法，而 gitignore 里的 `*` **不跨 `/`**。
// 片段 `utils` 只包成 `**/*utils*` 时：
//   - 命中 `sendNotify.js` 这种「名字里含片段」的根目录单文件 → OK
//   - 命中 `utils` 这个目录条目本身 → OK
//   - 命中 `utils/date.js` → 不 OK，`*utils*` 跨不过那个 `/`
//
// 后果就是主脚本 require('./utils/xxx') 直接报错。修法是每个片段成对下发
// `**/*x*` + `**/*x*/**`，包含侧（白名单 / 依赖）与排除侧（黑名单）都改。

// 排除侧为什么必须跟着改：包含侧一旦多了 `**/*jd_*/**` 这条能直接命中子文件的规则，
// 非递归的 `!**/*backUp*`（只匹配 backUp 目录条目本身）就压不住它了
// —— sparse-checkout 是「最后匹配者胜出」。只改一半会让 jd_group/backUp/old.js
// 从「现在挡得住」退化成「落盘」。见
// TestPullGitRepoWithCallbackDoesNotLeakBlacklistDirUnderMatchedDir。

func TestBuildSparseCheckoutPatternsPairsRecursiveRuleForEachFragment(t *testing.T) {
	t.Run("白名单片段成对下发且递归规则紧跟其后", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_|utils"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"**/*jd_*", "**/*jd_*/**", "**/*utils*", "**/*utils*/**"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})

	t.Run("黑名单片段同样成对下发", func(t *testing.T) {
		sub := &model.Subscription{Whitelist: "jd_", Blacklist: "backUp|Archive"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{
			"**/*jd_*", "**/*jd_*/**",
			"!**/*backUp*", "!**/*backUp*/**",
			"!**/*Archive*", "!**/*Archive*/**",
		}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})

	t.Run("排除规则必须排在包含规则之后", func(t *testing.T) {
		// sparse-checkout 是「最后匹配者胜出」，顺序反了黑名单就完全失效。
		sub := &model.Subscription{Whitelist: "jd_", Blacklist: "backUp", DependOn: "utils"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		firstExclude := -1
		lastInclude := -1
		for i, p := range patterns {
			if strings.HasPrefix(p, "!") {
				if firstExclude < 0 {
					firstExclude = i
				}
				continue
			}
			lastInclude = i
		}
		if firstExclude < 0 {
			t.Fatalf("没有生成排除规则: %#v", patterns)
		}
		if firstExclude < lastInclude {
			t.Fatalf("排除规则出现在包含规则之前，黑名单会失效: %#v", patterns)
		}
	})

	t.Run("白名单与依赖填了同一个片段时共用去重", func(t *testing.T) {
		// addPattern 的 seen map 对递归规则同样生效，不能出现重复条目。
		sub := &model.Subscription{Whitelist: "utils", DependOn: "utils|sendNotify"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{
			"**/*utils*", "**/*utils*/**",
			"**/*sendNotify*", "**/*sendNotify*/**",
		}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
		seen := map[string]bool{}
		for _, p := range patterns {
			if seen[p] {
				t.Fatalf("规则 %q 重复下发: %#v", p, patterns)
			}
			seen[p] = true
		}
	})

	t.Run("递归规则不破坏包含侧的完整检出降级", func(t *testing.T) {
		// 白名单含 gitignore 元字符 → 整体退回完整检出，patterns 必须仍然为空；
		// 依赖规则也不能借递归规则把它重新激活成「只检出依赖」。
		sub := &model.Subscription{Whitelist: "^jd[^_]|utils", DependOn: "sendNotify"}
		patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
		if len(patterns) != 0 {
			t.Fatalf("包含侧已退回完整检出，不应产出任何规则, got %#v", patterns)
		}
		if !strings.Contains(strings.Join(warnings, "\n"), "^jd[^_]") {
			t.Fatalf("应点名不安全的白名单片段, got %#v", warnings)
		}
	})

	t.Run("指定子目录仍按明确路径下发，不做片段包装", func(t *testing.T) {
		// sub_path 填的是明确路径，语义与「子串包含」的片段匹配不同：
		// 包成 `**/*scripts/daily*` 会把精确路径变成子串匹配，属于语义变更。
		sub := &model.Subscription{SubPath: "scripts/daily|tools"}
		patterns, _ := buildSubscriptionSparseCheckoutPatterns(sub)
		want := []string{"scripts/daily", "tools"}
		if !reflect.DeepEqual(patterns, want) {
			t.Fatalf("sparse patterns = %#v, want %#v", patterns, want)
		}
	})
}

// sparseRecursiveRepoFiles 是下面几个真机 git 用例共用的仓库布局。
// 每一条都对应一种「片段命中目录」的形态。
var sparseRecursiveRepoFiles = map[string]string{
	// 白名单命中：根目录单文件
	"jd_bean_change.js": "//cron: 1 1 * * *\nconsole.log('jd')\n",
	// 白名单命中：目录名含片段，目录里的文件要一起检出并建任务
	"jd_group/helper.js": "//cron: 2 2 * * *\nconsole.log('jd group')\n",
	// 白名单命中：多级子目录同样要递归带出
	"jd_group/nested/deep.js": "//cron: 3 3 * * *\nconsole.log('jd deep')\n",
	// 黑名单目录嵌在白名单目录里：递归包含规则不能把它带出来
	"jd_group/backUp/old.js": "//cron: 4 4 * * *\nconsole.log('jd group backup')\n",
	// 依赖命中：根目录单文件（改造前就能检出）
	"sendNotify.js": "//cron: 5 5 * * *\nmodule.exports = {}\n",
	// 依赖命中：目录名含片段 —— 本次修复的正主，主脚本 require('./utils/date') 要它
	"utils/date.js": "//cron: 6 6 * * *\nmodule.exports = {}\n",
	// 依赖命中：多级子目录
	"utils/nested/http.js": "//cron: 7 7 * * *\nmodule.exports = {}\n",
	// 黑名单目录：子文件恰好命中正向白名单，必须被排除规则压住
	"backUp/jd_old.js": "//cron: 8 8 * * *\nconsole.log('old')\n",
	// 黑名单目录的多级子目录
	"backUp/nested/jd_older.js": "//cron: 9 9 * * *\nconsole.log('older')\n",
	// 白名单/依赖都没命中
	"other_task.js": "//cron: 10 10 * * *\nconsole.log('other')\n",
	"README.md":     "readme\n",
}

// newSparseRecursiveRemote 造一个带多级子目录的裸仓库，返回远端路径。
func newSparseRecursiveRemote(t *testing.T, root string) string {
	t.Helper()

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	for name, body := range sparseRecursiveRepoFiles {
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

// 真机 git 验证：片段命中目录时，目录里的文件（含多级子目录）必须真的被检出，
// 黑名单目录里的文件必须真的不落盘。
//
// 这是本次修复的核心用例：改造前 `**/*utils*` 只匹配到 utils 这个目录条目本身，
// utils/date.js 是否落盘完全取决于 git 的目录继承细节，属于「碰运气」；
// 现在 `**/*utils*/**` 把它写死了。
func TestPullGitRepoWithCallbackChecksOutFilesInsideMatchedDirectory(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := newSparseRecursiveRemote(t, root)

	sub := &model.Subscription{
		Name:      "sparse-recursive",
		Type:      model.SubTypeGitRepo,
		URL:       remoteDir,
		Branch:    "main",
		SaveDir:   "sparse-recursive-repo",
		Whitelist: "jd_",
		Blacklist: "backUp",
		DependOn:  "sendNotify|utils",
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
	mustExist := []string{
		"jd_bean_change.js",
		// 白名单片段命中目录 → 目录里的文件也要检出
		"jd_group/helper.js",
		"jd_group/nested/deep.js",
		"sendNotify.js",
		// 依赖片段命中目录 → 目录里的文件也要检出（本次修复的正主）
		"utils/date.js",
		"utils/nested/http.js",
	}
	for _, name := range mustExist {
		if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); statErr != nil {
			t.Errorf("%s 应被检出: %v", name, statErr)
		}
	}

	mustNotExist := []string{
		// 黑名单目录（含多级子目录）里的文件不能落盘
		"backUp/jd_old.js",
		"backUp/nested/jd_older.js",
		// 黑名单目录嵌在白名单目录里，递归包含规则不能把它带出来
		"jd_group/backUp/old.js",
		// 白名单/依赖都没命中的仍然不落盘，sparse-checkout 的收敛效果不能丢
		"other_task.js",
		"README.md",
	}
	for _, name := range mustNotExist {
		if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); !os.IsNotExist(statErr) {
			t.Errorf("%s 不应被检出, stat err=%v", name, statErr)
		}
	}
}

// 回归守卫：包含侧改成递归之后，黑名单目录嵌在白名单目录里这种形态必须仍然挡得住。
//
// 只改包含侧不改排除侧时，`**/*jd_*/**` 会直接命中 jd_group/backUp/old.js，
// 而非递归的 `!**/*backUp*` 只匹配 backUp 目录条目本身、压不住它
// —— 本来挡得住的文件反而会落盘。这条是「黑名单必须同步改成递归」的直接依据。
func TestPullGitRepoWithCallbackDoesNotLeakBlacklistDirUnderMatchedDir(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := newSparseRecursiveRemote(t, root)

	sub := &model.Subscription{
		Name:      "sparse-blacklist-nested",
		Type:      model.SubTypeGitRepo,
		URL:       remoteDir,
		Branch:    "main",
		SaveDir:   "sparse-blacklist-nested-repo",
		Whitelist: "jd_",
		Blacklist: "backUp",
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
	// 白名单目录本身照常检出，证明不是「整个 jd_group 都没拉下来」造成的假通过。
	if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash("jd_group/helper.js"))); statErr != nil {
		t.Fatalf("白名单目录里的 jd_group/helper.js 应被检出: %v", statErr)
	}
	for _, name := range []string{"jd_group/backUp/old.js", "backUp/jd_old.js", "backUp/nested/jd_older.js"} {
		if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); !os.IsNotExist(statErr) {
			t.Errorf("黑名单命中的 %s 不应被检出, stat err=%v", name, statErr)
		}
	}
}

// 真机 git + 任务同步串起来跑：
//   - 白名单片段命中目录 → 目录里的文件既检出、又建任务
//   - 依赖片段命中目录 → 目录里的文件只检出、不建任务（utils/date.js 场景）
//   - 黑名单目录里的文件既不落盘、也不建任务
func TestSubscriptionRecursiveSparseCheckoutThenTaskSync(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := newSparseRecursiveRemote(t, root)

	sub := &model.Subscription{
		Name:        "sparse-recursive-sync",
		Type:        model.SubTypeGitRepo,
		URL:         remoteDir,
		Branch:      "main",
		SaveDir:     "sparse-recursive-sync-repo",
		Whitelist:   "jd_",
		Blacklist:   "backUp",
		DependOn:    "sendNotify|utils",
		AutoAddTask: true,
		Enabled:     true,
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull repo: %v\n%s", err, output)
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

	// 白名单命中目录 → 目录里的文件检出且建任务
	for _, want := range []string{"jd_bean_change.js", "helper.js", "deep.js"} {
		if _, ok := byBase[want]; !ok {
			t.Errorf("白名单命中的 %s 应建任务\n%s", want, joined)
		}
	}
	// 仅命中依赖 → 检出但不建任务（utils/date.js 场景）；
	// 这些文件刻意都带了合法 cron 头，能建任务的话一定会被建出来。
	for _, unwanted := range []string{"sendNotify.js", "date.js", "http.js"} {
		if _, ok := byBase[unwanted]; ok {
			t.Errorf("仅命中依赖的 %s 不应建任务\n%s", unwanted, joined)
		}
	}
	// 黑名单 / 未命中 → 既不落盘也不建任务
	for _, unwanted := range []string{"jd_old.js", "jd_older.js", "old.js", "other_task.js"} {
		if _, ok := byBase[unwanted]; ok {
			t.Errorf("%s 不应建任务\n%s", unwanted, joined)
		}
	}
	if len(tasks) != 3 {
		for _, task := range tasks {
			t.Logf("  task: cmd=%q cron=%q", task.Command, task.CronExpression)
		}
		t.Fatalf("expected 3 tasks, got %d\n%s", len(tasks), joined)
	}

	// 依赖文件落了盘却不建任务，这件事必须在日志里点名，否则用户以为文件被漏掉了。
	if !strings.Contains(joined, "[依赖文件]") {
		t.Errorf("日志应有 [依赖文件] 段落\n%s", joined)
	}
	for _, name := range []string{"utils/date.js", "utils/nested/http.js"} {
		if !strings.Contains(joined, name) {
			t.Errorf("日志应点名依赖文件 %s\n%s", name, joined)
		}
	}
}
