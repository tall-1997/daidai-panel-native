package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

var canonicalRuntimeIDs = []string{
	"python-3.12-android-arm64",
	"node-lts-android-arm64",
	"typescript-stable",
	"shell-android-arm64",
	"git-android-arm64",
	"ssh-android-arm64",
	"yaegi-go",
	"go-builder-android-arm64",
}

var excludedDefaultAPKRuntimeEntries = map[string]bool{
	"lib/arm64-v8a/libpython_exec.so": true,
}

type runtimeContract struct {
	Manifest      runtimeManifest
	Compatibility compatibilityMatrix
	Smoke         smokeEvidence
	Release       releaseRuntimeContract
	Rootfs        rootfsManifest
}

type releaseRuntimeContract struct {
	SchemaVersion            int                                   `json:"schema_version"`
	DeviceSmoke              releaseDeviceSmoke                    `json:"device_smoke"`
	RuntimeIDs               []string                              `json:"runtime_ids"`
	StableRequiredRuntimeIDs []string                              `json:"stable_required_runtime_ids"`
	RuntimeEntries           map[string]runtimeEntryContract       `json:"runtime_entries"`
	RuntimeEvidence          map[string]runtimeEvidenceRequirement `json:"runtime_evidence"`
	ReleaseGateScope         releaseGateScope                      `json:"release_gate_scope"`
}

type runtimeEvidenceRequirement struct {
	StepID            string `json:"step_id"`
	Command           string `json:"command"`
	ExpectedVersion   string `json:"expected_version,omitempty"`
	OutputPattern     string `json:"output_pattern,omitempty"`
	VersionConstraint string `json:"version_constraint,omitempty"`
}

type releaseGateScope struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
}

type runtimeEntryContract struct {
	EntryType  string `json:"entry_type"`
	Entrypoint string `json:"entrypoint"`
}

type rootfsManifest struct {
	SchemaVersion    int            `json:"schema_version"`
	ABI              string         `json:"abi"`
	Distribution     string         `json:"distribution"`
	UbuntuArch       string         `json:"ubuntu_arch"`
	UbuntuVersion    string         `json:"ubuntu_version"`
	SHA256           string         `json:"sha256"`
	Size             int64          `json:"size"`
	AptPackages      []string       `json:"apt_packages"`
	GlobalTools      map[string]any `json:"global_tools"`
	RequiredCommands []string       `json:"required_commands"`
	BaseArchive      map[string]any `json:"base_archive"`
	Capabilities     map[string]any `json:"capabilities"`
}

type releaseDeviceSmoke struct {
	Matrix           []releaseDeviceMatrix `json:"matrix"`
	VerifiedArtifact string                `json:"verified_artifact"`
	BlockedArtifact  string                `json:"blocked_artifact"`
}

type releaseDeviceMatrix struct {
	ID            string `json:"id"`
	API           int    `json:"api"`
	PageSizeBytes int    `json:"page_size_bytes"`
	ABI           string `json:"abi"`
}

type runtimeManifest struct {
	Version    string             `json:"version"`
	UpdatedAt  string             `json:"updated_at,omitempty"`
	Components []runtimeComponent `json:"components"`
}

type runtimeComponent struct {
	ID            string            `json:"id"`
	Version       string            `json:"version"`
	ABI           string            `json:"abi"`
	PythonTag     string            `json:"python_tag,omitempty"`
	Entrypoint    string            `json:"entrypoint"`
	EntryType     string            `json:"entry_type"`
	SHA256        string            `json:"sha256"`
	RuntimeSHA256 string            `json:"runtime_sha256,omitempty"`
	RuntimeType   string            `json:"runtime_type,omitempty"`
	Isolation     string            `json:"isolation,omitempty"`
	ArtifactCount int               `json:"artifact_count,omitempty"`
	AssetRevision string            `json:"asset_revision,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	Artifacts     []runtimeArtifact `json:"artifacts,omitempty"`
}

type runtimeArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type compatibilityMatrix struct {
	Version              string                 `json:"version"`
	UpdatedAt            string                 `json:"updated_at,omitempty"`
	ABI                  string                 `json:"abi"`
	DistributionABIs     []string               `json:"distribution_abis,omitempty"`
	DistributionProfiles map[string]string      `json:"distribution_profiles,omitempty"`
	RequiredChecks       []string               `json:"required_checks,omitempty"`
	RuntimeIDs           []string               `json:"runtime_ids"`
	ContainerModel       string                 `json:"container_model,omitempty"`
	PythonWheelPolicy    map[string]any         `json:"python_wheel_policy,omitempty"`
	Runtimes             []compatibilityRuntime `json:"runtimes"`
}

type compatibilityRuntime struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Entry   string `json:"entry"`
}

type smokeEvidence struct {
	Version   string        `json:"version"`
	UpdatedAt string        `json:"updated_at,omitempty"`
	Matrix    []string      `json:"matrix"`
	Records   []smokeRecord `json:"records"`
}

type smokeRecord struct {
	RuntimeID      string       `json:"runtime_id"`
	Version        string       `json:"version"`
	Entry          string       `json:"entry"`
	Status         string       `json:"status"`
	EvidenceSource string       `json:"evidence_source"`
	IsolationLevel string       `json:"isolation_level"`
	TimeoutSeconds int          `json:"timeout_seconds"`
	Checks         []smokeCheck `json:"checks"`
}

type smokeCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func main() {
	manifestPath := flag.String("manifest", "runtime/manifest.json", "runtime manifest path")
	compatibilityPath := flag.String("compatibility", "runtime/compatibility.json", "runtime compatibility path")
	smokePath := flag.String("smoke-evidence", "runtime/smoke-evidence.json", "runtime smoke evidence path")
	releaseContractPath := flag.String("release-contract", "scripts/release-runtime-contract.json", "shared release runtime contract path")
	rootfsManifestPath := flag.String("rootfs-manifest", "app/android/app/src/main/assets/android-runtime/arm64-v8a/ubuntu/runtime-manifest.json", "packaged rootfs manifest path")
	nativeLibraryDir := flag.String("native-lib-dir", "", "optional Android native library directory")
	apkPath := flag.String("apk", "", "optional APK whose entries and embedded metadata are verified")
	strict := flag.Bool("strict", false, "fail on blocked smoke records or placeholder ELF entries")
	strictRuntimeIDs := flag.String("strict-runtime-ids", "", "comma-separated runtime IDs that must pass in strict mode; defaults to all")
	flag.Parse()
	strictScope := parseRuntimeIDSet(*strictRuntimeIDs)

	contract, err := readContract(*manifestPath, *compatibilityPath, *smokePath, *releaseContractPath, *rootfsManifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	errs := validateContractWithScope(contract, *strict, strictScope)
	if strings.TrimSpace(*nativeLibraryDir) != "" {
		errs = append(errs, validateNativeEntriesWithScope(*nativeLibraryDir, contract, *strict, strictScope)...)
	}
	if strings.TrimSpace(*apkPath) != "" {
		errs = append(errs, validateAPKWithScope(*apkPath, contract, *strict, strictScope)...)
	}
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	fmt.Printf("runtime contract ok: %d runtimes\n", len(contract.Manifest.Components))
}

func readContract(manifestPath, compatibilityPath, smokePath, releaseContractPath, rootfsManifestPath string) (runtimeContract, error) {
	var contract runtimeContract
	if err := readJSON(manifestPath, &contract.Manifest); err != nil {
		return runtimeContract{}, err
	}
	if err := readJSON(compatibilityPath, &contract.Compatibility); err != nil {
		return runtimeContract{}, err
	}
	if err := readJSON(smokePath, &contract.Smoke); err != nil {
		return runtimeContract{}, err
	}
	if err := readJSON(releaseContractPath, &contract.Release); err != nil {
		return runtimeContract{}, err
	}
	if err := readJSON(rootfsManifestPath, &contract.Rootfs); err != nil {
		return runtimeContract{}, err
	}
	return contract, nil
}

func readJSON(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func validateContract(contract runtimeContract, strict bool) []error {
	return validateContractWithScope(contract, strict, nil)
}

func validateContractWithScope(contract runtimeContract, strict bool, strictScope map[string]bool) []error {
	var errs []error
	errs = append(errs, validateReleaseRuntimeContract(contract.Release, contract.Smoke, contract.Manifest)...)
	if contract.Rootfs.SchemaVersion != 2 || contract.Rootfs.ABI != "arm64-v8a" || contract.Rootfs.Distribution != "ubuntu" || len(contract.Rootfs.RequiredCommands) == 0 {
		errs = append(errs, fmt.Errorf("rootfs manifest command contract is incomplete"))
	}
	if strict {
		for id := range strictScope {
			if !contains(canonicalRuntimeIDs, id) {
				errs = append(errs, fmt.Errorf("unknown strict runtime ID %q", id))
			}
		}
	}
	manifestByID := make(map[string]runtimeComponent, len(contract.Manifest.Components))
	for _, component := range contract.Manifest.Components {
		if component.ID == "" || component.Version == "" || component.ABI != "arm64-v8a" || component.Entrypoint == "" || (component.EntryType != "apk_elf" && component.EntryType != "rootfs_command") {
			errs = append(errs, fmt.Errorf("invalid manifest component %q", component.ID))
		}
		if _, exists := manifestByID[component.ID]; exists {
			errs = append(errs, fmt.Errorf("duplicate manifest runtime ID %q", component.ID))
		}
		manifestByID[component.ID] = component
	}
	errs = append(errs, validateIDSet("manifest runtime IDs", mapKeys(manifestByID))...)

	compatibilityByID := make(map[string]compatibilityRuntime, len(contract.Compatibility.Runtimes))
	for _, runtime := range contract.Compatibility.Runtimes {
		if _, exists := compatibilityByID[runtime.ID]; exists {
			errs = append(errs, fmt.Errorf("duplicate compatibility runtime ID %q", runtime.ID))
		}
		compatibilityByID[runtime.ID] = runtime
	}
	errs = append(errs, validateIDSet("compatibility runtime IDs", mapKeys(compatibilityByID))...)
	errs = append(errs, validateIDSet("compatibility runtime_ids", append([]string(nil), contract.Compatibility.RuntimeIDs...))...)

	smokeByID := make(map[string]smokeRecord, len(contract.Smoke.Records))
	for _, record := range contract.Smoke.Records {
		if _, exists := smokeByID[record.RuntimeID]; exists {
			errs = append(errs, fmt.Errorf("duplicate smoke runtime ID %q", record.RuntimeID))
		}
		smokeByID[record.RuntimeID] = record
	}
	errs = append(errs, validateIDSet("smoke runtime IDs", mapKeys(smokeByID))...)

	for _, id := range canonicalRuntimeIDs {
		component, manifestOK := manifestByID[id]
		compatibility, compatibilityOK := compatibilityByID[id]
		record, smokeOK := smokeByID[id]
		if !manifestOK || !compatibilityOK || !smokeOK {
			continue
		}
		if compatibility.Version != component.Version {
			errs = append(errs, fmt.Errorf("%s compatibility version mismatch: got %q want %q", id, compatibility.Version, component.Version))
		}
		if compatibility.Entry != component.Entrypoint {
			errs = append(errs, fmt.Errorf("%s compatibility entry mismatch: got %q want %q", id, compatibility.Entry, component.Entrypoint))
		}
		if record.Version != component.Version {
			errs = append(errs, fmt.Errorf("%s smoke version mismatch: got %q want %q", id, record.Version, component.Version))
		}
		if record.Entry != component.Entrypoint {
			errs = append(errs, fmt.Errorf("%s smoke entry mismatch: got %q want %q", id, record.Entry, component.Entrypoint))
		}
		if component.EntryType == "rootfs_command" && !contains(contract.Rootfs.RequiredCommands, component.Entrypoint) {
			errs = append(errs, fmt.Errorf("%s rootfs command %q is absent from packaged rootfs required_commands", id, component.Entrypoint))
		}
		if component.EntryType == "rootfs_command" && !strings.EqualFold(component.SHA256, contract.Rootfs.SHA256) {
			errs = append(errs, fmt.Errorf("%s rootfs command checksum does not bind the packaged rootfs", id))
		}
		errs = append(errs, validateSmokeRecord(record, strict && runtimeIsRequired(id, strictScope))...)
	}
	return errs
}

func validateReleaseRuntimeContract(release releaseRuntimeContract, smoke smokeEvidence, manifest runtimeManifest) []error {
	var errs []error
	if release.SchemaVersion != 1 || release.DeviceSmoke.VerifiedArtifact == "" || release.DeviceSmoke.BlockedArtifact == "" {
		errs = append(errs, fmt.Errorf("release runtime contract is incomplete"))
	}
	errs = append(errs, validateIDSet("release runtime IDs", append([]string(nil), release.RuntimeIDs...))...)
	errs = append(errs, validateIDSet("release runtime entry IDs", mapKeys(release.RuntimeEntries))...)
	manifestByID := make(map[string]runtimeComponent, len(manifest.Components))
	for _, component := range manifest.Components {
		manifestByID[component.ID] = component
	}
	for id, entry := range release.RuntimeEntries {
		component, ok := manifestByID[id]
		if !ok || entry.EntryType != component.EntryType || entry.Entrypoint != component.Entrypoint {
			errs = append(errs, fmt.Errorf("%s release runtime entry differs from manifest", id))
		}
	}
	for id, requirement := range release.RuntimeEvidence {
		component, ok := manifestByID[id]
		if !ok || requirement.StepID == "" || requirement.Command == "" {
			errs = append(errs, fmt.Errorf("%s release runtime evidence is incomplete", id))
			continue
		}
		if requirement.ExpectedVersion != "" && requirement.ExpectedVersion != component.Version {
			errs = append(errs, fmt.Errorf("%s release expected version differs from manifest", id))
		}
	}
	seenRequired := map[string]bool{}
	for _, id := range release.StableRequiredRuntimeIDs {
		if seenRequired[id] {
			errs = append(errs, fmt.Errorf("duplicate stable required runtime ID %q", id))
		}
		seenRequired[id] = true
		if !contains(canonicalRuntimeIDs, id) {
			errs = append(errs, fmt.Errorf("unknown stable required runtime ID %q", id))
		}
		if entry, ok := release.RuntimeEntries[id]; !ok || entry.EntryType != "rootfs_command" {
			errs = append(errs, fmt.Errorf("stable required runtime %q must be a rootfs_command", id))
		}
	}
	if len(release.StableRequiredRuntimeIDs) == 0 {
		errs = append(errs, fmt.Errorf("stable required runtime IDs are empty"))
	}
	matrixIDs := make([]string, 0, len(release.DeviceSmoke.Matrix))
	seenMatrix := map[string]bool{}
	for _, matrix := range release.DeviceSmoke.Matrix {
		matrixIDs = append(matrixIDs, matrix.ID)
		if matrix.ID == "" || matrix.API <= 0 || matrix.PageSizeBytes <= 0 || matrix.ABI == "" {
			errs = append(errs, fmt.Errorf("invalid release device matrix %q", matrix.ID))
		}
		if seenMatrix[matrix.ID] {
			errs = append(errs, fmt.Errorf("duplicate release device matrix ID %q", matrix.ID))
		}
		seenMatrix[matrix.ID] = true
	}
	if !reflect.DeepEqual(smoke.Matrix, matrixIDs) {
		errs = append(errs, fmt.Errorf("smoke evidence matrix differs from release runtime contract: got %v want %v", smoke.Matrix, matrixIDs))
	}
	return errs
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseRuntimeIDSet(value string) map[string]bool {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	result := map[string]bool{}
	for _, id := range strings.Split(value, ",") {
		if id = strings.TrimSpace(id); id != "" {
			result[id] = true
		}
	}
	return result
}

func runtimeIsRequired(id string, strictScope map[string]bool) bool {
	return strictScope == nil || strictScope[id]
}

func validateSmokeRecord(record smokeRecord, strict bool) []error {
	var errs []error
	if record.IsolationLevel == "" || record.TimeoutSeconds <= 0 || len(record.Checks) == 0 {
		errs = append(errs, fmt.Errorf("%s smoke record is incomplete", record.RuntimeID))
	}
	switch record.Status {
	case "pass":
		if record.EvidenceSource != "android-device" {
			errs = append(errs, fmt.Errorf("%s pass requires android-device evidence", record.RuntimeID))
		}
		for _, check := range record.Checks {
			if check.ID == "" || check.Status != "pass" || strings.TrimSpace(check.Output) == "" {
				errs = append(errs, fmt.Errorf("%s pass check %q is incomplete", record.RuntimeID, check.ID))
			}
		}
	case "blocked":
		if strict {
			errs = append(errs, fmt.Errorf("%s is blocked in strict mode", record.RuntimeID))
		}
		for _, check := range record.Checks {
			if check.ID == "" || check.Status != "blocked" || strings.TrimSpace(check.Reason) == "" || check.Output != "" {
				errs = append(errs, fmt.Errorf("%s blocked check %q is incomplete", record.RuntimeID, check.ID))
			}
		}
	default:
		errs = append(errs, fmt.Errorf("%s has invalid smoke status %q", record.RuntimeID, record.Status))
	}
	return errs
}

func validateIDSet(label string, got []string) []error {
	want := append([]string(nil), canonicalRuntimeIDs...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		return []error{fmt.Errorf("%s mismatch: got %v want %v", label, got, want)}
	}
	return nil
}

func mapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func recordsByRuntimeID(records []smokeRecord) map[string]smokeRecord {
	result := make(map[string]smokeRecord, len(records))
	for _, record := range records {
		result[record.RuntimeID] = record
	}
	return result
}

func validateNativeEntries(nativeDir string, contract runtimeContract, strict bool) []error {
	return validateNativeEntriesWithScope(nativeDir, contract, strict, nil)
}

func validateNativeEntriesWithScope(nativeDir string, contract runtimeContract, strict bool, strictScope map[string]bool) []error {
	var errs []error
	records := make(map[string]smokeRecord, len(contract.Smoke.Records))
	for _, record := range contract.Smoke.Records {
		records[record.RuntimeID] = record
	}
	seenEntries := map[string]bool{}
	for _, component := range contract.Manifest.Components {
		if component.EntryType != "apk_elf" {
			continue
		}
		componentStrict := strict && runtimeIsRequired(component.ID, strictScope)
		if seenEntries[component.Entrypoint] {
			continue
		}
		seenEntries[component.Entrypoint] = true
		payload, err := os.ReadFile(filepath.Join(nativeDir, component.Entrypoint))
		if err != nil {
			record := records[component.ID]
			if !componentStrict && record.Status == "blocked" {
				continue
			}
			errs = append(errs, fmt.Errorf("%s missing native entry %s: %w", component.ID, component.Entrypoint, err))
			continue
		}
		if err := validateRuntimeELF(payload); err != nil {
			errs = append(errs, fmt.Errorf("%s invalid ELF: %w", component.ID, err))
			continue
		}
		placeholder := strings.Contains(string(payload), "RUNTIME_STUB_OK")
		if placeholder {
			record := records[component.ID]
			if record.Status != "blocked" {
				errs = append(errs, fmt.Errorf("%s placeholder ELF requires blocked smoke evidence", component.ID))
			}
			if componentStrict {
				errs = append(errs, fmt.Errorf("%s placeholder ELF blocked in strict mode", component.ID))
			}
			continue
		}
		sum := sha256.Sum256(payload)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), component.SHA256) {
			errs = append(errs, fmt.Errorf("%s native entry sha256 mismatch", component.ID))
		}
	}
	return errs
}

func validateRuntimeELF(payload []byte) error {
	if len(payload) < 20 || string(payload[:4]) != "\x7fELF" {
		return fmt.Errorf("invalid ELF header")
	}
	if payload[4] != 2 || payload[5] != 1 {
		return fmt.Errorf("ELF must be 64-bit little-endian")
	}
	if int(payload[18])|int(payload[19])<<8 != 183 {
		return fmt.Errorf("ELF must target AArch64")
	}
	return nil
}

func validateAPK(path string, contract runtimeContract, strict bool) []error {
	return validateAPKWithScope(path, contract, strict, nil)
}

func validateAPKWithScope(path string, contract runtimeContract, strict bool, strictScope map[string]bool) []error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return []error{fmt.Errorf("open APK %s: %w", path, err)}
	}
	defer archive.Close()

	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		entries[file.Name] = file
	}
	metadata := []struct {
		path string
		want any
		got  any
	}{
		{path: "assets/manifest.json", want: contract.Manifest, got: &runtimeManifest{}},
		{path: "assets/compatibility.json", want: contract.Compatibility, got: &compatibilityMatrix{}},
		{path: "assets/smoke-evidence.json", want: contract.Smoke, got: &smokeEvidence{}},
		{path: "assets/android-runtime/arm64-v8a/ubuntu/runtime-manifest.json", want: contract.Rootfs, got: &rootfsManifest{}},
	}
	var errs []error
	for _, item := range metadata {
		file, ok := entries[item.path]
		if !ok {
			errs = append(errs, fmt.Errorf("missing APK metadata %s", item.path))
			continue
		}
		if err := decodeZipJSON(file, item.got); err != nil {
			errs = append(errs, fmt.Errorf("decode APK metadata %s: %w", item.path, err))
			continue
		}
		got := reflect.ValueOf(item.got).Elem().Interface()
		// Device evidence is produced after the signed APK. Strict release checks bind
		// the APK digest separately and use the post-install smoke evidence as truth.
		if strict && item.path == "assets/smoke-evidence.json" {
			continue
		}
		if !reflect.DeepEqual(got, item.want) {
			errs = append(errs, fmt.Errorf("APK metadata %s differs from source contract", item.path))
		}
	}
	rootfsAsset := "assets/android-runtime/arm64-v8a/ubuntu/rootfs.tar.gz.bin"
	rootfsFile, ok := entries[rootfsAsset]
	if !ok {
		errs = append(errs, fmt.Errorf("missing APK rootfs asset %s", rootfsAsset))
	} else if int64(rootfsFile.UncompressedSize64) != contract.Rootfs.Size {
		errs = append(errs, fmt.Errorf("APK rootfs asset size differs from rootfs manifest"))
	} else if payload, readErr := readZipFile(rootfsFile); readErr != nil {
		errs = append(errs, readErr)
	} else {
		sum := sha256.Sum256(payload)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), contract.Rootfs.SHA256) {
			errs = append(errs, fmt.Errorf("APK rootfs asset sha256 differs from rootfs manifest"))
		}
	}
	seen := map[string]bool{}
	for _, component := range contract.Manifest.Components {
		if component.EntryType != "apk_elf" {
			continue
		}
		componentStrict := strict && runtimeIsRequired(component.ID, strictScope)
		apkEntry := "lib/arm64-v8a/" + component.Entrypoint
		if seen[apkEntry] {
			continue
		}
		seen[apkEntry] = true
		file, ok := entries[apkEntry]
		if !ok {
			record := recordsByRuntimeID(contract.Smoke.Records)[component.ID]
			if !componentStrict && record.Status == "blocked" {
				continue
			}
			errs = append(errs, fmt.Errorf("%s missing APK entry %s", component.ID, apkEntry))
			continue
		}
		if componentStrict {
			payload, readErr := readZipFile(file)
			if readErr != nil {
				errs = append(errs, readErr)
			} else if elfErr := validateRuntimeELF(payload); elfErr != nil {
				errs = append(errs, fmt.Errorf("%s APK entry is invalid ELF: %w", component.ID, elfErr))
			} else if strings.Contains(string(payload), "RUNTIME_STUB_OK") {
				errs = append(errs, fmt.Errorf("%s APK entry is placeholder ELF in strict mode", component.ID))
			} else {
				sum := sha256.Sum256(payload)
				if !strings.EqualFold(hex.EncodeToString(sum[:]), component.SHA256) {
					errs = append(errs, fmt.Errorf("%s APK entry sha256 mismatch", component.ID))
				}
			}
		}
	}
	for name := range entries {
		if excludedDefaultAPKRuntimeEntries[name] {
			errs = append(errs, fmt.Errorf("optional runtime entry is forbidden in default APK: %s", name))
			continue
		}
		if strings.HasPrefix(name, "lib/arm64-v8a/lib") && strings.HasSuffix(name, "_exec.so") && !seen[name] {
			// Legacy launcher aliases are retained for migration compatibility and
			// are not independent runtime contract entries.
			if name == "lib/arm64-v8a/libnodejs_exec.so" {
				continue
			}
			errs = append(errs, fmt.Errorf("undeclared APK runtime entry %s", name))
		}
	}
	return errs
}

func decodeZipJSON(file *zip.File, target any) error {
	payload, err := readZipFile(file)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open APK entry %s: %w", file.Name, err)
	}
	defer reader.Close()
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read APK entry %s: %w", file.Name, err)
	}
	return payload, nil
}
