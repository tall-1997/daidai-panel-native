package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateContractRequiresCanonicalBidirectionalSet(t *testing.T) {
	contract := validTestContract(t)
	contract.Compatibility.Runtimes = contract.Compatibility.Runtimes[:7]

	errs := validateContract(contract, false)
	assertErrorContains(t, errs, "compatibility runtime IDs")
}

func TestValidateContractRequiresCompatibilityLegacyIDsToMatchRecords(t *testing.T) {
	contract := validTestContract(t)
	contract.Compatibility.RuntimeIDs[0] = "python-3.12-android-arm64"

	errs := validateContract(contract, false)
	assertErrorContains(t, errs, "compatibility runtime_ids")
}

func TestValidateContractRequiresMatchingVersionAndEntry(t *testing.T) {
	contract := validTestContract(t)
	contract.Smoke.Records[0].Version = "wrong"
	contract.Compatibility.Runtimes[1].Entry = "wrong.so"

	errs := validateContract(contract, false)
	assertErrorContains(t, errs, "smoke version mismatch")
	assertErrorContains(t, errs, "compatibility entry mismatch")
}

func TestValidateContractRejectsUnsubstantiatedPass(t *testing.T) {
	contract := validTestContract(t)
	contract.Smoke.Records[0].Status = "pass"
	contract.Smoke.Records[0].EvidenceSource = "host"
	contract.Smoke.Records[0].Checks[0] = smokeCheck{ID: "PY_OK", Status: "pass", Output: "PY_OK"}

	errs := validateContract(contract, false)
	assertErrorContains(t, errs, "pass requires android-device evidence")
}

func TestStrictModeRejectsBlockedRecords(t *testing.T) {
	contract := validTestContract(t)

	errs := validateContract(contract, true)
	assertErrorContains(t, errs, "blocked in strict mode")
}

func TestReadContractAcceptsGeneratedRuntimeMetadataExtensions(t *testing.T) {
	root := t.TempDir()
	contract := validTestContract(t)
	contract.Manifest.Components[0].PythonTag = "cp314"
	contract.Manifest.Components[0].RuntimeSHA256 = strings.Repeat("1", 64)
	contract.Manifest.Components[0].ArtifactCount = 1
	contract.Manifest.Components[0].AssetRevision = "3.14.6-android-arm64-r1"
	contract.Manifest.Components[0].Artifacts = []runtimeArtifact{{Path: "assets/runtime.py", SHA256: strings.Repeat("2", 64), Size: 42}}
	contract.Compatibility.PythonWheelPolicy = map[string]any{"python_tag": "cp314", "offline": []any{"py3-none-any"}}
	manifestPath := filepath.Join(root, "manifest.json")
	compatibilityPath := filepath.Join(root, "compatibility.json")
	smokePath := filepath.Join(root, "smoke-evidence.json")
	writeJSONFile(t, manifestPath, contract.Manifest)
	writeJSONFile(t, compatibilityPath, contract.Compatibility)
	writeJSONFile(t, smokePath, contract.Smoke)

	decoded, err := readContract(manifestPath, compatibilityPath, smokePath)
	if err != nil {
		t.Fatalf("readContract() error = %v", err)
	}
	if decoded.Manifest.Components[0].PythonTag != "cp314" || decoded.Manifest.Components[0].ArtifactCount != 1 {
		t.Fatalf("generated runtime metadata was not decoded: %#v", decoded.Manifest.Components[0])
	}
}

func TestValidateAPKChecksMetadataAndManifestEntrypoints(t *testing.T) {
	contract := validTestContract(t)
	apkPath := filepath.Join(t.TempDir(), "app.apk")
	writeTestAPK(t, apkPath, contract, true)

	errs := validateAPK(apkPath, contract, false)
	if len(errs) != 0 {
		t.Fatalf("validateAPK() errors = %v", errs)
	}

	writeTestAPK(t, apkPath, contract, false)
	contract.Smoke.Records[len(contract.Smoke.Records)-1].Status = "pass"
	errs = validateAPK(apkPath, contract, false)
	assertErrorContains(t, errs, "missing APK entry")
}

func TestValidateAPKRejectsEmbeddedMetadataDrift(t *testing.T) {
	contract := validTestContract(t)
	apkPath := filepath.Join(t.TempDir(), "app.apk")
	drifted := contract
	drifted.Manifest.Components = append([]runtimeComponent(nil), contract.Manifest.Components...)
	drifted.Manifest.Components[0].Version = "wrong"
	writeTestAPKWithMetadata(t, apkPath, contract, drifted)

	errs := validateAPK(apkPath, contract, false)
	assertErrorContains(t, errs, "APK metadata assets/manifest.json differs")
}

func TestValidateAPKRejectsUndeclaredRuntimeEntrypoint(t *testing.T) {
	contract := validTestContract(t)
	apkPath := filepath.Join(t.TempDir(), "app.apk")
	writeTestAPKWithExtraEntry(t, apkPath, contract, "lib/arm64-v8a/libundeclared_exec.so")

	errs := validateAPK(apkPath, contract, false)
	assertErrorContains(t, errs, "undeclared APK runtime entry")
}

func TestNonStrictAllowsMissingBlockedNativeEntry(t *testing.T) {
	contract := validTestContract(t)
	nativeDir := t.TempDir()

	errs := validateNativeEntries(nativeDir, contract, false)
	if len(errs) != 0 {
		t.Fatalf("non-strict missing blocked entry errors = %v", errs)
	}
	errs = validateNativeEntries(nativeDir, contract, true)
	assertErrorContains(t, errs, "missing native entry")
}

func TestPlaceholderELFIsBlockedAndFailsStrictMode(t *testing.T) {
	contract := validTestContract(t)
	nativeDir := t.TempDir()
	placeholder := append(testELFHeader(), []byte("RUNTIME_STUB_OK")...)
	for _, component := range contract.Manifest.Components {
		if err := os.WriteFile(filepath.Join(nativeDir, component.Entrypoint), placeholder, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	errs := validateNativeEntries(nativeDir, contract, false)
	if len(errs) != 0 {
		t.Fatalf("non-strict placeholder validation errors = %v", errs)
	}
	errs = validateNativeEntries(nativeDir, contract, true)
	assertErrorContains(t, errs, "placeholder ELF blocked in strict mode")
}

func validTestContract(t *testing.T) runtimeContract {
	t.Helper()
	manifest := runtimeManifest{Version: "1"}
	compatibility := compatibilityMatrix{Version: "1"}
	smoke := smokeEvidence{Version: "1", Matrix: []string{"api35-16k"}}
	for i, id := range canonicalRuntimeIDs {
		entry := strings.ReplaceAll(id, "-", "_") + ".so"
		component := runtimeComponent{ID: id, Version: "v" + string(rune('1'+i)), ABI: "arm64-v8a", Entrypoint: entry, SHA256: strings.Repeat("0", 64), RuntimeType: "language-runtime", Isolation: "android-app-sandbox"}
		manifest.Components = append(manifest.Components, component)
		compatibility.Runtimes = append(compatibility.Runtimes, compatibilityRuntime{ID: id, Version: component.Version, Entry: entry})
		compatibility.RuntimeIDs = append(compatibility.RuntimeIDs, id)
		smoke.Records = append(smoke.Records, smokeRecord{
			RuntimeID: id, Version: component.Version, Entry: entry, Status: "blocked", EvidenceSource: "none",
			IsolationLevel: "android-app-sandbox", TimeoutSeconds: 10,
			Checks: []smokeCheck{{ID: "CHECK", Status: "blocked", Reason: "runtime-placeholder-elf"}},
		})
	}
	return runtimeContract{Manifest: manifest, Compatibility: compatibility, Smoke: smoke}
}

func writeTestAPK(t *testing.T, path string, contract runtimeContract, includeAllEntries bool) {
	t.Helper()
	writeTestAPKWithMetadataAndEntries(t, path, contract, contract, includeAllEntries)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestAPKWithMetadata(t *testing.T, path string, entries runtimeContract, metadata runtimeContract) {
	t.Helper()
	writeTestAPKWithMetadataAndEntries(t, path, entries, metadata, true)
}

func writeTestAPKWithMetadataAndEntries(t *testing.T, path string, entries, metadata runtimeContract, includeAllEntries bool) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writeJSONEntry := func(name string, value any) {
		payload, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write(payload); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	writeJSONEntry("assets/manifest.json", metadata.Manifest)
	writeJSONEntry("assets/compatibility.json", metadata.Compatibility)
	writeJSONEntry("assets/smoke-evidence.json", metadata.Smoke)
	limit := len(entries.Manifest.Components)
	if !includeAllEntries {
		limit--
	}
	for _, component := range entries.Manifest.Components[:limit] {
		entry, createErr := writer.Create("lib/arm64-v8a/" + component.Entrypoint)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write(append(testELFHeader(), []byte("RUNTIME_STUB_OK")...)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestAPKWithExtraEntry(t *testing.T, path string, contract runtimeContract, extraEntry string) {
	t.Helper()
	writeTestAPK(t, path, contract, true)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, source := range reader.File {
		target, createErr := writer.Create(source.Name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		contents, readErr := readZipFile(source)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, writeErr := target.Write(contents); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	extra, err := writer.Create(extraEntry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extra.Write(testELFHeader()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testELFHeader() []byte {
	header := make([]byte, 20)
	copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	header[18] = 183
	return header
}

func assertErrorContains(t *testing.T, errs []error, want string) {
	t.Helper()
	var text bytes.Buffer
	for _, err := range errs {
		text.WriteString(err.Error())
		text.WriteByte('\n')
	}
	if !strings.Contains(text.String(), want) {
		t.Fatalf("errors = %q, want substring %q", text.String(), want)
	}
}
