package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

type evidence struct {
	Version   string   `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	Matrix    []string `json:"matrix"`
	Records   []record `json:"records"`
}

type record struct {
	RuntimeID      string  `json:"runtime_id"`
	Version        string  `json:"version"`
	Entry          string  `json:"entry"`
	Status         string  `json:"status"`
	EvidenceSource string  `json:"evidence_source"`
	IsolationLevel string  `json:"isolation_level"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Checks         []check `json:"checks"`
}

type check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func main() {
	outputPath := flag.String("output", "runtime/smoke-evidence.json", "smoke evidence output path")
	flag.Parse()
	result := buildBlockedEvidence(time.Now().UTC().Format(time.RFC3339))
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode evidence: %v\n", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(*outputPath, encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write evidence: %v\n", err)
		os.Exit(1)
	}
}

func buildBlockedEvidence(updatedAt string) evidence {
	specs := []struct {
		id, version, entry, isolation, checkID string
		timeout                                int
	}{
		{"python-3.14-android-arm64", "3.14.6", "libpython_exec.so", "trusted-runner", "PY_OK_SSL_SQLITE_VENV_PIP", 10},
		{"node-lts-android-arm64", "18.20.4", "libnode_exec.so", "trusted-runner", "COMMONJS_ESM_HTTPS", 10},
		{"typescript-stable", "5.9.3", "libnode_exec.so", "trusted-runner", "TS_OK", 10},
		{"shell-android-arm64", "blocked-placeholder", "libshell_exec.so", "trusted-runner", "SHELL_PIPE_EXIT_STOP", 10},
		{"git-android-arm64", "blocked-placeholder", "libgit_exec.so", "broker", "GIT_CLONE_FETCH_SPARSE", 30},
		{"ssh-android-arm64", "blocked-placeholder", "libssh_exec.so", "broker", "SSH_HOSTKEY", 30},
		{"yaegi-go", "blocked-placeholder", "libyaegi_exec.so", "isolated-worker", "GO_INTERPRET_OK", 10},
		{"go-builder-android-arm64", "blocked-placeholder", "libgobuilder_exec.so", "trusted-builder", "GO_BUILD_EXPORT_ONLY", 60},
	}
	result := evidence{
		Version:   "1",
		UpdatedAt: updatedAt,
		Matrix:    []string{"api28-4k", "api35-4k", "api35-16k"},
		Records:   make([]record, 0, len(specs)),
	}
	for _, spec := range specs {
		result.Records = append(result.Records, record{
			RuntimeID: spec.id, Version: spec.version, Entry: spec.entry,
			Status: "blocked", EvidenceSource: "none", IsolationLevel: spec.isolation, TimeoutSeconds: spec.timeout,
			Checks: []check{{ID: spec.checkID, Status: "blocked", Reason: "android-smoke-not-run"}},
		})
	}
	return result
}
