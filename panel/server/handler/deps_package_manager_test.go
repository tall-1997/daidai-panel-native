package handler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectLinuxPackageManagerWithLookPath(t *testing.T) {
	manager, err := detectLinuxPackageManagerWithLookPath(func(file string) (string, error) {
		if file == "apk" {
			return "/sbin/apk", nil
		}
		return "", errors.New("missing")
	})
	if err != nil {
		t.Fatalf("expected apk manager, got error: %v", err)
	}
	if manager.Name != "apk" || manager.Binary != "apk" {
		t.Fatalf("unexpected manager: %+v", manager)
	}

	manager, err = detectLinuxPackageManagerWithLookPath(func(file string) (string, error) {
		if file == "apt-get" {
			return "/usr/bin/apt-get", nil
		}
		return "", errors.New("missing")
	})
	if err != nil {
		t.Fatalf("expected apt manager, got error: %v", err)
	}
	if manager.Name != "apt" || manager.Binary != "apt-get" {
		t.Fatalf("unexpected manager: %+v", manager)
	}
}

func TestShouldRefreshAptPackageListsFromDir(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	if !shouldRefreshAptPackageListsFromDir(dir, now, time.Hour) {
		t.Fatalf("expected empty apt lists directory to require refresh")
	}

	indexFile := filepath.Join(dir, "archive.example.com_dists_stable_InRelease")
	if err := os.WriteFile(indexFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write apt list file: %v", err)
	}

	recentTime := now.Add(-30 * time.Minute)
	if err := os.Chtimes(indexFile, recentTime, recentTime); err != nil {
		t.Fatalf("set recent mtime: %v", err)
	}
	if shouldRefreshAptPackageListsFromDir(dir, now, time.Hour) {
		t.Fatalf("expected recent apt lists to skip refresh")
	}

	oldTime := now.Add(-2 * time.Hour)
	if err := os.Chtimes(indexFile, oldTime, oldTime); err != nil {
		t.Fatalf("set old mtime: %v", err)
	}
	if !shouldRefreshAptPackageListsFromDir(dir, now, time.Hour) {
		t.Fatalf("expected stale apt lists to require refresh")
	}
}

func TestLinuxInstallCommandSpec(t *testing.T) {
	bin, args, err := linuxInstallCommandSpec(linuxPackageManager{Name: "apk", Binary: "apk"}, "curl", false)
	if err != nil {
		t.Fatalf("apk install spec error: %v", err)
	}
	if bin != "apk" {
		t.Fatalf("expected apk binary, got %s", bin)
	}
	if strings.Join(args, " ") != "add --no-cache curl" {
		t.Fatalf("unexpected apk args: %v", args)
	}

	bin, args, err = linuxInstallCommandSpec(linuxPackageManager{Name: "apt", Binary: "apt-get"}, "python3-pip", true)
	if err != nil {
		t.Fatalf("apt install spec error: %v", err)
	}
	if bin != "sh" {
		t.Fatalf("expected sh wrapper for apt install, got %s", bin)
	}
	script := strings.Join(args, " ")
	if !strings.Contains(script, "apt-get update") {
		t.Fatalf("expected apt script to refresh indexes, got %v", args)
	}
	if !strings.Contains(script, "apt-get install -y --no-install-recommends 'python3-pip'") {
		t.Fatalf("expected apt install command in script, got %v", args)
	}
}

func TestRewriteAPTListLine(t *testing.T) {
	line := "deb [arch=amd64] http://archive.ubuntu.com/ubuntu jammy main restricted"
	updated, changed := rewriteAPTListLine(line, "ubuntu", "https://mirrors.aliyun.com/ubuntu")
	if !changed {
		t.Fatalf("expected apt list line to change")
	}
	if updated != "deb [arch=amd64] https://mirrors.aliyun.com/ubuntu jammy main restricted" {
		t.Fatalf("unexpected updated line: %s", updated)
	}

	defaulted, changed := rewriteAPTListLine(updated, "ubuntu", "")
	if changed {
		t.Fatalf("expected apt list line already using default accelerated mirror to remain unchanged")
	}
	if defaulted != "deb [arch=amd64] https://mirrors.aliyun.com/ubuntu jammy main restricted" {
		t.Fatalf("unexpected defaulted line: %s", defaulted)
	}
}

func TestRewriteAPTSourcesContent(t *testing.T) {
	content := "Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble noble-updates\nComponents: main restricted\n"
	updated, changed := rewriteAPTSourcesContent(content, "ubuntu", "https://mirrors.aliyun.com/ubuntu")
	if !changed {
		t.Fatalf("expected apt sources content to change")
	}
	if !strings.Contains(updated, "URIs: https://mirrors.aliyun.com/ubuntu") {
		t.Fatalf("unexpected rewritten sources content: %s", updated)
	}
}

func TestEffectiveLinuxMirrorFallsBackToDefaultAcceleratedMirror(t *testing.T) {
	apkManager := linuxPackageManager{Name: "apk", Binary: "apk"}
	if got := effectiveLinuxMirror(apkManager, "", ""); got != "https://mirrors.aliyun.com/alpine" {
		t.Fatalf("expected apk default mirror, got %q", got)
	}
	if got := effectiveLinuxMirror(apkManager, "", "https://dl-cdn.alpinelinux.org/alpine"); got != "https://mirrors.aliyun.com/alpine" {
		t.Fatalf("expected apk official mirror to fall back to accelerated mirror, got %q", got)
	}

	aptManager := linuxPackageManager{Name: "apt", Binary: "apt-get"}
	if got := effectiveLinuxMirror(aptManager, "ubuntu", ""); got != "https://mirrors.aliyun.com/ubuntu" {
		t.Fatalf("expected ubuntu default mirror, got %q", got)
	}
	if got := effectiveLinuxMirror(aptManager, "debian", "http://deb.debian.org/debian"); got != "https://mirrors.aliyun.com/debian" {
		t.Fatalf("expected debian official mirror to fall back to accelerated mirror, got %q", got)
	}
	if got := effectiveLinuxMirror(aptManager, "ubuntu", "https://mirrors.aliyun.com/ubuntu"); got != "https://mirrors.aliyun.com/ubuntu" {
		t.Fatalf("expected custom ubuntu mirror to be preserved, got %q", got)
	}
}
