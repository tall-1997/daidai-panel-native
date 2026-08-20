package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/cron"

	"gorm.io/gorm"
)

type PullCallback func(line string)

type subscriptionWorktreeJournal struct {
	DestDir    string `json:"dest_dir"`
	BackupDir  string `json:"backup_dir"`
	StagingDir string `json:"staging_dir"`
	Phase      string `json:"phase"`
}

func PullSubscription(sub *model.Subscription) (string, error) {
	return PullSubscriptionWithCallback(sub, nil)
}

func PullSubscriptionWithCallback(sub *model.Subscription, onOutput PullCallback) (string, error) {
	return PullSubscriptionWithContext(context.Background(), sub, onOutput)
}

func PullSubscriptionWithContext(ctx context.Context, sub *model.Subscription, onOutput PullCallback) (string, error) {
	return pullSubscriptionWithContext(ctx, sub, onOutput, "")
}

func pullSubscriptionWithContext(ctx context.Context, sub *model.Subscription, onOutput PullCallback, operationID string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	startTime := time.Now()
	store := DefaultOperationStore()
	if operationID != "" {
		_ = store.Start(operationID, "pulling")
	}

	sshKeyPath, cleanupSSHKey, err := resolveSubscriptionSSHKeyPath(ctx, sub)
	if err != nil {
		return "", err
	}
	defer cleanupSSHKey()

	authCfg, err := buildGitAuthConfigWithContext(ctx, os.Environ(), sub.URL, sub, sshKeyPath)
	if err != nil {
		return "", err
	}
	defer authCfg.CleanupFunc()

	var fullLog strings.Builder
	lineCursor := int64(0)
	subLog := model.SubLog{
		SubscriptionID: sub.ID,
		OperationID:    operationID,
		Status:         1,
		Content:        "",
		LogCursor:      0,
	}
	logPersisted := database.DB.Create(&subLog).Error == nil
	emit := func(line string) {
		lineCursor++
		fullLog.WriteString(line)
		fullLog.WriteString("\n")
		if logPersisted {
			updates := map[string]interface{}{"content": fullLog.String(), "log_cursor": lineCursor}
			if lineCursor == 1 || lineCursor%5 == 0 {
				_ = database.DB.Model(&model.SubLog{}).Where("id = ?", subLog.ID).Updates(updates).Error
			}
		}
		if operationID != "" && lineCursor%5 == 0 {
			_ = store.Progress(operationID, "pulling", 50, lineCursor)
		}
		if onOutput != nil {
			onOutput(line)
		}
	}

	emit(fmt.Sprintf("[开始拉取] %s (%s)", sub.Name, sub.Type))
	if operationID != "" {
		emit(fmt.Sprintf("[Operation] %s", operationID))
	}
	applySubscriptionForceOverwriteSetting(sub)
	hookHandledByPull := sub.Type == model.SubTypeGitRepo && (sub.ForceOverwrite == nil || *sub.ForceOverwrite)

	var output string
	var pullErr error

	pullErr = runSubscriptionPreScriptWithContext(ctx, sub, emit)
	if pullErr == nil && ctx.Err() != nil {
		pullErr = fmt.Errorf("拉取已停止")
	}

	if pullErr == nil {
		switch sub.Type {
		case model.SubTypeSingleFile:
			output, pullErr = pullSingleFileWithCallback(ctx, sub, sshKeyPath, emit)
		default:
			output, pullErr = pullGitRepoWithCallback(ctx, sub, authCfg, emit)
		}
	}

	if pullErr == nil && ctx.Err() != nil {
		pullErr = fmt.Errorf("拉取已停止")
	}
	if pullErr == nil && !hookHandledByPull {
		pullErr = runSubscriptionHookIfConfigured(ctx, sub, emit)
	}
	if pullErr == nil && ctx.Err() != nil {
		pullErr = fmt.Errorf("拉取已停止")
	}
	if pullErr == nil {
		syncSubscriptionTasks(sub, emit)
	}

	duration := time.Since(startTime).Seconds()

	status := 0
	if pullErr != nil {
		status = 1
		emit(fmt.Sprintf("[错误] %s", pullErr.Error()))
	}

	emit(fmt.Sprintf("[完成] 耗时 %.2f 秒, 状态: %s", duration, map[int]string{0: "成功", 1: "失败"}[status]))

	if logPersisted {
		database.DB.Model(&model.SubLog{}).Where("id = ?", subLog.ID).Updates(map[string]interface{}{
			"status":     status,
			"content":    fullLog.String(),
			"duration":   duration,
			"log_cursor": lineCursor,
		})
	} else {
		database.DB.Create(&model.SubLog{
			SubscriptionID: sub.ID,
			OperationID:    operationID,
			Status:         status,
			Content:        fullLog.String(),
			Duration:       duration,
			LogCursor:      lineCursor,
		})
	}

	now := time.Now()
	database.DB.Model(sub).Updates(map[string]interface{}{
		"last_pull_at": &now,
		"status":       status,
	})

	if operationID != "" {
		if pullErr == nil {
			_ = store.Finish(operationID, 0, lineCursor)
		} else if ctx.Err() != nil {
			_ = store.Cancel(operationID, "stopped", lineCursor)
		} else {
			exitCode := 1
			_ = store.Fail(operationID, &exitCode, "pull_failed", lineCursor)
		}
	}

	return output, pullErr
}

func resolveSubscriptionSSHKeyPath(ctx context.Context, sub *model.Subscription) (string, func(), error) {
	cleanup := func() {}
	if sub == nil || sub.EffectiveAuthType() != model.SubAuthTypeSSH {
		return "", cleanup, nil
	}

	if sub.HasSSHKeySecret() {
		path, err := writeSubscriptionSSHKeySecret(ctx, sub)
		if err != nil {
			return "", cleanup, err
		}
		return path, func() { _ = os.Remove(path) }, nil
	}

	if sub.SSHKeyID == nil {
		return "", cleanup, nil
	}
	var sshKey model.SSHKey
	if err := database.DB.First(&sshKey, *sub.SSHKeyID).Error; err != nil {
		return "", cleanup, nil
	}
	privateKey, err := OpenSSHKeyPrivateKey(ctx, &sshKey)
	if err != nil {
		return "", cleanup, err
	}
	tmpFile, err := writeTempSSHKey(privateKey)
	if err != nil {
		return "", cleanup, fmt.Errorf("写入 SSH 密钥失败: %w", err)
	}
	return tmpFile, func() { _ = os.Remove(tmpFile) }, nil
}

func applySubscriptionForceOverwriteSetting(sub *model.Subscription) {
	if sub == nil || sub.Type != model.SubTypeGitRepo {
		return
	}
	overwrite := isConfigEnabled("subscription_force_overwrite", true)
	sub.ForceOverwrite = &overwrite
}

func runCmdWithCallback(ctx context.Context, cmd *exec.Cmd, emit PullCallback) (string, error) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var buf strings.Builder
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteString("\n")
		emit(line)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		if ctx != nil && ctx.Err() != nil {
			return buf.String(), fmt.Errorf("拉取已停止")
		}
		return buf.String(), scanErr
	}

	err = cmd.Wait()
	if ctx != nil && ctx.Err() != nil {
		return buf.String(), fmt.Errorf("拉取已停止")
	}
	if err != nil {
		if hint := classifyGitFailure(buf.String(), err); hint != "" {
			emit(hint)
		}
	}
	return buf.String(), err
}

func gitHasWorkingTreeChanges(ctx context.Context, repoDir string, env []string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = repoDir
	cmd.Env = env

	output, err := cmd.Output()
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return false, fmt.Errorf("拉取已停止")
		}
		return false, wrapGitCommandError("检查本地改动", gitCommandStderr(err), err)
	}

	return strings.TrimSpace(string(output)) != "", nil
}

func pullGitRepoWithCallback(ctx context.Context, sub *model.Subscription, authCfg gitAuthConfig, emit PullCallback) (string, error) {
	saveDir := sub.SaveDir
	if saveDir == "" {
		saveDir = sub.Alias
		if saveDir == "" {
			parts := strings.Split(sub.URL, "/")
			saveDir = strings.TrimSuffix(parts[len(parts)-1], ".git")
		}
	}

	destDir := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	if absDestDir, err := filepath.Abs(destDir); err == nil {
		destDir = absDestDir
	}
	env := authCfg.Env

	forceOverwrite := sub.ForceOverwrite == nil || *sub.ForceOverwrite
	if IsGitRepo(destDir) && forceOverwrite {
		return replaceSubscriptionGitWorktree(ctx, sub, authCfg, destDir, saveDir, env, emit)
	}

	if IsGitRepo(destDir) {
		var fullOutput strings.Builder
		branchLabel := "默认分支"
		if strings.TrimSpace(sub.Branch) != "" {
			branchLabel = strings.TrimSpace(sub.Branch)
		}

		emit(fmt.Sprintf("[检测到已有仓库] %s 已存在 Git 仓库，接下来会同步远端并覆盖更新本地文件", saveDir))
		emit(fmt.Sprintf("[同步远端地址] 正在校正订阅地址 -> %s", authCfg.DisplayURL))
		output, err := syncGitRemoteWithCallback(ctx, destDir, authCfg.RemoteURL, env, emit)
		fullOutput.WriteString(output)
		if err != nil {
			return fullOutput.String(), err
		}

		fetchArgs := []string{"fetch", "--depth", "1", "--prune", "origin"}
		if strings.TrimSpace(sub.Branch) != "" {
			fetchArgs = append(fetchArgs, strings.TrimSpace(sub.Branch))
		}
		emit(fmt.Sprintf("[拉取远端更新] 正在获取分支 %s 的最新提交", branchLabel))
		cmd := exec.CommandContext(ctx, "git", fetchArgs...)
		cmd.Dir = destDir
		cmd.Env = env
		output, err = runCmdWithCallback(ctx, cmd, emit)
		fullOutput.WriteString(output)
		if err != nil {
			return fullOutput.String(), err
		}

		if err := applySparseCheckout(ctx, destDir, sub, env, emit); err != nil {
			return fullOutput.String(), err
		}

		if forceOverwrite {
			emit("[覆盖更新本地文件] 正在用远端最新提交覆盖当前订阅目录中的仓库内容")
			cmd = exec.CommandContext(ctx, "git", "reset", "--hard", "FETCH_HEAD")
			cmd.Dir = destDir
			cmd.Env = env
			output, err = runCmdWithCallback(ctx, cmd, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}
			emit("[已完成] 已覆盖更新所有仓库文件，本地新增的文件已保留")
		} else {
			emit("[保留本地修改] 正在合并远端更新（保留本地修改的文件）")
			hasStash, err := gitHasWorkingTreeChanges(ctx, destDir, env)
			if err != nil {
				return fullOutput.String(), err
			}
			if hasStash {
				cmd = exec.CommandContext(ctx, "git", "stash", "push", "--include-untracked", "-m", "daidai-panel-subscription-update")
				cmd.Dir = destDir
				cmd.Env = env
				output, err = runCmdWithCallback(ctx, cmd, emit)
				fullOutput.WriteString(output)
				if err != nil {
					return fullOutput.String(), err
				}
			} else {
				emit("[保留本地修改] 未检测到本地改动，跳过暂存恢复")
			}

			cmd = exec.CommandContext(ctx, "git", "reset", "--hard", "FETCH_HEAD")
			cmd.Dir = destDir
			cmd.Env = env
			output, err = runCmdWithCallback(ctx, cmd, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}

			if hasStash {
				emit("[恢复本地修改] 正在恢复之前暂存的本地修改")
				cmd = exec.CommandContext(ctx, "git", "stash", "pop")
				cmd.Dir = destDir
				cmd.Env = env
				output, err = runCmdWithCallback(ctx, cmd, emit)
				fullOutput.WriteString(output)
				if err != nil {
					emit("[提示] 本地修改与远端更新存在冲突，请手动处理")
				}
			}
		}
		return fullOutput.String(), err
	}

	if destInfo, err := os.Stat(destDir); err == nil {
		if !destInfo.IsDir() {
			return "", fmt.Errorf("保存目录已被文件占用: %s", saveDir)
		}

		entries, readErr := os.ReadDir(destDir)
		if readErr != nil {
			return "", fmt.Errorf("读取保存目录失败: %w", readErr)
		}
		if len(entries) > 0 && forceOverwrite {
			return replaceSubscriptionGitWorktree(ctx, sub, authCfg, destDir, saveDir, env, emit)
		}

		if len(entries) > 0 {
			var fullOutput strings.Builder
			branchLabel := "默认分支"
			if strings.TrimSpace(sub.Branch) != "" {
				branchLabel = strings.TrimSpace(sub.Branch)
			}

			emit(fmt.Sprintf("[检测到已存在脚本目录] %s 当前不是 Git 仓库，接下来会原地初始化仓库并覆盖本地文件", saveDir))
			emit("[git init] 正在初始化本地仓库")
			cmd := exec.CommandContext(ctx, "git", "init")
			cmd.Dir = destDir
			cmd.Env = env
			output, err := runCmdWithCallback(ctx, cmd, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}

			emit(fmt.Sprintf("[同步远端地址] 正在校正订阅地址 -> %s", authCfg.DisplayURL))
			output, err = syncGitRemoteWithCallback(ctx, destDir, authCfg.RemoteURL, env, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}

			fetchArgs := []string{"fetch", "--depth", "1", "--prune", "origin"}
			if strings.TrimSpace(sub.Branch) != "" {
				fetchArgs = append(fetchArgs, strings.TrimSpace(sub.Branch))
			}
			emit(fmt.Sprintf("[拉取远端更新] 正在获取分支 %s 的最新提交", branchLabel))
			cmd = exec.CommandContext(ctx, "git", fetchArgs...)
			cmd.Dir = destDir
			cmd.Env = env
			output, err = runCmdWithCallback(ctx, cmd, emit)
			if err != nil {
				fullOutput.WriteString(output)
				return fullOutput.String(), err
			}
			fullOutput.WriteString(output)
			if ctx.Err() != nil {
				return fullOutput.String(), fmt.Errorf("拉取已停止")
			}

			if err := applySparseCheckout(ctx, destDir, sub, env, emit); err != nil {
				return fullOutput.String(), err
			}

			emit("[覆盖更新本地文件] 正在用远端最新提交覆盖当前脚本目录内容")
			cmd = exec.CommandContext(ctx, "git", "reset", "--hard", "FETCH_HEAD")
			cmd.Dir = destDir
			cmd.Env = env
			output, err = runCmdWithCallback(ctx, cmd, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}

			emit("[清理多余文件] 正在移除原脚本目录中不属于远端仓库的旧文件")
			cmd = exec.CommandContext(ctx, "git", "clean", "-fd")
			cmd.Dir = destDir
			cmd.Env = env
			output, err = runCmdWithCallback(ctx, cmd, emit)
			fullOutput.WriteString(output)
			if err != nil {
				return fullOutput.String(), err
			}

			emit("[已完成] 已覆盖更新所有仓库文件，并清理原目录中的多余旧文件")
			return fullOutput.String(), nil
		}
	}

	return replaceSubscriptionGitWorktree(ctx, sub, authCfg, destDir, saveDir, env, emit)
}

func replaceSubscriptionGitWorktree(ctx context.Context, sub *model.Subscription, authCfg gitAuthConfig, destDir, saveDir string, env []string, emit PullCallback) (string, error) {
	parent := filepath.Dir(destDir)
	base := filepath.Base(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("创建订阅目录失败: %w", err)
	}

	stagingDir, err := os.MkdirTemp(parent, "."+base+".staging-*")
	if err != nil {
		return "", fmt.Errorf("创建订阅 staging 工作区失败: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	var fullOutput strings.Builder
	emit(fmt.Sprintf("[git clone] %s -> %s (staging)", authCfg.DisplayURL, saveDir))
	output, err := cloneSubscriptionGitWorktree(ctx, sub, authCfg, stagingDir, env, emit)
	fullOutput.WriteString(output)
	if err != nil {
		return fullOutput.String(), normalizeGitProviderError(output, err)
	}
	if err := runSubscriptionHookInWorkDir(ctx, sub, stagingDir, emit); err != nil {
		return fullOutput.String(), err
	}

	emit("[原子切换] 正在用新工作区替换当前订阅目录")
	if err := atomicReplaceSubscriptionWorktree(destDir, stagingDir); err != nil {
		return fullOutput.String(), err
	}
	stagingActive = false
	emit("[已完成] 已原子切换到新的健康订阅工作区")
	return fullOutput.String(), nil
}

func cloneSubscriptionGitWorktree(ctx context.Context, sub *model.Subscription, authCfg gitAuthConfig, destDir string, env []string, emit PullCallback) (string, error) {
	args := []string{"clone", "--depth", "1"}
	sparsePatterns, sparseWarnings := buildSubscriptionSparseCheckoutPatterns(sub)
	for _, warning := range sparseWarnings {
		emit(warning)
	}
	if len(sparsePatterns) > 0 {
		args = append(args, "--filter=blob:none", "--no-checkout")
	}
	if sub.Branch != "" {
		args = append(args, "-b", sub.Branch)
	}
	args = append(args, authCfg.RemoteURL, destDir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = config.C.Data.ScriptsDir
	cmd.Env = env
	output, err := runCmdWithCallback(ctx, cmd, emit)
	if err != nil {
		return output, normalizeGitProviderError(output, err)
	}
	if len(sparsePatterns) > 0 {
		var fullOutput strings.Builder
		fullOutput.WriteString(output)
		if spErr := applySparseCheckout(ctx, destDir, sub, env, emit); spErr != nil {
			return fullOutput.String(), spErr
		}
		emit("[checkout] 正在按子目录/白名单规则检出订阅文件")
		cmd = exec.CommandContext(ctx, "git", "checkout", "HEAD")
		cmd.Dir = destDir
		cmd.Env = env
		output, err = runCmdWithCallback(ctx, cmd, emit)
		fullOutput.WriteString(output)
		return fullOutput.String(), err
	}
	return output, nil
}

func atomicReplaceSubscriptionWorktree(destDir, stagingDir string) error {
	backupDir := filepath.Join(filepath.Dir(destDir), "."+filepath.Base(destDir)+".previous")
	journalPath := filepath.Join(filepath.Dir(destDir), "."+filepath.Base(destDir)+".replace-journal.json")
	if err := recoverSubscriptionWorktreeJournal(journalPath); err != nil {
		return err
	}
	backupActive := false
	_ = os.RemoveAll(backupDir)

	if _, err := os.Stat(destDir); err == nil {
		if err := writeSubscriptionWorktreeJournal(journalPath, subscriptionWorktreeJournal{DestDir: destDir, BackupDir: backupDir, StagingDir: stagingDir, Phase: "backup"}); err != nil {
			return err
		}
		if err := os.Rename(destDir, backupDir); err != nil {
			return fmt.Errorf("备份当前订阅工作区失败: %w", err)
		}
		backupActive = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查当前订阅工作区失败: %w", err)
	}

	if err := writeSubscriptionWorktreeJournal(journalPath, subscriptionWorktreeJournal{DestDir: destDir, BackupDir: backupDir, StagingDir: stagingDir, Phase: "publish"}); err != nil {
		return err
	}
	if err := os.Rename(stagingDir, destDir); err != nil {
		if backupActive {
			_ = os.Rename(backupDir, destDir)
		}
		return fmt.Errorf("发布新订阅工作区失败: %w", err)
	}

	_ = os.Remove(journalPath)
	_ = backupActive
	return nil
}

func writeSubscriptionWorktreeJournal(path string, journal subscriptionWorktreeJournal) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入订阅工作区 crash journal 失败: %w", err)
	}
	return nil
}

func recoverSubscriptionWorktreeJournal(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取订阅工作区 crash journal 失败: %w", err)
	}
	var journal subscriptionWorktreeJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("解析订阅工作区 crash journal 失败: %w", err)
	}
	if journal.DestDir == "" || journal.BackupDir == "" {
		return fmt.Errorf("订阅工作区 crash journal 缺少路径")
	}
	if _, err := os.Stat(journal.DestDir); os.IsNotExist(err) {
		if _, backupErr := os.Stat(journal.BackupDir); backupErr == nil {
			if restoreErr := os.Rename(journal.BackupDir, journal.DestDir); restoreErr != nil {
				return fmt.Errorf("恢复订阅工作区 crash journal 失败: %w", restoreErr)
			}
		}
	} else if err != nil {
		return fmt.Errorf("检查订阅工作区 crash journal 目标失败: %w", err)
	}
	_ = os.Remove(path)
	return nil
}

const subscriptionSparseUnsafeChars = "?[]\\"

// subscriptionSparseUnsafeCharsHint 是给用户看的可读版本（日志里直接打元字符会糊成一团）。
const subscriptionSparseUnsafeCharsHint = "? [ ] \\"

// splitSubscriptionSparseTargets 把过滤字段拆成两组：
// 能安全下发给 sparse-checkout 的模式，和含 gitignore 元字符、下发后会静默失配的模式。
func splitSubscriptionSparseTargets(raw string) (safe []string, risky []string) {
	for _, p := range splitSubscriptionFilterPatterns(raw) {
		p = normalizeSubscriptionFilterTarget(p)
		if p == "" || isWildcardFilterPattern(p) {
			continue
		}
		if strings.ContainsAny(p, subscriptionSparseUnsafeChars) {
			risky = append(risky, p)
			continue
		}
		safe = append(safe, p)
	}
	return safe, risky
}

// splitSubscriptionSparseDependencyTargets 在 splitSubscriptionDependencyPatterns 之上，
// 再按 gitignore 元字符把依赖模式分成「能安全下发」和「下发会静默失配」两组。
func splitSubscriptionSparseDependencyTargets(raw string) (safe []string, risky []string, notes []string) {
	patterns, skippedNotes := splitSubscriptionDependencyPatterns(raw)
	for _, p := range patterns {
		if strings.ContainsAny(p, subscriptionSparseUnsafeChars) {
			risky = append(risky, p)
			continue
		}
		safe = append(safe, p)
	}
	return safe, risky, skippedNotes
}

func formatSubscriptionPatternList(patterns []string) string {
	quoted := make([]string, 0, len(patterns))
	for _, p := range patterns {
		quoted = append(quoted, "`"+p+"`")
	}
	return strings.Join(quoted, " / ")
}

// formatSubscriptionFileList 把文件列表压成一行日志；超过 limit 只列前 limit 个并带上总数，
// 避免大仓库把整个拉取日志刷爆。
func formatSubscriptionFileList(files []string, limit int) string {
	if limit > 0 && len(files) > limit {
		return strings.Join(files[:limit], ", ") + fmt.Sprintf(" ...（共 %d 个）", len(files))
	}
	return strings.Join(files, ", ")
}

func buildSubscriptionSparseCheckoutPatterns(sub *model.Subscription) (patterns []string, warnings []string) {
	if sub == nil {
		return nil, nil
	}

	seen := map[string]bool{}
	addPattern := func(pattern string) {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		pattern = strings.TrimPrefix(pattern, "./")
		pattern = strings.TrimPrefix(pattern, "/")
		if pattern == "" || seen[pattern] {
			return
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}

	// addFragmentPatterns 为一个「子串包含」片段成对下发两条 sparse 规则：
	//
	//	**/*x*      名字里含 x 的文件，以及名为 *x* 的目录条目本身
	//	**/*x*/**   名为 *x* 的目录下的全部内容（含多级子目录）
	//
	// 为什么必须成对：sparse-checkout 用的是 gitignore 语法，而 gitignore 里的 `*`
	// **不跨 `/`**。片段 `utils` 只包成 `**/*utils*` 时，它能命中 `sendNotify.js`
	// 这种「名字里含片段」的文件、也能命中 `utils` 这个目录条目本身，但命不中
	// `utils/date.js`——`*utils*` 跨不过那个 `/`。反过来只留 `**/*utils*/**` 又会丢掉
	// 根目录下的单文件。两条缺一不可。
	//
	// 排除侧（黑名单）同样成对下发，理由不是「对称好看」，是不成对会真的变得更糟：
	//   - git 的 sparse-checkout 是「最后匹配者胜出」，且父目录整体未命中时子项会继承
	//     未命中。`jd_dir/backUp/old.js` 现在正是靠这条继承被挡住的。
	//   - 一旦包含侧多了 `**/*jd_*/**` 这种直接命中子文件的规则，非递归的
	//     `!**/*backUp*`（只匹配到 backUp 目录条目本身）就压不住它了，本来挡得住的
	//     文件反而会落盘。
	//   - Go 侧 checkBlacklist 对完整相对路径做 strings.Contains，本来就是递归语义；
	//     递归排除只是让 git 侧与它对齐，不会多挡任何「本来会被建成定时任务」的文件。
	addFragmentPatterns := func(fragment string, exclude bool) {
		prefix := ""
		if exclude {
			prefix = "!"
		}
		addPattern(prefix + "**/*" + fragment + "*")
		addPattern(prefix + "**/*" + fragment + "*/**")
	}

	subPaths, unsafeSubPaths := splitSubscriptionSparseTargets(sub.SubPath)
	whitelist, unsafeWhitelist := splitSubscriptionSparseTargets(sub.Whitelist)
	blacklist, unsafeBlacklist := splitSubscriptionSparseTargets(sub.Blacklist)

	// 包含侧（指定子目录 / 白名单）是「或」语义：只跳过其中一条不安全的子模式，
	// 会让那条本该命中的文件静默检不出来，用户看到的还是「拉取成功但任务是空的」。
	// 所以只要有一条不安全，就整体放弃包含侧的 sparse 限制、改为检出完整仓库，
	// 再交给 Go 侧的 matchesSubscriptionFilters 决定给哪些脚本建任务。
	// 宁可多落几个文件，也不要静默丢文件。
	switch {
	case len(unsafeSubPaths) > 0:
		warnings = append(warnings, fmt.Sprintf(
			"[警告] 指定子目录 %s 含 git 通配特殊字符（%s），无法安全转成 sparse-checkout 规则；本次改为检出完整仓库，请改用不含这些字符的普通路径片段",
			formatSubscriptionPatternList(unsafeSubPaths), subscriptionSparseUnsafeCharsHint))
	case len(subPaths) > 0:
		// 指定子目录优先级最高：它代表用户明确只想要仓库里的某几个目录/文件。
		//
		// 这里刻意**不**走 addFragmentPatterns：子目录填的是明确路径（`scripts/daily`），
		// 直接作为 gitignore 模式下发时会精确命中那个目录条目，git 的
		// clear_ce_flags_dir 再把「命中」向下继承给目录里的全部文件，语义已经完整。
		// 若也包成 `**/*scripts/daily*` 反而会把它从「精确路径」变成「子串包含」，
		// 与该字段既有的语义不符。
		for _, p := range subPaths {
			addPattern(p)
		}
	case len(unsafeWhitelist) > 0:
		warnings = append(warnings, fmt.Sprintf(
			"[警告] 白名单 %s 含 git 通配特殊字符（%s），无法安全转成 sparse-checkout 规则；本次改为检出完整仓库，扫描任务时仍按白名单过滤。白名单是「子串包含」匹配，不支持正则",
			formatSubscriptionPatternList(unsafeWhitelist), subscriptionSparseUnsafeCharsHint))
	default:
		// 没有指定子目录时，才用白名单限制真实检出的文件范围。
		// 白名单历史上是「完整相对路径的子串包含匹配」（见 matchesSubscriptionWhitelist），
		// 所以片段命中目录名时，目录里的文件也算命中——成对下发递归规则才对得上。
		for _, p := range whitelist {
			addFragmentPatterns(p, false)
		}
	}

	// 依赖规则并进「包含侧」：命中的文件会被检出落盘（主脚本 require 的辅助库），
	// 但 Go 侧的任务候选筛选仍然只认白名单，所以它们不会被建成定时任务，
	// 见 isSubscriptionDependencyOnlyFile。
	//
	// 守卫 `len(patterns) > 0` 是本次改造最关键的一处，去掉会直接把订阅打空：
	//   - 上面的 switch 因元字符退回完整检出时 patterns 为空 → 本来就全量落盘，
	//     依赖规则天然满足；此时若追加依赖模式，sparse-checkout 反而会被激活成
	//     「只检出依赖文件」，白名单文件全丢。
	//   - 用户既没填子目录也没填白名单时 patterns 同样为空 → 本来就检出全部文件
	//     （白名单为空 = 全部文件都算命中白名单），追加依赖模式一样会把「全量」
	//     缩成「只有依赖」，主脚本反而没了。
	if len(patterns) > 0 {
		dependPatterns, unsafeDepend, dependNotes := splitSubscriptionSparseDependencyTargets(sub.DependOn)
		// 依赖片段最典型的写法就是目录名（青龙那条真实指令里的 `utils`），
		// 主脚本 require('./utils/xxx') 要的是目录里的文件而不是目录条目本身，
		// 所以这里同样成对下发递归规则。
		for _, p := range dependPatterns {
			addFragmentPatterns(p, false)
		}
		if len(dependPatterns) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"[依赖规则] %s 已并入检出范围：命中的文件会被拉取到脚本目录供主脚本调用，但不会建成定时任务（只有命中白名单的文件才建任务）",
				formatSubscriptionPatternList(dependPatterns)))
		}
		// 依赖规则跳过一条只会「少落一个辅助文件」，退化到改造前的行为（主脚本照常检出、
		// 照常建任务，只是跑起来可能缺依赖），方向安全，所以逐条跳过而不像白名单那样整体退回。
		// 但必须打出来：静默少一个 sendNotify.js，用户看到的是任务跑起来才报错。
		if len(unsafeDepend) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"[警告] 依赖规则 %s 含 git 通配特殊字符（%s），已跳过对应的检出规则；这些依赖文件不会被拉取，主脚本运行时可能因缺少依赖而失败，请改用不含这些字符的普通文件名片段",
				formatSubscriptionPatternList(unsafeDepend), subscriptionSparseUnsafeCharsHint))
		}
		if len(dependNotes) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"[提示] 依赖规则中的 %s 含空格/中文或过长，已按文字备注跳过、未参与文件检出；依赖规则现在是功能性字段，请填写文件名片段（多个用 `,` 或 `|` 分隔，匹配方式是「子串包含」）",
				formatSubscriptionPatternList(dependNotes)))
		}
	} else if strings.TrimSpace(sub.DependOn) != "" {
		warnings = append(warnings, "[提示] 未配置指定子目录/白名单（或包含侧已退回完整检出），本次检出完整仓库，依赖规则无需额外生效")
	}

	// 黑名单是「排除」语义：跳过一条不安全的排除规则，只会让对应文件多落一份盘，
	// Go 侧的 checkBlacklist 仍然会把它们挡在定时任务之外，方向是安全的，逐条跳过即可。
	if len(unsafeBlacklist) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"[警告] 黑名单 %s 含 git 通配特殊字符（%s），已跳过对应的 sparse-checkout 排除规则；这些文件仍会落盘，但不会被建成定时任务",
			formatSubscriptionPatternList(unsafeBlacklist), subscriptionSparseUnsafeCharsHint))
	}

	if len(blacklist) == 0 {
		// 包含侧被迫放弃、又没有可用排除规则时 patterns 为空，
		// 等价于「不做任何过滤」，直接返回空让调用方关掉 sparse-checkout。
		return patterns, warnings
	}

	// 只有黑名单（或包含侧被迫放弃）时先包含全部，再用 !pattern 排除，
	// 避免"黑名单目录"也落到 scripts 里。
	if len(patterns) == 0 {
		addPattern("*")
	}
	// 排除规则必须排在包含规则之后：sparse-checkout 是「最后匹配者胜出」，
	// 只有这样 `!**/*backUp*/**` 才能压住前面 `**/*jd_*/**` 对 backUp/jd_old.js 的命中。
	for _, p := range blacklist {
		addFragmentPatterns(p, true)
	}

	return patterns, warnings
}

func applySparseCheckout(ctx context.Context, repoDir string, sub *model.Subscription, env []string, emit PullCallback) error {
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(sub)
	for _, warning := range warnings {
		emit(warning)
	}
	if len(patterns) == 0 {
		// 用户清空子目录/白名单后，要把之前的 sparse-checkout 关掉，
		// 否则旧过滤规则会一直残留，导致后续看起来"仓库文件丢了"。
		cmd := exec.CommandContext(ctx, "git", "config", "--bool", "core.sparseCheckout")
		cmd.Dir = repoDir
		cmd.Env = env
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "true" {
			emit("[sparse-checkout] 当前未配置子目录/白名单，正在恢复完整仓库检出")
			cmd = exec.CommandContext(ctx, "git", "sparse-checkout", "disable")
			cmd.Dir = repoDir
			cmd.Env = env
			if _, runErr := runCmdWithCallback(ctx, cmd, emit); runErr != nil {
				return fmt.Errorf("关闭 sparse-checkout 失败: %w", runErr)
			}
		}
		return nil
	}

	emit(fmt.Sprintf("[sparse-checkout] 设置订阅路径过滤: %s", strings.Join(patterns, ", ")))

	cmd := exec.CommandContext(ctx, "git", "sparse-checkout", "init", "--no-cone")
	cmd.Dir = repoDir
	cmd.Env = env
	if _, err := runCmdWithCallback(ctx, cmd, emit); err != nil {
		return fmt.Errorf("sparse-checkout init 失败: %w", err)
	}

	args := append([]string{"sparse-checkout", "set", "--no-cone"}, patterns...)
	cmd = exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	cmd.Env = env
	if _, err := runCmdWithCallback(ctx, cmd, emit); err != nil {
		return fmt.Errorf("sparse-checkout set 失败: %w", err)
	}

	return nil
}

func pullSingleFileWithCallback(ctx context.Context, sub *model.Subscription, _ string, emit PullCallback) (string, error) {
	saveDir := sub.SaveDir
	if saveDir == "" {
		saveDir = "downloads"
	}

	parts := strings.Split(sub.URL, "/")
	filename := parts[len(parts)-1]
	if sub.Alias != "" {
		filename = sub.Alias
	}

	destPath := filepath.Join(config.C.Data.ScriptsDir, saveDir, filename)
	parent := filepath.Dir(destPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	tmpFile, err := os.CreateTemp(parent, "."+filepath.Base(filename)+".staging-*")
	if err != nil {
		return "", fmt.Errorf("创建单文件 staging 失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.Remove(tmpPath)
		}
	}()

	emit(fmt.Sprintf("[下载] %s -> %s/%s (staging)", sub.URL, saveDir, filename))
	output, err := DownloadFileWithContext(ctx, sub.URL, tmpPath)
	if output != "" {
		emit(output)
	}
	if err != nil {
		return output, err
	}
	if ctx.Err() != nil {
		return output, fmt.Errorf("拉取已停止")
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return output, fmt.Errorf("发布单文件订阅失败: %w", err)
	}
	stagingActive = false
	emit("[已完成] 已原子切换到新的单文件订阅版本")
	return output, nil
}

func syncGitRemoteWithCallback(ctx context.Context, repoDir, remoteURL string, env []string, emit PullCallback) (string, error) {
	remoteURL, err := cleanGitRemoteURL(remoteURL)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "remote")
	cmd.Dir = repoDir
	cmd.Env = env

	remoteOutput, err := cmd.Output()
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return "", fmt.Errorf("拉取已停止")
		}
		return "", wrapGitCommandError("读取远端配置", gitCommandStderr(err), err)
	}

	args := []string{"remote", "add", "origin", remoteURL}
	for _, name := range strings.Fields(string(remoteOutput)) {
		if name == "origin" {
			args = []string{"remote", "set-url", "origin", remoteURL}
			break
		}
	}

	cmd = exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	cmd.Env = env
	return runCmdWithCallback(ctx, cmd, emit)
}

func writeTempSSHKey(privateKey string) (string, error) {
	tmpFile, err := os.CreateTemp("", "ssh_key_*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(privateKey); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	os.Chmod(tmpFile.Name(), 0600)
	return tmpFile.Name(), nil
}

var (
	// 兼容多种 cron 声明前缀：
	//   cron: 30 8 * * *
	//   # cron: 30 8 * * *
	//   #cron 8 9,10,11 * * *
	//   cron 0 12 * * *
	//   * cron 8 10 * * *           (JSDoc 块注释每行的 `*` 前缀)
	//   * cron: 12 8 * * *
	//   @cron: 30 8 * * *           (JSDoc `@cron` 标签)
	//   * @cron 0 0 * * *
	//   // cron: 0 0 * * *
	// 通过 `\b` 词界避免误匹配 `crontab` / `cron-utils` 等关键字。
	cronLabelPrefixRe      = regexp.MustCompile(`(?im)^[\s#*@/]*@?cron\b\s*[:：]?\s*(\S.*)$`)
	subscriptionTaskNameRe = regexp.MustCompile(`new\s+Env\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]\s*\)`)
	// Only accept name declarations from comment headers to avoid matching object fields.
	subscriptionTaskNameLabelRe = regexp.MustCompile(`(?i)^\s*(?:<!--|//+|#+|\*+|--+|@)\s*@?name\s*[:：]\s*(\S.*)$`)
	// 青龙风格 `cron "EXPR" filename, tag:xxx` 单行声明，常见于 JS 顶部注释。
	// 例如：cron "6 6 6 6 *" jd_CheckCK.js, tag:京东CK检测by-ccwav
	cronDirectiveLineRe = regexp.MustCompile(`(?i)\bcron\s+["']([^"'\n\r]+)["']\s+([^\s,;]+)`)
)

type subscriptionTaskSyncOptions struct {
	autoAdd     bool
	autoDelete  bool
	defaultCron string
	allowedExts map[string]bool
}

type subscriptionTaskCandidate struct {
	Name           string
	Command        string
	CronExpression string
}

func subscriptionTaskLabel(subID uint) string {
	return fmt.Sprintf("subscription:%d", subID)
}

func hasLabel(labels []string, target string) bool {
	for _, item := range labels {
		if item == target {
			return true
		}
	}
	return false
}

func withLabel(labels []string, target string) []string {
	if hasLabel(labels, target) {
		return labels
	}
	return append(labels, target)
}

func subscriptionSaveDir(sub *model.Subscription) string {
	saveDir := sub.SaveDir
	if saveDir == "" {
		saveDir = sub.Alias
		if saveDir == "" {
			parts := strings.Split(sub.URL, "/")
			saveDir = strings.TrimSuffix(parts[len(parts)-1], ".git")
		}
	}
	return saveDir
}

// isWildcardFilterPattern 判断"用户填的 pattern 是不是通配符"——
// 如 `*`、`**`、`*.*`、`.*`、`/`、`all`。这些显然是用户想"全部放行"的意图，
// 但旧逻辑用 strings.Contains 字面匹配 → 全部不匹配 → 全部文件被过滤掉。
// 现在视为"等价于不填"。
func isWildcardFilterPattern(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return true
	}
	switch strings.ToLower(p) {
	case "*", "**", "*.*", ".*", "/", "all", "any", "全部":
		return true
	}
	return false
}

func normalizeSubscriptionFilterTarget(value string) string {
	value = strings.TrimSpace(filepath.ToSlash(value))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	return value
}

func subscriptionFilterContains(target string, pattern string) bool {
	target = normalizeSubscriptionFilterTarget(target)
	pattern = normalizeSubscriptionFilterTarget(pattern)
	if target == "" || pattern == "" {
		return false
	}
	return strings.Contains(target, pattern)
}

func splitSubscriptionFilterPatterns(raw string) []string {
	var patterns []string
	seen := make(map[string]bool)
	for _, pattern := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '|'
	}) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || seen[pattern] {
			continue
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	return patterns
}

func hasNonWildcardSubscriptionFilter(raw string) bool {
	for _, pattern := range splitSubscriptionFilterPatterns(raw) {
		if !isWildcardFilterPattern(pattern) {
			return true
		}
	}
	return false
}

func matchesSubscriptionWhitelist(sub *model.Subscription, filePath string) bool {
	hasNonWildcard := false
	for _, pattern := range splitSubscriptionFilterPatterns(sub.Whitelist) {
		if isWildcardFilterPattern(pattern) {
			return true
		}
		hasNonWildcard = true
		if subscriptionFilterContains(filePath, pattern) {
			return true
		}
	}
	return !hasNonWildcard
}

func matchesSubscriptionFilters(sub *model.Subscription, filePath string) bool {
	if !matchesSubscriptionWhitelist(sub, filePath) {
		return false
	}
	return checkBlacklist(sub, filePath)
}

const subscriptionDependencyPatternMaxLen = 64

// looksLikeSubscriptionDependencyNote 判断依赖规则里的某一段是「文字备注」还是「文件名片段」。
//
// 兼容性背景：depend_on 在本次改造之前是纯备注字段（前端 placeholder 明写
// 「仅备注，不参与文件检出」），全链路一次都没读过它。存量数据里很可能是
// 「依赖 xxx 库，迁移自青龙」这类说明文字。现在它变成功能性字段后，直接拿去做
// 子串匹配有两种结果：
//  1. 匹配不到任何文件（绝大多数）——无害，只是没有额外检出；
//  2. 恰好命中（比如整段就是 `utils`）——会多检出一批文件。
//
// 第 2 种的杀伤力本身有限：依赖命中的文件只是落盘，不会被建成定时任务
// （见 isSubscriptionDependencyOnlyFile）。但没必要白白多下载，所以这里对
// 「一眼就是人话」的片段做启发式跳过，并在日志里明确列出被跳过的内容。
//
// 判定为备注的条件（任意一条命中）：
//   - 含空白字符：文件名片段不带空格；青龙 `ql repo` 的第 4 个位置参数是 shell 引号里的
//     `grep -E` 模式，各段用 `|` 分隔，段内也不含空格
//   - 含非 ASCII 字符（中文说明、中文标点）：脚本仓库里的路径是 ASCII
//   - rune 长度超过 subscriptionDependencyPatternMaxLen：单条文件名片段不会这么长
//
// 误判的代价是「该依赖文件没被额外检出」，也就是退回改造前的行为，方向安全。
func looksLikeSubscriptionDependencyNote(pattern string) bool {
	if pattern == "" {
		return false
	}
	if utf8.RuneCountInString(pattern) > subscriptionDependencyPatternMaxLen {
		return true
	}
	for _, r := range pattern {
		if r > unicode.MaxASCII || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// splitSubscriptionDependencyPatterns 把「依赖规则」字段拆成真正参与匹配的模式，
// 以及被判定为文字备注、直接跳过的片段（调用方负责把 notes 打到日志里）。
//
// 分隔符与白/黑名单完全一致（`,` 和 `|`），匹配语义也一致（子串包含，不是正则）。
//
// 通配符（`*` / `all` / `全部`）刻意跳过而不是「全部当依赖」：依赖命中的文件不建任务，
// 若把整个仓库都算成依赖，等于一个定时任务都建不出来。跳过后依赖规则视为未配置，
// 行为与改造前一致。
func splitSubscriptionDependencyPatterns(raw string) (patterns []string, notes []string) {
	for _, p := range splitSubscriptionFilterPatterns(raw) {
		p = normalizeSubscriptionFilterTarget(p)
		if p == "" || isWildcardFilterPattern(p) {
			continue
		}
		if looksLikeSubscriptionDependencyNote(p) {
			notes = append(notes, p)
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns, notes
}

// matchesSubscriptionDependency 判断文件是否命中「依赖规则」。
func matchesSubscriptionDependency(sub *model.Subscription, filePath string) bool {
	if sub == nil {
		return false
	}
	patterns, _ := splitSubscriptionDependencyPatterns(sub.DependOn)
	for _, pattern := range patterns {
		if subscriptionFilterContains(filePath, pattern) {
			return true
		}
	}
	return false
}

// isSubscriptionDependencyOnlyFile 判断文件是不是「只因为依赖规则才落盘」的辅助库文件。
//
// 这类文件（sendNotify.js、utils/*.js 等）是主脚本 require/import 的依赖，
// 检出它们是为了让主脚本能跑起来，本身不是定时任务，所以不进任务候选。
//
// 白名单判定放在前面且优先级更高：
//   - 白名单为空（或全是通配符）时 matchesSubscriptionWhitelist 恒 true，
//     这里恒返回 false → 依赖规则完全不改变任何行为。这正是「白名单为空 =
//     全部文件都算命中白名单，此时 depend_on 不应改变任何结果」的落点。
//   - 同时命中白名单和依赖规则的文件按白名单算，照常建任务。
func isSubscriptionDependencyOnlyFile(sub *model.Subscription, filePath string) bool {
	if sub == nil {
		return false
	}
	if matchesSubscriptionWhitelist(sub, filePath) {
		return false
	}
	return matchesSubscriptionDependency(sub, filePath)
}

func checkBlacklist(sub *model.Subscription, filePath string) bool {
	for _, pattern := range splitSubscriptionFilterPatterns(sub.Blacklist) {
		if isWildcardFilterPattern(pattern) {
			continue
		}
		if subscriptionFilterContains(filePath, pattern) {
			return false
		}
	}
	return true
}

func syncSubscriptionTasks(sub *model.Subscription, emit PullCallback) {
	options := getSubscriptionTaskSyncOptions(sub)
	if !options.autoAdd && !options.autoDelete {
		emit("[跳过自动同步任务] 订阅与系统设置中均未启用 auto_add_cron / auto_del_cron")
		return
	}

	saveDir := subscriptionSaveDir(sub)
	scriptsDir := filepath.Join(config.C.Data.ScriptsDir, saveDir)
	candidates, dependencyFiles := collectSubscriptionTaskCandidates(sub, options)
	label := subscriptionTaskLabel(sub.ID)

	// 可观测兜底：v2.2.8 之前任何空候选 / DB 创建失败都被静默吞掉，用户只看到
	// "[完成]" 就以为同步成功了。这里把每一步都打日志出来。
	scannedFileCount := countSubscriptionScriptFiles(scriptsDir, options.allowedExts, sub)
	emit(fmt.Sprintf("[扫描脚本] 目录 %s 共扫描 %d 个候选文件（按白/黑名单过滤后），识别出 %d 个含 cron 的脚本",
		scriptsDir, scannedFileCount, len(candidates)))
	if len(dependencyFiles) > 0 {
		emit(fmt.Sprintf("[依赖文件] 依赖规则命中 %d 个文件，已拉取到脚本目录供主脚本调用，不会建成定时任务: %s",
			len(dependencyFiles), formatSubscriptionFileList(dependencyFiles, 10)))
	}
	if len(candidates) == 0 && scannedFileCount > 0 {
		emit("[提示] 仓库内有脚本但没有识别到 cron 表达式：请检查脚本头部是否含 `cron <表达式>` 注释，或在系统设置 default_cron_rule 里配置默认 cron")
	}
	if scannedFileCount == 0 {
		emit("[提示] 没有扫描到任何候选脚本，常见原因：1) 指定子目录/白名单/黑名单把文件全过滤掉了（多个模式用 `,` 或 `|` 分隔，匹配方式是「子串包含」而非正则）；2) 上一步 sparse-checkout 规则没命中任何文件；3) 系统设置 repo_file_extensions 不含该脚本扩展名")
		if len(dependencyFiles) > 0 {
			emit("[提示] 本次只有依赖规则命中了文件、白名单一个都没命中：依赖规则只负责把辅助库文件拉下来，不会建任务。请确认主脚本的文件名片段是否填在「白名单」里")
		}
	}

	var managedTasks []model.Task
	queryTasksByLabel(label).Find(&managedTasks)
	managedByCommand := make(map[string]*model.Task, len(managedTasks))
	for i := range managedTasks {
		managedByCommand[strings.TrimSpace(managedTasks[i].Command)] = &managedTasks[i]
	}

	created := 0
	updated := 0
	deleted := 0
	adopted := 0
	failed := 0

	if options.autoAdd {
		for command, candidate := range candidates {
			if existing, ok := managedByCommand[command]; ok {
				if existing.SubscriptionLocked {
					if existing.Name != candidate.Name || existing.CronExpression != candidate.CronExpression {
						emit(fmt.Sprintf("[保留手动定时] %s 已被手动调整过，跳过订阅源的名称/定时覆盖（订阅源 cron: %s）；如需重新跟随订阅，请在任务详情点「恢复为订阅默认」",
							existing.Name, candidate.CronExpression))
					}
					continue
				}
				changes := map[string]interface{}{}
				if existing.Name != candidate.Name {
					changes["name"] = candidate.Name
					existing.Name = candidate.Name
				}
				if existing.CronExpression != candidate.CronExpression {
					changes["cron_expression"] = candidate.CronExpression
					existing.CronExpression = candidate.CronExpression
				}
				if len(changes) > 0 {
					if err := database.DB.Model(existing).Updates(changes).Error; err != nil {
						failed++
						emit(fmt.Sprintf("[自动更新任务失败] %s: %v", candidate.Name, err))
					} else {
						if scheduler := GetSchedulerV2(); scheduler != nil {
							scheduler.UpdateJob(existing)
						} else {
							emit(fmt.Sprintf("[调度器未初始化] 任务 %s 已保存，将在调度器启动时加载", candidate.Name))
						}
						updated++
						emit(fmt.Sprintf("[自动更新任务] %s (cron: %s)", candidate.Name, candidate.CronExpression))
					}
				}
				continue
			}

			var existing model.Task
			if err := database.DB.Where("command = ?", command).First(&existing).Error; err == nil {
				labels := withLabel(existing.GetLabels(), label)
				existing.SetLabelsFromSlice(labels)
				existing.SubscriptionLocked = true
				if err := database.DB.Model(&existing).Updates(map[string]interface{}{
					"labels":              existing.Labels,
					"subscription_locked": true,
				}).Error; err != nil {
					failed++
					emit(fmt.Sprintf("[关联已有任务失败] %s: %v", existing.Name, err))
				} else {
					managedByCommand[command] = &existing
					adopted++
					emit(fmt.Sprintf("[关联已有任务] %s", existing.Name))
				}
				continue
			}

			task := model.Task{
				Name:            candidate.Name,
				Command:         candidate.Command,
				CronExpression:  candidate.CronExpression,
				TaskType:        model.TaskTypeCron,
				Status:          model.TaskStatusEnabled,
				Timeout:         0,
				NotifyOnFailure: true,
			}
			task.SetLabelsFromSlice([]string{label})
			if err := database.DB.Select("*").Create(&task).Error; err != nil {
				failed++
				emit(fmt.Sprintf("[自动添加任务失败] %s (cron: %s) command=%s err=%v",
					candidate.Name, candidate.CronExpression, candidate.Command, err))
			} else {
				if scheduler := GetSchedulerV2(); scheduler != nil {
					_ = scheduler.AddJob(&task)
				} else {
					emit(fmt.Sprintf("[调度器未初始化] 任务 %s 已保存，将在调度器启动时加载", candidate.Name))
				}
				managedByCommand[command] = &task
				created++
				emit(fmt.Sprintf("[自动添加任务] %s (cron: %s)", candidate.Name, candidate.CronExpression))
			}
		}
	}

	if options.autoDelete {
		for _, task := range managedTasks {
			command := strings.TrimSpace(task.Command)
			if !strings.HasPrefix(command, "task ") {
				continue
			}
			if _, ok := candidates[command]; ok {
				continue
			}
			if task.SubscriptionLocked {
				emit(fmt.Sprintf("[保留任务] %s 已加锁，订阅源中已无对应脚本，已保留，请手动确认", task.Name))
				continue
			}

			if scheduler := GetSchedulerV2(); scheduler != nil {
				scheduler.RemoveJob(task.ID)
			}
			database.DB.Where("task_id = ?", task.ID).Delete(&model.TaskLog{})
			database.DB.Delete(&task)
			deleted++
			emit(fmt.Sprintf("[自动删除任务] %s", task.Name))
		}
	}

	if created > 0 {
		emit(fmt.Sprintf("[共自动添加 %d 个定时任务]", created))
	}
	if updated > 0 {
		emit(fmt.Sprintf("[共自动更新 %d 个定时任务]", updated))
	}
	if adopted > 0 {
		emit(fmt.Sprintf("[共关联 %d 个已有任务]", adopted))
	}
	if deleted > 0 {
		emit(fmt.Sprintf("[共自动删除 %d 个失效任务]", deleted))
	}
	if failed > 0 {
		emit(fmt.Sprintf("[警告] 共 %d 个任务操作失败，详见上方日志", failed))
	}
	if created == 0 && updated == 0 && adopted == 0 && deleted == 0 && failed == 0 {
		emit("[同步完成] 本次未对定时任务做任何变更")
	}
}

// countSubscriptionScriptFiles 统计 scriptsDir 下符合扩展名 + 白/黑名单过滤的文件数。
// 仅用于日志可观测：让用户知道"扫到了 X 个候选文件、识别出 Y 个 cron"——
// 当 X>0 而 Y=0 时能立刻看出是 cron 解析问题而不是路径问题。
func countSubscriptionScriptFiles(scriptsDir string, allowedExts map[string]bool, sub *model.Subscription) int {
	if _, err := os.Stat(scriptsDir); err != nil {
		return 0
	}
	count := 0
	filepath.Walk(scriptsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch strings.ToLower(info.Name()) {
			case ".git", "node_modules", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		relPath := subscriptionRelativeScriptPath(scriptsDir, path, info)
		if shouldManageSubscriptionFile(sub, relPath, allowedExts) {
			count++
		}
		return nil
	})
	return count
}

// FallbackSubscriptionCron 是订阅脚本未声明 cron 时使用的"硬兜底"。
// 用户既没在脚本头部写 cron 注释、也没在系统设置 default_cron_rule 里配自定义默认值时，
// 用这个兜底——每天 0 点跑一次，保证 git 拉到的脚本都会变成定时任务。
// 用户可以在任务详情里手动改 cron，或者把脚本注释加上 cron 头让下次同步用真值覆盖。
const FallbackSubscriptionCron = "0 0 * * *"

func getSubscriptionTaskSyncOptions(sub *model.Subscription) subscriptionTaskSyncOptions {
	defaultCron := strings.TrimSpace(model.GetRegisteredConfig("default_cron_rule"))
	if defaultCron != "" && !cron.Parse(defaultCron).Valid {
		defaultCron = ""
	}
	// 系统设置里 default_cron_rule 是空时，落到硬兜底。这是用户"git 拉了但一个任务都没建"
	// 困惑的根因：原默认是 "" → cron 头没识别就 skip，整个仓库一个任务都建不出来。
	// v2.2.10 起改为：默认兜底 = 每天 0 点。用户想关闭兜底，可以把 default_cron_rule
	// 设成非法值（比如 "off"），代码会回退到 "" 然后跳过没 cron 的脚本。
	if defaultCron == "" {
		defaultCron = FallbackSubscriptionCron
	}

	return subscriptionTaskSyncOptions{
		autoAdd:     sub.AutoAddTask || isConfigEnabled("auto_add_cron", true),
		autoDelete:  sub.AutoDelTask || isConfigEnabled("auto_del_cron", true),
		defaultCron: defaultCron,
		allowedExts: getSubscriptionAllowedExtensions(model.GetRegisteredConfig("repo_file_extensions")),
	}
}

func isConfigEnabled(key string, defaultValue bool) bool {
	if _, exists := model.GetSystemConfigDefinition(key); exists {
		return model.GetRegisteredConfigBool(key)
	}
	return model.GetConfigBool(key, defaultValue)
}

func getSubscriptionAllowedExtensions(raw string) map[string]bool {
	exts := make(map[string]bool)
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		token = strings.TrimSpace(strings.ToLower(token))
		token = strings.TrimPrefix(token, "*")
		if token == "" {
			continue
		}
		if !strings.HasPrefix(token, ".") {
			token = "." + token
		}
		exts[token] = true
	}
	if len(exts) > 0 {
		return exts
	}

	return map[string]bool{
		".js":  true,
		".mjs": true,
		".ts":  true,
		".py":  true,
		".sh":  true,
	}
}

// subscriptionHelperScriptNames 列出"通知辅助脚本"——这些脚本本身不是定时任务，
// 而是被业务脚本 require/import 调用的工具。订阅同步时不应该为它们建定时任务，
// 即使没有 cron 头并且系统配置了 default_cron_rule 兜底也不建。
// 名字按"去掉扩展名后的 basename，小写"匹配。
var subscriptionHelperScriptNames = map[string]bool{
	"sendnotify":  true, // QLScriptPublic / jdpro 风格的通知 helper（多种大小写拼写都收）
	"sendnofity":  true, // 实际仓库里 sendNofity.js 这种笔误也常见
	"notify":      true, // 青龙原版 notify.py
	"sendnotify_": true, // sendNotify_.js 这种带下划线后缀的变体
	"jdcookie":    true,
	"ql":          true,
	"qlapi":       true,
	"utils":       true,
	"util":        true,
	"common":      true,
	"helper":      true,
	"sign":        true, // 通用签名 helper
	"magic":       true, // jd_magic 类
	"jsencrypt":   true,
	"cryptojs":    true,
}

// isSubscriptionHelperScript 判断"该脚本是不是被业务脚本调用的辅助脚本"。
// 注意：只在脚本本身没有 cron 头注释时才用——脚本明确写了 cron 表达式
// 视为用户主动声明"这是定时任务"，必须建。
func isSubscriptionHelperScript(filename string) bool {
	base := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	return subscriptionHelperScriptNames[base]
}

func subscriptionRelativeScriptPath(root, path string, info os.FileInfo) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != "" && rel != "." {
		return rel
	}
	if info != nil {
		return info.Name()
	}
	return filepath.Base(path)
}

func shouldManageSubscriptionFile(sub *model.Subscription, filePath string, allowedExts map[string]bool) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !allowedExts[ext] {
		return false
	}
	return matchesSubscriptionFilters(sub, filePath)
}

func collectSubscriptionTaskCandidates(sub *model.Subscription, options subscriptionTaskSyncOptions) (map[string]subscriptionTaskCandidate, []string) {
	candidates := make(map[string]subscriptionTaskCandidate)
	saveDir := subscriptionSaveDir(sub)
	scriptsDir := filepath.Join(config.C.Data.ScriptsDir, saveDir)

	if _, err := os.Stat(scriptsDir); err != nil {
		return candidates, nil
	}

	// 收集"所有受支持扩展名的文件"。用 walk + 兜底的 ReadDir，确保:
	// 1) 子目录里的脚本能扫到（用 walk）
	// 2) 即使 walk 在某些挂载卷（NAS / Android Magisk 容器）下 readdir 异常返回 0，
	//    至少根目录平铺扫一遍兜底
	type fileEntry struct {
		path    string
		relPath string
		info    os.FileInfo
	}
	var allFiles []fileEntry
	seen := map[string]bool{}

	addEntry := func(path string, info os.FileInfo) {
		if info == nil || info.IsDir() {
			return
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if !options.allowedExts[ext] {
			return
		}
		if seen[path] {
			return
		}
		seen[path] = true
		allFiles = append(allFiles, fileEntry{
			path:    path,
			relPath: subscriptionRelativeScriptPath(scriptsDir, path, info),
			info:    info,
		})
	}

	filepath.Walk(scriptsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch strings.ToLower(info.Name()) {
			case ".git", "node_modules", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		addEntry(path, info)
		return nil
	})

	// 兜底：walk 一个文件都没拿到，平铺扫根目录（不递归）
	if len(allFiles) == 0 {
		entries, _ := os.ReadDir(scriptsDir)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fullPath := filepath.Join(scriptsDir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				if stat, statErr := os.Stat(fullPath); statErr == nil {
					info = stat
				} else {
					continue
				}
			}
			addEntry(fullPath, info)
		}
	}

	// 依赖规则命中、白名单没命中的文件，是「为了让主脚本能跑起来才检出」的辅助库，
	// 在这里就摘出去，位置必须在下面的兜底 #2 之前，原因有两个：
	//  1) 它们本来就不该建任务；
	//  2) 兜底 #2 会把 Whitelist/Blacklist 整个清空，若依赖文件还留在 allFiles 里，
	//     兜底一触发就会把 sendNotify.js / utils/*.js 这些库文件全建成定时任务 ——
	//     正好是「依赖规则改成功能性」最需要避免的副作用。摘掉之后 allFiles 的内容
	//     与改造前（依赖文件压根不落盘）等价，兜底 #2 的判定结果也就完全不变。
	var dependencyOnly []string
	if strings.TrimSpace(sub.DependOn) != "" {
		kept := allFiles[:0]
		for _, f := range allFiles {
			if isSubscriptionDependencyOnlyFile(sub, f.relPath) {
				dependencyOnly = append(dependencyOnly, filepath.ToSlash(f.relPath))
				continue
			}
			kept = append(kept, f)
		}
		allFiles = kept
	}

	// 兜底 #2：白/黑名单填错了导致全部被过滤 → 自动忽略过滤规则
	effectiveSub := sub
	if (sub.Whitelist != "" || sub.Blacklist != "") && len(allFiles) > 0 {
		matchedCount := 0
		for _, f := range allFiles {
			if matchesSubscriptionFilters(sub, f.relPath) {
				matchedCount++
			}
		}
		if matchedCount == 0 && hasNonWildcardSubscriptionFilter(sub.Whitelist) {
			fallback := *sub
			fallback.Whitelist = ""
			fallback.Blacklist = ""
			effectiveSub = &fallback
		}
	}

	for _, f := range allFiles {
		path := f.path
		info := f.info

		if !shouldManageSubscriptionFile(effectiveSub, f.relPath, options.allowedExts) {
			continue
		}

		// 先尝试从脚本头部识别 cron。脚本明确写了 cron 就完全按它来。
		cronExpr := resolveCronForSubscriptionTask(path, "")
		if cronExpr == "" {
			// 脚本头没 cron 注释。两种处理：
			//   1) 已知是通知/工具辅助脚本（sendNotify.js / notify.py 等）→ 不建任务
			//   2) 否则用兜底 cron（系统配置 default_cron_rule，或硬兜底每天 0 点）
			//      —— 保证 git 拉到的业务脚本必定变成任务，不会"明明拉成功但任务列表空"
			if isSubscriptionHelperScript(info.Name()) {
				continue
			}
			cronExpr = options.defaultCron
			if cronExpr == "" {
				continue
			}
		}

		relPath, err := filepath.Rel(config.C.Data.ScriptsDir, path)
		if err != nil {
			continue
		}
		command := "task " + relPath
		taskName := resolveSubscriptionTaskName(path, strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())))
		candidates[command] = subscriptionTaskCandidate{
			Name:           taskName,
			Command:        command,
			CronExpression: cronExpr,
		}
	}

	return candidates, dependencyOnly
}

func queryTasksByLabel(label string) *gorm.DB {
	return database.DB.Where(
		"labels = ? OR labels LIKE ? OR labels LIKE ? OR labels LIKE ?",
		label,
		label+",%",
		"%,"+label,
		"%,"+label+",%",
	)
}

func resolveCronForSubscriptionTask(path string, defaultCron string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	scriptBase := strings.ToLower(filepath.Base(path))
	for scanner.Scan() {
		lineCount++
		if lineCount > 50 {
			break
		}
		line := scanner.Text()
		if expr := extractSubscriptionCronExpression(line, scriptBase); expr != "" {
			return expr
		}
	}
	return strings.TrimSpace(defaultCron)
}

func resolveSubscriptionTaskName(path, fallback string) string {
	fallback = strings.TrimSpace(fallback)

	f, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer f.Close()

	var envName, labelName string
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > 120 {
			break
		}
		line := scanner.Text()
		if envName == "" {
			if matches := subscriptionTaskNameRe.FindStringSubmatch(line); len(matches) > 1 {
				envName = strings.TrimSpace(matches[1])
			}
		}
		if labelName == "" {
			labelName = extractSubscriptionTaskNameFromLabel(line)
		}
		if envName != "" && labelName != "" {
			break
		}
	}

	if envName != "" {
		return envName
	}
	if labelName != "" {
		return labelName
	}
	return fallback
}

func extractSubscriptionTaskNameFromLabel(line string) string {
	matches := subscriptionTaskNameLabelRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return ""
	}

	name := strings.TrimSpace(matches[1])
	name = strings.TrimSpace(strings.TrimSuffix(name, "-->"))
	name = strings.TrimSpace(strings.TrimSuffix(name, "*/"))
	name = strings.TrimSpace(strings.Trim(name, "\"'`"))
	if name == "" {
		return ""
	}
	if runes := []rune(name); len(runes) > 128 {
		name = strings.TrimSpace(string(runes[:128]))
	}
	return name
}

func extractSubscriptionCronExpression(line, scriptBase string) string {
	if expr := extractSubscriptionCronExpressionFromLabel(line); expr != "" {
		return expr
	}

	if matches := cronDirectiveLineRe.FindStringSubmatch(line); len(matches) > 2 && scriptBase != "" {
		expr := strings.TrimSpace(matches[1])
		fileToken := normalizeSubscriptionCronScriptToken(matches[2])
		if fileToken != "" &&
			strings.EqualFold(filepath.Base(fileToken), scriptBase) &&
			cron.Parse(expr).Valid {
			return expr
		}
	}

	return extractSubscriptionCronExpressionFromFilenameLine(line, scriptBase)
}

// extractSubscriptionCronExpressionFromLabel 处理“cron”标签开头的行，
// 兼容 `cron:`、`cron`（无冒号）、JSDoc `* cron`、`@cron:` 等多种写法。
// 当行尾跟随文件名提示（例如 `cron 8 10 * * *  qtx.js`）时，只截取前 5 或 6 个字段做 cron。
func extractSubscriptionCronExpressionFromLabel(line string) string {
	matches := cronLabelPrefixRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return ""
	}
	rest := strings.TrimSpace(matches[1])
	if rest == "" {
		return ""
	}

	if cron.Parse(rest).Valid {
		return rest
	}

	fields := strings.Fields(rest)
	for _, cnt := range []int{6, 5} {
		if len(fields) < cnt {
			continue
		}
		expr := strings.Join(fields[:cnt], " ")
		if cron.Parse(expr).Valid {
			return expr
		}
	}
	return ""
}

func extractSubscriptionCronExpressionFromFilenameLine(line, scriptBase string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || scriptBase == "" {
		return ""
	}

	cleaned := strings.TrimSpace(strings.Trim(trimmed, `"'`))
	fields := strings.Fields(cleaned)
	if len(fields) < 6 {
		return ""
	}

	for _, cronFieldCount := range []int{6, 5} {
		if len(fields) <= cronFieldCount {
			continue
		}

		expr := strings.Join(fields[:cronFieldCount], " ")
		if !cron.Parse(expr).Valid {
			continue
		}

		fileToken := normalizeSubscriptionCronScriptToken(fields[cronFieldCount])
		if fileToken == "" {
			continue
		}

		if strings.EqualFold(filepath.Base(fileToken), scriptBase) {
			return expr
		}
	}

	return ""
}

func normalizeSubscriptionCronScriptToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, `"'`)
	token = strings.TrimRight(token, ",;:)")
	token = strings.TrimLeft(token, "(")
	return strings.TrimSpace(token)
}
