package model

import "testing"

func TestNormalizeSuccessExitCodes(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty uses default", raw: "", want: "0"},
		{name: "deduplicates and accepts chinese comma", raw: " 0， 1,1 ", want: "0,1"},
		{name: "accepts whitespace separators", raw: "0  2\n3", want: "0,2,3"},
		{name: "rejects text", raw: "0,done", wantErr: true},
		{name: "rejects negative code", raw: "0,-1", wantErr: true},
		{name: "rejects code over shell range", raw: "0,256", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeSuccessExitCodes(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize success exit codes: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestTaskIsSuccessExitCode(t *testing.T) {
	defaultTask := &Task{}
	if !defaultTask.IsSuccessExitCode(0) {
		t.Fatal("expected empty legacy setting to accept exit code 0")
	}
	if defaultTask.IsSuccessExitCode(1) {
		t.Fatal("did not expect empty legacy setting to accept exit code 1")
	}

	compatibleTask := &Task{SuccessExitCodes: "0,1"}
	if !compatibleTask.IsSuccessExitCode(1) {
		t.Fatal("expected configured task to accept exit code 1")
	}
	if compatibleTask.IsSuccessExitCode(2) {
		t.Fatal("did not expect configured task to accept exit code 2")
	}
	if compatibleTask.IsSuccessExitCode(-1) {
		t.Fatal("timeout or signal exit code -1 must never be accepted")
	}
}
