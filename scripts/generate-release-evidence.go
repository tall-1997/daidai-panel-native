package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type releaseEvidence struct {
	SchemaVersion   string                 `json:"schema_version"`
	BundleStatus    string                 `json:"bundle_status"`
	Status          string                 `json:"status"`
	StatusScope     string                 `json:"status_scope"`
	RequiredGates   []string               `json:"required_gates"`
	OptionalGates   []string               `json:"optional_gates"`
	PendingEvidence map[string]gateSummary `json:"pending_evidence"`
	GeneratedAt     string                 `json:"generated_at"`
	Release         releaseInfo            `json:"release"`
	Inputs          evidenceInputs         `json:"inputs"`
	Artifacts       []artifactInfo         `json:"artifacts"`
	Reports         map[string]string      `json:"reports"`
	Gates           map[string]gateSummary `json:"gates"`
	Notes           []string               `json:"notes"`
}

type releaseInfo struct {
	Version    string `json:"version"`
	Channel    string `json:"channel"`
	GitCommit  string `json:"git_commit"`
	Actor      string `json:"actor"`
	Repository string `json:"repository"`
}

type evidenceInputs struct {
	APKPath         string `json:"apk_path"`
	TestAPKPath     string `json:"test_apk_path"`
	RuntimeManifest string `json:"runtime_manifest"`
	RuntimeSmoke    string `json:"runtime_smoke"`
	Compatibility   string `json:"compatibility_matrix"`
	RouteTrace      string `json:"route_trace"`
	GoMod           string `json:"go_mod"`
	FlutterLock     string `json:"flutter_lock"`
	PackageLock     string `json:"package_lock"`
}

type artifactInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type gateSummary struct {
	Status      string `json:"status"`
	Report      string `json:"report"`
	Description string `json:"description"`
}

type runtimeManifest struct {
	Version    string             `json:"version"`
	UpdatedAt  string             `json:"updated_at"`
	Components []runtimeComponent `json:"components"`
}

type runtimeComponent struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	ABI          string   `json:"abi"`
	EntryType    string   `json:"entry_type"`
	Entrypoint   string   `json:"entrypoint"`
	SHA256       string   `json:"sha256"`
	Capabilities []string `json:"capabilities"`
}

type releaseGateContract struct {
	SchemaVersion            int      `json:"schema_version"`
	StableRequiredRuntimeIDs []string `json:"stable_required_runtime_ids"`
	ReleaseGateScope         struct {
		Required []string `json:"required"`
		Optional []string `json:"optional"`
	} `json:"release_gate_scope"`
}

type runtimeSmokeEvidence struct {
	Records []struct {
		RuntimeID      string `json:"runtime_id"`
		Status         string `json:"status"`
		EvidenceSource string `json:"evidence_source"`
		Checks         []struct {
			Status string `json:"status"`
			Output string `json:"output"`
		} `json:"checks"`
	} `json:"records"`
}

type routeTrace struct {
	SourceVersion string        `json:"sourceVersion"`
	Fixtures      []string      `json:"fixtures"`
	Routes        []routeRecord `json:"routes"`
}

type routeRecord struct {
	Method            string `json:"method"`
	Path              string `json:"path"`
	Module            string `json:"module"`
	MobileStatus      string `json:"mobileStatus"`
	AndroidEquivalent string `json:"androidEquivalent"`
	AuthContract      string `json:"authContract"`
	StreamContract    string `json:"streamContract"`
	TestCase          string `json:"testCase"`
}

type sbomDocument struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	SerialNumber string          `json:"serialNumber"`
	Version      int             `json:"version"`
	Metadata     sbomMetadata    `json:"metadata"`
	Components   []sbomComponent `json:"components"`
}

type sbomMetadata struct {
	Timestamp string        `json:"timestamp"`
	Component sbomComponent `json:"component"`
}

type sbomComponent struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	PURL    string `json:"purl,omitempty"`
}

func main() {
	apkPath := flag.String("apk", "", "release APK path")
	testAPKPath := flag.String("test-apk", "", "instrumentation APK path")
	version := flag.String("version", "snapshot", "release version")
	outputDir := flag.String("output-dir", "release/evidence", "evidence output directory")
	runtimeManifestPath := flag.String("runtime-manifest", "runtime/manifest.json", "runtime manifest path")
	runtimeSmokePath := flag.String("runtime-smoke", "runtime/smoke-evidence.json", "runtime smoke evidence path")
	compatibilityPath := flag.String("compatibility", "runtime/compatibility.json", "runtime compatibility matrix path")
	routeTracePath := flag.String("route-trace", "contracts/backend-api-mobile.json", "route trace path")
	goModPath := flag.String("go-mod", "panel/server/go.mod", "Go module file path")
	flutterLockPath := flag.String("flutter-lock", "app/pubspec.lock", "Flutter lockfile path")
	packageLockPath := flag.String("package-lock", "panel/web/package-lock.json", "Node package lock path")
	releaseContractPath := flag.String("release-contract", "scripts/release-runtime-contract.json", "release gate scope contract")
	gitCommit := flag.String("git-commit", os.Getenv("GITHUB_SHA"), "git commit for the evidence bundle")
	actor := flag.String("actor", os.Getenv("GITHUB_ACTOR"), "release actor")
	repository := flag.String("repository", os.Getenv("GITHUB_REPOSITORY"), "repository name")
	channel := flag.String("channel", "prerelease", "release channel: snapshot, prerelease, or stable")
	flag.Parse()

	if strings.TrimSpace(*apkPath) == "" {
		fatalf("missing required flag: --apk")
	}
	gateContract := readReleaseGateContract(*releaseContractPath)
	runtimeSmoke := readRuntimeSmokeEvidence(*runtimeSmokePath)
	topLevelStatus, normalizedGateStatus, err := releaseGateState(*channel, gateContract, runtimeSmoke)
	if err != nil {
		fatalf("release gate state: %v", err)
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fatalf("create evidence dir: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	artifacts := collectArtifacts(*apkPath, *testAPKPath, *runtimeManifestPath, *runtimeSmokePath, *compatibilityPath, *routeTracePath)
	runtimeManifest := readRuntimeManifest(*runtimeManifestPath)
	routes := readRouteTrace(*routeTracePath)
	components := collectSBOMComponents(runtimeManifest, *goModPath, *flutterLockPath, *packageLockPath)

	writeJSON(filepath.Join(*outputDir, "sbom.cyclonedx.json"), sbomDocument{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.6",
		SerialNumber: "urn:uuid:" + deterministicUUID("daidai-panel-native-"+sanitize(*version)),
		Version:      1,
		Metadata: sbomMetadata{Timestamp: now, Component: sbomComponent{
			Type:    "application",
			Name:    "daidai-panel-native",
			Version: *version,
		}},
		Components: components,
	})
	writeLicenses(filepath.Join(*outputDir, "third-party-licenses.md"), now, components)
	writeJSON(filepath.Join(*outputDir, "runtime-manifest.release.json"), runtimeManifest)
	writeJSON(filepath.Join(*outputDir, "compatibility-matrix.release.json"), buildCompatibilityReport(runtimeManifest))
	writeJSON(filepath.Join(*outputDir, "route-trace-summary.json"), buildRouteTraceSummary(routes))
	writeJSON(filepath.Join(*outputDir, "page-size-report.json"), buildPageSizeReport(runtimeManifest))
	writeJSON(filepath.Join(*outputDir, "test-report-placeholders.json"), buildTestPlaceholders())
	writeJSON(filepath.Join(*outputDir, "core-cycle-evidence.json"), buildCoreCycleEvidence(now))
	writeJSON(filepath.Join(*outputDir, "api-matrix-evidence.json"), buildAPIMatrixEvidence(routes))
	writeJSON(filepath.Join(*outputDir, "scheduler-24h-evidence.json"), buildSchedulerEvidence("24h", now))
	writeJSON(filepath.Join(*outputDir, "scheduler-7d-evidence.json"), buildSchedulerEvidence("7d", now))

	release := releaseEvidence{
		SchemaVersion:   "1",
		BundleStatus:    "generated",
		Status:          topLevelStatus,
		StatusScope:     "required-gates-only",
		RequiredGates:   gateContract.ReleaseGateScope.Required,
		OptionalGates:   gateContract.ReleaseGateScope.Optional,
		PendingEvidence: buildPendingEvidence(),
		GeneratedAt:     now,
		Release: releaseInfo{
			Version:    *version,
			Channel:    strings.TrimSpace(*channel),
			GitCommit:  strings.TrimSpace(*gitCommit),
			Actor:      strings.TrimSpace(*actor),
			Repository: strings.TrimSpace(*repository),
		},
		Inputs: evidenceInputs{
			APKPath:         *apkPath,
			TestAPKPath:     *testAPKPath,
			RuntimeManifest: *runtimeManifestPath,
			RuntimeSmoke:    *runtimeSmokePath,
			Compatibility:   *compatibilityPath,
			RouteTrace:      *routeTracePath,
			GoMod:           *goModPath,
			FlutterLock:     *flutterLockPath,
			PackageLock:     *packageLockPath,
		},
		Artifacts: artifacts,
		Reports: map[string]string{
			"sbom":                 "sbom.cyclonedx.json",
			"licenses":             "third-party-licenses.md",
			"runtime_manifest":     "runtime-manifest.release.json",
			"route_trace":          "route-trace-summary.json",
			"compatibility_matrix": "compatibility-matrix.release.json",
			"page_size":            "page-size-report.json",
			"test_placeholders":    "test-report-placeholders.json",
			"core_cycles":          "core-cycle-evidence.json",
			"api_matrix":           "api-matrix-evidence.json",
			"scheduler_24h":        "scheduler-24h-evidence.json",
			"scheduler_7d":         "scheduler-7d-evidence.json",
		},
		Gates: buildGateSummaries(normalizedGateStatus),
		Notes: buildReleaseNotes(strings.TrimSpace(*channel)),
	}
	writeJSON(filepath.Join(*outputDir, "release-evidence.json"), release)
	writeIndex(filepath.Join(*outputDir, "README.md"), release)
	fmt.Printf("generated release evidence in %s\n", *outputDir)
}

func buildPendingEvidence() map[string]gateSummary {
	return map[string]gateSummary{
		"long_running_device_samples": {
			Status:      "pending",
			Report:      "scheduler-24h-evidence.json, scheduler-7d-evidence.json",
			Description: "Long-running 24h and 7d device samples are tracked independently from current required release gates.",
		},
	}
}

func releaseGateState(channel string, contract releaseGateContract, smoke runtimeSmokeEvidence) (string, string, error) {
	channel = strings.TrimSpace(channel)
	if channel != "snapshot" && channel != "prerelease" && channel != "stable" {
		return "", "", fmt.Errorf("unsupported channel %q", channel)
	}
	if len(contract.ReleaseGateScope.Required) == 0 || len(contract.ReleaseGateScope.Optional) == 0 {
		return "", "", fmt.Errorf("release gate scope is incomplete")
	}
	if channel != "stable" {
		return "pending", "pending", nil
	}
	for _, runtimeID := range contract.StableRequiredRuntimeIDs {
		var recordIndex = -1
		for index, record := range smoke.Records {
			if record.RuntimeID == runtimeID {
				recordIndex = index
				break
			}
		}
		if recordIndex < 0 {
			return "", "", fmt.Errorf("stable required runtime %s lacks verified device evidence", runtimeID)
		}
		record := smoke.Records[recordIndex]
		if record.Status != "pass" || record.EvidenceSource != "android-device" || len(record.Checks) == 0 {
			return "", "", fmt.Errorf("stable required runtime %s lacks verified device evidence", runtimeID)
		}
		for _, check := range record.Checks {
			if check.Status != "pass" || strings.TrimSpace(check.Output) == "" {
				return "", "", fmt.Errorf("stable required runtime %s has incomplete device checks", runtimeID)
			}
		}
	}
	return "completed", "pass", nil
}

func readReleaseGateContract(path string) releaseGateContract {
	var contract releaseGateContract
	readJSON(path, &contract)
	if contract.SchemaVersion != 1 || len(contract.StableRequiredRuntimeIDs) == 0 {
		fatalf("release gate contract is incomplete")
	}
	return contract
}

func readRuntimeSmokeEvidence(path string) runtimeSmokeEvidence {
	var smoke runtimeSmokeEvidence
	readJSON(path, &smoke)
	return smoke
}

func buildGateSummaries(status string) map[string]gateSummary {
	return map[string]gateSummary{
		"task_6_3_release_evidence": {
			Status:      status,
			Report:      "release-evidence.json",
			Description: "Release inputs and generated reports are bundled; placeholder report statuses remain pending until their own evidence exists.",
		},
		"task_6_4_device_stability_gate": {
			Status:      status,
			Report:      "runtime/smoke-evidence.json",
			Description: "Stable pass records are backed by verified Android device evidence for stable_required_runtime_ids.",
		},
	}
}

func buildReleaseNotes(channel string) []string {
	notes := []string{
		"Long-running 24h and 7d samples are represented as append-only evidence structures and remain pending until device execution attaches records.",
		"Public APK re-download verification is an exit-gate check performed after the GitHub Release asset is available.",
	}
	if channel == "stable" {
		return append([]string{"Stable runtime gates cover verified Android device evidence for stable_required_runtime_ids; other canonical runtimes retain complete blocked records when unverified."}, notes...)
	}
	return append([]string{"Prerelease runtime and device gates remain pending with blocked boundaries recorded explicitly."}, notes...)
}

func collectArtifacts(paths ...string) []artifactInfo {
	artifacts := make([]artifactInfo, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		info, err := artifact(path)
		if err != nil {
			fatalf("artifact %s: %v", path, err)
		}
		artifacts = append(artifacts, info)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts
}

func artifact(path string) (artifactInfo, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return artifactInfo{}, err
	}
	sum := sha256.Sum256(payload)
	return artifactInfo{Name: filepath.Base(path), Path: filepath.ToSlash(path), Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func readRuntimeManifest(path string) runtimeManifest {
	var manifest runtimeManifest
	readJSON(path, &manifest)
	if len(manifest.Components) == 0 {
		fatalf("runtime manifest has no components")
	}
	return manifest
}

func readRouteTrace(path string) routeTrace {
	var trace routeTrace
	readJSON(path, &trace)
	if len(trace.Routes) == 0 {
		fatalf("route trace has no routes")
	}
	return trace
}

func collectSBOMComponents(manifest runtimeManifest, goModPath, flutterLockPath, packageLockPath string) []sbomComponent {
	seen := map[string]bool{}
	var components []sbomComponent
	add := func(component sbomComponent) {
		key := component.Type + "|" + component.Name + "|" + component.Version
		if seen[key] || component.Name == "" {
			return
		}
		seen[key] = true
		components = append(components, component)
	}
	for _, runtime := range manifest.Components {
		add(sbomComponent{Type: "application", Name: runtime.ID, Version: runtime.Version, PURL: "pkg:generic/" + runtime.ID + "@" + runtime.Version})
	}
	for _, module := range parseGoMod(goModPath) {
		add(sbomComponent{Type: "library", Name: module.name, Version: module.version, PURL: "pkg:golang/" + module.name + "@" + module.version})
	}
	for _, pkg := range parsePubspecLock(flutterLockPath) {
		add(sbomComponent{Type: "library", Name: pkg.name, Version: pkg.version, PURL: "pkg:pub/" + pkg.name + "@" + pkg.version})
	}
	for _, pkg := range parsePackageLock(packageLockPath) {
		add(sbomComponent{Type: "library", Name: pkg.name, Version: pkg.version, PURL: "pkg:npm/" + pkg.name + "@" + pkg.version})
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Type == components[j].Type {
			return components[i].Name < components[j].Name
		}
		return components[i].Type < components[j].Type
	})
	return components
}

type packageVersion struct {
	name    string
	version string
}

func parseGoMod(path string) []packageVersion {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var modules []packageVersion
	inRequire := false
	for _, rawLine := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(strings.Split(rawLine, "//")[0])
		switch {
		case line == "require (":
			inRequire = true
			continue
		case inRequire && line == ")":
			inRequire = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		}
		if line == "" || strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "tool ") {
			continue
		}
		if inRequire || strings.Count(line, " ") >= 1 {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.Contains(fields[0], ".") {
				modules = append(modules, packageVersion{name: fields[0], version: fields[1]})
			}
		}
	}
	return modules
}

func parsePubspecLock(path string) []packageVersion {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var packages []packageVersion
	var current string
	inPackages := false
	for _, rawLine := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "packages:" {
			inPackages = true
			continue
		}
		if !inPackages || line == "" {
			continue
		}
		if !strings.HasPrefix(rawLine, "  ") && strings.HasSuffix(line, ":") {
			break
		}
		if strings.HasPrefix(rawLine, "  ") && !strings.HasPrefix(rawLine, "    ") && strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(line, ":")
			continue
		}
		if current != "" && strings.HasPrefix(line, "version:") {
			version := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "version:")), "\"")
			packages = append(packages, packageVersion{name: current, version: version})
			current = ""
		}
	}
	return packages
}

func parsePackageLock(path string) []packageVersion {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(payload, &lock); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var packages []packageVersion
	for path, item := range lock.Packages {
		if !strings.HasPrefix(path, "node_modules/") || item.Version == "" {
			continue
		}
		name := strings.TrimPrefix(path, "node_modules/")
		if !seen[name] {
			seen[name] = true
			packages = append(packages, packageVersion{name: name, version: item.Version})
		}
	}
	for name, item := range lock.Dependencies {
		if item.Version != "" && !seen[name] {
			seen[name] = true
			packages = append(packages, packageVersion{name: name, version: item.Version})
		}
	}
	return packages
}

func buildCompatibilityReport(manifest runtimeManifest) map[string]any {
	apiLevels := []int{28, 29, 31, 34, 35}
	pageSizes := []string{"4k", "16k"}
	requiredCombos := []string{"api35-16k"}
	rows := make([]map[string]any, 0, len(manifest.Components)*len(requiredCombos))
	for _, component := range manifest.Components {
		for _, combo := range requiredCombos {
			rows = append(rows, map[string]any{
				"runtime_id": component.ID,
				"entry_type": component.EntryType,
				"entrypoint": component.Entrypoint,
				"abi":        component.ABI,
				"combo":      combo,
				"status":     "pending-device-verification",
			})
		}
	}
	return map[string]any{"schema_version": "1", "api_levels": apiLevels, "page_sizes": pageSizes, "required_combinations": requiredCombos, "rows": rows}
}

func buildRouteTraceSummary(trace routeTrace) map[string]any {
	byStatus := map[string]int{}
	byModule := map[string]int{}
	missingTests := 0
	for _, route := range trace.Routes {
		byStatus[route.MobileStatus]++
		byModule[route.Module]++
		if strings.TrimSpace(route.TestCase) == "" {
			missingTests++
		}
	}
	return map[string]any{"schema_version": "1", "source_version": trace.SourceVersion, "fixtures": trace.Fixtures, "total_routes": len(trace.Routes), "routes_by_mobile_status": byStatus, "routes_by_module": byModule, "missing_test_cases": missingTests, "coverage_status": statusForMissing(missingTests)}
}

func buildPageSizeReport(manifest runtimeManifest) map[string]any {
	rows := make([]map[string]string, 0, len(manifest.Components)*2)
	for _, component := range manifest.Components {
		if component.EntryType != "apk_elf" {
			continue
		}
		for _, pageSize := range []string{"4k", "16k"} {
			rows = append(rows, map[string]string{"runtime_id": component.ID, "entrypoint": component.Entrypoint, "page_size": pageSize, "status": "pending-elf-note-verification"})
		}
	}
	return map[string]any{"schema_version": "1", "required_page_sizes": []string{"4k", "16k"}, "rows": rows, "notes": []string{"Attach readelf page-alignment output per runtime entrypoint before release exit."}}
}

func buildTestPlaceholders() map[string]any {
	return map[string]any{
		"schema_version": "1",
		"reports": []map[string]string{
			{"id": "go_core_race", "path": "test-reports/go-core-race.json", "status": "pending-targeted-run"},
			{"id": "flutter_test", "path": "test-reports/flutter-test.json", "status": "pending-targeted-run"},
			{"id": "flutter_analyze", "path": "test-reports/flutter-analyze.json", "status": "pending-targeted-run"},
			{"id": "kotlin_unit", "path": "test-reports/kotlin-unit/", "status": "ci-uploaded-when-present"},
			{"id": "runtime_smoke", "path": "runtime/smoke-evidence.json", "status": "pending-device-verification"},
		},
	}
}

func buildCoreCycleEvidence(now string) map[string]any {
	return map[string]any{
		"schema_version": "1",
		"generated_at":   now,
		"target_cycles":  100,
		"summary":        map[string]int{"total": 0, "pass": 0, "fail": 0, "data_corruption": 0},
		"record_schema":  []string{"cycle", "api_level", "device", "start_utc", "stop_utc", "database_hash_before", "database_hash_after", "status", "notes"},
		"records":        []map[string]any{},
	}
}

func buildAPIMatrixEvidence(trace routeTrace) map[string]any {
	return map[string]any{
		"schema_version": "1",
		"api_levels":     []int{28, 29, 31, 34, 35},
		"route_count":    len(trace.Routes),
		"record_schema":  []string{"api_level", "device", "page_size", "apk_sha256", "core_start", "runtime_smoke", "route_contract", "upgrade_recovery", "status", "notes"},
		"records":        []map[string]any{},
	}
}

func buildSchedulerEvidence(window, now string) map[string]any {
	return map[string]any{
		"schema_version": "1",
		"window":         window,
		"generated_at":   now,
		"slo":            map[string]any{"scheduled_to_start_seconds": 60, "eligible_foreground_success_rate": 0.99},
		"summary":        map[string]int{"eligible": 0, "started_within_slo": 0, "failed": 0, "system_stopped": 0, "result_unknown": 0},
		"record_schema":  []string{"task_id", "scheduled_utc", "expression_hash", "process_started_utc", "monotonic_delta_ms", "api_level", "device", "policy", "status", "reason_code"},
		"records":        []map[string]any{},
	}
}

func statusForMissing(missing int) string {
	if missing == 0 {
		return "complete"
	}
	return "missing-test-cases"
}

func writeLicenses(path, generatedAt string, components []sbomComponent) {
	var b strings.Builder
	b.WriteString("# Third-Party Licenses\n\n")
	b.WriteString("Generated at: ")
	b.WriteString(generatedAt)
	b.WriteString("\n\n")
	b.WriteString("This report enumerates release SBOM components. License texts are resolved from upstream package metadata during release review and attached to this artifact when available.\n\n")
	b.WriteString("| Component | Version | Package URL | License status |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, component := range components {
		b.WriteString("| `")
		b.WriteString(component.Name)
		b.WriteString("` | `")
		b.WriteString(component.Version)
		b.WriteString("` | `")
		b.WriteString(component.PURL)
		b.WriteString("` | pending metadata resolution |\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fatalf("write licenses: %v", err)
	}
}

func writeIndex(path string, evidence releaseEvidence) {
	var b strings.Builder
	b.WriteString("# Release Evidence Bundle\n\n")
	b.WriteString("Version: `")
	b.WriteString(evidence.Release.Version)
	b.WriteString("`\n\n")
	b.WriteString("## Artifacts\n\n")
	for _, artifact := range evidence.Artifacts {
		b.WriteString("- `")
		b.WriteString(artifact.Path)
		b.WriteString("` SHA-256 `")
		b.WriteString(artifact.SHA256)
		b.WriteString("`\n")
	}
	b.WriteString("\n## Reports\n\n")
	keys := make([]string, 0, len(evidence.Reports))
	for key := range evidence.Reports {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString("- `")
		b.WriteString(key)
		b.WriteString("`: `")
		b.WriteString(evidence.Reports[key])
		b.WriteString("`\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fatalf("write index: %v", err)
	}
}

func readJSON(path string, target any) {
	payload, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		fatalf("decode %s: %v", path, err)
	}
}

func writeJSON(path string, value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatalf("encode %s: %v", path, err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "snapshot"
	}
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

func deterministicUUID(value string) string {
	sum := sha256.Sum256([]byte(value))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
