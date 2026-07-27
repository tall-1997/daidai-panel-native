package service

import (
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
