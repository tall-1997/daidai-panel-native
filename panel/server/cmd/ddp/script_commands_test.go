package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

// gitConfigWithToken 复刻订阅 Token 鉴权落盘后的真实内容：remote URL 里内嵌了 PAT。
const gitConfigWithToken = "[remote \"origin\"]\n\turl = https://x-access-token:ghp_cliRegressionToken@github.com/demo/scripts.git\n"

func prepareCLIScriptsWithGitRepo(t *testing.T) string {
	t.Helper()

	scriptsDir := config.C.Data.ScriptsDir
	if err := os.MkdirAll(filepath.Join(scriptsDir, "SmallWorld", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir nested git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "SmallWorld", ".git", "config"), []byte(gitConfigWithToken), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "SmallWorld", "demo.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}
	return scriptsDir
}

// resolveCLIScriptPath 的扩展名闸门对 config/HEAD/index 这类无扩展名文件一律放行，
// 少了隐藏名单判断，`ddp script cat SmallWorld/.git/config` 会把 PAT 直接打印到终端。
func TestResolveCLIScriptPathRejectsGitSegments(t *testing.T) {
	testutil.SetupTestEnv(t)
	scriptsDir := prepareCLIScriptsWithGitRepo(t)

	rejected := []string{
		".git/config",
		"SmallWorld/.git",
		"SmallWorld/.git/config",
		`SmallWorld\.git\HEAD`,
		"SmallWorld/.svn/entries",
		"SmallWorld/node_modules/pkg/index.js",
	}
	for _, relPath := range rejected {
		if _, _, err := resolveCLIScriptPath(scriptsDir, relPath, false); err == nil {
			t.Fatalf("expected resolveCLIScriptPath(%q) to be rejected", relPath)
		} else if !strings.Contains(err.Error(), "该路径不可访问") {
			t.Fatalf("expected access rejection for %q, got %v", relPath, err)
		}
	}

	if _, _, err := resolveCLIScriptPath(scriptsDir, "SmallWorld/demo.py", true); err != nil {
		t.Fatalf("expected normal script path to pass, got %v", err)
	}
}

func TestScriptCatAndFetchRejectGitPaths(t *testing.T) {
	testutil.SetupTestEnv(t)
	prepareCLIScriptsWithGitRepo(t)

	rt := &cliRuntime{cfg: config.C}

	err := runScriptCat(rt, []string{"SmallWorld/.git/config"})
	if err == nil {
		t.Fatal("expected ddp script cat to reject .git/config")
	}
	if !strings.Contains(err.Error(), "该路径不可访问") {
		t.Fatalf("expected access rejection, got %v", err)
	}

	// fetch 的路径校验发生在发起 HTTP 请求之前，所以这里不会真的联网
	err = runScriptFetch(rt, []string{"https://example.invalid/config", "--path", "SmallWorld/.git/config"})
	if err == nil {
		t.Fatal("expected ddp script fetch to reject .git/config")
	}
	if !strings.Contains(err.Error(), "该路径不可访问") {
		t.Fatalf("expected access rejection, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(config.C.Data.ScriptsDir, "SmallWorld", ".git", "config")); statErr != nil {
		t.Fatalf("expected .git/config to survive the rejected commands: %v", statErr)
	}
}
