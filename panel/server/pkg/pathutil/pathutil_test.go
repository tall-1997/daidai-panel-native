package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWithinBaseAllowsChildPath(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "scripts")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	got, err := ResolveWithinBase(base, filepath.Join("jobs", "task.js"), false)
	if err != nil {
		t.Fatalf("ResolveWithinBase returned error: %v", err)
	}

	want := resolvePathFromExistingAncestor(filepath.Join(base, "jobs", "task.js"))
	if got != want {
		t.Fatalf("unexpected resolved path: got %q want %q", got, want)
	}
}

func TestResolveWithinBaseAllowsChildPathWhenBaseDoesNotExistYet(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "scripts")

	got, err := ResolveWithinBase(base, "demo.sh", false)
	if err != nil {
		t.Fatalf("ResolveWithinBase returned error: %v", err)
	}

	want := resolvePathFromExistingAncestor(filepath.Join(base, "demo.sh"))
	if got != want {
		t.Fatalf("unexpected resolved path: got %q want %q", got, want)
	}
}

func TestResolveWithinBaseRejectsPrefixCollision(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "scripts")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	outside := filepath.Join(root, "scripts_evil", "task.js")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(outside, []byte("console.log('x')"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	if _, err := ResolveWithinBase(base, outside, true); err == nil {
		t.Fatalf("expected prefix collision to be rejected")
	}
}

func TestResolveWithinBaseRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "scripts")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	if _, err := ResolveWithinBase(base, filepath.Join("..", "evil.js"), false); err == nil {
		t.Fatalf("expected traversal to be rejected")
	}
}

func TestResolveWithinBaseRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "scripts")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	linkPath := filepath.Join(base, "link")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := ResolveWithinBase(base, filepath.Join("link", "task.js"), false); err == nil {
		t.Fatalf("expected symlink escape to be rejected")
	}
}
