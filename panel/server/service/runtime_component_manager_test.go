package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeComponentManagerLoadsManifestWithPlaceholderHash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libpython_exec.so"), []byte("python"), 0o755); err != nil {
		t.Fatalf("write runtime entrypoint: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{{
			ID:         "python-3.12-android-arm64",
			ABI:        "arm64-v8a",
			Entrypoint: "libpython_exec.so",
			SHA256:     "PLACEHOLDER_SHA256_PYTHON",
		}},
	}
	compatibility := RuntimeCompatibility{
		Version:    "1",
		ABI:        "arm64-v8a",
		RuntimeIDs: []string{"python-3.12-android-arm64"},
	}
	writeJSON(t, manifestPath, manifest)
	writeJSON(t, compatibilityPath, compatibility)

	manager := &RuntimeComponentManager{
		manifestPath:      manifestPath,
		compatibilityPath: compatibilityPath,
		nativeLibraryDir:  nativeDir,
	}
	baseline, err := manager.LoadAndValidate()
	if err != nil {
		t.Fatalf("load and validate runtime baseline: %v", err)
	}
	if len(baseline.Components) != 1 {
		t.Fatalf("component count=%d want=1", len(baseline.Components))
	}
	if !baseline.Components[0].Present || !baseline.Components[0].Verified {
		t.Fatalf("unexpected component status: %+v", baseline.Components[0])
	}
}

func TestRuntimeComponentManagerDetectsSHA256Mismatch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libnode_exec.so"), []byte("node"), 0o755); err != nil {
		t.Fatalf("write runtime entrypoint: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{{
			ID:         "node-lts-android-arm64",
			ABI:        "arm64-v8a",
			Entrypoint: "libnode_exec.so",
			SHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
	compatibility := RuntimeCompatibility{
		Version:    "1",
		ABI:        "arm64-v8a",
		RuntimeIDs: []string{"node-lts-android-arm64"},
	}
	writeJSON(t, manifestPath, manifest)
	writeJSON(t, compatibilityPath, compatibility)

	manager := &RuntimeComponentManager{
		manifestPath:      manifestPath,
		compatibilityPath: compatibilityPath,
		nativeLibraryDir:  nativeDir,
	}
	baseline, err := manager.LoadAndValidate()
	if err != nil {
		t.Fatalf("load and validate runtime baseline: %v", err)
	}
	if len(baseline.Components) != 1 {
		t.Fatalf("component count=%d want=1", len(baseline.Components))
	}
	if baseline.Components[0].Reason != "sha256-mismatch" {
		t.Fatalf("reason=%q want=sha256-mismatch", baseline.Components[0].Reason)
	}
}

func TestRuntimeComponentManagerBuildsSmokeSuitesAndPolicies(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libpython_exec.so"), []byte("python"), 0o755); err != nil {
		t.Fatalf("write python runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libnode_exec.so"), []byte("node"), 0o755); err != nil {
		t.Fatalf("write node runtime: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{
			{ID: "python-3.12-android-arm64", ABI: "arm64-v8a", Entrypoint: "libpython_exec.so", SHA256: "PLACEHOLDER_SHA256_PYTHON"},
			{ID: "node-lts-android-arm64", ABI: "arm64-v8a", Entrypoint: "libnode_exec.so", SHA256: "PLACEHOLDER_SHA256_NODE"},
			{ID: "typescript-stable", ABI: "arm64-v8a", Entrypoint: "libnode_exec.so", SHA256: "PLACEHOLDER_SHA256_TYPESCRIPT"},
		},
	}
	compatibility := RuntimeCompatibility{
		Version: "1",
		ABI:     "arm64-v8a",
		RuntimeIDs: []string{
			"python-3.12-android-arm64",
			"node-lts-android-arm64",
			"typescript-stable",
		},
	}
	writeJSON(t, manifestPath, manifest)
	writeJSON(t, compatibilityPath, compatibility)

	SeedRuntimeTrustRecord("subscription", "v1", strings.Repeat("a", 64), []string{"python", "venv"})

	manager := &RuntimeComponentManager{
		manifestPath:      manifestPath,
		compatibilityPath: compatibilityPath,
		nativeLibraryDir:  nativeDir,
	}
	baseline, err := manager.LoadAndValidate()
	if err != nil {
		t.Fatalf("load and validate runtime baseline: %v", err)
	}
	if len(baseline.SmokeSuites) != 8 {
		t.Fatalf("smoke suite count=%d want=8", len(baseline.SmokeSuites))
	}
	if !baseline.ExecutionPolicies.Node.DisableLifecycleScripts {
		t.Fatal("expected node lifecycle scripts to be disabled by default")
	}
	if !baseline.ExecutionPolicies.GitSSH.DisableCredentialHelper {
		t.Fatal("expected git credential helper to be disabled by default")
	}
	if !baseline.ExecutionPolicies.GoBuilder.ExportOnly || baseline.ExecutionPolicies.GoBuilder.ExecuteGeneratedBinaries {
		t.Fatalf("unexpected go builder policy: %+v", baseline.ExecutionPolicies.GoBuilder)
	}
	if !baseline.SecretStore.Ready {
		t.Fatalf("expected secret store ready status, got %+v", baseline.SecretStore)
	}
	if len(baseline.TrustRecords) == 0 {
		t.Fatal("expected at least one trust authorization record")
	}
	pythonSuite := findRuntimeSuite(baseline.SmokeSuites, "python-3.12-android-arm64")
	if pythonSuite == nil {
		t.Fatal("python runtime suite missing")
	}
	if !hasCheckID(*pythonSuite, "PY_OK") || !hasCheckID(*pythonSuite, "SSL") || !hasCheckID(*pythonSuite, "SQLite") || !hasCheckID(*pythonSuite, "venv") || !hasCheckID(*pythonSuite, "wheel") {
		t.Fatalf("python suite checks mismatch: %+v", pythonSuite.Checks)
	}
	nodeSuite := findRuntimeSuite(baseline.SmokeSuites, "node-lts-android-arm64")
	if nodeSuite == nil {
		t.Fatal("node runtime suite missing")
	}
	if !hasCheckID(*nodeSuite, "CommonJS") || !hasCheckID(*nodeSuite, "ESM") || !hasCheckID(*nodeSuite, "HTTPS") {
		t.Fatalf("node suite checks mismatch: %+v", nodeSuite.Checks)
	}
}

func TestApplyGitSSHRuntimePolicySetsSecurityEnv(t *testing.T) {
	t.Parallel()

	env := ApplyGitSSHRuntimePolicy([]string{"PATH=/usr/bin", "GIT_EDITOR=vim"})
	assertEnvValue(t, env, "GIT_TERMINAL_PROMPT", "0")
	assertEnvValue(t, env, "GIT_PAGER", "cat")
	assertEnvValue(t, env, "GIT_EDITOR", ":")
	assertEnvValue(t, env, "GIT_CONFIG_COUNT", "7")
	assertEnvValue(t, env, "GIT_CONFIG_KEY_3", "credential.helper")
	assertEnvValue(t, env, "GIT_CONFIG_VALUE_3", "")
}

func TestLocalSecretStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := RuntimeSecretStoreInstance()
	sealed, err := store.Seal(context.Background(), "runtime-token", []byte("secret"))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	opened, err := store.Open(context.Background(), "runtime-token", sealed)
	if err != nil {
		t.Fatalf("open secret: %v", err)
	}
	if string(opened) != "secret" {
		t.Fatalf("opened secret=%q want=secret", string(opened))
	}
}

func findRuntimeSuite(suites []RuntimeSmokeSuite, runtimeID string) *RuntimeSmokeSuite {
	for i := range suites {
		if suites[i].RuntimeID == runtimeID {
			return &suites[i]
		}
	}
	return nil
}

func hasCheckID(suite RuntimeSmokeSuite, id string) bool {
	for _, check := range suite.Checks {
		if check.ID == id {
			return true
		}
	}
	return false
}

func assertEnvValue(t *testing.T, env []string, key, want string) {
	t.Helper()
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			got := strings.TrimPrefix(entry, prefix)
			if got != want {
				t.Fatalf("env %s=%q want=%q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("missing env key %s", key)
}

func writeJSON(t *testing.T, path string, payload any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
}
