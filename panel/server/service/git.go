package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"daidai-panel/config"
	"daidai-panel/model"
)

func GitClone(url, branch, destDir string, sshKeyPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, url, destDir)

	authCfg, err := buildGitAuthConfig(os.Environ(), url, &model.Subscription{
		URL:      url,
		AuthType: gitAuthTypeForSSHKeyPath(sshKeyPath),
	}, sshKeyPath)
	if err != nil {
		return "", err
	}
	defer authCfg.CleanupFunc()

	if strings.TrimSpace(authCfg.RemoteURL) != "" {
		args[len(args)-2] = authCfg.RemoteURL
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = config.C.Data.ScriptsDir
	cmd.Env = authCfg.Env
	output, err := cmd.CombinedOutput()
	return string(output), normalizeGitProviderError(string(output), err)
}

func GitPull(repoDir string, sshKeyPath string) (string, error) {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoDir
	remoteURL := resolveRepoRemoteURL(repoDir)

	authCfg, err := buildGitAuthConfig(os.Environ(), remoteURL, &model.Subscription{URL: remoteURL, AuthType: gitAuthTypeForSSHKeyPath(sshKeyPath)}, sshKeyPath)
	if err != nil {
		return "", err
	}
	defer authCfg.CleanupFunc()
	cmd.Env = authCfg.Env

	output, err := cmd.CombinedOutput()
	return string(output), normalizeGitProviderError(string(output), err)
}

func gitAuthTypeForSSHKeyPath(sshKeyPath string) string {
	if strings.TrimSpace(sshKeyPath) != "" {
		return model.SubAuthTypeSSH
	}
	return ""
}

func normalizeGitProviderError(output string, err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "remote host identification has changed") || strings.Contains(lower, "host key verification failed") {
		return fmt.Errorf("SSH Host Key 已改变或无法验证，Git 操作已停止；请确认新的主机指纹后更新 known_hosts: %w", err)
	}
	return err
}

func GitReset(repoDir string) (string, error) {
	cmd := exec.Command("git", "reset", "--hard")
	cmd.Dir = repoDir
	remoteURL := resolveRepoRemoteURL(repoDir)
	authCfg, err := buildGitAuthConfig(os.Environ(), remoteURL, &model.Subscription{URL: remoteURL}, "")
	if err != nil {
		return "", err
	}
	defer authCfg.CleanupFunc()
	cmd.Env = authCfg.Env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}

	cmd2 := exec.Command("git", "clean", "-fd")
	cmd2.Dir = repoDir
	cmd2.Env = authCfg.Env
	output2, err2 := cmd2.CombinedOutput()
	return string(output) + "\n" + string(output2), err2
}

func resolveRepoRemoteURL(repoDir string) string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func IsGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	return err == nil && info.IsDir()
}

func DownloadFile(url, destPath string) (string, error) {
	return DownloadFileWithContext(context.Background(), url, destPath)
}

func DownloadFileWithContext(ctx context.Context, url, destPath string) (string, error) {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	args := []string{"-fsSL", "-o", destPath, url}
	cmd := exec.CommandContext(ctx, "curl", args...)
	cmd.Env = AppendProxyEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	if err != nil {
		args = []string{"-q", "-O", destPath, url}
		cmd = exec.CommandContext(ctx, "wget", args...)
		cmd.Env = AppendProxyEnv(os.Environ())
		output, err = cmd.CombinedOutput()
	}
	return string(output), err
}

func ListRepoFiles(repoDir string) ([]string, error) {
	var files []string
	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(repoDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		files = append(files, rel)
		return nil
	})
	return files, err
}
