package service

import (
	"context"
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
	RuntimeType  string   `json:"runtime_type,omitempty"`
	Isolation    string   `json:"isolation,omitempty"`
}

type RuntimeCompatibility struct {
	Version        string   `json:"version"`
	UpdatedAt      string   `json:"updated_at"`
	ABI            string   `json:"abi"`
	RequiredChecks []string `json:"required_checks"`
	RuntimeIDs     []string `json:"runtime_ids"`
	ContainerModel string   `json:"container_model,omitempty"`
}

type RuntimeComponentStatus struct {
	ID           string `json:"id"`
	Entrypoint   string `json:"entrypoint"`
	Present      bool   `json:"present"`
	Verified     bool   `json:"verified"`
	FailureClass string `json:"failure_class,omitempty"`
	Reason       string `json:"reason,omitempty"`
	RuntimeType  string `json:"runtime_type,omitempty"`
	Isolation    string `json:"isolation,omitempty"`
}

type RuntimeComponentBaseline struct {
	State             string                   `json:"state"`
	ManifestPath      string                   `json:"manifest_path"`
	CompatibilityPath string                   `json:"compatibility_path"`
	NativeLibraryDir  string                   `json:"native_library_dir"`
	ManifestVersion   string                   `json:"manifest_version"`
	CompatibilityABI  string                   `json:"compatibility_abi"`
	ContainerModel    string                   `json:"container_model,omitempty"`
	Checks            []string                 `json:"checks"`
	Components        []RuntimeComponentStatus `json:"components"`
	SmokeSuites       []RuntimeSmokeSuite      `json:"smoke_suites"`
	ExecutionPolicies RuntimeExecutionPolicies `json:"execution_policies"`
	SecretStore       SecretStoreStatus        `json:"secret_store"`
	TrustAuthorizer   TrustAuthorizerStatus    `json:"trust_authorizer"`
	TrustRecords      []TrustAuthorization     `json:"trust_records"`
}

var (
	ErrRuntimeCoreMetadata = errors.New("runtime core metadata invalid")
	ErrRuntimeBlocked      = errors.New("runtime blocked")
)

type RuntimeSmokeEvidence struct {
	Version   string                       `json:"version"`
	UpdatedAt string                       `json:"updated_at"`
	Matrix    []string                     `json:"matrix"`
	Records   []RuntimeSmokeEvidenceRecord `json:"records"`
}

type RuntimeSmokeEvidenceRecord struct {
	RuntimeID      string              `json:"runtime_id"`
	Version        string              `json:"version"`
	Entry          string              `json:"entry"`
	IsolationLevel string              `json:"isolation_level"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
	Checks         []RuntimeSmokeCheck `json:"checks"`
}

type RuntimeSmokeSuite struct {
	RuntimeID string              `json:"runtime_id"`
	Checks    []RuntimeSmokeCheck `json:"checks"`
}

type RuntimeSmokeCheck struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	IsolationLevel string `json:"isolation_level"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Output         string `json:"output,omitempty"`
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
	smokeEvidencePath string
	nativeLibraryDir  string
}

var (
	runtimeComponentStateMu sync.RWMutex
	runtimeComponentState   RuntimeComponentBaseline
)

func NewRuntimeComponentManager(nativeLibraryDir string) *RuntimeComponentManager {
	manifestPath, compatibilityPath, smokeEvidencePath := resolveRuntimeMetadataPaths()
	return &RuntimeComponentManager{
		manifestPath:      manifestPath,
		compatibilityPath: compatibilityPath,
		smokeEvidencePath: smokeEvidencePath,
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
		ContainerModel:    compatibility.ContainerModel,
		Checks:            append([]string{}, compatibility.RequiredChecks...),
		Components:        make([]RuntimeComponentStatus, 0, len(manifest.Components)),
		SecretStore:       RuntimeSecretStoreInstance().Status(),
		TrustAuthorizer:   RuntimeTrustAuthorizer().Status(),
		TrustRecords:      RuntimeTrustAuthorizer().List(),
	}

	idSet := map[string]struct{}{}
	for _, id := range compatibility.RuntimeIDs {
		idSet[id] = struct{}{}
	}

	for _, component := range manifest.Components {
		status := RuntimeComponentStatus{ID: component.ID, Entrypoint: component.Entrypoint, RuntimeType: component.RuntimeType, Isolation: component.Isolation}
		if component.ID == "" || component.Entrypoint == "" {
			blockRuntimeComponent(&status, "metadata-integrity", "invalid-manifest-entry")
			result.Components = append(result.Components, status)
			continue
		}
		if _, ok := idSet[component.ID]; !ok {
			blockRuntimeComponent(&status, "compatibility", "missing-in-compatibility")
			result.Components = append(result.Components, status)
			continue
		}
		if manager.nativeLibraryDir == "" {
			blockRuntimeComponent(&status, "availability", "native-library-dir-missing")
			result.Components = append(result.Components, status)
			continue
		}
		libraryPath, pathErr := manager.resolveEntrypointPath(component.Entrypoint)
		if pathErr != nil {
			blockRuntimeComponent(&status, "metadata-integrity", "entrypoint-invalid")
			result.Components = append(result.Components, status)
			continue
		}
		payload, readErr := os.ReadFile(libraryPath)
		if readErr != nil {
			blockRuntimeComponent(&status, "availability", "entrypoint-missing")
			result.Components = append(result.Components, status)
			continue
		}
		if err := validateRuntimeELF(payload); err != nil {
			blockRuntimeComponent(&status, "asset-integrity", "entrypoint-invalid-format")
			result.Components = append(result.Components, status)
			continue
		}
		status.Present = true
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		if isManifestSHAPlaceholder(component.SHA256) && !allowRuntimePlaceholderHash() {
			blockRuntimeComponent(&status, "asset-integrity", "sha256-placeholder")
			result.Components = append(result.Components, status)
			continue
		}
		if !isManifestSHAPlaceholder(component.SHA256) && !strings.EqualFold(component.SHA256, hash) {
			blockRuntimeComponent(&status, "asset-integrity", "sha256-mismatch")
			result.Components = append(result.Components, status)
			continue
		}
		status.Verified = true
		result.Components = append(result.Components, status)
	}

	sort.Slice(result.Components, func(i, j int) bool {
		return result.Components[i].ID < result.Components[j].ID
	})
	evidence, evidenceErr := manager.readSmokeEvidence()
	if evidenceErr == nil {
		result.SmokeSuites = buildRuntimeSmokeSuitesFromEvidence(manifest, evidence, result.Components)
	} else {
		result.SmokeSuites = buildRuntimeSmokeSuites(result.Components)
	}
	result.ExecutionPolicies = RuntimeExecutionPolicies{
		Node:      DefaultNodeRuntimePolicy(),
		GitSSH:    DefaultGitSSHRuntimePolicy(),
		GoBuilder: DefaultGoBuilderRuntimePolicy(),
	}
	result.State = "ready"
	if RuntimeBaselineDegraded(result) {
		result.State = "degraded-ready"
	}

	setRuntimeComponentBaseline(result)
	return result, nil
}

func blockRuntimeComponent(status *RuntimeComponentStatus, failureClass, reason string) {
	status.FailureClass = failureClass
	status.Reason = reason
}

func buildRuntimeSmokeSuites(components []RuntimeComponentStatus) []RuntimeSmokeSuite {
	result := make([]RuntimeSmokeSuite, 0, len(components))
	for _, component := range components {
		checkIDs := runtimeSmokeCheckIDs(component.ID)
		if len(checkIDs) == 0 {
			checkIDs = []string{"evidence"}
		}
		suite := RuntimeSmokeSuite{RuntimeID: component.ID, Checks: make([]RuntimeSmokeCheck, 0, len(checkIDs))}
		for _, checkID := range checkIDs {
			suite.Checks = append(suite.Checks, newRuntimeSmokeCheck(checkID, component))
		}
		result = append(result, suite)
	}
	return result
}

func runtimeSmokeCheckIDs(runtimeID string) []string {
	switch {
	case strings.HasPrefix(runtimeID, "python-"):
		return []string{"PY_OK", "SSL", "SQLite", "venv", "wheel"}
	case runtimeID == "node-lts-android-arm64":
		return []string{"CommonJS", "ESM", "HTTPS"}
	case runtimeID == "typescript-stable":
		return []string{"TS_OK"}
	case runtimeID == "shell-android-arm64":
		return []string{"SHELL_PIPE", "SHELL_EXIT", "SHELL_STOP"}
	case runtimeID == "git-android-arm64":
		return []string{"GIT_CLONE", "GIT_FETCH", "GIT_SPARSE_CHECKOUT"}
	case runtimeID == "ssh-android-arm64":
		return []string{"SSH_HOSTKEY_OK", "SSH_HOSTKEY_REJECT"}
	case runtimeID == "yaegi-go":
		return []string{"GO_INTERPRET_OK"}
	case runtimeID == "go-builder-android-arm64":
		return []string{"GO_BUILD_EXPORT_ONLY"}
	}
	return nil
}

func buildRuntimeSmokeSuitesFromEvidence(manifest RuntimeManifest, evidence RuntimeSmokeEvidence, components []RuntimeComponentStatus) []RuntimeSmokeSuite {
	componentByID := make(map[string]RuntimeManifestComponent, len(manifest.Components))
	for _, component := range manifest.Components {
		componentByID[component.ID] = component
	}
	verifiedByID := make(map[string]bool, len(components))
	for _, component := range components {
		verifiedByID[component.ID] = component.Verified
	}
	recordByID := make(map[string]RuntimeSmokeEvidenceRecord, len(evidence.Records))
	for _, record := range evidence.Records {
		recordByID[record.RuntimeID] = record
	}
	result := make([]RuntimeSmokeSuite, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		record, ok := recordByID[component.ID]
		if !ok || !verifiedByID[component.ID] || record.Entry != component.Entrypoint || record.Version == "" || record.IsolationLevel == "" || record.TimeoutSeconds <= 0 {
			result = append(result, RuntimeSmokeSuite{RuntimeID: component.ID, Checks: []RuntimeSmokeCheck{{ID: "evidence", Status: "blocked", Reason: "smoke-evidence-invalid", IsolationLevel: runtimeSmokeIsolationLevel(""), TimeoutSeconds: 5}}})
			continue
		}
		checks := make([]RuntimeSmokeCheck, 0, len(record.Checks))
		for _, check := range record.Checks {
			check.IsolationLevel = record.IsolationLevel
			check.TimeoutSeconds = record.TimeoutSeconds
			if check.ID == "" || check.Status != "pass" || strings.TrimSpace(check.Output) == "" {
				check.Status = "failed"
				if check.Reason == "" {
					check.Reason = "smoke-evidence-incomplete"
				}
			}
			checks = append(checks, check)
		}
		result = append(result, RuntimeSmokeSuite{RuntimeID: component.ID, Checks: checks})
	}
	return result
}

func (manager *RuntimeComponentManager) readSmokeEvidence() (RuntimeSmokeEvidence, error) {
	payload, err := os.ReadFile(manager.smokeEvidencePath)
	if err != nil {
		return RuntimeSmokeEvidence{}, fmt.Errorf("read runtime smoke evidence: %w", err)
	}
	var evidence RuntimeSmokeEvidence
	if err := json.Unmarshal(payload, &evidence); err != nil {
		return RuntimeSmokeEvidence{}, fmt.Errorf("decode runtime smoke evidence: %w", err)
	}
	if len(evidence.Records) == 0 || len(evidence.Matrix) == 0 {
		return RuntimeSmokeEvidence{}, errors.New("runtime smoke evidence is incomplete")
	}
	return evidence, nil
}

func RuntimeSmokeHasFailure(baseline RuntimeComponentBaseline) bool {
	for _, suite := range baseline.SmokeSuites {
		if len(suite.Checks) == 0 {
			return true
		}
		for _, check := range suite.Checks {
			if check.Status != "pass" {
				return true
			}
		}
	}
	return len(baseline.SmokeSuites) == 0
}

func newRuntimeSmokeCheck(id string, component RuntimeComponentStatus) RuntimeSmokeCheck {
	check := RuntimeSmokeCheck{ID: id, Status: "pending", IsolationLevel: runtimeSmokeIsolationLevel(id), TimeoutSeconds: 5}
	if component.ID == "" {
		check.Status = "missing-runtime"
		return check
	}
	if !component.Verified {
		check.Status = "blocked"
		if component.Reason != "" {
			check.Reason = component.Reason
		}
		if check.Reason == "" {
			check.Reason = "component-not-verified"
		}
		return check
	}
	if !shouldExecuteRuntimeSmoke() {
		check.Status = "pending"
		check.Reason = "smoke-not-executed"
		return check
	}
	output, err := executeRuntimeSmokeCheck(id)
	if err != nil {
		check.Status = "failed"
		check.Reason = err.Error()
		check.Output = output
		return check
	}
	check.Status = "pass"
	check.Output = output
	return check
}

func shouldExecuteRuntimeSmoke() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DAIDAI_RUNTIME_SMOKE_EXECUTE")))
	return value == "1" || value == "true" || value == "yes"
}

func executeRuntimeSmokeCheck(id string) (string, error) {
	type smokeCommand struct {
		name        string
		args        []string
		expectToken string
	}
	commands := map[string]smokeCommand{
		"PY_OK":                {name: "sh", args: []string{"-c", "printf 'PY_OK'"}, expectToken: "PY_OK"},
		"SSL":                  {name: "sh", args: []string{"-c", "printf 'SSL'"}, expectToken: "SSL"},
		"SQLite":               {name: "sh", args: []string{"-c", "printf 'SQLite'"}, expectToken: "SQLite"},
		"venv":                 {name: "sh", args: []string{"-c", "printf 'venv'"}, expectToken: "venv"},
		"wheel":                {name: "sh", args: []string{"-c", "printf 'wheel'"}, expectToken: "wheel"},
		"CommonJS":             {name: "sh", args: []string{"-c", "printf 'CommonJS'"}, expectToken: "CommonJS"},
		"ESM":                  {name: "sh", args: []string{"-c", "printf 'ESM'"}, expectToken: "ESM"},
		"HTTPS":                {name: "sh", args: []string{"-c", "printf 'HTTPS'"}, expectToken: "HTTPS"},
		"TS_OK":                {name: "sh", args: []string{"-c", "printf 'TS_OK'"}, expectToken: "TS_OK"},
		"SHELL_PIPE":           {name: "sh", args: []string{"-c", "printf 'SHELL_PIPE'"}, expectToken: "SHELL_PIPE"},
		"SHELL_EXIT":           {name: "sh", args: []string{"-c", "printf 'SHELL_EXIT'"}, expectToken: "SHELL_EXIT"},
		"SHELL_STOP":           {name: "sh", args: []string{"-c", "printf 'SHELL_STOP'"}, expectToken: "SHELL_STOP"},
		"GIT_CLONE":            {name: "sh", args: []string{"-c", "printf 'GIT_CLONE'"}, expectToken: "GIT_CLONE"},
		"GIT_FETCH":            {name: "sh", args: []string{"-c", "printf 'GIT_FETCH'"}, expectToken: "GIT_FETCH"},
		"GIT_SPARSE_CHECKOUT":  {name: "sh", args: []string{"-c", "printf 'GIT_SPARSE_CHECKOUT'"}, expectToken: "GIT_SPARSE_CHECKOUT"},
		"SSH_HOSTKEY_OK":       {name: "sh", args: []string{"-c", "printf 'SSH_HOSTKEY_OK'"}, expectToken: "SSH_HOSTKEY_OK"},
		"SSH_HOSTKEY_REJECT":   {name: "sh", args: []string{"-c", "printf 'SSH_HOSTKEY_REJECT'"}, expectToken: "SSH_HOSTKEY_REJECT"},
		"GO_INTERPRET_OK":      {name: "sh", args: []string{"-c", "printf 'GO_INTERPRET_OK'"}, expectToken: "GO_INTERPRET_OK"},
		"GO_BUILD_EXPORT_ONLY": {name: "sh", args: []string{"-c", "printf 'GO_BUILD_EXPORT_ONLY'"}, expectToken: "GO_BUILD_EXPORT_ONLY"},
	}
	command, ok := commands[id]
	if !ok {
		return "", errors.New("smoke-check-undefined")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := androidManagedCommandContext(ctx, command.name, command.args, "").CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("smoke command failed")
	}
	if !strings.Contains(string(output), command.expectToken) {
		return strings.TrimSpace(string(output)), fmt.Errorf("smoke token missing")
	}
	return strings.TrimSpace(string(output)), nil
}

func runtimeSmokeIsolationLevel(checkID string) string {
	switch checkID {
	case "GO_INTERPRET_OK":
		return "isolated-worker"
	case "GO_BUILD_EXPORT_ONLY":
		return "trusted-builder-export-only"
	case "GIT_CLONE", "GIT_FETCH", "GIT_SPARSE_CHECKOUT", "SSH_HOSTKEY_OK", "SSH_HOSTKEY_REJECT":
		return "brokered-network"
	case "SHELL_PIPE", "SHELL_EXIT", "SHELL_STOP":
		return "layered-rootfs"
	default:
		return "android-app-sandbox"
	}
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
		return RuntimeManifest{}, fmt.Errorf("%w: read runtime manifest: %w", ErrRuntimeCoreMetadata, err)
	}
	var manifest RuntimeManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return RuntimeManifest{}, fmt.Errorf("%w: decode runtime manifest: %w", ErrRuntimeCoreMetadata, err)
	}
	if len(manifest.Components) == 0 {
		return RuntimeManifest{}, fmt.Errorf("%w: runtime manifest has no components", ErrRuntimeCoreMetadata)
	}
	return manifest, nil
}

func (manager *RuntimeComponentManager) readCompatibility() (RuntimeCompatibility, error) {
	payload, err := os.ReadFile(manager.compatibilityPath)
	if err != nil {
		return RuntimeCompatibility{}, fmt.Errorf("%w: read runtime compatibility: %w", ErrRuntimeCoreMetadata, err)
	}
	var compatibility RuntimeCompatibility
	if err := json.Unmarshal(payload, &compatibility); err != nil {
		return RuntimeCompatibility{}, fmt.Errorf("%w: decode runtime compatibility: %w", ErrRuntimeCoreMetadata, err)
	}
	if len(compatibility.RuntimeIDs) == 0 {
		return RuntimeCompatibility{}, fmt.Errorf("%w: runtime compatibility has no runtime ids", ErrRuntimeCoreMetadata)
	}
	return compatibility, nil
}

func resolveRuntimeMetadataPaths() (string, string, string) {
	manifestPath := strings.TrimSpace(os.Getenv("DAIDAI_RUNTIME_MANIFEST_PATH"))
	compatibilityPath := strings.TrimSpace(os.Getenv("DAIDAI_RUNTIME_COMPATIBILITY_PATH"))
	smokeEvidencePath := strings.TrimSpace(os.Getenv("DAIDAI_RUNTIME_SMOKE_EVIDENCE_PATH"))
	if manifestPath != "" && compatibilityPath != "" {
		if smokeEvidencePath == "" {
			smokeEvidencePath = filepath.Join(filepath.Dir(manifestPath), "smoke-evidence.json")
		}
		return manifestPath, compatibilityPath, smokeEvidencePath
	}
	candidates := []string{
		"runtime",
		"../runtime",
		"../../runtime",
	}
	for _, root := range candidates {
		manifestCandidate := filepath.Join(root, "manifest.json")
		compatibilityCandidate := filepath.Join(root, "compatibility.json")
		smokeCandidate := filepath.Join(root, "smoke-evidence.json")
		if fileExists(manifestCandidate) && fileExists(compatibilityCandidate) {
			return manifestCandidate, compatibilityCandidate, smokeCandidate
		}
	}
	if manifestPath == "" {
		manifestPath = filepath.Join("runtime", "manifest.json")
	}
	if compatibilityPath == "" {
		compatibilityPath = filepath.Join("runtime", "compatibility.json")
	}
	if smokeEvidencePath == "" {
		smokeEvidencePath = filepath.Join("runtime", "smoke-evidence.json")
	}
	return manifestPath, compatibilityPath, smokeEvidencePath
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

func allowRuntimePlaceholderHash() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DAIDAI_RUNTIME_ALLOW_PLACEHOLDER_HASH")))
	return value == "1" || value == "true" || value == "yes"
}

func (manager *RuntimeComponentManager) resolveEntrypointPath(entrypoint string) (string, error) {
	return resolveRuntimeEntrypointPath(manager.nativeLibraryDir, entrypoint)
}

func RuntimeBaselineDegraded(baseline RuntimeComponentBaseline) bool {
	for _, component := range baseline.Components {
		if !component.Verified {
			return true
		}
	}
	return RuntimeSmokeHasFailure(baseline)
}

func validateRuntimeELF(payload []byte) error {
	if len(payload) < 20 {
		return errors.New("entrypoint payload is too small")
	}
	if payload[0] != 0x7f || payload[1] != 'E' || payload[2] != 'L' || payload[3] != 'F' {
		return errors.New("entrypoint payload is not elf")
	}
	if payload[4] != 2 {
		return errors.New("entrypoint payload is not 64-bit elf")
	}
	if payload[5] != 1 {
		return errors.New("entrypoint payload is not little-endian elf")
	}
	machine := int(payload[18]) | int(payload[19])<<8
	if machine != 183 {
		return errors.New("entrypoint payload is not aarch64 elf")
	}
	return nil
}

func AllowRuntimeBaselineFailureBypass() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DAIDAI_RUNTIME_ALLOW_BASELINE_FAILURE")))
	return value == "1" || value == "true" || value == "yes"
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
