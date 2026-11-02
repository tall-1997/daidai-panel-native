package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestGitHasWorkingTreeChangesDetectsTrackedAndUntrackedFiles(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	repoDir := filepath.Join(root, "repo")

	runGit(t, root, "init", repoDir)
	if err := os.WriteFile(filepath.Join(repoDir, "tracked.js"), []byte("console.log('v1')\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, repoDir, "add", "tracked.js")
	runGit(t, repoDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")

	hasChanges, err := gitHasWorkingTreeChanges(context.Background(), repoDir, os.Environ())
	if err != nil {
		t.Fatalf("check clean repo changes: %v", err)
	}
	if hasChanges {
		t.Fatal("expected clean repo to report no working tree changes")
	}

	if err := os.WriteFile(filepath.Join(repoDir, "tracked.js"), []byte("console.log('v2')\n"), 0o644); err != nil {
		t.Fatalf("update tracked file: %v", err)
	}
	hasChanges, err = gitHasWorkingTreeChanges(context.Background(), repoDir, os.Environ())
	if err != nil {
		t.Fatalf("check tracked changes: %v", err)
	}
	if !hasChanges {
		t.Fatal("expected modified tracked file to be detected as working tree change")
	}

	runGit(t, repoDir, "checkout", "--", "tracked.js")

	hasChanges, err = gitHasWorkingTreeChanges(context.Background(), repoDir, os.Environ())
	if err != nil {
		t.Fatalf("check repo after reverting tracked changes: %v", err)
	}
	if hasChanges {
		t.Fatal("expected repo to be clean after reverting tracked changes")
	}

	if err := os.WriteFile(filepath.Join(repoDir, "local-only.js"), []byte("console.log('local')\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	hasChanges, err = gitHasWorkingTreeChanges(context.Background(), repoDir, os.Environ())
	if err != nil {
		t.Fatalf("check untracked changes: %v", err)
	}
	if !hasChanges {
		t.Fatal("expected untracked file to be detected as working tree change")
	}
}

// TestResolveSubscriptionForceOverwrite 锁死覆盖拉取的三态优先级：
// 订阅显式选了 force / preserve 就以订阅为准，全局开关只在 inherit（含空串、脏值）时才生效。
// 这条替换了老的 TestApplySubscriptionForceOverwriteSettingUsesGlobalConfig ——
// 那条断言的正是「全局值无条件盖掉订阅值」，也就是本次要推翻的行为。
func TestResolveSubscriptionForceOverwrite(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		globalConfig string
		want         bool
	}{
		{"inherit 跟随全局开", model.SubOverwriteInherit, "true", true},
		{"inherit 跟随全局关", model.SubOverwriteInherit, "false", false},
		{"force 覆盖全局关", model.SubOverwriteForce, "false", true},
		{"preserve 覆盖全局开", model.SubOverwritePreserve, "true", false},
		{"空串按 inherit 处理", "", "false", false},
		{"脏值按 inherit 处理", "not-a-mode", "true", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = testutil.SetupTestEnv(t)

			if err := model.SetConfig("subscription_force_overwrite", tc.globalConfig); err != nil {
				t.Fatalf("set subscription_force_overwrite: %v", err)
			}

			// 旧列刻意反着设：验证它已经彻底不参与判定，只剩只读兼容。
			legacy := !tc.want
			sub := &model.Subscription{
				Type:           model.SubTypeGitRepo,
				OverwriteMode:  tc.mode,
				ForceOverwrite: &legacy,
			}

			if got := resolveSubscriptionForceOverwrite(sub); got != tc.want {
				t.Fatalf("overwrite_mode=%q 全局=%s 期望 %v，实际 %v", tc.mode, tc.globalConfig, tc.want, got)
			}
			// 解析器不能改写 sub —— 这个对象稍后还会被 database.DB.Model(sub).Updates(...) 用到。
			if sub.OverwriteMode != tc.mode {
				t.Fatalf("expected resolve to leave overwrite_mode untouched, got %q", sub.OverwriteMode)
			}
			if sub.ForceOverwrite == nil || *sub.ForceOverwrite != legacy {
				t.Fatalf("expected resolve to leave legacy force_overwrite untouched, got %#v", sub.ForceOverwrite)
			}
		})
	}
}

func TestPullGitRepoWithCallbackPreserveModeSkipsFalseConflictWhenRepoIsClean(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	repoFile := filepath.Join(worktreeDir, "repo.js")
	if err := os.WriteFile(repoFile, []byte("console.log('v1')\n"), 0o644); err != nil {
		t.Fatalf("write initial repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	// 走订阅级「保留本地修改」。旧列 ForceOverwrite 已经不参与判定，
	// 这里必须用 OverwriteMode 表达，否则会落到 inherit → 全局默认 true → 跑成覆盖模式。
	sub := &model.Subscription{
		Name:          "preserve-sub",
		Type:          model.SubTypeGitRepo,
		URL:           remoteDir,
		Branch:        "main",
		SaveDir:       "preserve-repo",
		OverwriteMode: model.SubOverwritePreserve,
	}

	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("initial pull failed: %v\n%s", err, output)
	}

	if err := os.WriteFile(repoFile, []byte("console.log('v2')\n"), 0o644); err != nil {
		t.Fatalf("write updated repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "update")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	authCfg, err = buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config for update: %v", err)
	}
	output, err = pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("preserve-mode update failed: %v\n%s", err, output)
	}

	if strings.Contains(output, "本地修改与远端更新存在冲突") {
		t.Fatalf("expected clean repo preserve update to avoid false conflict, got output:\n%s", output)
	}
	if strings.Contains(output, "未发现贮藏条目") {
		t.Fatalf("expected clean repo preserve update to skip stash pop, got output:\n%s", output)
	}

	destFile := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir, "repo.js")
	content, readErr := os.ReadFile(destFile)
	if readErr != nil {
		t.Fatalf("read pulled repo file: %v", readErr)
	}
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	if normalized != "console.log('v2')\n" {
		t.Fatalf("expected pulled repo file to update to v2, got %q", string(content))
	}
}
