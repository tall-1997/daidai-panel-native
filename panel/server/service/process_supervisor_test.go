package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateProcessWorkingDirRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := validateProcessWorkingDir(link, allowed); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestFilterProcessEnvDropsDangerousVariables(t *testing.T) {
	filtered := filterProcessEnv([]string{
		"PATH=/usr/bin",
		"LD_PRELOAD=/tmp/inject.so",
		"LD_LIBRARY_PATH=/tmp/lib",
		"DYLD_INSERT_LIBRARIES=/tmp/inject.dylib",
		"PYTHONPATH=/tmp/python",
		"NODE_OPTIONS=--require /tmp/hook.js",
		"JAVA_TOOL_OPTIONS=-javaagent:/tmp/agent.jar",
		"APP_SAFE=value",
		"=bad",
		"BAD\x00KEY=value",
	})

	joined := strings.Join(filtered, "\n")
	for _, key := range []string{
		"LD_PRELOAD=",
		"LD_LIBRARY_PATH=",
		"DYLD_INSERT_LIBRARIES=",
		"PYTHONPATH=",
		"NODE_OPTIONS=",
		"JAVA_TOOL_OPTIONS=",
	} {
		if strings.Contains(joined, key) {
			t.Fatalf("expected %s to be filtered, got %q", key, filtered)
		}
	}
	for _, entry := range []string{"PATH=/usr/bin", "APP_SAFE=value"} {
		if !containsString(filtered, entry) {
			t.Fatalf("expected %s to be preserved, got %q", entry, filtered)
		}
	}
}

func TestStartRejectsUnsupportedProcessQuotas(t *testing.T) {
	supervisor := DefaultProcessSupervisor{}
	for name, quota := range map[string]ProcessResourceQuota{
		"max processes": {MaxProcesses: 2},
		"max memory":    {MaxMemoryBytes: 1024},
	} {
		t.Run(name, func(t *testing.T) {
			proc, err := supervisor.Start(context.Background(), ProcessSpec{
				Argv:       []string{"sh", "-c", "exit 0"},
				WorkingDir: t.TempDir(),
				Quota:      quota,
			}, nil)
			if err == nil {
				if proc != nil {
					_ = proc.Cancel()
				}
				t.Fatal("expected unsupported quota error")
			}
			if !errors.Is(err, ErrUnsupportedProcessQuota) {
				t.Fatalf("expected unsupported quota error, got %v", err)
			}
		})
	}
}

func TestStartKeepsMaxOutputBytesEnforced(t *testing.T) {
	supervisor := DefaultProcessSupervisor{}
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("locate sh: %v", err)
	}
	proc, err := supervisor.Start(context.Background(), ProcessSpec{
		Argv:       []string{shellPath, "-c", "printf abcdef"},
		WorkingDir: t.TempDir(),
		Quota: ProcessResourceQuota{
			MaxOutputBytes: 3,
		},
	}, nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	result, err := proc.Wait()
	if err != nil {
		t.Fatalf("wait process: %v", err)
	}
	if result.Output != "abc" {
		t.Fatalf("expected output to be capped, got %q", result.Output)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
