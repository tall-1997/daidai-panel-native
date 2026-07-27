package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestAppendGitSSHEnvUsesPersistentKnownHosts(t *testing.T) {
	testutil.SetupTestEnv(t)

	sshKeyPath := filepath.Join(t.TempDir(), "deploy key")
	if err := os.WriteFile(sshKeyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write ssh key: %v", err)
	}

	sub := &model.Subscription{
		AuthType: model.SubAuthTypeSSH,
	}
	envCfg, err := buildGitAuthConfig([]string{"BASE=1"}, "git@github.com:demo/private.git", sub, sshKeyPath)
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}
	env := envCfg.Env

	var sshCommand string
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
			sshCommand = strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
			break
		}
	}
	if sshCommand == "" {
		t.Fatalf("expected GIT_SSH_COMMAND to be set, env=%v", env)
	}

	if strings.Contains(sshCommand, "StrictHostKeyChecking=no") {
		t.Fatalf("expected host key checking to stay enabled, got %q", sshCommand)
	}
	if strings.Contains(sshCommand, "/dev/null") {
		t.Fatalf("expected persistent known_hosts instead of /dev/null, got %q", sshCommand)
	}
	if !strings.Contains(sshCommand, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("expected accept-new host key policy, got %q", sshCommand)
	}

	knownHostsPath := filepath.Join(config.C.Data.Dir, "ssh", "known_hosts")
	if _, err := os.Stat(knownHostsPath); err != nil {
		t.Fatalf("expected known_hosts file to exist: %v", err)
	}
	if !strings.Contains(sshCommand, shellEscapeSSHArg(knownHostsPath)) {
		t.Fatalf("expected ssh command to reference known_hosts %q, got %q", knownHostsPath, sshCommand)
	}
}

func TestBuildGitAuthConfigEmbedsTokenIntoURL(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		URL:       "https://github.com/example/private.git",
		AuthType:  model.SubAuthTypeToken,
		AuthToken: "ghp_test_token",
	}
	cfg, err := buildGitAuthConfig([]string{"BASE=1"}, sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config with token: %v", err)
	}

	if cfg.DisplayURL != sub.URL {
		t.Fatalf("expected DisplayURL to stay clean, got %q", cfg.DisplayURL)
	}
	if cfg.RemoteURL == sub.URL {
		t.Fatalf("expected RemoteURL to embed credentials, got unchanged %q", cfg.RemoteURL)
	}
	if !strings.Contains(cfg.RemoteURL, "x-access-token:ghp_test_token@github.com") {
		t.Fatalf("expected default x-access-token user with token embedded, got %q", cfg.RemoteURL)
	}
	if strings.Contains(cfg.DisplayURL, "ghp_test_token") {
		t.Fatalf("DisplayURL should not leak token, got %q", cfg.DisplayURL)
	}

	for _, entry := range cfg.Env {
		if strings.HasPrefix(entry, "GIT_HTTP_EXTRA_HEADER=") {
			t.Fatalf("token auth should not rely on GIT_HTTP_EXTRA_HEADER, got %q", entry)
		}
	}
}

func TestBuildGitAuthConfigUsesCustomUsernameForToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		URL:          "https://gitee.com/example/private.git",
		AuthType:     model.SubAuthTypeToken,
		AuthUsername: "demo-user",
		AuthToken:    "secret token+/?",
	}
	cfg, err := buildGitAuthConfig(nil, sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}

	if !strings.HasPrefix(cfg.RemoteURL, "https://demo-user:") {
		t.Fatalf("expected custom username in remote URL, got %q", cfg.RemoteURL)
	}
	if !strings.Contains(cfg.RemoteURL, "@gitee.com/example/private.git") {
		t.Fatalf("expected gitee host preserved after credentials, got %q", cfg.RemoteURL)
	}
	if strings.Contains(cfg.RemoteURL, "secret token+/?") {
		t.Fatalf("expected token to be URL-encoded, got %q", cfg.RemoteURL)
	}
}

func TestBuildGitAuthConfigRejectsTokenForSSHRemote(t *testing.T) {
	testutil.SetupTestEnv(t)

	sub := &model.Subscription{
		URL:       "git@github.com:example/private.git",
		AuthType:  model.SubAuthTypeToken,
		AuthToken: "ghp_test_token",
	}
	if _, err := buildGitAuthConfig([]string{"BASE=1"}, sub.URL, sub, ""); err == nil {
		t.Fatal("expected token auth to reject SSH remote URL")
	}
}
