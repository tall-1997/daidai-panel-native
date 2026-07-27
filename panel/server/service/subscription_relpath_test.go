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

func TestPullGitRepoWithCallbackHandlesRelativeScriptsDir(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	t.Chdir(root)

	relScripts := filepath.Join("data", "scripts")
	config.C.Data.ScriptsDir = relScripts
	if err := os.MkdirAll(relScripts, 0o755); err != nil {
		t.Fatalf("create relative scripts dir: %v", err)
	}

	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")
	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)

	scriptContent := "new Env('relpath-task');\ncron: 0 1 2 3 4\nconsole.log('hi');\n"
	scriptName := "relpath_task.js"
	if err := os.WriteFile(filepath.Join(worktreeDir, scriptName), []byte(scriptContent), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGit(t, worktreeDir, "add", scriptName)
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{
		Name:    "relpath-sub",
		Type:    model.SubTypeGitRepo,
		URL:     remoteDir,
		Branch:  "main",
		SaveDir: "relpath_repo",
	}

	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {})
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, output)
	}

	expected := filepath.Join(root, "data", "scripts", "relpath_repo", scriptName)
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected cloned file at %s, got err: %v", expected, err)
	}

	duplicated := filepath.Join(root, "data", "scripts", "data", "scripts", "relpath_repo")
	if _, err := os.Stat(duplicated); !os.IsNotExist(err) {
		t.Fatalf("did not expect duplicated path %s to exist (stat err=%v)", duplicated, err)
	}

	entries, err := os.ReadDir(expected[:strings.LastIndex(expected, string(filepath.Separator))])
	if err != nil {
		t.Fatalf("read pulled repo dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected pulled repo directory to be non-empty after clone")
	}

	options := subscriptionTaskSyncOptions{
		autoAdd:     true,
		allowedExts: map[string]bool{".js": true},
	}
	candidates := collectSubscriptionTaskCandidates(sub, options)
	if len(candidates) == 0 {
		t.Fatal("expected at least one task candidate after pull, got none")
	}
	var found bool
	for _, c := range candidates {
		if strings.HasSuffix(c.Command, scriptName) && c.CronExpression == "0 1 2 3 4" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected candidate for %s with cron '0 1 2 3 4', got %+v", scriptName, candidates)
	}
}
