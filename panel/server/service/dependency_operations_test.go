package service

import (
	"os"
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
	testutil.SetupTestEnv(t)

	cmd, err := NewNpmInstallCommand("left-pad")
	if err != nil {
		t.Fatalf("build npm command: %v", err)
	}
	cmd.Env = append(cmd.Env, "npm_config_ignore_scripts=false")
	EnforceNpmScriptPolicy(cmd)
	if !stringSliceContains(cmd.Args, "--ignore-scripts") {
		t.Fatalf("expected --ignore-scripts in args: %v", cmd.Args)
	}
	found := false
	for _, entry := range cmd.Env {
		if entry == "npm_config_ignore_scripts=true" {
			found = true
		}
		if strings.EqualFold(entry, "npm_config_ignore_scripts=false") {
			t.Fatalf("stale npm script policy remained in env: %v", cmd.Env)
		}
	}
	if !found {
		t.Fatalf("expected npm_config_ignore_scripts=true in env")
	}
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
