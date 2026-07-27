package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestEnsureNodePackageManifestRepairsBrokenPackageJSON(t *testing.T) {
	testutil.SetupTestEnv(t)

	nodeDir := filepath.Join(config.C.Data.Dir, "deps", "nodejs")
	moduleDir := filepath.Join(nodeDir, "node_modules", "axios")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("create node module dir: %v", err)
	}

	// 模拟截图里的 EJSONPARSE：package.json 末尾多了一个 `}`，npm install 会直接失败。
	brokenPackageJSON := "{\n  \"dependencies\": {\n    \"axios\": \"^1.7.0\"\n  }\n}\n}\n"
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(brokenPackageJSON), 0o644); err != nil {
		t.Fatalf("write broken package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"), []byte(`{"name":"axios","version":"1.7.9"}`), 0o644); err != nil {
		t.Fatalf("write module package.json: %v", err)
	}

	if err := ensureNodePackageManifest(nodeDir); err != nil {
		t.Fatalf("repair node package manifest: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(nodeDir, "package.json"))
	if err != nil {
		t.Fatalf("read repaired package.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("package.json should be valid JSON after repair: %v\n%s", err, string(data))
	}

	deps, ok := manifest["dependencies"].(map[string]any)
	if !ok {
		t.Fatalf("expected dependencies object after repair, got %#v", manifest["dependencies"])
	}
	if deps["axios"] != "^1.7.9" {
		t.Fatalf("expected axios dependency to be rebuilt from node_modules, got %#v", deps["axios"])
	}

	backups, err := filepath.Glob(filepath.Join(nodeDir, "package.json.broken-*"))
	if err != nil {
		t.Fatalf("glob broken backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one broken package.json backup, got %d", len(backups))
	}
	backupData, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read broken backup: %v", err)
	}
	if strings.TrimSpace(string(backupData)) != strings.TrimSpace(brokenPackageJSON) {
		t.Fatalf("broken backup should preserve original content, got:\n%s", string(backupData))
	}
}

func TestResolveNodeInstallPackageSpecPinsRequireCompatibleDefaults(t *testing.T) {
	tests := map[string]string{
		"uuid":              "uuid@8.3.2",
		"node-fetch":        "node-fetch@2.7.0",
		"chalk":             "chalk@4.1.2",
		"got":               "got@11.8.6",
		"nanoid":            "nanoid@3.3.7",
		"axios":             "axios@0.27.2",
		"cheerio":           "cheerio@1.0.0-rc.12",
		"https-proxy-agent": "https-proxy-agent@5.0.1",
		"query-string":      "query-string@7.1.3",
		"left-pad":          "left-pad",
	}

	for input, expected := range tests {
		if got := ResolveNodeInstallPackageSpec(input); got != expected {
			t.Fatalf("ResolveNodeInstallPackageSpec(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestResolveNodeInstallPackageSpecKeepsExplicitVersionOrSource(t *testing.T) {
	tests := map[string]string{
		"uuid@9.0.0":                  "uuid@9.0.0",
		"uuid@latest":                 "uuid@latest",
		"@scope/pkg":                  "@scope/pkg",
		"@scope/pkg@1.2.3":            "@scope/pkg@1.2.3",
		"file:../local-pkg":           "file:../local-pkg",
		"https://example.com/pkg.tgz": "https://example.com/pkg.tgz",
		"github:user/repo":            "github:user/repo",
	}

	for input, expected := range tests {
		if got := ResolveNodeInstallPackageSpec(input); got != expected {
			t.Fatalf("ResolveNodeInstallPackageSpec(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNodeInstallCompatibilityNotice(t *testing.T) {
	mapped := NodeInstallCompatibilityNotice("uuid")
	if !strings.Contains(mapped, "uuid@8.3.2") || !strings.Contains(mapped, "CommonJS 兼容映射") {
		t.Fatalf("expected mapped package notice to mention pinned version, got %q", mapped)
	}

	unmapped := NodeInstallCompatibilityNotice("left-pad")
	if !strings.Contains(unmapped, "该包未在兼容映射中，将按 npm 默认版本安装。") {
		t.Fatalf("expected unmapped package notice, got %q", unmapped)
	}

	explicit := NodeInstallCompatibilityNotice("uuid@9.0.0")
	if !strings.Contains(explicit, "已按你指定的版本或来源安装") {
		t.Fatalf("expected explicit version notice, got %q", explicit)
	}
}

func TestNewNpmInstallCommandPinsRequireCompatibleDefault(t *testing.T) {
	testutil.SetupTestEnv(t)

	cmd, err := NewNpmInstallCommand("uuid")
	if err != nil {
		t.Fatalf("build npm install command: %v", err)
	}
	if len(cmd.Args) == 0 || cmd.Args[len(cmd.Args)-1] != "uuid@8.3.2" {
		t.Fatalf("expected npm install to pin uuid@8.3.2, args=%v", cmd.Args)
	}
}
