package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestPullGitRepoWithCallbackAtomicSwitchKeepsPreviousHealthyVersion(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)
	writeRepoFile(t, filepath.Join(worktreeDir, "repo.js"), "console.log('v1')\n")
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{Name: "atomic-sub", Type: model.SubTypeGitRepo, URL: remoteDir, Branch: "main", SaveDir: "atomic-repo"}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {}); err != nil {
		t.Fatalf("initial pull failed: %v\n%s", err, output)
	}

	writeRepoFile(t, filepath.Join(worktreeDir, "repo.js"), "console.log('v2')\n")
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "update")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	authCfg, err = buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config for update: %v", err)
	}
	if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {}); err != nil {
		t.Fatalf("second pull failed: %v\n%s", err, output)
	}

	destFile := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir, "repo.js")
	if got := readNormalizedFile(t, destFile); got != "console.log('v2')\n" {
		t.Fatalf("expected active worktree v2, got %q", got)
	}
	previousFile := filepath.Join(config.C.Data.ScriptsDir, "."+sub.SaveDir+".previous", "repo.js")
	if got := readNormalizedFile(t, previousFile); got != "console.log('v1')\n" {
		t.Fatalf("expected previous healthy worktree v1, got %q", got)
	}
}

func TestPullGitRepoWithCallbackCancelledStagingLeavesActiveVersion(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)
	writeRepoFile(t, filepath.Join(worktreeDir, "repo.js"), "console.log('v1')\n")
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{Name: "cancel-sub", Type: model.SubTypeGitRepo, URL: remoteDir, Branch: "main", SaveDir: "cancel-repo"}
	authCfg, err := buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	if output, err := pullGitRepoWithCallback(context.Background(), sub, authCfg, func(string) {}); err != nil {
		t.Fatalf("initial pull failed: %v\n%s", err, output)
	}

	writeRepoFile(t, filepath.Join(worktreeDir, "repo.js"), "console.log('v2')\n")
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "update")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	authCfg, err = buildGitAuthConfig(os.Environ(), sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config for canceled update: %v", err)
	}
	_, _ = pullGitRepoWithCallback(canceledCtx, sub, authCfg, func(string) {})

	destFile := filepath.Join(config.C.Data.ScriptsDir, sub.SaveDir, "repo.js")
	if got := readNormalizedFile(t, destFile); got != "console.log('v1')\n" {
		t.Fatalf("expected canceled pull to keep active v1, got %q", got)
	}
}

func TestExecuteSubscriptionPullTracksOperationAndLog(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	remoteDir := filepath.Join(root, "remote.git")
	worktreeDir := filepath.Join(root, "worktree")

	runGit(t, root, "init", "--bare", remoteDir)
	runGit(t, root, "clone", remoteDir, worktreeDir)
	writeRepoFile(t, filepath.Join(worktreeDir, "repo.js"), "console.log('tracked')\n")
	runGit(t, worktreeDir, "add", "repo.js")
	runGit(t, worktreeDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGit(t, worktreeDir, "push", "origin", "HEAD:main")

	sub := &model.Subscription{Name: "op-sub", Type: model.SubTypeGitRepo, URL: remoteDir, Branch: "main", SaveDir: "op-repo", Enabled: true}
	if err := database.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if output, err := ExecuteSubscriptionPull(sub, func(string) {}); err != nil {
		t.Fatalf("execute subscription pull failed: %v\n%s", err, output)
	}

	var op model.Operation
	if err := database.DB.Where("kind = ?", model.OperationKindSubscription).First(&op).Error; err != nil {
		t.Fatalf("query operation: %v", err)
	}
	if op.State != model.OperationStateSuccess {
		t.Fatalf("expected operation success, got %q", op.State)
	}
	var log model.SubLog
	if err := database.DB.Where("subscription_id = ?", sub.ID).First(&log).Error; err != nil {
		t.Fatalf("query sub log: %v", err)
	}
	if log.OperationID != op.ID {
		t.Fatalf("expected log operation_id %q, got %q", op.ID, log.OperationID)
	}
}

func TestReconcileInterruptedSubscriptionPullsMarksRunningOperationsUnknown(t *testing.T) {
	testutil.SetupTestEnv(t)

	running := model.Operation{ID: "subscription_1_running", Kind: model.OperationKindSubscription, State: model.OperationStateRunning, Phase: "pulling", Sequence: 1}
	pending := model.Operation{ID: "subscription_2_pending", Kind: model.OperationKindSubscription, State: model.OperationStatePending, Phase: "queued", Sequence: 2}
	if err := database.DB.Create(&running).Error; err != nil {
		t.Fatalf("create running operation: %v", err)
	}
	if err := database.DB.Create(&pending).Error; err != nil {
		t.Fatalf("create pending operation: %v", err)
	}

	ReconcileInterruptedSubscriptionPulls()

	var ops []model.Operation
	if err := database.DB.Order("id").Find(&ops).Error; err != nil {
		t.Fatalf("query operations: %v", err)
	}
	for _, op := range ops {
		if op.State != model.OperationStateUnknown || op.ErrorCode != "interrupted_pull" {
			t.Fatalf("expected interrupted operation unknown, got id=%s state=%s error=%s", op.ID, op.State, op.ErrorCode)
		}
	}
}

func writeRepoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
}

func readNormalizedFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
