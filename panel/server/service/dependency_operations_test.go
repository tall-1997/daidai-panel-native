package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestEvaluateDependencyCompatibilityReturnsStableUnsupportedDetails(t *testing.T) {
	details := EvaluateDependencyCompatibility(model.DepTypeNodeJS, "sharp", "")
	if details.Supported() {
		t.Fatalf("expected native addon to be unsupported: %+v", details)
	}
	if details.ReasonCode != DependencyReasonNativeUnsupported {
		t.Fatalf("expected native reason, got %s", details.ReasonCode)
	}
	if details.ABI != "android-arm64-bionic" || details.NpmScriptPolicy != "ignore-scripts" {
		t.Fatalf("expected stable ABI and npm script policy, got %+v", details)
	}
	if len(details.Alternatives) == 0 {
		t.Fatalf("expected alternatives in compatibility details")
	}
}

func TestEvaluateDependencyCompatibilityAcceptsSignedNativeAllowlist(t *testing.T) {
	details := EvaluateDependencyCompatibility(model.DepTypePython, "cryptography", "3.12")
	if !details.Supported() {
		t.Fatalf("expected signed native allowlist package to be supported: %+v", details)
	}
	if !details.Native || !details.SignedAllowlist {
		t.Fatalf("expected native signed allowlist markers: %+v", details)
	}
	if details.AllowlistSHA256 == "" || details.CompatibilityKey == "" {
		t.Fatalf("expected stable allowlist metadata: %+v", details)
	}
}

func TestEnforceNpmScriptPolicy(t *testing.T) {
	cmd := exec.Command("npm", "install", "left-pad")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"npm_config_ignore_scripts=false",
		"NPM_CONFIG_IGNORE_SCRIPTS=0",
		"Npm_Config_Ignore_Scripts=yes",
		"npm_config_ignore_scripts=false",
	}

	EnforceNpmScriptPolicy(cmd)
	EnforceNpmScriptPolicy(cmd)

	if !stringSliceContains(cmd.Args, "--ignore-scripts") {
		t.Fatalf("expected --ignore-scripts in args: %v", cmd.Args)
	}
	if count := countString(cmd.Args, "--ignore-scripts"); count != 1 {
		t.Fatalf("expected one --ignore-scripts argument, got %d in %v", count, cmd.Args)
	}

	policyEntries := 0
	for _, entry := range cmd.Env {
		if entry == "npm_config_ignore_scripts=true" {
			policyEntries++
			continue
		}
		if key, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, "npm_config_ignore_scripts") {
			t.Fatalf("unexpected npm script policy variant remained in env: %q", entry)
		}
	}
	if policyEntries != 1 {
		t.Fatalf("expected one npm_config_ignore_scripts=true entry, got %d in %v", policyEntries, cmd.Env)
	}
}

func countString(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

func TestCheckDependencyQuota(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_DEPENDENCY_QUOTA_BYTES", "1")
	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("mkdir deps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "payload"), []byte("overflow"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := CheckDependencyQuota(); err == nil || !strings.Contains(err.Error(), DependencyReasonQuotaExceeded) {
		t.Fatalf("expected quota exceeded error, got %v", err)
	}
}

func TestCheckDependencyQuotaUsesProjectedUsage(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_DEPENDENCY_QUOTA_BYTES", "10")
	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("mkdir deps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "payload"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	if err := CheckDependencyQuotaWithProjection(4); err != nil {
		t.Fatalf("expected projected usage within quota, got %v", err)
	}
	if err := CheckDependencyQuotaWithProjection(6); err == nil || !strings.Contains(err.Error(), "projected 11 bytes") {
		t.Fatalf("expected projected quota error, got %v", err)
	}
}

func TestPrepareDependencyStagingRestoresNodeTargetOnFailure(t *testing.T) {
	testutil.SetupTestEnv(t)
	target := filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "left-pad")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"left-pad"}`), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	finish, state := PrepareDependencyStaging(model.DepTypeNodeJS, "left-pad", "")
	if state != "prepared" {
		t.Fatalf("expected prepared staging, got %s", state)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target to move aside during staging")
	}
	if msg := finish("failed"); !strings.Contains(msg, "restored") {
		t.Fatalf("expected restore message, got %s", msg)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("expected previous target restored: %v", err)
	}
}
