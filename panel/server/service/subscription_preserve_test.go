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

func TestApplySubscriptionForceOverwriteSettingUsesGlobalConfig(t *testing.T) {
	_ = testutil.SetupTestEnv(t)

	forceOverwrite := true
	sub := &model.Subscription{
		Type:           model.SubTypeGitRepo,
		ForceOverwrite: &forceOverwrite,
	}

	if err := model.SetConfig("subscription_force_overwrite", "false"); err != nil {
		t.Fatalf("set subscription_force_overwrite: %v", err)
	}
	applySubscriptionForceOverwriteSetting(sub)

	if sub.ForceOverwrite == nil || *sub.ForceOverwrite {
		t.Fatalf("expected global subscription_force_overwrite=false to override subscription field, got %#v", sub.ForceOverwrite)
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

	forceOverwrite := false
	sub := &model.Subscription{
		Name:           "preserve-sub",
		Type:           model.SubTypeGitRepo,
		URL:            remoteDir,
		Branch:         "main",
		SaveDir:        "preserve-repo",
		ForceOverwrite: &forceOverwrite,
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
