package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 指定子目录 + 依赖规则只有普通片段：依赖片段并进 sparse 的包含侧，子目录外命中依赖规则的文件会落盘
// （buildSubscriptionSparseCheckoutPatterns 里 addFragmentPatterns 那条分支）。落盘是对的，主脚本要 require 它们；
// 但它们不能被建成定时任务。白名单留空时每个文件都算命中白名单，isDependencyOnly 一个都摘不掉，
// 只能靠子目录护栏（subPathScope）把建任务的范围收回子目录，口径与依赖规则含正则时一致。

// 真机 git：仓库里 scripts/a.js 是主脚本，utils/ 是依赖库；订阅只填 sub_path=scripts 与依赖规则 utils。
func TestPullGitRepoWithCallbackPlainDependencyUnderSubPathKeepsTaskScope(t *testing.T) {
	for _, subPath := range []string{"scripts", "scripts/", "scripts/*"} {
		t.Run(subPath, func(t *testing.T) {
			root := testutil.SetupTestEnv(t)
			remoteDir := newSubscriptionRegexRemote(t, root, map[string]string{
				"scripts/a.js": "//cron: 1 1 * * *\nconsole.log('a')\n",
				// helper 在辅助脚本名单里，没有 cron 头时本来就不建任务；这里刻意带上 cron 头：
				// 依赖文件哪怕写了合法 cron 也不能被建成任务。
				"utils/helper.js": "//cron: 2 2 * * *\nmodule.exports = {}\n",
				// 更常见的依赖库形态：没有 cron 头、名字也不在辅助脚本名单里，护栏缺位时会按默认 cron 建成任务。
				"utils/date.js": "module.exports = {}\n",
				// 子目录外、依赖规则也没命中：sparse 照旧不让它落盘。
				"tools/build.js": "//cron: 3 3 * * *\nconsole.log('build')\n",
			})

			sub := &model.Subscription{
				Name:        "subpath-plain-depend",
				Type:        model.SubTypeGitRepo,
				URL:         remoteDir,
				Branch:      "main",
				SaveDir:     "subpath-plain-depend-repo",
				SubPath:     subPath,
				DependOn:    "utils",
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
			for _, name := range []string{"scripts/a.js", "utils/helper.js", "utils/date.js"} {
				if _, statErr := os.Stat(filepath.Join(destDir, filepath.FromSlash(name))); statErr != nil {
					t.Errorf("%s 应落盘: %v\n%s", name, statErr, strings.Join(logs, "\n"))
				}
			}
			if _, statErr := os.Stat(filepath.Join(destDir, "tools", "build.js")); !os.IsNotExist(statErr) {
				t.Errorf("子目录外、依赖规则也没命中的 tools/build.js 不应落盘, stat err=%v", statErr)
			}

			bases, joined := syncedSubscriptionTaskBases(t, sub)
			if !reflect.DeepEqual(bases, []string{"a.js"}) {
				t.Fatalf("只有子目录里的脚本应建任务，子目录外的依赖文件不建, got %#v\n%s", bases, joined)
			}
		})
	}
}

// 同一场景的扫描层面，用 pin 用例那份整仓布局。真实检出时子目录外只有命中依赖规则的文件在盘上，结论相同。
func TestSubscriptionPlainDependencyUnderSubPathDoesNotGrowTaskSet(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeSubscriptionPinFixture(t)
	daily := []string{"task pin-repo/scripts/daily/keep.js", "task pin-repo/scripts/daily/keep_task.py"}

	t.Run("白名单留空：只给子目录里的脚本建任务", func(t *testing.T) {
		sub := model.Subscription{SubPath: "scripts/daily", DependOn: "sendNotify"}
		got := evaluateSubscriptionPinConfig(sub)
		assertSubscriptionPinSlice(t, "subpath-plain-depend", "candidates", got.candidates, daily)
		// 日志里「扫描 X 个候选文件」走同一个匹配器，口径必须与候选一致。
		scriptsDir := filepath.Join(config.C.Data.ScriptsDir, subscriptionPinSaveDir)
		if n := countSubscriptionScriptFiles(scriptsDir, subscriptionPinExts, &sub); n != len(daily) {
			t.Errorf("countSubscriptionScriptFiles = %d, want %d", n, len(daily))
		}
	})

	t.Run("白名单只命中子目录外的依赖文件：兜底 #2 只按子目录判断", func(t *testing.T) {
		// 依赖规则还是纯备注时 sendNotify.js 根本不落盘，白名单在子目录里一个没命中，走兜底 #2，子目录里的脚本照常建任务。
		// 护栏只挡建任务、不管兜底计数的话，sendNotify.js 会让兜底失效、自己又被护栏挡掉，结果一个任务都不建。
		got := evaluateSubscriptionPinConfig(model.Subscription{SubPath: "scripts/daily", Whitelist: "sendNotify", DependOn: "sendNotify"})
		assertSubscriptionPinSlice(t, "subpath-whitelist-hits-depend-only", "candidates", got.candidates, daily)
	})
}
