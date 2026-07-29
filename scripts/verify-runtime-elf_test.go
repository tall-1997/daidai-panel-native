package main

import "testing"

func TestValidateSmokeRecordAcceptsAuditableBlockedRecordOutsideStrictMode(t *testing.T) {
	component := runtimeComponent{ID: "python-3.14-android-arm64", Version: "3.14.6", Entrypoint: "libpython_exec.so"}
	record := smokeRecord{
		RuntimeID: "python-3.14-android-arm64", Version: "3.14.6", Entry: "libpython_exec.so",
		Status: "blocked", EvidenceSource: "none", IsolationLevel: "trusted-runner", TimeoutSeconds: 10,
		Checks: []smokeCheck{{ID: "PY_OK", Status: "blocked", Reason: "runtime-placeholder-elf"}},
	}
	if err := validateSmokeRecord(component, record, false); err != nil {
		t.Fatalf("validateSmokeRecord() error = %v", err)
	}
}

func TestValidateSmokeRecordRejectsBlockedRecordInStrictMode(t *testing.T) {
	component := runtimeComponent{ID: "python-3.14-android-arm64", Version: "3.14.6", Entrypoint: "libpython_exec.so"}
	record := smokeRecord{
		RuntimeID: "python-3.14-android-arm64", Version: "3.14.6", Entry: "libpython_exec.so",
		Status: "blocked", EvidenceSource: "none", IsolationLevel: "trusted-runner", TimeoutSeconds: 10,
		Checks: []smokeCheck{{ID: "PY_OK", Status: "blocked", Reason: "runtime-placeholder-elf"}},
	}
	if err := validateSmokeRecord(component, record, true); err == nil {
		t.Fatal("validateSmokeRecord() expected strict blocked error")
	}
}

func TestValidateSmokeRecordRequiresMatchingVersion(t *testing.T) {
	component := runtimeComponent{ID: "python-3.14-android-arm64", Version: "3.14.6", Entrypoint: "libpython_exec.so"}
	record := smokeRecord{
		RuntimeID: "python-3.14-android-arm64", Version: "3.12.0", Entry: "libpython_exec.so",
		Status: "pass", EvidenceSource: "android-device", IsolationLevel: "trusted-runner", TimeoutSeconds: 10,
		Checks: []smokeCheck{{ID: "PY_OK", Status: "pass", Output: "PY_OK"}},
	}
	if err := validateSmokeRecord(component, record, false); err == nil {
		t.Fatal("validateSmokeRecord() expected version mismatch error")
	}
}
