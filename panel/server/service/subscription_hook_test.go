package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestNormalizeSubscriptionHookScriptRewritesQingLongPaths(t *testing.T) {
	sub := &model.Subscription{
		URL:        "https://gitee.com/hkyya/qljb.git",
		HookScript: `bash $QL_DIR/data/repo/hkyya_qljb/copyfiles.sh ; cd $QL_DIR/data/scripts/hkyya_qljb && python jbdl.py`,
	}

	got := normalizeSubscriptionHookScript(sub)
	want := `bash $SUB_DIR/copyfiles.sh ; cd $SUB_DIR && python jbdl.py`
	if got != want {
		t.Fatalf("unexpected hook rewrite: got %q want %q", got, want)
	}
}

func TestRunSubscriptionHookRejectsUnauthorizedScriptBeforeExecution(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := InitializeRuntimeSecurity(t.TempDir()); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	workDir := t.TempDir()
	marker := filepath.Join(workDir, "marker")
	sub := &model.Subscription{
		ID:         11,
		Name:       "unauthorized-hook",
		Type:       model.SubTypeGitRepo,
		URL:        "https://example.com/org/repo.git",
		Branch:     "main",
		HookScript: "printf executed > marker",
	}

	err := runSubscriptionHookInWorkDir(context.Background(), sub, workDir, func(string) {})
	if !errors.Is(err, ErrSubscriptionHookUnauthorized) {
		t.Fatalf("expected ErrSubscriptionHookUnauthorized, got %v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hook script should not execute before authorization, stat err=%v", statErr)
	}
}

func TestRunSubscriptionHookAllowsExplicitlyAuthorizedScript(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := InitializeRuntimeSecurity(t.TempDir()); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	workDir := t.TempDir()
	marker := filepath.Join(workDir, "marker")
	sub := &model.Subscription{
		ID:         12,
		Name:       "authorized-hook",
		Type:       model.SubTypeGitRepo,
		URL:        "https://example.com/org/repo.git",
		Branch:     "main",
		HookScript: "printf executed > marker",
	}
	AuthorizeSubscriptionHookForTest(sub)

	if err := runSubscriptionHookInWorkDir(context.Background(), sub, workDir, func(string) {}); err != nil {
		t.Fatalf("run authorized hook: %v", err)
	}
	payload, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read hook marker: %v", err)
	}
	if string(payload) != "executed" {
		t.Fatalf("marker payload=%q want executed", string(payload))
	}
}

func TestBuildSubscriptionHookEnvUsesSubscriptionWorkDir(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		ID:      7,
		Name:    "demo",
		Type:    model.SubTypeGitRepo,
		URL:     "https://gitee.com/hkyya/qljb.git",
		SaveDir: "hkyya_qljb",
	}

	workDir := filepath.Join(config.C.Data.ScriptsDir, "hkyya_qljb")
	env := buildSubscriptionHookEnv(sub, workDir)

	if env["SUB_DIR"] != workDir {
		t.Fatalf("expected SUB_DIR=%q, got %q", workDir, env["SUB_DIR"])
	}
	if env["SUB_SAVE_DIR"] != "hkyya_qljb" {
		t.Fatalf("expected SUB_SAVE_DIR to use save dir, got %q", env["SUB_SAVE_DIR"])
	}
	if env["PANEL_SCRIPTS_DIR"] != config.C.Data.ScriptsDir {
		t.Fatalf("expected PANEL_SCRIPTS_DIR=%q, got %q", config.C.Data.ScriptsDir, env["PANEL_SCRIPTS_DIR"])
	}
}
