package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeLocatorResolvesManifestEntrypointWithinNativeLibraryDir(t *testing.T) {
	nativeDir := t.TempDir()
	entry := filepath.Join(nativeDir, "libyaegi_go_exec.so")
	if err := os.WriteFile(entry, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write entrypoint: %v", err)
	}

	locator := NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "libyaegi_go_exec.so"}}})
	executable, err := locator.Resolve(RuntimeIDYaegiGo)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if executable.Path != entry || executable.NativeLibraryDir != nativeDir || executable.RuntimeID != RuntimeIDYaegiGo {
		t.Fatalf("unexpected executable: %+v", executable)
	}
}

func TestRuntimeLocatorRejectsEscapedEntrypoint(t *testing.T) {
	nativeDir := t.TempDir()
	locator := NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "../escape.so"}}})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestRuntimeLocatorRejectsSymlinkEscapedEntrypoint(t *testing.T) {
	nativeDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideEntry := filepath.Join(outsideDir, "libescape.so")
	if err := os.WriteFile(outsideEntry, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write outside entrypoint: %v", err)
	}
	linkPath := filepath.Join(nativeDir, "libyaegi_go_exec.so")
	if err := os.Symlink(outsideEntry, linkPath); err != nil {
		t.Fatalf("create escaped symlink: %v", err)
	}

	locator := NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "libyaegi_go_exec.so"}}})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func TestRuntimeLocatorRejectsSymlinkEscapedEntrypointDirectory(t *testing.T) {
	nativeDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideEntry := filepath.Join(outsideDir, "libescape.so")
	if err := os.WriteFile(outsideEntry, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write outside entrypoint: %v", err)
	}
	linkDir := filepath.Join(nativeDir, "linkdir")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Fatalf("create escaped directory symlink: %v", err)
	}

	locator := NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "linkdir/libescape.so"}}})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected symlink directory escape error, got %v", err)
	}
}

func TestRuntimeLocatorReportsMissingRuntimeID(t *testing.T) {
	locator := NewManifestRuntimeLocator(t.TempDir(), RuntimeManifest{})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing runtime error, got %v", err)
	}
}

func TestRuntimeLocatorRejectsEmptyRuntimeID(t *testing.T) {
	locator := NewManifestRuntimeLocator(t.TempDir(), RuntimeManifest{})
	_, err := locator.Resolve("  ")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required runtime id error, got %v", err)
	}
}

func TestRuntimeLocatorRejectsEmptyNativeLibraryDir(t *testing.T) {
	locator := NewManifestRuntimeLocator(" ", RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "libyaegi_go_exec.so"}}})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "native library dir") {
		t.Fatalf("expected native library dir error, got %v", err)
	}
}

func TestRuntimeLocatorRejectsEmptyEntrypoint(t *testing.T) {
	locator := NewManifestRuntimeLocator(t.TempDir(), RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: " "}}})
	_, err := locator.Resolve(RuntimeIDYaegiGo)
	if err == nil || !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("expected entrypoint error, got %v", err)
	}
}
