package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

type gitAuthTestSecretStore struct {
	openedKeys []string
}

func (s *gitAuthTestSecretStore) Seal(context.Context, string, []byte) (SealedValue, error) {
	return SealedValue{}, fmt.Errorf("seal not used")
}

func (s *gitAuthTestSecretStore) Open(_ context.Context, key string, value SealedValue) ([]byte, error) {
	s.openedKeys = append(s.openedKeys, key)
	return append([]byte{}, value.Cipher...), nil
}

func (s *gitAuthTestSecretStore) Status() SecretStoreStatus {
	return SecretStoreStatus{Provider: "test", Ready: true}
}

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
	assertEnvContainsPrefix(t, env, "GIT_TERMINAL_PROMPT=0")
	assertEnvContainsPrefix(t, env, "GIT_EDITOR=:")
	assertEnvContainsPrefix(t, env, "GIT_PAGER=cat")
	assertEnvContainsPrefix(t, env, "GIT_CONFIG_KEY_0=core.hooksPath")
	assertEnvContainsPrefix(t, env, "GIT_CONFIG_VALUE_0=/dev/null")
	assertEnvContainsPrefix(t, env, "GIT_CONFIG_KEY_3=credential.helper")
	assertEnvContainsPrefix(t, env, "GIT_CONFIG_VALUE_3=")

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
	if strings.Contains(sshCommand, "UserKnownHostsFile=/dev/null") {
		t.Fatalf("expected persistent user known_hosts instead of /dev/null, got %q", sshCommand)
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

func TestBuildGitAuthConfigUsesCAFileAndSecretStoreToken(t *testing.T) {
	testutil.SetupTestEnv(t)
	store := &gitAuthTestSecretStore{}
	restore := SetRuntimeSecretStoreForTest(store)
	defer restore()

	sub := &model.Subscription{
		ID:                      42,
		URL:                     "https://github.com/example/private.git",
		AuthType:                model.SubAuthTypeToken,
		AuthTokenSecretProvider: "test",
		AuthTokenSecretCipher:   []byte("secret-token"),
		CACertPath:              filepath.Join(t.TempDir(), "ca.pem"),
	}
	cfg, err := buildGitAuthConfigWithContext(context.Background(), []string{"BASE=1"}, sub.URL, sub, "")
	if err != nil {
		t.Fatalf("build git auth config with secret token: %v", err)
	}
	credentialPath := assertGitCredentialStoreFile(t, cfg.Env)
	defer cfg.CleanupFunc()

	if cfg.RemoteURL != sub.URL {
		t.Fatalf("expected RemoteURL to stay clean, got %q", cfg.RemoteURL)
	}
	if strings.Contains(cfg.DisplayURL, "secret-token") || strings.Contains(cfg.RemoteURL, "secret-token") {
		t.Fatalf("git URLs should not leak token, display=%q remote=%q", cfg.DisplayURL, cfg.RemoteURL)
	}
	assertEnvContainsPrefix(t, cfg.Env, "GIT_SSL_CAINFO="+sub.CACertPath)
	assertFileMode(t, credentialPath, 0o600)
	if len(store.openedKeys) != 1 || store.openedKeys[0] != "subscription:42:git-token" {
		t.Fatalf("expected token to open through subscription SecretStore key, got %#v", store.openedKeys)
	}
	cfg.CleanupFunc()
	if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("expected credential file cleanup, stat err=%v", err)
	}
}

func TestResolveSubscriptionSSHKeyPathUsesSecretStoreKeyMaterial(t *testing.T) {
	testutil.SetupTestEnv(t)
	store := &gitAuthTestSecretStore{}
	restore := SetRuntimeSecretStoreForTest(store)
	defer restore()

	sub := &model.Subscription{
		ID:                   7,
		AuthType:             model.SubAuthTypeSSH,
		SSHKeySecretProvider: "test",
		SSHKeySecretCipher:   []byte("test-private-key"),
	}
	path, cleanup, err := resolveSubscriptionSSHKeyPath(context.Background(), sub)
	if err != nil {
		t.Fatalf("resolve ssh key secret: %v", err)
	}
	defer cleanup()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized ssh key: %v", err)
	}
	if string(content) != "test-private-key" {
		t.Fatalf("expected materialized key content from SecretStore")
	}
	if len(store.openedKeys) != 1 || store.openedKeys[0] != "subscription:7:ssh-key" {
		t.Fatalf("expected ssh key to open through subscription SecretStore key, got %#v", store.openedKeys)
	}
}

func TestBuildGitAuthConfigRejectsInlineSSHPrivateKeyMaterial(t *testing.T) {
	testutil.SetupTestEnv(t)
	sub := &model.Subscription{AuthType: model.SubAuthTypeSSH}
	_, err := buildGitAuthConfig(nil, "git@github.com:demo/private.git", sub, "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----")
	if err == nil || !strings.Contains(err.Error(), "文件路径") {
		t.Fatalf("expected inline private key material to be rejected as path misuse, got %v", err)
	}
}

func TestNormalizeGitProviderErrorRequiresHostKeyConfirmation(t *testing.T) {
	err := normalizeGitProviderError("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", fmt.Errorf("git failed"))
	if err == nil || !strings.Contains(err.Error(), "请确认新的主机指纹") {
		t.Fatalf("expected host key change to require confirmation, got %v", err)
	}
}

func assertEnvContainsPrefix(t *testing.T, env []string, expected string) {
	t.Helper()
	for _, entry := range env {
		if entry == expected {
			return
		}
	}
	t.Fatalf("expected env to contain %q, got %v", expected, env)
}

func TestBuildGitAuthConfigUsesCredentialStoreForToken(t *testing.T) {
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
	credentialPath := assertGitCredentialStoreFile(t, cfg.Env)
	defer cfg.CleanupFunc()

	if cfg.DisplayURL != sub.URL {
		t.Fatalf("expected DisplayURL to stay clean, got %q", cfg.DisplayURL)
	}
	if cfg.RemoteURL != sub.URL {
		t.Fatalf("expected RemoteURL to stay clean, got %q", cfg.RemoteURL)
	}
	if strings.Contains(cfg.DisplayURL, "ghp_test_token") || strings.Contains(cfg.RemoteURL, "ghp_test_token") {
		t.Fatalf("git URLs should not leak token, display=%q remote=%q", cfg.DisplayURL, cfg.RemoteURL)
	}
	assertFileMode(t, credentialPath, 0o600)

	for _, entry := range cfg.Env {
		if strings.HasPrefix(entry, "GIT_HTTP_EXTRA_HEADER=") {
			t.Fatalf("token auth should not rely on GIT_HTTP_EXTRA_HEADER, got %q", entry)
		}
		if strings.Contains(entry, "ghp_test_token") {
			t.Fatalf("token auth should not put token in env value, got %q", entry)
		}
	}
}

func TestBuildGitAuthConfigCredentialStoreUsesCustomUsername(t *testing.T) {
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
	defer cfg.CleanupFunc()

	if cfg.RemoteURL != sub.URL {
		t.Fatalf("expected RemoteURL to stay clean, got %q", cfg.RemoteURL)
	}
	if strings.Contains(cfg.RemoteURL, "secret token+/?") {
		t.Fatalf("RemoteURL should not contain token, got %q", cfg.RemoteURL)
	}
}

func TestBuildGitAuthConfigCleansCredentialBearingRemoteURL(t *testing.T) {
	testutil.SetupTestEnv(t)

	remoteURL := "https://demo-user:secret-token@github.com/example/private.git"
	cfg, err := buildGitAuthConfig(nil, remoteURL, &model.Subscription{URL: remoteURL}, "")
	if err != nil {
		t.Fatalf("build git auth config: %v", err)
	}

	want := "https://github.com/example/private.git"
	if cfg.RemoteURL != want || cfg.DisplayURL != want {
		t.Fatalf("expected clean git URLs, remote=%q display=%q", cfg.RemoteURL, cfg.DisplayURL)
	}
}

func TestSyncGitRemoteWithCallbackStoresCleanRemoteURL(t *testing.T) {
	testutil.SetupTestEnv(t)
	repoDir := t.TempDir()
	runGitForTest(t, repoDir, "init")

	cleanURL := "https://github.com/example/private.git"
	credentialURL := "https://demo-user:secret-token@github.com/example/private.git"
	if _, err := syncGitRemoteWithCallback(context.Background(), repoDir, cleanURL, os.Environ(), func(string) {}); err != nil {
		t.Fatalf("sync clean remote: %v", err)
	}
	if _, err := syncGitRemoteWithCallback(context.Background(), repoDir, credentialURL, os.Environ(), func(string) {}); err != nil {
		t.Fatalf("sync credential-bearing remote set-url: %v", err)
	}

	stored := strings.TrimSpace(runGitForTest(t, repoDir, "config", "--get", "remote.origin.url"))
	if stored != cleanURL {
		t.Fatalf("expected clean stored remote URL, got %q", stored)
	}
	if strings.Contains(stored, "secret-token") || stored == credentialURL {
		t.Fatalf("stored remote URL contains credentials: %q", stored)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func assertGitCredentialStoreFile(t *testing.T, env []string) string {
	t.Helper()
	const prefix = "GIT_CONFIG_VALUE_3=store --file "
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			path := strings.TrimPrefix(entry, prefix)
			if path == "" {
				t.Fatalf("expected credential store path")
			}
			return path
		}
	}
	t.Fatalf("expected credential store helper in env, got %v", env)
	return ""
}

func assertFileMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != expected {
		t.Fatalf("file mode=%#o want %#o", got, expected)
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

func TestGitAuthTypeForSSHKeyPath(t *testing.T) {
	if got := gitAuthTypeForSSHKeyPath("/tmp/key"); got != model.SubAuthTypeSSH {
		t.Fatalf("auth type=%q want=%q", got, model.SubAuthTypeSSH)
	}
	if got := gitAuthTypeForSSHKeyPath("   "); got != "" {
		t.Fatalf("empty key auth type=%q want empty", got)
	}
}
