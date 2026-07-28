package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func TestPullGitRepoWithCallbackConvertsExistingNonGitDirectoryInPlace(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	if err := os.WriteFile(filepath.Join(worktreeDir, "repo.js"), []byte("console.log('repo')"), 0644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:    "demo-sub",
		Type:    model.SubTypeGitRepo,
		URL:     remoteDir,
		Branch:  "main",
		SaveDir: "demo-repo",
	}
	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "old.js"), []byte("old"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull git repo with existing dir: %v\n%s", err, output)
	}

	if !IsGitRepo(destDir) {
		t.Fatalf("expected %s to become a git repo", destDir)
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo.js")); err != nil {
		t.Fatalf("expected repo file to exist after pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "old.js")); !os.IsNotExist(err) {
		t.Fatalf("expected old file to be cleaned, got err=%v", err)
	}
}

func TestPullGitRepoWithCallbackReplacesExistingRepoAtomically(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	if err := os.WriteFile(filepath.Join(worktreeDir, "repo.js"), []byte("console.log('v1')\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:    "atomic-sub",
		Type:    model.SubTypeGitRepo,
		URL:     remoteDir,
		Branch:  "main",
		SaveDir: "atomic-repo",
	}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("initial pull failed: %v\n%s", err, output)
	}

	destDir := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir)
	if err := os.WriteFile(filepath.Join(destDir, "local-only.js"), []byte("local"), 0o644); err != nil {
		t.Fatalf("write local-only file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeDir, "repo.js"), []byte("console.log('v2')\n"), 0o644); err != nil {
		t.Fatalf("write updated repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "update")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	authCfg, err = buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config for update: %v", err)
	}
	var lines []string
	output, err = pullGitRepoWithCallback(context.Background(), sub, authCfg, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("atomic update failed: %v\n%s", err, output)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "原子切换") {
		t.Fatalf("expected atomic switch log, got lines=%v output:\n%s", lines, output)
	}
	content, err := os.ReadFile(filepath.Join(destDir, "repo.js"))
	if err != nil {
		t.Fatalf("read updated repo file: %v", err)
	}
	if strings.ReplaceAll(string(content), "\r\n", "\n") != "console.log('v2')\n" {
		t.Fatalf("expected updated repo content, got %q", string(content))
	}
	if _, err := os.Stat(filepath.Join(destDir, "local-only.js")); !os.IsNotExist(err) {
		t.Fatalf("expected atomic replacement to drop old local-only file, stat err=%v", err)
	}
}

func TestAtomicReplaceSubscriptionWorktreeRecoversCrashJournal(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	parent := filepath.Join(root, "scripts")
	destDir := filepath.Join(parent, "journal-repo")
	backupDir := filepath.Join(parent, ".journal-repo.previous")
	stagingDir := filepath.Join(parent, ".journal-repo.staging-test")
	journalPath := filepath.Join(parent, ".journal-repo.replace-journal.json")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "healthy.js"), []byte("healthy"), 0o644); err != nil {
		t.Fatalf("write backup file: %v", err)
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	if err := writeSubscriptionWorktreeJournal(journalPath, subscriptionWorktreeJournal{DestDir: destDir, BackupDir: backupDir, StagingDir: stagingDir, Phase: "publish"}); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	nextStaging := filepath.Join(parent, ".journal-repo.staging-next")
	if err := os.MkdirAll(nextStaging, 0o755); err != nil {
		t.Fatalf("mkdir next staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nextStaging, "new.js"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write next staging file: %v", err)
	}
	if err := atomicReplaceSubscriptionWorktree(destDir, nextStaging); err != nil {
		t.Fatalf("atomic replace after crash journal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "new.js")); err != nil {
		t.Fatalf("expected new worktree published: %v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("expected journal removed, stat err=%v", err)
	}
}
