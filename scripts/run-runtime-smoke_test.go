package main

import "testing"

func TestBuildBlockedEvidenceCoversAllRuntimesWithoutFakePasses(t *testing.T) {
	evidence := buildBlockedEvidence("2026-07-29T00:00:00Z")
	if len(evidence.Matrix) != 1 || evidence.Matrix[0] != "api35-16k" {
		t.Fatalf("matrix = %v, want [api35-16k]", evidence.Matrix)
	}
	wantIDs := []string{
		"python-3.12-android-arm64",
		"node-lts-android-arm64",
		"typescript-stable",
		"shell-android-arm64",
		"git-android-arm64",
		"ssh-android-arm64",
		"yaegi-go",
		"go-builder-android-arm64",
	}
	if len(evidence.Records) != len(wantIDs) {
		t.Fatalf("records = %d, want %d", len(evidence.Records), len(wantIDs))
	}
	for i, record := range evidence.Records {
		if record.RuntimeID != wantIDs[i] {
			t.Errorf("record[%d].RuntimeID = %q, want %q", i, record.RuntimeID, wantIDs[i])
		}
		if record.Status != "blocked" || record.EvidenceSource != "none" {
			t.Errorf("record[%d] has unaudited status/source: %+v", i, record)
		}
		for _, check := range record.Checks {
			if check.Status != "blocked" || check.Reason != "android-smoke-not-run" || check.Output != "" {
				t.Errorf("record[%d] has invalid blocked check: %+v", i, check)
			}
		}
	}
}
