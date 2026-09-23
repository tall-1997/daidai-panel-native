package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectArtifactsIncludesAppAndInstrumentationDigests(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "release.apk")
	testAPK := filepath.Join(dir, "release-androidTest.apk")
	if err := os.WriteFile(app, []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testAPK, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts := collectArtifacts(app, testAPK)
	if len(artifacts) != 2 || artifacts[0].SHA256 == artifacts[1].SHA256 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
}

func TestReleaseEvidencePendingStatusIsExplicit(t *testing.T) {
	evidence := releaseEvidence{SchemaVersion: "1", BundleStatus: "generated", Status: "pending"}
	if evidence.BundleStatus != "generated" {
		t.Fatalf("bundle status = %q, want generated", evidence.BundleStatus)
	}
	if evidence.Status != "pending" {
		t.Fatalf("status = %q, want pending", evidence.Status)
	}
	placeholders := buildTestPlaceholders()
	reports := placeholders["reports"].([]map[string]string)
	for _, report := range reports {
		if report["status"] == "generated" || report["status"] == "pass" || report["status"] == "passed" {
			t.Fatalf("pending report %q uses success-like status %q", report["id"], report["status"])
		}
	}
}

func TestStableGateSummaryUsesRealDeviceRuntimeEvidence(t *testing.T) {
	gates := buildGateSummaries("pass")
	deviceGate := gates["task_6_4_device_stability_gate"]
	if deviceGate.Status != "pass" || deviceGate.Report != "runtime/smoke-evidence.json" {
		t.Fatalf("device gate = %#v", deviceGate)
	}
	if strings.Contains(deviceGate.Description, "long-duration") {
		t.Fatalf("device gate describes placeholder evidence: %q", deviceGate.Description)
	}
}

func TestLongRunningEvidenceRemainsIndependentFromCompletedStableGates(t *testing.T) {
	pending := buildPendingEvidence()["long_running_device_samples"]
	if pending.Status != "pending" || !strings.Contains(pending.Report, "scheduler-24h") {
		t.Fatalf("pending long-running evidence = %#v", pending)
	}
}

func TestReleaseGateStateByChannel(t *testing.T) {
	contract := releaseGateContract{SchemaVersion: 1, StableRequiredRuntimeIDs: []string{"python"}}
	contract.ReleaseGateScope.Required = []string{"release", "device"}
	contract.ReleaseGateScope.Optional = []string{"long"}
	verified := runtimeSmokeEvidence{}
	verified.Records = append(verified.Records, struct {
		RuntimeID      string `json:"runtime_id"`
		Status         string `json:"status"`
		EvidenceSource string `json:"evidence_source"`
		Checks         []struct {
			Status string `json:"status"`
			Output string `json:"output"`
		} `json:"checks"`
	}{RuntimeID: "python", Status: "pass", EvidenceSource: "android-device", Checks: []struct {
		Status string `json:"status"`
		Output string `json:"output"`
	}{{Status: "pass", Output: "device proof"}}})
	tests := []struct {
		name, channel, wantTop string
		smoke                  runtimeSmokeEvidence
		wantError              string
	}{
		{name: "stable verified", channel: "stable", smoke: verified, wantTop: "completed"},
		{name: "prerelease pending", channel: "prerelease", wantTop: "pending"},
		{name: "snapshot pending", channel: "snapshot", wantTop: "pending"},
		{name: "stable pending rejected", channel: "stable", wantError: "lacks verified device evidence"},
		{name: "unknown channel rejected", channel: "nightly", wantError: "unsupported channel"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			top, gate, err := releaseGateState(test.channel, contract, test.smoke)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			wantGate := "pending"
			if test.channel == "stable" {
				wantGate = "pass"
			}
			if err != nil || top != test.wantTop || gate != wantGate {
				t.Fatalf("state = (%q, %q, %v), want (%q, %q, nil)", top, gate, err, test.wantTop, wantGate)
			}
		})
	}
}

func TestCompatibilityReportUsesCurrentDeviceMatrix(t *testing.T) {
	report := buildCompatibilityReport(runtimeManifest{Components: []runtimeComponent{{ID: "python", Entrypoint: "python.so", ABI: "arm64-v8a"}}})
	combinations := report["required_combinations"].([]string)
	if len(combinations) != 1 || combinations[0] != "api35-16k" {
		t.Fatalf("required combinations = %v", combinations)
	}
}
