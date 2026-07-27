package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/middleware"
	"daidai-panel/testutil"
)

func TestBuildNotifyHelperEnvCreatesManagedHelpers(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	workDir := filepath.Join(scriptsDir, "nested")

	env, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	if env["DAIDAI_NOTIFY_URL"] == "" || env["DAIDAI_NOTIFY_TOKEN"] == "" {
		t.Fatalf("expected notify url/token in env, got %#v", env)
	}
	if _, err := middleware.ParseToken(env["DAIDAI_NOTIFY_TOKEN"]); err != nil {
		t.Fatalf("parse helper token: %v", err)
	}

	paths := []string{
		filepath.Join(scriptsDir, notifyPyFilename),
		filepath.Join(scriptsDir, sendNotifyJSFilename),
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read helper %s: %v", path, err)
		}
		if !strings.Contains(string(content), "DAIDAI_PANEL_MANAGED_NOTIFY_HELPER") {
			t.Fatalf("expected helper marker in %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, notifyPyFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested notify.py to stay absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, sendNotifyJSFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested sendNotify.js to stay absent, got err=%v", err)
	}
	if got := env["DAIDAI_SCRIPTS_DIR"]; got != scriptsDir {
		t.Fatalf("expected DAIDAI_SCRIPTS_DIR=%q, got %q", scriptsDir, got)
	}

	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		t.Fatalf("expected test data dir to exist: %v", err)
	}
}

func TestBuildNotifyHelperEnvUsesAbsoluteHelperPaths(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := filepath.Join(config.C.Data.ScriptsDir, "nested")
	workDir := filepath.Join(config.C.Data.ScriptsDir, "jobs")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	env, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	for _, key := range []string{"DAIDAI_SCRIPTS_DIR", "DAIDAI_NOTIFY_PY", "DAIDAI_SEND_NOTIFY_JS"} {
		if !filepath.IsAbs(env[key]) {
			t.Fatalf("expected %s to be absolute, got %q", key, env[key])
		}
	}
}

func TestEnsureManagedHelperFileRewritesManagedJSFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), sendNotifyJSFilename)
	if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\nmodule.exports = {}\n"), 0o644); err != nil {
		t.Fatalf("seed helper file: %v", err)
	}

	if err := ensureManagedHelperFile(path, managedSendNotifyJSContent+"\n"); err != nil {
		t.Fatalf("rewrite managed helper file: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten helper file: %v", err)
	}
	if string(content) != managedSendNotifyJSContent+"\n" {
		t.Fatalf("expected managed JS helper to be refreshed")
	}
}

func TestManagedHelperContentIncludesUsageDocs(t *testing.T) {
	if !strings.Contains(managedNotifyPyContent, "Usage:") {
		t.Fatalf("expected python helper usage docs")
	}
	if !strings.Contains(managedNotifyPyContent, "def send(title, content, ignore_default_config=False, **kwargs):") {
		t.Fatalf("expected python helper send signature docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "QingLong-style notify entry point.") {
		t.Fatalf("expected js helper entry point docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "@param {object} params") {
		t.Fatalf("expected js helper JSDoc params")
	}
}

func TestAppendScriptHelperPathsKeepsExistingEntries(t *testing.T) {
	env := map[string]string{
		"NODE_PATH":    "/tmp/node_modules",
		"PYTHONPATH":   "/tmp/site-packages",
		"NODE_OPTIONS": "--trace-warnings",
	}

	AppendScriptHelperPaths(env, "/tmp/scripts")
	AppendScriptHelperPaths(env, "/tmp/scripts")

	if got := env["NODE_PATH"]; !strings.Contains(got, "/tmp/node_modules") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected NODE_PATH: %q", got)
	}
	if strings.Count(env["NODE_PATH"], "/tmp/scripts") != 1 {
		t.Fatalf("expected deduplicated NODE_PATH, got %q", env["NODE_PATH"])
	}
	if got := env["PYTHONPATH"]; !strings.Contains(got, "/tmp/site-packages") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected PYTHONPATH: %q", got)
	}
	if got := env["NODE_OPTIONS"]; !strings.Contains(got, "--trace-warnings") || !strings.Contains(got, "/tmp/scripts/sendNotify.js") {
		t.Fatalf("unexpected NODE_OPTIONS: %q", got)
	}
	if strings.Count(env["NODE_OPTIONS"], "/tmp/scripts/sendNotify.js") != 1 {
		t.Fatalf("expected deduplicated NODE_OPTIONS, got %q", env["NODE_OPTIONS"])
	}
}

func TestCleanupManagedHelperCopiesRemovesOnlyManagedNestedHelpers(t *testing.T) {
	scriptsDir := filepath.Join(t.TempDir(), "scripts")
	workDir := filepath.Join(scriptsDir, "nested")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	managedNested := filepath.Join(workDir, sendNotifyJSFilename)
	customNested := filepath.Join(workDir, notifyPyFilename)
	if err := os.WriteFile(managedNested, []byte("// "+managedNotifyHelperToken+"\nmodule.exports={}\n"), 0o644); err != nil {
		t.Fatalf("write managed nested helper: %v", err)
	}
	if err := os.WriteFile(customNested, []byte("# custom helper\n"), 0o644); err != nil {
		t.Fatalf("write custom nested helper: %v", err)
	}

	if err := cleanupManagedHelperCopies(scriptsDir, workDir); err != nil {
		t.Fatalf("cleanup helper copies: %v", err)
	}

	if _, err := os.Stat(managedNested); !os.IsNotExist(err) {
		t.Fatalf("expected managed nested helper to be removed, err=%v", err)
	}
	if _, err := os.Stat(customNested); err != nil {
		t.Fatalf("expected custom nested helper to be preserved, err=%v", err)
	}
}

func TestCleanupManagedHelperCopiesUnderRootRemovesManagedCopiesInNestedDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scripts")
	firstNested := filepath.Join(root, "a")
	secondNested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(secondNested, 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}

	rootHelper := filepath.Join(root, sendNotifyJSFilename)
	firstHelper := filepath.Join(firstNested, sendNotifyJSFilename)
	secondHelper := filepath.Join(secondNested, notifyPyFilename)
	for _, path := range []string{rootHelper, firstHelper, secondHelper} {
		if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\n"), 0o644); err != nil {
			t.Fatalf("write helper %s: %v", path, err)
		}
	}

	if err := CleanupManagedHelperCopiesUnderRoot(root); err != nil {
		t.Fatalf("cleanup under root: %v", err)
	}

	if _, err := os.Stat(rootHelper); err != nil {
		t.Fatalf("expected root helper to stay, err=%v", err)
	}
	if _, err := os.Stat(firstHelper); !os.IsNotExist(err) {
		t.Fatalf("expected first nested helper removed, err=%v", err)
	}
	if _, err := os.Stat(secondHelper); !os.IsNotExist(err) {
		t.Fatalf("expected second nested helper removed, err=%v", err)
	}
}
