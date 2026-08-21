package service

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestLockDependencyInstallDeduplicatesNormalizedPythonKey(t *testing.T) {
	var installs atomic.Int32
	var installed atomic.Bool
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			name := "Requests"
			if index%2 == 1 {
				name = "requests"
			}
			unlock := LockDependencyInstall(model.DepTypePython, name, "3.14")
			defer unlock()
			if installed.Load() {
				return
			}
			installs.Add(1)
			time.Sleep(time.Millisecond)
			installed.Store(true)
		}(i)
	}
	close(start)
	wg.Wait()
	if got := installs.Load(); got != 1 {
		t.Fatalf("expected one install for one normalized key, got %d", got)
	}
}

func TestTrimNpmCacheKeepsNewestFilesWithinLimit(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dataDir := filepath.Join(root, "data")
	cache := filepath.Join(dataDir, "deps", "cache", "npm")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(cache, "old")
	recent := filepath.Join(cache, "recent")
	if err := os.WriteFile(old, make([]byte, maxNpmCacheBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recent, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	TrimNpmCache()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expected oldest cache file removed, err=%v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("expected newest cache file retained: %v", err)
	}
}

func TestBuildPipInstallArgsDisablesCache(t *testing.T) {
	args := BuildPipInstallArgs(nil, "requests")
	if len(args) < 2 || args[0] != "install" || args[1] != "--no-cache-dir" {
		t.Fatalf("pip install must disable cache, got %v", args)
	}
}
