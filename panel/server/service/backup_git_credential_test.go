package service

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const backupGitConfigWithToken = "[core]\n\trepositoryformatversion = 0\n" +
	"[remote \"origin\"]\n" +
	"\turl = https://x-access-token:ghp_backupRegressionToken@github.com/demo/scripts.git\n" +
	"\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

func TestSanitizeGitConfigCredentials(t *testing.T) {
	sanitized := string(sanitizeGitConfigCredentials([]byte(backupGitConfigWithToken)))

	if strings.Contains(sanitized, "ghp_backupRegressionToken") {
		t.Fatalf("expected PAT to be stripped, got %q", sanitized)
	}
	if !strings.Contains(sanitized, "url = https://github.com/demo/scripts.git") {
		t.Fatalf("expected remote URL to survive without credentials, got %q", sanitized)
	}

	// SSH remote 的 git@ 只是用户名、不是凭据，动了它会让还原后的 SSH 订阅连不上
	sshConfig := "[remote \"origin\"]\n\turl = ssh://git@github.com/demo/scripts.git\n"
	if got := string(sanitizeGitConfigCredentials([]byte(sshConfig))); got != sshConfig {
		t.Fatalf("expected ssh remote to stay untouched, got %q", got)
	}

	scpConfig := "[remote \"origin\"]\n\turl = git@github.com:demo/scripts.git\n"
	if got := string(sanitizeGitConfigCredentials([]byte(scpConfig))); got != scpConfig {
		t.Fatalf("expected scp-style remote to stay untouched, got %q", got)
	}
}

// 备份仍然打包 .git（还原行为完全不变、零数据丢失风险），
// 但写进 tar 的那份 .git/config 必须已经不含 PAT，
// 且磁盘上的真实 .git/config 不能被改动——改了后续 fetch 就没鉴权了。
func TestAddDirectoryToTarSanitizesGitConfig(t *testing.T) {
	scriptsDir := t.TempDir()
	gitDir := filepath.Join(scriptsDir, "SmallWorld", ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir git dir: %v", err)
	}
	configPath := filepath.Join(gitDir, "config")
	if err := os.WriteFile(configPath, []byte(backupGitConfigWithToken), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write git HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "SmallWorld", "demo.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	archiveRoot := filepath.Join("files", "scripts")
	if err := addDirectoryToTar(tw, scriptsDir, archiveRoot); err != nil {
		t.Fatalf("add scripts dir to tar: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	entries := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar body %s: %v", header.Name, err)
		}
		entries[header.Name] = string(data)
	}

	gitConfigEntry := "files/scripts/SmallWorld/.git/config"
	packedConfig, ok := entries[gitConfigEntry]
	if !ok {
		t.Fatalf("expected .git/config to stay in the backup archive, got entries=%v", keysOf(entries))
	}
	if strings.Contains(packedConfig, "ghp_backupRegressionToken") {
		t.Fatalf("backup archive must not contain the PAT, got %q", packedConfig)
	}
	if !strings.Contains(packedConfig, "https://github.com/demo/scripts.git") {
		t.Fatalf("expected sanitized remote URL in archive, got %q", packedConfig)
	}
	if _, ok := entries["files/scripts/SmallWorld/.git/HEAD"]; !ok {
		t.Fatalf("expected the rest of .git to stay packed, got entries=%v", keysOf(entries))
	}
	if _, ok := entries["files/scripts/SmallWorld/demo.py"]; !ok {
		t.Fatalf("expected normal scripts to stay packed, got entries=%v", keysOf(entries))
	}

	onDisk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reload git config: %v", err)
	}
	if string(onDisk) != backupGitConfigWithToken {
		t.Fatalf("backup must not rewrite the real .git/config, got %q", string(onDisk))
	}
}

func keysOf(entries map[string]string) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}
