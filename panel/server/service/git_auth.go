package service

import (
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
	env := AppendProxyEnv(baseEnv)
	cleanup := func() {}
	remoteURL = strings.TrimSpace(remoteURL)
	displayURL := remoteURL
	authType := ""
	if sub != nil {
		authType = sub.EffectiveAuthType()
	}

	switch authType {
	case model.SubAuthTypeSSH:
		sshKeyPath = strings.TrimSpace(sshKeyPath)
		if sshKeyPath == "" {
			return gitAuthConfig{}, fmt.Errorf("已配置 SSH 鉴权，但未找到可用 SSH 密钥")
		}

		knownHostsPath, err := ensureGitKnownHostsFile()
		if err != nil {
			return gitAuthConfig{}, err
		}
		env = append(env, "GIT_SSH_COMMAND="+buildGitSSHCommand(sshKeyPath, knownHostsPath))
	case model.SubAuthTypeToken:
		if sub == nil || strings.TrimSpace(sub.AuthToken) == "" {
			return gitAuthConfig{}, fmt.Errorf("已配置 Token 鉴权，但访问令牌为空")
		}
		if !isHTTPGitRemoteURL(remoteURL) {
			return gitAuthConfig{}, fmt.Errorf("Token 鉴权仅支持 HTTP/HTTPS 仓库地址，请改用 HTTPS 地址")
		}
		embedded, err := injectGitTokenIntoURL(remoteURL, sub.AuthUsername, sub.AuthToken)
		if err != nil {
			return gitAuthConfig{}, err
		}
		remoteURL = embedded
	}

	return gitAuthConfig{
		Env:         env,
		RemoteURL:   remoteURL,
		DisplayURL:  displayURL,
		CleanupFunc: cleanup,
	}, nil
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
		"ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=%s",
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

func injectGitTokenIntoURL(remoteURL, username, token string) (string, error) {
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
	return parsed.String(), nil
}
