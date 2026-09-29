package handler

import (
	"testing"
)

func TestAndroidArchiveTargetPathRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	if _, err := androidArchiveTargetPath(base, "../escape.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	if _, err := androidArchiveTargetPath(base, "..\\escape.txt"); err == nil {
		t.Fatal("expected backslash traversal to be rejected")
	}
}

func TestAndroidArchiveTargetPathAcceptsInsidePath(t *testing.T) {
	base := t.TempDir()
	got, err := androidArchiveTargetPath(base, "sub/dir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == base {
		t.Fatalf("expected nested path, got root %q", got)
	}
}

func TestValidateAndroidRuntimeDownloadURLRejectsNonHTTPS(t *testing.T) {
	if err := validateAndroidRuntimeDownloadURL("http://example.com/pkg.tar.gz"); err == nil {
		t.Fatal("expected http URL to be rejected")
	}
	if err := validateAndroidRuntimeDownloadURL("file:///etc/passwd"); err == nil {
		t.Fatal("expected file URL to be rejected")
	}
}

func TestValidateAndroidRuntimeDownloadURLRejectsLoopback(t *testing.T) {
	if err := validateAndroidRuntimeDownloadURL("https://127.0.0.1/pkg.tar.gz"); err == nil {
		t.Fatal("expected loopback host to be rejected")
	}
	if err := validateAndroidRuntimeDownloadURL("https://localhost/pkg.tar.gz"); err == nil {
		t.Fatal("expected localhost to be rejected")
	}
}
