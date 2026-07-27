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
