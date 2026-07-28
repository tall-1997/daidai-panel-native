package service

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"daidai-panel/config"
	"daidai-panel/model"
)

const (
	DependencyCompatibilitySupported   = "supported"
	DependencyCompatibilityUnsupported = "unsupported"
	DependencyReasonInvalidSpec        = "INVALID_PACKAGE_SPEC"
	DependencyReasonNativeUnsupported  = "NATIVE_NOT_ALLOWLISTED"
	DependencyReasonSourceUnsigned     = "SOURCE_REQUIRES_SIGNED_AUTHORIZATION"
	DependencyReasonQuotaExceeded      = "DEPENDENCY_QUOTA_EXCEEDED"

	DefaultDependencyQuotaBytes int64 = 1024 * 1024 * 1024
)

type DependencyCompatibilityDetails struct {
	Package          string                 `json:"package"`
	Type             string                 `json:"type"`
	Runtime          string                 `json:"runtime,omitempty"`
	Status           string                 `json:"status"`
	ReasonCode       string                 `json:"reason_code,omitempty"`
	Message          string                 `json:"message"`
	ABI              string                 `json:"abi"`
	Native           bool                   `json:"native"`
	SignedAllowlist  bool                   `json:"signed_native_allowlist"`
	NpmScriptPolicy  string                 `json:"npm_script_policy,omitempty"`
	Staging          string                 `json:"staging"`
	Rollback         string                 `json:"rollback"`
	Quota            DependencyQuotaDetails `json:"quota"`
	Alternatives     []string               `json:"alternatives,omitempty"`
	AllowlistSource  string                 `json:"allowlist_source,omitempty"`
	AllowlistSHA256  string                 `json:"allowlist_sha256,omitempty"`
	CompatibilityKey string                 `json:"compatibility_key"`
}

type DependencyQuotaDetails struct {
	LimitBytes int64  `json:"limit_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
	Status     string `json:"status"`
}

type nativeAllowlistEntry struct {
	Source       string
	SHA256       string
	Alternatives []string
}

var pythonNativeDependencyAllowlist = map[string]nativeAllowlistEntry{
	"cryptography":  {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest", Alternatives: []string{"pyopenssl"}},
	"lxml":          {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest", Alternatives: []string{"beautifulsoup4"}},
	"pynacl":        {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest"},
	"pycryptodome":  {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest", Alternatives: []string{"crypto-js for Node.js scripts"}},
	"pycryptodomex": {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest"},
	"pyyaml":        {Source: "android-arm64-bionic-wheel", SHA256: "signed-runtime-compatibility-manifest"},
}

var knownPythonNativeDependencies = map[string][]string{
	"bcrypt":        {"passlib"},
	"grpcio":        {"pure-Python HTTP client"},
	"mysqlclient":   {"pymysql"},
	"numpy":         {"use a compatible signed wheel when available"},
	"opencv-python": {"opencv-python-headless signed Android build"},
	"pandas":        {"polars signed Android build"},
	"pillow":        {"pure-Python image metadata libraries"},
	"psycopg2":      {"psycopg2-binary signed Android build", "asyncpg signed Android build"},
	"scipy":         {"use a server-side task for SciPy workloads"},
}

var nodeNativeDependencyAllowlist = map[string]nativeAllowlistEntry{
	"bufferutil":      {Source: "android-arm64-bionic-node-addon", SHA256: "signed-runtime-compatibility-manifest"},
	"utf-8-validate":  {Source: "android-arm64-bionic-node-addon", SHA256: "signed-runtime-compatibility-manifest"},
	"@parcel/watcher": {Source: "android-arm64-bionic-node-addon", SHA256: "signed-runtime-compatibility-manifest"},
	"node-expat":      {Source: "android-arm64-bionic-node-addon", SHA256: "signed-runtime-compatibility-manifest", Alternatives: []string{"fast-xml-parser", "xml2js"}},
}

var knownNodeNativeDependencies = map[string][]string{
	"bcrypt":         {"bcryptjs"},
	"better-sqlite3": {"use the bundled SQLite path through Python or server APIs"},
	"canvas":         {"pure SVG/HTML rendering"},
	"esbuild":        {"typescript", "ts-node"},
	"ffi-napi":       {"avoid native FFI in Android scripts"},
	"node-sass":      {"sass"},
	"ref-napi":       {"avoid native FFI in Android scripts"},
	"serialport":     {"Android platform adapter"},
	"sharp":          {"jimp"},
	"sqlite3":        {"use the bundled SQLite path through Python or server APIs"},
}

func EvaluateDependencyCompatibility(depType, packageName, pythonVersion string) DependencyCompatibilityDetails {
	packageName = strings.TrimSpace(packageName)
	runtime := ""
	if depType == model.DepTypePython {
		runtime = NormalizePythonVersionOrDefault(pythonVersion)
	}
	details := DependencyCompatibilityDetails{
		Package:          packageName,
		Type:             depType,
		Runtime:          runtime,
		Status:           DependencyCompatibilitySupported,
		Message:          "dependency is compatible with managed Android runtime policy",
		ABI:              "android-arm64-bionic",
		Staging:          "enabled",
		Rollback:         "enabled",
		Quota:            CurrentDependencyQuotaDetails(),
		CompatibilityKey: dependencyCompatibilityKey(depType, packageName, runtime),
	}
	if depType == model.DepTypeNodeJS {
		details.NpmScriptPolicy = "ignore-scripts"
	}

	if packageName == "" || strings.ContainsAny(packageName, " \t\n\r;|&`$(){}") {
		details.Status = DependencyCompatibilityUnsupported
		details.ReasonCode = DependencyReasonInvalidSpec
		details.Message = "dependency package spec contains unsupported shell metacharacters or is empty"
		return details
	}

	switch depType {
	case model.DepTypePython:
		key := canonicalPythonDependencyName(packageName)
		if entry, ok := pythonNativeDependencyAllowlist[key]; ok {
			details.Native = true
			details.SignedAllowlist = true
			details.AllowlistSource = entry.Source
			details.AllowlistSHA256 = entry.SHA256
			details.Alternatives = append([]string{}, entry.Alternatives...)
			return details
		}
		if alternatives, native := knownPythonNativeDependencies[key]; native {
			details.Native = true
			details.Status = DependencyCompatibilityUnsupported
			details.ReasonCode = DependencyReasonNativeUnsupported
			details.Message = "native Python wheel is outside the signed Android ARM64 compatibility allowlist"
			details.Alternatives = append([]string{}, alternatives...)
			return details
		}
	case model.DepTypeNodeJS:
		if nodePackageSpecRequiresSignedSource(packageName) {
			details.Status = DependencyCompatibilityUnsupported
			details.ReasonCode = DependencyReasonSourceUnsigned
			details.Message = "npm dependency sources must be signed with source and SHA-256 authorization"
			return details
		}
		key := NormalizeNodeDependencyPackageName(packageName)
		if entry, ok := nodeNativeDependencyAllowlist[key]; ok {
			details.Native = true
			details.SignedAllowlist = true
			details.AllowlistSource = entry.Source
			details.AllowlistSHA256 = entry.SHA256
			details.Alternatives = append([]string{}, entry.Alternatives...)
			return details
		}
		if alternatives, native := knownNodeNativeDependencies[key]; native {
			details.Native = true
			details.Status = DependencyCompatibilityUnsupported
			details.ReasonCode = DependencyReasonNativeUnsupported
			details.Message = "native Node addon is outside the signed Android ARM64 compatibility allowlist"
			details.Alternatives = append([]string{}, alternatives...)
			return details
		}
	}
	return details
}

func (d DependencyCompatibilityDetails) Supported() bool {
	return d.Status == DependencyCompatibilitySupported
}

func (d DependencyCompatibilityDetails) JSON() string {
	data, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (d DependencyCompatibilityDetails) Map() map[string]any {
	data, err := json.Marshal(d)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func CurrentDependencyQuotaDetails() DependencyQuotaDetails {
	limit := DependencyQuotaLimitBytes()
	used := directorySize(filepath.Join(config.C.Data.Dir, "deps"))
	status := "ok"
	if limit > 0 && used > limit {
		status = "exceeded"
	}
	return DependencyQuotaDetails{LimitBytes: limit, UsedBytes: used, Status: status}
}

func DependencyQuotaLimitBytes() int64 {
	value := strings.TrimSpace(os.Getenv("DAIDAI_DEPENDENCY_QUOTA_BYTES"))
	if value == "" {
		return DefaultDependencyQuotaBytes
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return DefaultDependencyQuotaBytes
	}
	return parsed
}

func CheckDependencyQuota() error {
	quota := CurrentDependencyQuotaDetails()
	if quota.LimitBytes > 0 && quota.UsedBytes > quota.LimitBytes {
		return fmt.Errorf("%s: used %d bytes exceeds limit %d bytes", DependencyReasonQuotaExceeded, quota.UsedBytes, quota.LimitBytes)
	}
	return nil
}

func EnforceNpmScriptPolicy(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.Env = appendEnvOverride(cmd.Env, "npm_config_ignore_scripts", "true")
	cmd.Env = appendEnvOverride(cmd.Env, "NPM_CONFIG_IGNORE_SCRIPTS", "true")
	if !stringSliceContains(cmd.Args, "--ignore-scripts") {
		cmd.Args = append(cmd.Args, "--ignore-scripts")
	}
}

func PrepareDependencyStaging(depType, name, pythonVersion string) (func(string) string, string) {
	switch depType {
	case model.DepTypeNodeJS:
		return prepareDirectoryStaging(filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", filepath.FromSlash(NormalizeNodeDependencyPackageName(name))))
	case model.DepTypePython:
		marker := filepath.Join(config.C.Data.Dir, "deps", "python", NormalizePythonVersionOrDefault(pythonVersion), ".staging-"+canonicalPythonDependencyName(name))
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
			return func(reason string) string { return "[staging] prepare failed: " + err.Error() }, "prepare_failed"
		}
		if err := os.WriteFile(marker, []byte("prepared\n"), 0o644); err != nil {
			return func(reason string) string { return "[staging] marker failed: " + err.Error() }, "prepare_failed"
		}
		return func(reason string) string {
			if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
				return "[staging] marker cleanup failed: " + err.Error()
			}
			return "[staging] marker released"
		}, "prepared"
	default:
		return func(reason string) string { return "[staging] no staging required" }, "skipped"
	}
}

func RollbackDependencyInstall(depType, name, pythonVersion string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "[rollback] skipped empty dependency name"
	}
	var cmd *exec.Cmd
	var err error
	switch depType {
	case model.DepTypeNodeJS:
		cmd, err = NewNpmUninstallCommand(name, true)
		if err == nil {
			EnforceNpmScriptPolicy(cmd)
		}
	case model.DepTypePython:
		cmd, err = NewPipUninstallCommandForPythonVersion(pythonVersion, name, "--no-deps")
		if err == nil {
			cmd.Env = SanitizePipEnv(AppendProxyEnv(os.Environ()))
		}
	default:
		return "[rollback] no rollback required for dependency type " + depType
	}
	if err != nil {
		return "[rollback] prepare failed: " + err.Error()
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "[rollback] failed: " + text
	}
	if text == "" {
		return "[rollback] completed"
	}
	return "[rollback] completed: " + text
}

func prepareDirectoryStaging(target string) (func(string) string, string) {
	if strings.TrimSpace(target) == "" {
		return func(reason string) string { return "[staging] empty target" }, "skipped"
	}
	backup := target + ".rollback"
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return func(reason string) string { return "[staging] new install has no previous target" }, "prepared"
	}
	if _, err := os.Stat(backup); err == nil {
		return func(reason string) string { return "[staging] existing rollback target preserved" }, "prepared"
	}
	if err := os.Rename(target, backup); err != nil {
		return func(reason string) string { return "[staging] backup failed: " + err.Error() }, "prepare_failed"
	}
	return func(reason string) string {
		if reason == "success" {
			if err := os.RemoveAll(backup); err != nil && !os.IsNotExist(err) {
				return "[staging] rollback backup cleanup failed: " + err.Error()
			}
			return "[staging] committed"
		}
		if _, err := os.Stat(target); err == nil {
			return "[staging] rollback kept backup because new target still exists"
		}
		if err := os.Rename(backup, target); err != nil && !os.IsNotExist(err) {
			return "[staging] rollback restore failed: " + err.Error()
		}
		return "[staging] previous target restored"
	}, "prepared"
}

func canonicalPythonDependencyName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if idx := strings.IndexAny(name, "=<>!~[ "); idx >= 0 {
		name = name[:idx]
	}
	name = strings.Trim(name, "._-")
	name = strings.NewReplacer("_", "-", ".", "-").Replace(name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

func dependencyCompatibilityKey(depType, packageName, runtime string) string {
	if depType == model.DepTypePython {
		return depType + ":" + runtime + ":" + canonicalPythonDependencyName(packageName)
	}
	if depType == model.DepTypeNodeJS {
		return depType + ":" + NormalizeNodeDependencyPackageName(packageName)
	}
	return depType + ":" + strings.TrimSpace(packageName)
}

func nodePackageSpecRequiresSignedSource(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	lower := strings.ToLower(spec)
	for _, prefix := range []string{"file:", "link:", "workspace:", "http://", "https://", "git+", "git://", "ssh://", "github:", "gitlab:", "bitbucket:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func directorySize(root string) int64 {
	var total int64
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func appendEnvOverride(env []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+value)
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
