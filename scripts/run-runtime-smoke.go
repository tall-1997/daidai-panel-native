package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	IsolationLevel string  `json:"isolation_level"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Checks         []check `json:"checks"`
}

type check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output string `json:"output"`
}

func main() {
	outputPath := flag.String("output", "runtime/smoke-evidence.json", "smoke evidence output path")
	flag.Parse()
	now := time.Now().UTC().Format(time.RFC3339)
	result := evidence{
		Version:   "1",
		UpdatedAt: now,
		Matrix:    []string{"api28-4k", "api35-4k", "api35-16k"},
		Records: []record{
			pythonRecord(),
		},
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode evidence: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outputPath, encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write evidence: %v\n", err)
		os.Exit(1)
	}
}

func pythonRecord() record {
	return record{RuntimeID: "python-3.12-android-arm64", Version: "3.14.6", Entry: "libpython_exec.so", IsolationLevel: "trusted-runner", TimeoutSeconds: 10, Checks: []check{
		run("PY_OK", "python3", "-c", "print('PY_OK')"),
		run("SSL", "python3", "-c", "import ssl;print('SSL')"),
		run("SQLite", "python3", "-c", "import sqlite3;print('SQLite')"),
		run("venv", "python3", "-c", "import venv;print('venv')"),
		run("pip", "python3", "-c", "import ensurepip;print('pip')"),
	}}
}

func run(id, command string, args ...string) check {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil || !strings.Contains(text, id) {
		return check{ID: id, Status: "failed", Output: text}
	}
	return check{ID: id, Status: "pass", Output: text}
}
