package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"daidai-panel/config"
	"daidai-panel/model"
)

type gitAuthConfig struct {
	Env         []string
	RemoteURL   string
	DisplayURL  string
	CleanupFunc func()
}

func buildGitAuthConfig(baseEnv []string, remoteURL string, sub *model.Subscription, sshKeyPath string) (gitAuthConfig, error) {
	return buildGitAuthConfigWithContext(context.Background(), baseEnv, remoteURL, sub, sshKeyPath)
}

func buildGitAuthConfigWithContext(ctx context.Context, baseEnv []string, remoteURL string, sub *model.Subscription, sshKeyPath string) (gitAuthConfig, error) {
	env := ApplyGitSSHRuntimePolicy(AppendProxyEnv(baseEnv))
	cleanup := func() {}
	remoteURL = strings.TrimSpace(remoteURL)
	cleanURL, err := cleanGitRemoteURL(remoteURL)
	if err != nil {
		return gitAuthConfig{}, err
	}
	remoteURL = cleanURL
	displayURL := cleanURL
	authType := ""
	if sub != nil {
		authType = sub.EffectiveAuthType()
		if caPath := strings.TrimSpace(sub.CACertPath); caPath != "" {
			env = appendOrReplaceEnv(env, "GIT_SSL_CAINFO", caPath)
		}
	}

	switch authType {
	case model.SubAuthTypeSSH:
		sshKeyPath = strings.TrimSpace(sshKeyPath)
		if sshKeyPath == "" {
			return gitAuthConfig{}, fmt.Errorf("已配置 SSH 鉴权，但未找到可用 SSH 密钥")
		}
		if err := validateGitSSHKeyPath(sshKeyPath); err != nil {
			return gitAuthConfig{}, err
		}

		knownHostsPath, err := ensureGitKnownHostsFile()
		if err != nil {
			return gitAuthConfig{}, err
		}
		env = append(env, "GIT_SSH_COMMAND="+buildGitSSHCommand(sshKeyPath, knownHostsPath))
	case model.SubAuthTypeToken:
		token, err := openSubscriptionGitToken(ctx, sub)
		if err != nil {
			return gitAuthConfig{}, err
		}
		if strings.TrimSpace(token) == "" {
			return gitAuthConfig{}, fmt.Errorf("已配置 Token 鉴权，但访问令牌为空")
		}
		if !isHTTPGitRemoteURL(remoteURL) {
			return gitAuthConfig{}, fmt.Errorf("Token 鉴权仅支持 HTTP/HTTPS 仓库地址，请改用 HTTPS 地址")
		}
		credentialPath, err := writeTempGitCredentialStore(remoteURL, sub.AuthUsername, token)
		if err != nil {
			return gitAuthConfig{}, err
		}
		cleanup = func() { _ = os.Remove(credentialPath) }
		env = appendOrReplaceEnv(env, "GIT_CONFIG_VALUE_3", "store --file "+credentialPath)
	}

	return gitAuthConfig{
		Env:         env,
		RemoteURL:   remoteURL,
		DisplayURL:  displayURL,
		CleanupFunc: cleanup,
	}, nil
}

func validateGitSSHKeyPath(sshKeyPath string) error {
	if strings.Contains(sshKeyPath, "BEGIN ") && strings.Contains(sshKeyPath, "PRIVATE KEY") {
		return fmt.Errorf("SSH 密钥参数必须是私钥文件路径，不能直接传入私钥内容")
	}
	info, err := os.Stat(sshKeyPath)
	if err != nil {
		return fmt.Errorf("读取 SSH 密钥文件失败: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("SSH 密钥路径指向目录: %s", sshKeyPath)
	}
	return nil
}

func openSubscriptionGitToken(ctx context.Context, sub *model.Subscription) (string, error) {
	if sub == nil {
		return "", nil
	}
	if sub.HasAuthTokenSecret() {
		opened, err := RuntimeSecretStoreInstance().Open(ctx, fmt.Sprintf("subscription:%d:git-token", sub.ID), SealedValue{
			Provider: sub.AuthTokenSecretProvider,
			Cipher:   append([]byte{}, sub.AuthTokenSecretCipher...),
		})
		if err != nil {
			return "", fmt.Errorf("打开 Git Token SecretStore 凭据失败: %w", err)
		}
		return string(opened), nil
	}
	return sub.AuthToken, nil
}

func SealSubscriptionAuthToken(ctx context.Context, sub *model.Subscription, token string) error {
	if sub == nil {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		sub.AuthToken = ""
		sub.AuthTokenSecretProvider = ""
		sub.AuthTokenSecretCipher = nil
		return nil
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(ctx, fmt.Sprintf("subscription:%d:git-token", sub.ID), []byte(token))
	if err != nil {
		return fmt.Errorf("封存 Git Token SecretStore 凭据失败: %w", err)
	}
	sub.AuthToken = ""
	sub.AuthTokenSecretProvider = sealed.Provider
	sub.AuthTokenSecretCipher = append([]byte{}, sealed.Cipher...)
	return nil
}

func ClearSubscriptionAuthSecrets(sub *model.Subscription) {
	if sub == nil {
		return
	}
	sub.AuthToken = ""
	sub.AuthTokenSecretProvider = ""
	sub.AuthTokenSecretCipher = nil
	sub.SSHKeySecretProvider = ""
	sub.SSHKeySecretCipher = nil
}

func writeSubscriptionSSHKeySecret(ctx context.Context, sub *model.Subscription) (string, error) {
	if sub == nil || !sub.HasSSHKeySecret() {
		return "", nil
	}
	opened, err := RuntimeSecretStoreInstance().Open(ctx, fmt.Sprintf("subscription:%d:ssh-key", sub.ID), SealedValue{
		Provider: sub.SSHKeySecretProvider,
		Cipher:   append([]byte{}, sub.SSHKeySecretCipher...),
	})
	if err != nil {
		return "", fmt.Errorf("打开 Git SSH Key SecretStore 凭据失败: %w", err)
	}
	return writeTempSSHKey(string(opened))
}

func OpenSSHKeyPrivateKey(ctx context.Context, key *model.SSHKey) (string, error) {
	if key == nil {
		return "", nil
	}
	if key.HasPrivateKeySecret() {
		opened, err := RuntimeSecretStoreInstance().Open(ctx, fmt.Sprintf("ssh-key:%d:private-key", key.ID), SealedValue{
			Provider: key.PrivateKeySecretProvider,
			Cipher:   append([]byte{}, key.PrivateKeySecretCipher...),
		})
		if err != nil {
			return "", fmt.Errorf("打开 SSH Key SecretStore 凭据失败: %w", err)
		}
		return string(opened), nil
	}
	return key.PrivateKey, nil
}

func SealSSHKeyPrivateKey(ctx context.Context, key *model.SSHKey, privateKey string) error {
	if key == nil {
		return nil
	}
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return nil
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(ctx, fmt.Sprintf("ssh-key:%d:private-key", key.ID), []byte(privateKey))
	if err != nil {
		return fmt.Errorf("封存 SSH Key SecretStore 凭据失败: %w", err)
	}
	key.PrivateKey = ""
	key.PrivateKeySecretProvider = sealed.Provider
	key.PrivateKeySecretCipher = append([]byte{}, sealed.Cipher...)
	return nil
}

func ensureGitKnownHostsFile() (string, error) {
	if config.C == nil {
		return "", fmt.Errorf("配置未初始化")
	}

	sshDir := filepath.Join(config.C.Data.Dir, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return "", fmt.Errorf("创建 SSH 配置目录失败: %w", err)
	}

	knownHostsPath := filepath.Join(sshDir, "known_hosts")
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		if err := os.WriteFile(knownHostsPath, []byte{}, 0o600); err != nil {
			return "", fmt.Errorf("创建 known_hosts 失败: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("读取 known_hosts 失败: %w", err)
	}

	return knownHostsPath, nil
}

func buildGitSSHCommand(sshKeyPath, knownHostsPath string) string {
	return fmt.Sprintf(
		"ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=%s -o GlobalKnownHostsFile=/dev/null",
		shellEscapeSSHArg(sshKeyPath),
		shellEscapeSSHArg(knownHostsPath),
	)
}

func shellEscapeSSHArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isHTTPGitRemoteURL(remoteURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(remoteURL))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func cleanGitRemoteURL(remoteURL string) (string, error) {
	if !isHTTPGitRemoteURL(remoteURL) {
		return remoteURL, nil
	}
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("解析仓库 URL 失败: %w", err)
	}
	parsed.User = nil
	return parsed.String(), nil
}

func writeTempGitCredentialStore(remoteURL, username, token string) (string, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("解析仓库 URL 失败: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("Token 鉴权仅支持 HTTP/HTTPS 仓库地址")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = "x-access-token"
	}
	parsed.User = url.UserPassword(username, strings.TrimSpace(token))

	tmpFile, err := os.CreateTemp("", "git_credentials_*")
	if err != nil {
		return "", fmt.Errorf("创建 Git 凭据临时文件失败: %w", err)
	}
	path := tmpFile.Name()
	if err := os.Chmod(path, 0o600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("设置 Git 凭据临时文件权限失败: %w", err)
	}

	if _, err := tmpFile.WriteString(parsed.String() + "\n"); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("写入 Git 凭据临时文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("关闭 Git 凭据临时文件失败: %w", err)
	}
	return path, nil
}
