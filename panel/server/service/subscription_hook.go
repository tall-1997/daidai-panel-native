package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
)

const (
	subscriptionHookTimeoutSeconds = 900
	subscriptionHookTrustVersion   = "v1"
	subscriptionHookCapability     = "subscription-hook"
)

var ErrSubscriptionHookUnauthorized = errors.New("subscription hook is not authorized")

type subscriptionHookTrustMetadata struct {
	Source     string
	Version    string
	SHA256     string
	Capability string
}

func runSubscriptionHookIfConfigured(ctx context.Context, sub *model.Subscription, emit PullCallback) error {
	hookScript := normalizeSubscriptionHookScript(sub)
	if hookScript == "" {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return fmt.Errorf("拉取已停止")
	}

	workDir := subscriptionWorkingDir(sub)
	if _, err := os.Stat(workDir); err != nil {
		workDir = config.C.Data.ScriptsDir
	}
	return runSubscriptionHookInWorkDir(ctx, sub, workDir, emit)
}

func runSubscriptionHookInWorkDir(ctx context.Context, sub *model.Subscription, workDir string, emit PullCallback) error {
	if ctx == nil {
		ctx = context.Background()
	}
	hookScript := normalizeSubscriptionHookScript(sub)
	if hookScript == "" {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return fmt.Errorf("拉取已停止")
	}
	metadata := buildSubscriptionHookTrustMetadata(sub, hookScript)
	if !RuntimeTrustAuthorizer().IsAuthorized(metadata.Source, metadata.Version, metadata.SHA256, metadata.Capability) {
		return fmt.Errorf("%w: source=%s version=%s sha256=%s capability=%s", ErrSubscriptionHookUnauthorized, metadata.Source, metadata.Version, metadata.SHA256, metadata.Capability)
	}
	emit("[执行订阅钩子]")
	err := runSubscriptionHookScript(ctx, hookScript, workDir, buildSubscriptionHookEnv(sub, workDir), func(line string) {
		emit("[hook] " + line)
	})
	if err != nil {
		return fmt.Errorf("执行订阅钩子失败: %w", err)
	}
	if ctx != nil && ctx.Err() != nil {
		return fmt.Errorf("拉取已停止")
	}

	emit("[订阅钩子完成]")
	return nil
}

func buildSubscriptionHookTrustMetadata(sub *model.Subscription, hookScript string) subscriptionHookTrustMetadata {
	sum := sha256.Sum256([]byte(hookScript))
	return subscriptionHookTrustMetadata{
		Source:     subscriptionHookTrustSource(sub),
		Version:    subscriptionHookTrustVersion,
		SHA256:     hex.EncodeToString(sum[:]),
		Capability: subscriptionHookCapability,
	}
}

func subscriptionHookTrustSource(sub *model.Subscription) string {
	if sub == nil {
		return "subscription-hook:unknown"
	}
	parts := []string{
		"subscription-hook",
		strings.TrimSpace(sub.Type),
		strings.TrimSpace(sub.URL),
		strings.TrimSpace(sub.Branch),
		subscriptionSaveDir(sub),
	}
	return strings.Join(parts, ":")
}

func AuthorizeSubscriptionHookForTest(sub *model.Subscription) {
	hookScript := normalizeSubscriptionHookScript(sub)
	if hookScript == "" {
		return
	}
	metadata := buildSubscriptionHookTrustMetadata(sub, hookScript)
	SeedRuntimeTrustRecord(metadata.Source, metadata.Version, metadata.SHA256, []string{metadata.Capability})
}

func runSubscriptionHookScript(ctx context.Context, content, workDir string, envVars map[string]string, onOutput OnOutputFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tmpFile := filepath.Join(workDir, fmt.Sprintf(".hook_%d.sh", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, NormalizeShellLineEndings([]byte(content)), 0o755); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	cmd, cleanup, err := CreateManagedCommand("bash", tmpFile, nil, workDir, envVars)
	if err != nil {
		return err
	}
	defer cleanup()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReaderSize(stdout, 256*1024)
	go func() {
		for {
			chunk, err := reader.ReadString('\n')
			if len(chunk) > 0 && onOutput != nil {
				onOutput(chunk)
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(subscriptionHookTimeoutSeconds * time.Second)
	defer timer.Stop()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case err := <-waitCh:
		return err
	case <-timer.C:
		KillProcessGroup(cmd.Process)
		<-waitCh
		return fmt.Errorf("钩子脚本超时，已超过 %d 秒", subscriptionHookTimeoutSeconds)
	case <-ctx.Done():
		KillProcessGroup(cmd.Process)
		<-waitCh
		return fmt.Errorf("拉取已停止")
	}
}

func buildSubscriptionHookEnv(sub *model.Subscription, workDir string) map[string]string {
	dataDir := strings.TrimSpace(config.C.Data.Dir)
	qlDir := dataDir
	if strings.EqualFold(filepath.Base(dataDir), "data") {
		qlDir = filepath.Dir(dataDir)
	}

	return map[string]string{
		"SUB_ID":            strconv.FormatUint(uint64(sub.ID), 10),
		"SUB_NAME":          sub.Name,
		"SUB_TYPE":          sub.Type,
		"SUB_URL":           sub.URL,
		"SUB_BRANCH":        sub.Branch,
		"SUB_SAVE_DIR":      subscriptionSaveDir(sub),
		"SUB_DIR":           workDir,
		"SUB_WORK_DIR":      workDir,
		"SCRIPTS_DIR":       config.C.Data.ScriptsDir,
		"PANEL_DATA_DIR":    dataDir,
		"PANEL_SCRIPTS_DIR": config.C.Data.ScriptsDir,
		"QL_DIR":            qlDir,
	}
}

func normalizeSubscriptionHookScript(sub *model.Subscription) string {
	hookScript := strings.TrimSpace(sub.HookScript)
	if hookScript == "" {
		return ""
	}

	repoKey := deriveSubscriptionRepoKey(sub.URL)
	if repoKey == "" {
		return hookScript
	}

	replacements := []string{
		"$QL_DIR/data/repo/" + repoKey,
		"${QL_DIR}/data/repo/" + repoKey,
		"$QL_DIR/data/scripts/" + repoKey,
		"${QL_DIR}/data/scripts/" + repoKey,
		"%QL_DIR%\\data\\repo\\" + repoKey,
		"%QL_DIR%\\data\\scripts\\" + repoKey,
	}

	normalized := hookScript
	for _, from := range replacements {
		normalized = strings.ReplaceAll(normalized, from, "$SUB_DIR")
	}
	return normalized
}

func deriveSubscriptionRepoKey(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return ""
	}

	trimmed = strings.TrimSuffix(trimmed, ".git")
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 {
		owner := strings.TrimSpace(parts[len(parts)-2])
		repo := strings.TrimSpace(parts[len(parts)-1])
		if owner != "" && repo != "" {
			return owner + "_" + repo
		}
	}

	if len(parts) > 0 {
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return ""
}

func subscriptionWorkingDir(sub *model.Subscription) string {
	saveDir := subscriptionSaveDir(sub)
	if sub.Type == model.SubTypeSingleFile && saveDir == "" {
		saveDir = "downloads"
	}
	if saveDir == "" {
		return config.C.Data.ScriptsDir
	}
	return filepath.Join(config.C.Data.ScriptsDir, saveDir)
}
