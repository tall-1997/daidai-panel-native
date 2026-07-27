package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestLoadConfigShellVarsSupportsMultilineQuotedValues(t *testing.T) {
	testutil.SetupTestEnv(t)

	content := strings.Join([]string{
		"# config.sh 支持单引号和双引号跨行值",
		"export csCk='111",
		"222",
		"333",
		"444'",
		"export DOUBLE=\"first=1",
		"second#2\"",
		"PLAIN=plain-value",
		"export EMPTY=''",
		"LEGACY.KEY=legacy-value",
	}, "\n")
	if err := os.WriteFile(filepath.Join(config.C.Data.Dir, "config.sh"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config.sh: %v", err)
	}

	envMap := map[string]string{}
	loadConfigShellVars(envMap)

	want := map[string]string{
		"csCk":       "111\n222\n333\n444",
		"DOUBLE":     "first=1\nsecond#2",
		"PLAIN":      "plain-value",
		"EMPTY":      "",
		"LEGACY.KEY": "legacy-value",
	}
	for key, expected := range want {
		if got := envMap[key]; got != expected {
			t.Fatalf("expected %s=%q, got %q", key, expected, got)
		}
	}
}

func TestLoadConfigShellVarsIgnoresBrokenMultilineAndKeepsFollowingExport(t *testing.T) {
	testutil.SetupTestEnv(t)

	content := "export BROKEN='line-one\nline-two\nexport VALID='still-loaded'\n"
	if err := os.WriteFile(filepath.Join(config.C.Data.Dir, "config.sh"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config.sh: %v", err)
	}

	envMap := map[string]string{}
	loadConfigShellVars(envMap)

	if _, exists := envMap["BROKEN"]; exists {
		t.Fatalf("expected unclosed BROKEN value to be ignored, got %q", envMap["BROKEN"])
	}
	if got := envMap["VALID"]; got != "still-loaded" {
		t.Fatalf("expected following VALID export to load, got %q", got)
	}
}

func TestBuildManagedRuntimeEnvMapKeepsDatabaseEnvPriorityOverConfigFile(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	if err := os.WriteFile(
		filepath.Join(config.C.Data.Dir, "config.sh"),
		[]byte("export SAME_NAME='config-value'\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config.sh: %v", err)
	}
	if err := database.DB.Create(&model.EnvVar{
		Name:    "SAME_NAME",
		Value:   "database-value",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if got := envMap["SAME_NAME"]; got != "database-value" {
		t.Fatalf("expected database env to keep priority, got %q", got)
	}
}

func TestConfigShellMultilineValueReachesNodeProcessEnv(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testutil.SetupTestEnv(t)

	content := "export csCk='111\n222\n333\n444'\n"
	if err := os.WriteFile(filepath.Join(config.C.Data.Dir, "config.sh"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config.sh: %v", err)
	}
	envMap := map[string]string{}
	loadConfigShellVars(envMap)

	tempDir, envFile, cleanup, err := writeManagedRuntimeEnvFile(envMap)
	if err != nil {
		t.Fatalf("write runtime env file: %v", err)
	}
	defer cleanup()
	preloadFile, err := writeNodePreloadScript(tempDir, envFile, envMap)
	if err != nil {
		t.Fatalf("write node preload: %v", err)
	}

	cmd := exec.Command(nodeBin, "--require", preloadFile, "-e", "process.stdout.write(process.env.csCk || '')")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node process failed: %v, output=%s", err, string(out))
	}
	if got := string(out); got != "111\n222\n333\n444" {
		t.Fatalf("expected node process.env to keep four lines, got %q", got)
	}
}
