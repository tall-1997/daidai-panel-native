package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeComponentManagerRejectsPlaceholderHashByDefault(t *testing.T) {
	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	pythonELF := fakeAArch64ELF("python")
	if err := os.WriteFile(filepath.Join(nativeDir, "libpython_exec.so"), pythonELF, 0o755); err != nil {
		t.Fatalf("write runtime entrypoint: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{{
			ID:         "python-launcher-3.14-android-arm64",
			ABI:        "arm64-v8a",
			Entrypoint: "libpython_exec.so",
			SHA256:     "PLACEHOLDER_SHA256_PYTHON",
		}},
	}
	compatibility := RuntimeCompatibility{
		Version:    "1",
		ABI:        "arm64-v8a",
		RuntimeIDs: []string{"python-launcher-3.14-android-arm64"},
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
	if !baseline.Components[0].Present {
		t.Fatalf("unexpected component status: %+v", baseline.Components[0])
	}
	if baseline.Components[0].Reason != "sha256-placeholder" {
		t.Fatalf("reason=%q want=sha256-placeholder", baseline.Components[0].Reason)
	}
}

func TestRuntimeComponentManagerAllowsPlaceholderHashWithDevFlag(t *testing.T) {
	t.Setenv("DAIDAI_RUNTIME_ALLOW_PLACEHOLDER_HASH", "1")

	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	pythonELF := fakeAArch64ELF("python")
	if err := os.WriteFile(filepath.Join(nativeDir, "libpython_exec.so"), pythonELF, 0o755); err != nil {
		t.Fatalf("write runtime entrypoint: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{{
			ID:         "python-launcher-3.14-android-arm64",
			ABI:        "arm64-v8a",
			Entrypoint: "libpython_exec.so",
			SHA256:     "PLACEHOLDER_SHA256_PYTHON",
		}},
	}
	compatibility := RuntimeCompatibility{
		Version:    "1",
		ABI:        "arm64-v8a",
		RuntimeIDs: []string{"python-launcher-3.14-android-arm64"},
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
	if !baseline.Components[0].Verified {
		t.Fatalf("expected placeholder to be accepted in dev mode, got %+v", baseline.Components[0])
	}
}

func TestRuntimeComponentManagerDetectsSHA256Mismatch(t *testing.T) {
	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	nodeELF := fakeAArch64ELF("node")
	if err := os.WriteFile(filepath.Join(nativeDir, "libnode_exec.so"), nodeELF, 0o755); err != nil {
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
	if baseline.Components[0].FailureClass != "asset-integrity" {
		t.Fatalf("failure class=%q want=asset-integrity", baseline.Components[0].FailureClass)
	}
}

func TestRuntimeComponentManagerBuildsSmokeSuitesAndPolicies(t *testing.T) {
	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	if err := InitializeRuntimeSecurity(tempDir); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}
	pythonELF := fakeAArch64ELF("python")
	nodeELF := fakeAArch64ELF("node")
	if err := os.WriteFile(filepath.Join(nativeDir, "libpython_exec.so"), pythonELF, 0o755); err != nil {
		t.Fatalf("write python runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "libnode_exec.so"), nodeELF, 0o755); err != nil {
		t.Fatalf("write node runtime: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{
			{ID: "python-launcher-3.14-android-arm64", ABI: "arm64-v8a", Entrypoint: "libpython_exec.so", SHA256: sha256Hex(pythonELF), RuntimeType: "optional-build-asset", Isolation: "android-app-sandbox"},
			{ID: "node-lts-android-arm64", ABI: "arm64-v8a", Entrypoint: "libnode_exec.so", SHA256: sha256Hex(nodeELF)},
			{ID: "typescript-stable", ABI: "arm64-v8a", Entrypoint: "libnode_exec.so", SHA256: sha256Hex(nodeELF)},
		},
	}
	compatibility := RuntimeCompatibility{
		Version:        "1",
		ABI:            "arm64-v8a",
		ContainerModel: "layered-linux-runtime",
		RuntimeIDs: []string{
			"python-launcher-3.14-android-arm64",
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
	if len(baseline.SmokeSuites) != len(manifest.Components) {
		t.Fatalf("smoke suite count=%d want=%d", len(baseline.SmokeSuites), len(manifest.Components))
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
	if !baseline.TrustAuthorizer.Ready {
		t.Fatalf("expected trust authorizer ready status, got %+v", baseline.TrustAuthorizer)
	}
	if len(baseline.TrustRecords) == 0 {
		t.Fatal("expected at least one trust authorization record")
	}
	if baseline.ContainerModel != "layered-linux-runtime" {
		t.Fatalf("container model=%q want=layered-linux-runtime", baseline.ContainerModel)
	}
	pythonComponent := findRuntimeComponent(baseline.Components, "python-launcher-3.14-android-arm64")
	if pythonComponent == nil || pythonComponent.RuntimeType == "" || pythonComponent.Isolation == "" {
		t.Fatalf("component runtime metadata missing: %+v", pythonComponent)
	}
	pythonSuite := findRuntimeSuite(baseline.SmokeSuites, "python-launcher-3.14-android-arm64")
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

func TestRuntimeComponentManagerRejectsEscapedEntrypoint(t *testing.T) {
	tempDir := t.TempDir()
	nativeDir := filepath.Join(tempDir, "libs")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	compatibilityPath := filepath.Join(tempDir, "compatibility.json")
	manifest := RuntimeManifest{
		Version: "1",
		Components: []RuntimeManifestComponent{{
			ID:         "python-launcher-3.14-android-arm64",
			ABI:        "arm64-v8a",
			Entrypoint: "../escape.so",
			SHA256:     strings.Repeat("a", 64),
		}},
	}
	compatibility := RuntimeCompatibility{
		Version:    "1",
		ABI:        "arm64-v8a",
		RuntimeIDs: []string{"python-launcher-3.14-android-arm64"},
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
	if baseline.Components[0].Reason != "entrypoint-invalid" {
		t.Fatalf("reason=%q want=entrypoint-invalid", baseline.Components[0].Reason)
	}
}

func TestRuntimeBaselineComponentFailuresAreIsolated(t *testing.T) {
	t.Parallel()
	baseline := RuntimeComponentBaseline{Components: []RuntimeComponentStatus{{Reason: "invalid-manifest-entry"}}}
	if !RuntimeBaselineDegraded(baseline) {
		t.Fatal("invalid manifest component entry must degrade readiness")
	}
	baseline = RuntimeComponentBaseline{Components: []RuntimeComponentStatus{{Reason: "native-library-dir-missing"}}}
	if !RuntimeBaselineDegraded(baseline) {
		t.Fatal("missing runtime directory must degrade readiness")
	}
}

func TestRuntimeBaselineSmokeFailuresAreIsolated(t *testing.T) {
	t.Parallel()
	baseline := RuntimeComponentBaseline{SmokeSuites: []RuntimeSmokeSuite{{RuntimeID: "python", Checks: []RuntimeSmokeCheck{{ID: "PY_OK", Status: "failed"}}}}}
	if !RuntimeBaselineDegraded(baseline) {
		t.Fatal("failed runtime smoke check must degrade readiness")
	}
}

func TestRuntimeBaselineErrorClassifiesUnparseableCoreMetadata(t *testing.T) {
	t.Parallel()
	manager := &RuntimeComponentManager{
		manifestPath:      filepath.Join(t.TempDir(), "missing-manifest.json"),
		compatibilityPath: filepath.Join(t.TempDir(), "missing-compatibility.json"),
	}
	_, err := manager.LoadAndValidate()
	if !errors.Is(err, ErrRuntimeCoreMetadata) {
		t.Fatalf("error=%v want runtime core metadata classification", err)
	}
}

func TestApplyGitSSHRuntimePolicySetsSecurityEnv(t *testing.T) {
	env := ApplyGitSSHRuntimePolicy([]string{"PATH=/usr/bin", "GIT_EDITOR=vim"})
	assertEnvValue(t, env, "GIT_TERMINAL_PROMPT", "0")
	assertEnvValue(t, env, "GIT_PAGER", "cat")
	assertEnvValue(t, env, "GIT_EDITOR", ":")
	assertEnvValue(t, env, "GIT_CONFIG_COUNT", "7")
	assertEnvValue(t, env, "GIT_CONFIG_KEY_3", "credential.helper")
	assertEnvValue(t, env, "GIT_CONFIG_VALUE_3", "")
}

func TestLocalSecretStoreRoundTrip(t *testing.T) {
	if err := InitializeRuntimeSecurity(t.TempDir()); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

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
	if string(sealed.Cipher) == "secret" {
		t.Fatal("cipher payload should be encrypted")
	}
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func findRuntimeSuite(suites []RuntimeSmokeSuite, runtimeID string) *RuntimeSmokeSuite {
	for i := range suites {
		if suites[i].RuntimeID == runtimeID {
			return &suites[i]
		}
	}
	return nil
}

func findRuntimeComponent(components []RuntimeComponentStatus, runtimeID string) *RuntimeComponentStatus {
	for i := range components {
		if components[i].ID == runtimeID {
			return &components[i]
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

func fakeAArch64ELF(tag string) []byte {
	base := make([]byte, 64)
	base[0] = 0x7f
	base[1] = 'E'
	base[2] = 'L'
	base[3] = 'F'
	base[4] = 2
	base[5] = 1
	base[6] = 1
	base[16] = 2
	base[18] = 183
	base[19] = 0
	return append(base, []byte(tag)...)
}
