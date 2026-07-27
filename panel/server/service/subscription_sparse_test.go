package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestPullGitRepoWithCallbackSubPathDoesNotCheckoutRepoRootFiles(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	// 模拟用户仓库：根目录和其他目录都有文件，但订阅只想要 scripts/daily。
	if err := os.MkdirAll(filepath.Join(worktreeDir, "scripts", "daily"), 0o755); err != nil {
		t.Fatalf("create daily dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(worktreeDir, "scripts", "other"), 0o755); err != nil {
		t.Fatalf("create other dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "root.js"), []byte("console.log('root')\n"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "scripts", "daily", "keep.js"), []byte("console.log('keep')\n"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "scripts", "other", "skip.js"), []byte("console.log('skip')\n"), 0o644); err != nil {
		t.Fatalf("write skip file: %v", err)
	}
	runGit(t, worktreeDir, "add", ".")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:    "daily-sub",
		Type:    model.SubTypeGitRepo,
		URL:     remoteDir,
		Branch:  "main",
		SaveDir: "daily-repo",
		SubPath: "scripts/daily",
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}

	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull sub path repo: %v\n%s", err, output)
	}

	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
	if _, err := os.Stat(filepath.Join(destDir, "scripts", "daily", "keep.js")); err != nil {
		t.Fatalf("expected sub_path file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "root.js")); !os.IsNotExist(err) {
		t.Fatalf("root file should not be checked out when sub_path is set, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "scripts", "other", "skip.js")); !os.IsNotExist(err) {
		t.Fatalf("other directory file should not be checked out when sub_path is set, stat err=%v", err)
	}
}

func TestPullGitRepoWithCallbackWhitelistLimitsCheckedOutFiles(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	// 白名单原本只影响任务扫描，这里验证它也会限制真实落盘文件，避免大仓库全部落到 scripts。
	if err := os.MkdirAll(filepath.Join(worktreeDir, "scripts"), 0o755); err != nil {
		t.Fatalf("create scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "scripts", "keep_task.js"), []byte("console.log('keep')\n"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "scripts", "skip_task.js"), []byte("console.log('skip')\n"), 0o644); err != nil {
		t.Fatalf("write skip file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "README.md"), []byte("readme\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, worktreeDir, "add", ".")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:      "whitelist-sub",
		Type:      model.SubTypeGitRepo,
		URL:       remoteDir,
		Branch:    "main",
		SaveDir:   "whitelist-repo",
		Whitelist: "keep_task",
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}

	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull whitelist repo: %v\n%s", err, output)
	}

	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
	if _, err := os.Stat(filepath.Join(destDir, "scripts", "keep_task.js")); err != nil {
		t.Fatalf("expected whitelisted file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "scripts", "skip_task.js")); !os.IsNotExist(err) {
		t.Fatalf("non-whitelisted file should not be checked out, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("non-whitelisted readme should not be checked out, stat err=%v", err)
	}
}
