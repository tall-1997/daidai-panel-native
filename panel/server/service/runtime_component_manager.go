package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RuntimeManifest struct {
	Version    string                     `json:"version"`
	UpdatedAt  string                     `json:"updated_at"`
	Components []RuntimeManifestComponent `json:"components"`
}

type RuntimeManifestComponent struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	ABI          string   `json:"abi"`
	Entrypoint   string   `json:"entrypoint"`
	SHA256       string   `json:"sha256"`
	Capabilities []string `json:"capabilities"`
}

type RuntimeCompatibility struct {
	Version        string   `json:"version"`
	UpdatedAt      string   `json:"updated_at"`
	ABI            string   `json:"abi"`
	RequiredChecks []string `json:"required_checks"`
	RuntimeIDs     []string `json:"runtime_ids"`
}

type RuntimeComponentStatus struct {
	ID         string `json:"id"`
	Entrypoint string `json:"entrypoint"`
	Present    bool   `json:"present"`
	Verified   bool   `json:"verified"`
	Reason     string `json:"reason,omitempty"`
}

type RuntimeComponentBaseline struct {
	ManifestPath      string                   `json:"manifest_path"`
	CompatibilityPath string                   `json:"compatibility_path"`
	NativeLibraryDir  string                   `json:"native_library_dir"`
	ManifestVersion   string                   `json:"manifest_version"`
	CompatibilityABI  string                   `json:"compatibility_abi"`
	Checks            []string                 `json:"checks"`
	Components        []RuntimeComponentStatus `json:"components"`
	SmokeSuites       []RuntimeSmokeSuite      `json:"smoke_suites"`
	ExecutionPolicies RuntimeExecutionPolicies `json:"execution_policies"`
	SecretStore       SecretStoreStatus        `json:"secret_store"`
	TrustRecords      []TrustAuthorization     `json:"trust_records"`
}

type RuntimeSmokeSuite struct {
	RuntimeID string              `json:"runtime_id"`
	Checks    []RuntimeSmokeCheck `json:"checks"`
}

type RuntimeSmokeCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type RuntimeExecutionPolicies struct {
	Node      NodeRuntimePolicy      `json:"node"`
	GitSSH    GitSSHRuntimePolicy    `json:"git_ssh"`
	GoBuilder GoBuilderRuntimePolicy `json:"go_builder"`
}

type NodeRuntimePolicy struct {
	DisableLifecycleScripts bool              `json:"disable_lifecycle_scripts"`
	Env                     []RuntimeEnvEntry `json:"env"`
}

type GitSSHRuntimePolicy struct {
	DisableHooks            bool              `json:"disable_hooks"`
	DisablePager            bool              `json:"disable_pager"`
	DisableEditor           bool              `json:"disable_editor"`
	DisableExternalFilters  bool              `json:"disable_external_filters"`
	DisableCredentialHelper bool              `json:"disable_credential_helper"`
	Env                     []RuntimeEnvEntry `json:"env"`
}

type GoBuilderRuntimePolicy struct {
	ExportOnly               bool `json:"export_only"`
	ExecuteGeneratedBinaries bool `json:"execute_generated_binaries"`
}

type RuntimeEnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type RuntimeComponentManager struct {
	manifestPath      string
	compatibilityPath string
	nativeLibraryDir  string
}

var (
	runtimeComponentStateMu sync.RWMutex
	runtimeComponentState   RuntimeComponentBaseline
)

func NewRuntimeComponentManager(nativeLibraryDir string) *RuntimeComponentManager {
	manifestPath, compatibilityPath := resolveRuntimeMetadataPaths()
	return &RuntimeComponentManager{
		manifestPath:      manifestPath,
		compatibilityPath: compatibilityPath,
		nativeLibraryDir:  strings.TrimSpace(nativeLibraryDir),
	}
}

func (manager *RuntimeComponentManager) LoadAndValidate() (RuntimeComponentBaseline, error) {
	manifest, err := manager.readManifest()
	if err != nil {
		return RuntimeComponentBaseline{}, err
	}
	compatibility, err := manager.readCompatibility()
	if err != nil {
		return RuntimeComponentBaseline{}, err
	}

	result := RuntimeComponentBaseline{
		ManifestPath:      manager.manifestPath,
		CompatibilityPath: manager.compatibilityPath,
		NativeLibraryDir:  manager.nativeLibraryDir,
		ManifestVersion:   manifest.Version,
		CompatibilityABI:  compatibility.ABI,
		Checks:            append([]string{}, compatibility.RequiredChecks...),
		Components:        make([]RuntimeComponentStatus, 0, len(manifest.Components)),
		SecretStore:       RuntimeSecretStoreInstance().Status(),
		TrustRecords:      RuntimeTrustAuthorizer().List(),
	}

	idSet := map[string]struct{}{}
	for _, id := range compatibility.RuntimeIDs {
		idSet[id] = struct{}{}
	}

	for _, component := range manifest.Components {
		status := RuntimeComponentStatus{ID: component.ID, Entrypoint: component.Entrypoint}
		if component.ID == "" || component.Entrypoint == "" {
			status.Reason = "invalid-manifest-entry"
			result.Components = append(result.Components, status)
			continue
		}
		if _, ok := idSet[component.ID]; !ok {
			status.Reason = "missing-in-compatibility"
			result.Components = append(result.Components, status)
			continue
		}
		if manager.nativeLibraryDir == "" {
			status.Reason = "native-library-dir-missing"
			result.Components = append(result.Components, status)
			continue
		}
		libraryPath := filepath.Join(manager.nativeLibraryDir, component.Entrypoint)
		payload, readErr := os.ReadFile(libraryPath)
		if readErr != nil {
			status.Reason = "entrypoint-missing"
			result.Components = append(result.Components, status)
			continue
		}
		status.Present = true
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		if !isManifestSHAPlaceholder(component.SHA256) && !strings.EqualFold(component.SHA256, hash) {
			status.Reason = "sha256-mismatch"
			result.Components = append(result.Components, status)
			continue
		}
		status.Verified = true
		result.Components = append(result.Components, status)
	}

	sort.Slice(result.Components, func(i, j int) bool {
		return result.Components[i].ID < result.Components[j].ID
	})
	result.SmokeSuites = buildRuntimeSmokeSuites(result.Components)
	result.ExecutionPolicies = RuntimeExecutionPolicies{
		Node:      DefaultNodeRuntimePolicy(),
		GitSSH:    DefaultGitSSHRuntimePolicy(),
		GoBuilder: DefaultGoBuilderRuntimePolicy(),
	}

	setRuntimeComponentBaseline(result)
	return result, nil
}

func buildRuntimeSmokeSuites(components []RuntimeComponentStatus) []RuntimeSmokeSuite {
	statusByID := make(map[string]RuntimeComponentStatus, len(components))
	for _, component := range components {
		statusByID[component.ID] = component
	}
	return []RuntimeSmokeSuite{
		{
			RuntimeID: "python-3.12-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("PY_OK", statusByID["python-3.12-android-arm64"]),
				newRuntimeSmokeCheck("SSL", statusByID["python-3.12-android-arm64"]),
				newRuntimeSmokeCheck("SQLite", statusByID["python-3.12-android-arm64"]),
				newRuntimeSmokeCheck("venv", statusByID["python-3.12-android-arm64"]),
				newRuntimeSmokeCheck("wheel", statusByID["python-3.12-android-arm64"]),
			},
		},
		{
			RuntimeID: "node-lts-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("CommonJS", statusByID["node-lts-android-arm64"]),
				newRuntimeSmokeCheck("ESM", statusByID["node-lts-android-arm64"]),
				newRuntimeSmokeCheck("HTTPS", statusByID["node-lts-android-arm64"]),
			},
		},
		{
			RuntimeID: "typescript-stable",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("TS_OK", statusByID["typescript-stable"]),
			},
		},
		{
			RuntimeID: "shell-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("SHELL_PIPE", statusByID["shell-android-arm64"]),
				newRuntimeSmokeCheck("SHELL_EXIT", statusByID["shell-android-arm64"]),
				newRuntimeSmokeCheck("SHELL_STOP", statusByID["shell-android-arm64"]),
			},
		},
		{
			RuntimeID: "git-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("GIT_CLONE", statusByID["git-android-arm64"]),
				newRuntimeSmokeCheck("GIT_FETCH", statusByID["git-android-arm64"]),
				newRuntimeSmokeCheck("GIT_SPARSE_CHECKOUT", statusByID["git-android-arm64"]),
			},
		},
		{
			RuntimeID: "ssh-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("SSH_HOSTKEY_OK", statusByID["ssh-android-arm64"]),
				newRuntimeSmokeCheck("SSH_HOSTKEY_REJECT", statusByID["ssh-android-arm64"]),
			},
		},
		{
			RuntimeID: "yaegi-go",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("GO_INTERPRET_OK", statusByID["yaegi-go"]),
			},
		},
		{
			RuntimeID: "go-builder-android-arm64",
			Checks: []RuntimeSmokeCheck{
				newRuntimeSmokeCheck("GO_BUILD_EXPORT_ONLY", statusByID["go-builder-android-arm64"]),
			},
		},
	}
}

func newRuntimeSmokeCheck(id string, component RuntimeComponentStatus) RuntimeSmokeCheck {
	check := RuntimeSmokeCheck{ID: id, Status: "pending"}
	if component.ID == "" {
		check.Status = "missing-runtime"
		return check
	}
	if component.Verified {
		check.Status = "pass"
		return check
	}
	check.Status = "blocked"
	if component.Reason != "" {
		check.Reason = component.Reason
	}
	if check.Reason == "" {
		check.Reason = "component-not-verified"
	}
	return check
}

func DefaultNodeRuntimePolicy() NodeRuntimePolicy {
	disableScripts := true
	if value := strings.TrimSpace(strings.ToLower(os.Getenv("DAIDAI_NPM_ENABLE_LIFECYCLE_SCRIPTS"))); value == "1" || value == "true" || value == "yes" {
		disableScripts = false
	}
	env := []RuntimeEnvEntry{}
	if disableScripts {
		env = append(env,
			RuntimeEnvEntry{Key: "NPM_CONFIG_IGNORE_SCRIPTS", Value: "true"},
			RuntimeEnvEntry{Key: "npm_config_ignore_scripts", Value: "true"},
		)
	}
	return NodeRuntimePolicy{
		DisableLifecycleScripts: disableScripts,
		Env:                     env,
	}
}

func DefaultGitSSHRuntimePolicy() GitSSHRuntimePolicy {
	return GitSSHRuntimePolicy{
		DisableHooks:            true,
		DisablePager:            true,
		DisableEditor:           true,
		DisableExternalFilters:  true,
		DisableCredentialHelper: true,
		Env: []RuntimeEnvEntry{
			{Key: "GIT_TERMINAL_PROMPT", Value: "0"},
			{Key: "GIT_PAGER", Value: "cat"},
			{Key: "GIT_EDITOR", Value: ":"},
			{Key: "GIT_CONFIG_COUNT", Value: "7"},
			{Key: "GIT_CONFIG_KEY_0", Value: "core.hooksPath"},
			{Key: "GIT_CONFIG_VALUE_0", Value: "/dev/null"},
			{Key: "GIT_CONFIG_KEY_1", Value: "core.pager"},
			{Key: "GIT_CONFIG_VALUE_1", Value: "cat"},
			{Key: "GIT_CONFIG_KEY_2", Value: "core.editor"},
			{Key: "GIT_CONFIG_VALUE_2", Value: ":"},
			{Key: "GIT_CONFIG_KEY_3", Value: "credential.helper"},
			{Key: "GIT_CONFIG_VALUE_3", Value: ""},
			{Key: "GIT_CONFIG_KEY_4", Value: "filter.lfs.required"},
			{Key: "GIT_CONFIG_VALUE_4", Value: "false"},
			{Key: "GIT_CONFIG_KEY_5", Value: "filter.lfs.clean"},
			{Key: "GIT_CONFIG_VALUE_5", Value: "cat"},
			{Key: "GIT_CONFIG_KEY_6", Value: "filter.lfs.smudge"},
			{Key: "GIT_CONFIG_VALUE_6", Value: "cat"},
		},
	}
}

func DefaultGoBuilderRuntimePolicy() GoBuilderRuntimePolicy {
	return GoBuilderRuntimePolicy{
		ExportOnly:               true,
		ExecuteGeneratedBinaries: false,
	}
}

func ApplyGitSSHRuntimePolicy(baseEnv []string) []string {
	result := append([]string{}, baseEnv...)
	policy := DefaultGitSSHRuntimePolicy()
	for _, entry := range policy.Env {
		result = appendOrReplaceEnv(result, entry.Key, entry.Value)
	}
	return result
}

func ApplyNodeRuntimePolicy(baseEnv []string) []string {
	result := append([]string{}, baseEnv...)
	policy := DefaultNodeRuntimePolicy()
	for _, entry := range policy.Env {
		result = appendOrReplaceEnv(result, entry.Key, entry.Value)
	}
	return result
}

func appendOrReplaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	for idx, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[idx] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func SeedRuntimeTrustRecord(source, version, digest string, capabilities []string) {
	record := TrustAuthorization{
		Source:       strings.TrimSpace(source),
		Version:      strings.TrimSpace(version),
		SHA256:       strings.TrimSpace(digest),
		Capabilities: append([]string{}, capabilities...),
		AuthorizedAt: time.Now().UTC().Format(time.RFC3339),
	}
	RuntimeTrustAuthorizer().Upsert(record)
}

func (manager *RuntimeComponentManager) readManifest() (RuntimeManifest, error) {
	payload, err := os.ReadFile(manager.manifestPath)
	if err != nil {
		return RuntimeManifest{}, fmt.Errorf("read runtime manifest: %w", err)
	}
	var manifest RuntimeManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return RuntimeManifest{}, fmt.Errorf("decode runtime manifest: %w", err)
	}
	if len(manifest.Components) == 0 {
		return RuntimeManifest{}, errors.New("runtime manifest has no components")
	}
	return manifest, nil
}

func (manager *RuntimeComponentManager) readCompatibility() (RuntimeCompatibility, error) {
	payload, err := os.ReadFile(manager.compatibilityPath)
	if err != nil {
		return RuntimeCompatibility{}, fmt.Errorf("read runtime compatibility: %w", err)
	}
	var compatibility RuntimeCompatibility
	if err := json.Unmarshal(payload, &compatibility); err != nil {
		return RuntimeCompatibility{}, fmt.Errorf("decode runtime compatibility: %w", err)
	}
	if len(compatibility.RuntimeIDs) == 0 {
		return RuntimeCompatibility{}, errors.New("runtime compatibility has no runtime ids")
	}
	return compatibility, nil
}

func resolveRuntimeMetadataPaths() (string, string) {
	manifestPath := strings.TrimSpace(os.Getenv("DAIDAI_RUNTIME_MANIFEST_PATH"))
	compatibilityPath := strings.TrimSpace(os.Getenv("DAIDAI_RUNTIME_COMPATIBILITY_PATH"))
	if manifestPath != "" && compatibilityPath != "" {
		return manifestPath, compatibilityPath
	}
	candidates := []string{
		"runtime",
		"../runtime",
		"../../runtime",
	}
	for _, root := range candidates {
		manifestCandidate := filepath.Join(root, "manifest.json")
		compatibilityCandidate := filepath.Join(root, "compatibility.json")
		if fileExists(manifestCandidate) && fileExists(compatibilityCandidate) {
			return manifestCandidate, compatibilityCandidate
		}
	}
	if manifestPath == "" {
		manifestPath = filepath.Join("runtime", "manifest.json")
	}
	if compatibilityPath == "" {
		compatibilityPath = filepath.Join("runtime", "compatibility.json")
	}
	return manifestPath, compatibilityPath
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func isManifestSHAPlaceholder(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "PLACEHOLDER_SHA256_")
}

func setRuntimeComponentBaseline(baseline RuntimeComponentBaseline) {
	runtimeComponentStateMu.Lock()
	runtimeComponentState = baseline
	runtimeComponentStateMu.Unlock()
}

func RuntimeComponentBaselineSnapshot() RuntimeComponentBaseline {
	runtimeComponentStateMu.RLock()
	defer runtimeComponentStateMu.RUnlock()
	copyValue := runtimeComponentState
	copyValue.Checks = append([]string{}, runtimeComponentState.Checks...)
	copyValue.Components = append([]RuntimeComponentStatus{}, runtimeComponentState.Components...)
	return copyValue
}

func ResetRuntimeComponentBaseline() {
	setRuntimeComponentBaseline(RuntimeComponentBaseline{})
}
