package handler

import (
	"strings"
	"testing"

	"daidai-panel/config"
)

func TestNormalizeScriptRelativePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "simple root file", input: "demo.sh", want: "demo.sh"},
		{name: "normalize repeated separators", input: " folder//sub/./demo.py ", want: "folder/sub/demo.py"},
		{name: "normalize windows separators", input: `folder\child\demo.js`, want: "folder/child/demo.js"},
		{name: "reject traversal", input: "../outside", wantErr: "不允许路径穿越"},
		{name: "reject nested traversal", input: "folder/../outside", wantErr: "不允许路径穿越"},
		{name: "reject absolute-like path", input: "/outside/demo.sh", wantErr: "不允许路径穿越"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeScriptRelativePath(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// safePath 是脚本读写删改 13 个入口的唯一收口，
// .git 一旦能从这里过去，PAT 就能经 content/download 被读出来。
func TestSafePathRejectsHiddenScriptSegments(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	config.C = &config.Config{}
	config.C.Data.Dir = t.TempDir()
	config.C.Data.ScriptsDir = t.TempDir()

	rejected := []string{
		".git",
		".git/config",
		"SmallWorld/.git",
		"SmallWorld/.git/config",
		`SmallWorld\.git\config`,
		"SmallWorld/.GIT/HEAD",
		"SmallWorld/.svn/entries",
		"SmallWorld/node_modules/pkg/index.js",
	}
	for _, relPath := range rejected {
		if _, err := safePath(relPath, false); err == nil {
			t.Fatalf("expected safePath(%q) to be rejected", relPath)
		} else if !strings.Contains(err.Error(), "该路径不可访问") {
			t.Fatalf("expected access rejection for %q, got %v", relPath, err)
		}
	}

	// 现有可见性契约不能被破坏：dotfile / .env 仍要能正常读写
	for _, relPath := range []string{".env", ".hidden-dir/.env", "SmallWorld/demo.py"} {
		if _, err := safePath(relPath, false); err != nil {
			t.Fatalf("expected safePath(%q) to pass, got %v", relPath, err)
		}
	}
}
