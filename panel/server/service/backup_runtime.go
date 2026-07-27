package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"

	"gorm.io/gorm"
)

type LegacyBackupData struct {
	Version   string                `json:"version"`
	CreatedAt time.Time             `json:"created_at"`
	Tasks     []model.Task          `json:"tasks"`
	EnvVars   []BackupEnvVar        `json:"env_vars"`
	Subs      []model.Subscription  `json:"subscriptions"`
	Channels  []model.NotifyChannel `json:"notify_channels"`
	SSHKeys   []model.SSHKey        `json:"ssh_keys"`
	Configs   []model.SystemConfig  `json:"system_configs"`
	Scripts   []ScriptFile          `json:"scripts,omitempty"`
	Deps      []model.Dependency    `json:"dependencies,omitempty"`
}

func createBackupArchive(options BackupCreateOptions) (string, error) {
	selection := options.Selection.NormalizeDefaults()
	if !selection.Any() {
		return "", fmt.Errorf("至少选择一个备份项")
	}

	manifest, err := buildBackupManifest(selection)
	if err != nil {
		return "", err
	}

	archiveData, err := buildBackupArchive(manifest)
	if err != nil {
		return "", err
	}

	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create backup dir: %w", err)
	}

	var (
		filename  string
		finalData []byte
	)

	if strings.TrimSpace(options.Password) != "" {
		finalData, err = encryptData(archiveData, options.Password)
		if err != nil {
			return "", fmt.Errorf("failed to encrypt backup: %w", err)
		}
		filename = normalizeBackupArchiveName(options.Name, true)
	} else {
		finalData = archiveData
		filename = normalizeBackupArchiveName(options.Name, false)
	}

	filePath := filepath.Join(backupDir, filename)
	if err := os.WriteFile(filePath, finalData, 0o644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return filePath, nil
}

func normalizeBackupArchiveName(raw string, encrypted bool) string {
	fallback := fmt.Sprintf("backup_%s", time.Now().Format("20060102_150405"))
	name := strings.TrimSpace(raw)
	for _, suffix := range []string{".tar.gz", ".tgz", ".enc", ".json"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			name = name[:len(name)-len(suffix)]
			break
		}
	}
	if name == "" {
		name = fallback
	}

	name = strings.Map(func(r rune) rune {
		switch {
		case r < 32:
			return -1
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		default:
			return r
		}
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = fallback
	}

	if encrypted {
		return name + ".enc"
	}
	return name + ".tgz"
}

func buildBackupManifest(selection BackupSelection) (BackupManifest, error) {
	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		CreatedAt: time.Now(),
		Selection: selection,
	}

	if selection.Configs {
		cfgBundle, err := snapshotConfigBundle()
		if err != nil {
			return BackupManifest{}, err
		}
		manifest.Data.Configs = cfgBundle
	}

	if selection.Tasks {
		if err := database.DB.Order("id ASC").Find(&manifest.Data.Tasks).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load tasks: %w", err)
		}
	}

	if selection.EnvVars {
		var envVars []model.EnvVar
		if err := database.DB.Order("position ASC, id ASC").Find(&envVars).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load env vars: %w", err)
		}
		manifest.Data.EnvVars = make([]BackupEnvVar, 0, len(envVars))
		for _, envVar := range envVars {
			manifest.Data.EnvVars = append(manifest.Data.EnvVars, backupEnvVarFromModel(envVar))
		}
	}

	if selection.Subscriptions {
		if err := database.DB.Order("id ASC").Find(&manifest.Data.Subscriptions).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load subscriptions: %w", err)
		}

		var sshKeys []model.SSHKey
		if err := database.DB.Order("id ASC").Find(&sshKeys).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load ssh keys: %w", err)
		}
		for _, key := range sshKeys {
			manifest.Data.SSHKeys = append(manifest.Data.SSHKeys, BackupSSHKey{
				ID:         key.ID,
				Name:       key.Name,
				PrivateKey: key.PrivateKey,
				CreatedAt:  key.CreatedAt,
				UpdatedAt:  key.UpdatedAt,
			})
		}
	}

	if selection.Dependencies {
		var deps []model.Dependency
		if err := database.DB.Order("id ASC").Find(&deps).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load dependencies: %w", err)
		}
		for _, dep := range deps {
			manifest.Data.Dependencies = append(manifest.Data.Dependencies, BackupDependency{
				Type:          dep.Type,
				Name:          dep.Name,
				PythonVersion: dep.PythonVersion,
			})
		}
	}

	if selection.TaskViews {
		if err := database.DB.Order("sort_order ASC, id ASC").Find(&manifest.Data.TaskViews).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load task views: %w", err)
		}
	}

	if selection.Logs {
		var taskLogs []model.TaskLog
		if err := database.DB.Preload("Task").Order("id ASC").Find(&taskLogs).Error; err != nil {
			return BackupManifest{}, fmt.Errorf("load task logs: %w", err)
		}
		for _, logItem := range taskLogs {
			taskName := ""
			if logItem.Task != nil {
				taskName = logItem.Task.Name
			}
			manifest.Data.TaskLogs = append(manifest.Data.TaskLogs, BackupTaskLog{
				TaskID:    logItem.TaskID,
				TaskName:  taskName,
				Content:   logItem.Content,
				Status:    logItem.Status,
				Duration:  logItem.Duration,
				LogPath:   logItem.LogPath,
				StartedAt: logItem.StartedAt,
				EndedAt:   logItem.EndedAt,
				CreatedAt: logItem.CreatedAt,
				UpdatedAt: logItem.UpdatedAt,
			})
		}
	}

	return manifest, nil
}

func snapshotConfigBundle() (BackupConfigBundle, error) {
	bundle := BackupConfigBundle{}
	mirrors := CurrentDependencyMirrorSettings()
	bundle.DependencyMirrors = &mirrors

	if err := database.DB.Order("key ASC").Find(&bundle.SystemConfigs).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load system configs: %w", err)
	}

	var apps []model.OpenApp
	if err := database.DB.Order("id ASC").Find(&apps).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load open apps: %w", err)
	}
	for _, app := range apps {
		bundle.OpenApps = append(bundle.OpenApps, BackupOpenApp{
			ID:        app.ID,
			Name:      app.Name,
			AppKey:    app.AppKey,
			AppSecret: app.AppSecret,
			Scopes:    app.Scopes,
			Enabled:   app.Enabled,
			RateLimit: app.RateLimit,
			CreatedAt: app.CreatedAt,
			UpdatedAt: app.UpdatedAt,
		})
	}

	var channels []model.NotifyChannel
	if err := database.DB.Order("id ASC").Find(&channels).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load notify channels: %w", err)
	}
	for _, channel := range channels {
		bundle.NotifyChannels = append(bundle.NotifyChannels, BackupNotifyChannel{
			ID:        channel.ID,
			Name:      channel.Name,
			Type:      channel.Type,
			Config:    channel.Config,
			Enabled:   channel.Enabled,
			CreatedAt: channel.CreatedAt,
			UpdatedAt: channel.UpdatedAt,
		})
	}

	var users []model.User
	if err := database.DB.Order("id ASC").Find(&users).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load users: %w", err)
	}
	for _, user := range users {
		bundle.Users = append(bundle.Users, BackupUser{
			ID:           user.ID,
			Username:     user.Username,
			PasswordHash: user.Password,
			Role:         user.Role,
			Enabled:      user.Enabled,
			LastLoginAt:  user.LastLoginAt,
			CreatedAt:    user.CreatedAt,
			UpdatedAt:    user.UpdatedAt,
		})
	}

	if err := database.DB.Order("id ASC").Find(&bundle.IPWhitelists).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load ip whitelists: %w", err)
	}

	var twoFactor []model.TwoFactorAuth
	if err := database.DB.Order("id ASC").Find(&twoFactor).Error; err != nil {
		return BackupConfigBundle{}, fmt.Errorf("load two factor auths: %w", err)
	}
	for _, item := range twoFactor {
		bundle.TwoFactorAuths = append(bundle.TwoFactorAuths, BackupTwoFactorAuth{
			UserID:    item.UserID,
			Secret:    item.Secret,
			Enabled:   item.Enabled,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}

	return bundle, nil
}

func buildBackupArchive(manifest BackupManifest) ([]byte, error) {
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeTarBytes(tarWriter, "manifest.json", manifestData, 0o644, manifest.CreatedAt); err != nil {
		return nil, err
	}

	if manifest.Selection.Scripts {
		if err := addDirectoryToTar(tarWriter, config.C.Data.ScriptsDir, "files/scripts"); err != nil {
			return nil, err
		}
	}

	if manifest.Selection.Logs {
		if err := addDirectoryToTar(tarWriter, config.C.Data.LogDir, "files/logs"); err != nil {
			return nil, err
		}
		panelLogPath := filepath.Join(config.C.Data.Dir, "panel.log")
		if _, err := os.Stat(panelLogPath); err == nil {
			if err := addFileToTar(tarWriter, panelLogPath, "files/panel.log"); err != nil {
				return nil, err
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	return raw.Bytes(), nil
}

func writeTarBytes(tw *tar.Writer, name string, data []byte, mode int64, modTime time.Time) error {
	header := &tar.Header{
		Name:    filepath.ToSlash(name),
		Mode:    mode,
		Size:    int64(len(data)),
		ModTime: modTime,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar body %s: %w", name, err)
	}
	return nil
}

func addFileToTar(tw *tar.Writer, sourcePath, archivePath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", sourcePath, err)
	}
	defer file.Close()

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("build tar header %s: %w", sourcePath, err)
	}
	header.Name = filepath.ToSlash(archivePath)

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header %s: %w", sourcePath, err)
	}
	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("copy tar file %s: %w", sourcePath, err)
	}
	return nil
}

func addDirectoryToTar(tw *tar.Writer, sourceDir, archiveRoot string) error {
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		if info.IsDir() {
			// 脚本目录打包时也要复用统一的异常目录过滤规则，
			// 避免已被隔离/应忽略的污染目录重新进入备份包。
			if strings.HasSuffix(filepath.Clean(archiveRoot), filepath.Clean("files"+string(os.PathSeparator)+"scripts")) && ShouldIgnoreScriptPath(sourceDir, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(filepath.Clean(archiveRoot), filepath.Clean("files"+string(os.PathSeparator)+"scripts")) && ShouldIgnoreScriptPath(sourceDir, path) {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		return addFileToTar(tw, path, filepath.Join(archiveRoot, relPath))
	})
}

func restoreBackupFile(filename, password string) (err error) {
	if err := BeginRestoreProgress(filename); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			FailRestoreProgress(err)
		}
	}()

	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	filePath := filepath.Join(backupDir, filepath.Base(filename))

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}
	UpdateRestoreProgress("reading", "正在读取备份文件...", 10)

	rawData := fileData
	if strings.HasSuffix(strings.ToLower(filename), ".enc") {
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("加密备份需要密码")
		}
		UpdateRestoreProgress("decrypting", "正在解密加密备份...", 18)
		rawData, err = decryptData(fileData, password)
		if err != nil {
			return fmt.Errorf("failed to decrypt backup: %w", err)
		}
	}

	if looksLikeGzip(rawData) {
		if err := restoreArchiveBytes(rawData); err != nil {
			return err
		}
		CompleteRestoreProgress("数据已恢复完成，正在准备重启面板...")
		return nil
	}
	if looksLikeJSON(rawData) {
		if err := restoreLegacyJSONBytes(rawData); err != nil {
			return err
		}
		CompleteRestoreProgress("数据已恢复完成，正在准备重启面板...")
		return nil
	}

	return fmt.Errorf("无法识别的备份格式")
}

func looksLikeGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func looksLikeJSON(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	return strings.HasPrefix(trimmed, "{")
}

func restoreArchiveBytes(data []byte) error {
	UpdateRestoreProgress("extracting", "正在解包并校验备份内容...", 28)

	tempDir, err := os.MkdirTemp("", "daidai-restore-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := extractTarGzBytes(data, tempDir); err != nil {
		return err
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		UpdateRestoreProgress("analyzing", "正在读取备份清单并分析恢复内容...", 40)
		var manifest BackupManifest
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("读取备份清单失败: %w", err)
		}
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			return fmt.Errorf("解析备份清单失败: %w", err)
		}
		BindRestoreProgressPlan(manifest.Source, manifest.Selection)
		return restoreBackupManifest(manifest, tempDir)
	}

	manifest, err := buildQingLongManifest(tempDir)
	if err != nil {
		return err
	}
	UpdateRestoreProgress("analyzing", "已识别青龙备份，正在转换为面板数据...", 40)
	BindRestoreProgressPlan(manifest.Source, manifest.Selection)
	return restoreBackupManifest(manifest, tempDir)
}

func restoreLegacyJSONBytes(data []byte) error {
	UpdateRestoreProgress("analyzing", "正在解析旧版备份并转换结构...", 34)

	var legacy LegacyBackupData
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("failed to parse backup: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "daidai-legacy-restore-*")
	if err != nil {
		return fmt.Errorf("创建旧备份临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	scriptsDir := filepath.Join(tempDir, "files", "scripts")
	for _, scriptFile := range legacy.Scripts {
		if strings.Contains(scriptFile.Path, "..") {
			continue
		}
		content, err := base64.StdEncoding.DecodeString(scriptFile.Content)
		if err != nil {
			continue
		}
		targetPath := filepath.Join(scriptsDir, filepath.FromSlash(scriptFile.Path))
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("创建旧脚本目录失败: %w", err)
		}
		if err := os.WriteFile(targetPath, content, 0o755); err != nil {
			return fmt.Errorf("写入旧脚本失败: %w", err)
		}
	}

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   legacy.Version,
		Source:    "daidai-panel",
		CreatedAt: legacy.CreatedAt,
		Selection: BackupSelection{
			Configs:       true,
			Tasks:         true,
			Subscriptions: true,
			EnvVars:       true,
			Logs:          false,
			Scripts:       len(legacy.Scripts) > 0,
			Dependencies:  len(legacy.Deps) > 0,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				SystemConfigs: legacy.Configs,
			},
			Tasks:         legacy.Tasks,
			EnvVars:       legacy.EnvVars,
			Subscriptions: legacy.Subs,
		},
	}

	for _, channel := range legacy.Channels {
		manifest.Data.Configs.NotifyChannels = append(manifest.Data.Configs.NotifyChannels, BackupNotifyChannel{
			ID:        channel.ID,
			Name:      channel.Name,
			Type:      channel.Type,
			Config:    channel.Config,
			Enabled:   channel.Enabled,
			CreatedAt: channel.CreatedAt,
			UpdatedAt: channel.UpdatedAt,
		})
	}

	for _, key := range legacy.SSHKeys {
		manifest.Data.SSHKeys = append(manifest.Data.SSHKeys, BackupSSHKey{
			ID:         key.ID,
			Name:       key.Name,
			PrivateKey: key.PrivateKey,
			CreatedAt:  key.CreatedAt,
			UpdatedAt:  key.UpdatedAt,
		})
	}

	for _, dep := range legacy.Deps {
		manifest.Data.Dependencies = append(manifest.Data.Dependencies, BackupDependency{
			Type:          dep.Type,
			Name:          dep.Name,
			PythonVersion: dep.PythonVersion,
		})
	}

	BindRestoreProgressPlan(manifest.Source, manifest.Selection)
	return restoreBackupManifest(manifest, tempDir)
}

func extractTarGzBytes(data []byte, targetDir string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("打开备份压缩包失败: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取备份压缩内容失败: %w", err)
		}

		targetPath := filepath.Join(targetDir, filepath.FromSlash(header.Name))
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, filepath.Clean(targetDir)+string(os.PathSeparator)) && cleanTarget != filepath.Clean(targetDir) {
			return fmt.Errorf("备份包含非法路径: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return fmt.Errorf("创建文件目录失败: %w", err)
			}
			file, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("创建文件失败: %w", err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("写入文件失败: %w", err)
			}
			file.Close()
		}
	}

	return nil
}

func restoreBackupManifest(manifest BackupManifest, extractedDir string) error {
	selection := manifest.Selection
	if !selection.Any() {
		return fmt.Errorf("备份内容为空")
	}
	UpdateRestoreProgress("restoring-data", "正在写入数据库与核心配置...", 56)

	runtimeConfigValues := snapshotProtectedRuntimeSystemConfigs()

	tx := database.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	var createdDependencies []model.Dependency
	taskIDMap := map[uint]uint{}
	sshKeyIDMap := map[uint]uint{}

	rollback := func(err error) error {
		tx.Rollback()
		return err
	}

	if selection.Logs || selection.Tasks {
		if err := deleteAll(tx, "task_logs"); err != nil {
			return rollback(err)
		}
	}
	if selection.Tasks {
		if err := deleteAll(tx, "tasks"); err != nil {
			return rollback(err)
		}
	}
	if selection.Subscriptions {
		for _, table := range []string{"sub_logs", "subscriptions", "ssh_keys"} {
			if err := deleteAll(tx, table); err != nil {
				return rollback(err)
			}
		}
	}
	if selection.EnvVars {
		if err := deleteAll(tx, "env_vars"); err != nil {
			return rollback(err)
		}
	}
	if selection.Configs {
		for _, table := range []string{
			"api_call_logs",
			"open_apps",
			"notify_channels",
			"ip_whitelists",
			"system_configs",
		} {
			if err := deleteAll(tx, table); err != nil {
				return rollback(err)
			}
		}
	}
	if selection.Dependencies {
		if err := deleteAll(tx, "dependencies"); err != nil {
			return rollback(err)
		}
	}
	if selection.TaskViews {
		if err := deleteAll(tx, "task_views"); err != nil {
			return rollback(err)
		}
	}

	notifyChannelIDMap := map[uint]uint{}

	if selection.Configs {
		if err := restoreSystemConfigs(tx, manifest.Data.Configs.SystemConfigs); err != nil {
			return rollback(err)
		}
		var err error
		notifyChannelIDMap, err = restoreNotifyChannels(tx, manifest.Data.Configs.NotifyChannels)
		if err != nil {
			return rollback(err)
		}
		if err := restoreOpenApps(tx, manifest.Data.Configs.OpenApps); err != nil {
			return rollback(err)
		}
		if err := restoreIPWhitelists(tx, manifest.Data.Configs.IPWhitelists); err != nil {
			return rollback(err)
		}
	}

	if selection.EnvVars {
		if err := restoreEnvVars(tx, manifest.Data.EnvVars); err != nil {
			return rollback(err)
		}
	}

	if selection.Tasks {
		var err error
		taskIDMap, err = restoreTasks(tx, manifest.Data.Tasks, notifyChannelIDMap)
		if err != nil {
			return rollback(err)
		}
	}

	if selection.Subscriptions {
		var err error
		sshKeyIDMap, err = restoreSSHKeys(tx, manifest.Data.SSHKeys)
		if err != nil {
			return rollback(err)
		}
		if err := restoreSubscriptions(tx, manifest.Data.Subscriptions, sshKeyIDMap); err != nil {
			return rollback(err)
		}
	}

	if selection.Logs {
		if err := restoreTaskLogs(tx, manifest.Data.TaskLogs, taskIDMap); err != nil {
			return rollback(err)
		}
	}

	if selection.Dependencies {
		var err error
		createdDependencies, err = restoreDependencies(tx, manifest.Data.Dependencies)
		if err != nil {
			return rollback(err)
		}
	}

	if selection.TaskViews {
		for _, view := range manifest.Data.TaskViews {
			newView := model.TaskView{
				Name:      view.Name,
				Filters:   view.Filters,
				SortRules: view.SortRules,
				Hidden:    view.Hidden,
				SortOrder: view.SortOrder,
			}
			if err := tx.Create(&newView).Error; err != nil {
				return rollback(fmt.Errorf("restore task view %q: %w", view.Name, err))
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	restoreProtectedRuntimeSystemConfigs(runtimeConfigValues)

	if selection.Scripts {
		UpdateRestoreProgress("restoring-files", "正在恢复脚本文件与资源...", 72)
		if err := restoreScriptFiles(extractedDir, manifest.Source); err != nil {
			return err
		}
	}
	if selection.Logs {
		UpdateRestoreProgress("restoring-files", "正在恢复日志文件与面板日志...", 78)
		if err := restoreLogFiles(extractedDir, manifest.Source); err != nil {
			return err
		}
	}
	if selection.Configs && manifest.Data.Configs.DependencyMirrors != nil {
		UpdateRestoreProgress("restoring-mirrors", "正在恢复依赖镜像与运行时配置...", 84)
		if err := ApplyDependencyMirrorSettings(*manifest.Data.Configs.DependencyMirrors); err != nil {
			return err
		}
	}

	UpdateRestoreProgress("finalizing", "正在刷新任务调度与恢复后状态...", 92)
	model.InitDefaultConfigs()
	_ = middleware.ConfigureTrustedProxyCIDRs(model.GetRegisteredConfig("trusted_proxy_cidrs"))

	if scheduler := GetSchedulerV2(); scheduler != nil {
		scheduler.ReloadAllJobs()
	}
	if subScheduler := GetSubscriptionScheduler(); subScheduler != nil {
		subScheduler.ReloadAllJobs()
	}
	if backupScheduler := GetBackupScheduler(); backupScheduler != nil {
		backupScheduler.Reload()
	}
	if selection.Dependencies {
		UpdateRestoreProgress("finalizing", "正在提交依赖重装任务...", 96)
		dependencyReinstallBatchFunc(createdDependencies)
	}

	return nil
}

func deleteAll(tx *gorm.DB, table string) error {
	return tx.Exec("DELETE FROM " + table).Error
}

func restoreSystemConfigs(tx *gorm.DB, configs []model.SystemConfig) error {
	for _, item := range configs {
		if shouldSkipRestoredSystemConfigKey(item.Key) {
			continue
		}
		item.ID = 0
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func shouldSkipRestoredSystemConfigKey(key string) bool {
	switch strings.TrimSpace(key) {
	case
		"auto_update_last_checked_at",
		"auto_update_pending_version",
		"auto_update_pending_started_at":
		return true
	default:
		return false
	}
}

func protectedRuntimeSystemConfigKeys() []string {
	return []string{
		"auto_update_last_checked_at",
		"auto_update_pending_version",
		"auto_update_pending_started_at",
	}
}

func snapshotProtectedRuntimeSystemConfigs() map[string]string {
	result := make(map[string]string, len(protectedRuntimeSystemConfigKeys()))
	for _, key := range protectedRuntimeSystemConfigKeys() {
		result[key] = model.GetConfig(key, "")
	}
	return result
}

func restoreProtectedRuntimeSystemConfigs(values map[string]string) {
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		_ = model.SetConfig(key, value)
	}
}

func restoreUsers(tx *gorm.DB, users []BackupUser) (map[uint]uint, error) {
	idMap := make(map[uint]uint, len(users))
	for _, item := range users {
		user := model.User{
			Username:    item.Username,
			Password:    item.PasswordHash,
			Role:        item.Role,
			Enabled:     item.Enabled,
			LastLoginAt: item.LastLoginAt,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		}
		if err := tx.Create(&user).Error; err != nil {
			return nil, err
		}
		idMap[item.ID] = user.ID
	}
	return idMap, nil
}

func restoreNotifyChannels(tx *gorm.DB, channels []BackupNotifyChannel) (map[uint]uint, error) {
	idMap := make(map[uint]uint, len(channels))
	for _, item := range channels {
		channel := model.NotifyChannel{
			Name:      item.Name,
			Type:      item.Type,
			Config:    item.Config,
			Enabled:   item.Enabled,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		}
		if err := tx.Create(&channel).Error; err != nil {
			return nil, err
		}
		idMap[item.ID] = channel.ID
	}
	return idMap, nil
}

func restoreOpenApps(tx *gorm.DB, apps []BackupOpenApp) error {
	for _, item := range apps {
		app := model.OpenApp{
			Name:      item.Name,
			AppKey:    item.AppKey,
			AppSecret: item.AppSecret,
			Scopes:    item.Scopes,
			Enabled:   item.Enabled,
			RateLimit: item.RateLimit,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		}
		if err := tx.Create(&app).Error; err != nil {
			return err
		}
	}
	return nil
}

func restoreIPWhitelists(tx *gorm.DB, items []model.IPWhitelist) error {
	for _, item := range items {
		item.ID = 0
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func restoreTwoFactorAuths(tx *gorm.DB, items []BackupTwoFactorAuth, userIDMap map[uint]uint) error {
	for _, item := range items {
		userID := userIDMap[item.UserID]
		if userID == 0 {
			continue
		}
		record := model.TwoFactorAuth{
			UserID:    userID,
			Secret:    item.Secret,
			Enabled:   item.Enabled,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

func backupEnvVarFromModel(item model.EnvVar) BackupEnvVar {
	enabled := item.Enabled
	return BackupEnvVar{
		ID:        item.ID,
		Name:      item.Name,
		Value:     item.Value,
		Remarks:   item.Remarks,
		Enabled:   &enabled,
		Position:  item.Position,
		SortOrder: item.SortOrder,
		Group:     item.Group,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func modelEnvVarFromBackup(item BackupEnvVar) model.EnvVar {
	// 老备份可能没有 enabled 字段；缺字段时沿用历史行为，默认按启用恢复。
	enabled := true
	if item.Enabled != nil {
		enabled = *item.Enabled
	}
	return model.EnvVar{
		Name:      item.Name,
		Value:     item.Value,
		Remarks:   item.Remarks,
		Enabled:   enabled,
		Position:  item.Position,
		SortOrder: item.SortOrder,
		Group:     item.Group,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func restoreEnvVars(tx *gorm.DB, envVars []BackupEnvVar) error {
	for _, item := range envVars {
		envVar := modelEnvVarFromBackup(item)
		shouldRestoreDisabled := !envVar.Enabled
		if err := tx.Create(&envVar).Error; err != nil {
			return err
		}
		// Enabled=false 是 Go 零值，EnvVar 又有 default:true；Create 时会被 GORM 交给 SQLite 默认成 true。
		// 所以明确禁用的备份项创建后再兜底写回 false，避免恢复后禁用变量全部变成启用。
		if shouldRestoreDisabled {
			if err := tx.Model(&model.EnvVar{}).Where("id = ?", envVar.ID).Update("enabled", false).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreTasks(tx *gorm.DB, tasks []model.Task, notifyChannelIDMap map[uint]uint) (map[uint]uint, error) {
	idMap := make(map[uint]uint, len(tasks))
	pendingDepends := make(map[uint]uint)

	for _, item := range tasks {
		oldID := item.ID
		oldDepends := item.DependsOn
		oldNotificationChannelID := item.NotificationChannelID
		item.ID = 0
		item.PID = nil
		item.TaskType = item.GetTaskType()
		item.Status = normalizeRestoredTaskStatus(item.Status)
		item.DependsOn = nil
		item.NotificationChannelID = nil
		if oldNotificationChannelID != nil {
			if mapped := notifyChannelIDMap[*oldNotificationChannelID]; mapped != 0 {
				item.NotificationChannelID = &mapped
			}
		}

		if err := tx.Select("*").Create(&item).Error; err != nil {
			return nil, err
		}
		idMap[oldID] = item.ID
		if oldDepends != nil {
			pendingDepends[item.ID] = *oldDepends
		}
	}

	for newID, oldDepends := range pendingDepends {
		mapped := idMap[oldDepends]
		if mapped == 0 {
			continue
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", newID).Update("depends_on", mapped).Error; err != nil {
			return nil, err
		}
	}

	return idMap, nil
}

func normalizeRestoredTaskStatus(status float64) float64 {
	if status == model.TaskStatusDisabled {
		return model.TaskStatusDisabled
	}
	return model.TaskStatusEnabled
}

func restoreSSHKeys(tx *gorm.DB, keys []BackupSSHKey) (map[uint]uint, error) {
	idMap := make(map[uint]uint, len(keys))
	for _, item := range keys {
		key := model.SSHKey{
			Name:       item.Name,
			PrivateKey: item.PrivateKey,
			CreatedAt:  item.CreatedAt,
			UpdatedAt:  item.UpdatedAt,
		}
		if err := tx.Create(&key).Error; err != nil {
			return nil, err
		}
		idMap[item.ID] = key.ID
	}
	return idMap, nil
}

func restoreSubscriptions(tx *gorm.DB, subscriptions []model.Subscription, sshKeyIDMap map[uint]uint) error {
	for _, item := range subscriptions {
		item.ID = 0
		item.Status = 0
		if item.SSHKeyID != nil {
			mapped := sshKeyIDMap[*item.SSHKeyID]
			if mapped == 0 {
				item.SSHKeyID = nil
			} else {
				item.SSHKeyID = &mapped
			}
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func restoreTaskLogs(tx *gorm.DB, logs []BackupTaskLog, taskIDMap map[uint]uint) error {
	for _, item := range logs {
		taskID := taskIDMap[item.TaskID]
		if taskID == 0 && item.TaskName != "" {
			var task model.Task
			if err := tx.Where("name = ?", item.TaskName).First(&task).Error; err == nil {
				taskID = task.ID
			}
		}
		if taskID == 0 {
			continue
		}

		logRecord := model.TaskLog{
			TaskID:    taskID,
			Content:   item.Content,
			Status:    item.Status,
			Duration:  item.Duration,
			LogPath:   item.LogPath,
			StartedAt: item.StartedAt,
			EndedAt:   item.EndedAt,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		}
		if err := tx.Create(&logRecord).Error; err != nil {
			return err
		}
	}
	return nil
}

func restoreDependencies(tx *gorm.DB, deps []BackupDependency) ([]model.Dependency, error) {
	pending := make([]model.Dependency, 0, len(deps))
	seen := map[string]struct{}{}
	for _, item := range deps {
		depType := strings.TrimSpace(item.Type)
		name := strings.TrimSpace(item.Name)
		pythonVersion := ""
		if depType == "" || name == "" {
			continue
		}
		if depType == model.DepTypePython {
			pythonVersion = NormalizeDependencyPythonVersion(item.PythonVersion)
		}

		key := depType + "::" + pythonVersion + "::" + strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		dep := model.Dependency{
			Type:          depType,
			Name:          name,
			PythonVersion: pythonVersion,
		}
		if DependencyInstalledForPythonVersion(depType, name, pythonVersion) {
			dep.Status = model.DepStatusInstalled
			dep.Log = "[恢复备份] 已检测到依赖已存在，无需重装"
		} else {
			dep.Status = model.DepStatusInstalling
			dep.Log = "[恢复备份] 已提交依赖重装"
		}
		if err := tx.Create(&dep).Error; err != nil {
			return nil, err
		}
		if dep.Status == model.DepStatusInstalling {
			pending = append(pending, dep)
		}
	}
	return pending, nil
}

func restoreScriptFiles(extractedDir, source string) error {
	switch source {
	case "qinglong":
		return restoreQingLongScripts(extractedDir)
	default:
		sourceDir := filepath.Join(extractedDir, "files", "scripts")
		if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
			return nil
		}
		return restoreDirectoryWithStage(config.C.Data.ScriptsDir, func(stageDir string) error {
			return copyDirectoryContentsFunc(sourceDir, stageDir)
		})
	}
}

func restoreLogFiles(extractedDir, source string) error {
	panelLogPath := filepath.Join(config.C.Data.Dir, "panel.log")

	switch source {
	case "qinglong":
		if err := restoreQingLongLogs(extractedDir); err != nil {
			return err
		}
		return removePathIfExists(panelLogPath)
	default:
		logDir := filepath.Join(extractedDir, "files", "logs")
		if err := restoreDirectoryWithStage(config.C.Data.LogDir, func(stageDir string) error {
			return copyDirectoryContentsFunc(logDir, stageDir)
		}); err != nil {
			return err
		}
		panelLogSource := filepath.Join(extractedDir, "files", "panel.log")
		if _, err := os.Stat(panelLogSource); err == nil {
			return restoreFileWithStage(panelLogPath, func(stagePath string) error {
				return copyFileFunc(panelLogSource, stagePath)
			})
		}
		return removePathIfExists(panelLogPath)
	}
}

func clearDirectoryContents(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

var (
	// copyDirectoryContentsFunc / copyFileFunc 保留测试替身入口，
	// 用于验证“恢复 staging 失败时 live 目录仍保持原样”这类高风险回归。
	copyDirectoryContentsFunc = copyDirectoryContents
	copyFileFunc              = copyFile
)

// restoreDirectoryWithStage 先把恢复结果完整写入同目录下的 staging 目录，
// staging 成功后再原子切换 live 目录，避免“先删 live，再拷贝新内容”导致半失败时旧数据丢失。
func restoreDirectoryWithStage(targetDir string, fillStage func(stageDir string) error) error {
	parentDir := filepath.Dir(targetDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return err
	}

	stageDir := filepath.Join(parentDir, ".restore-stage-"+filepath.Base(targetDir)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}

	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageDir)
		}
	}()

	if err := fillStage(stageDir); err != nil {
		return err
	}

	backupDir := filepath.Join(parentDir, ".restore-backup-"+filepath.Base(targetDir)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	targetExists := false
	if _, err := os.Stat(targetDir); err == nil {
		targetExists = true
		if err := os.Rename(targetDir, backupDir); err != nil {
			return err
		}
	}

	if err := os.Rename(stageDir, targetDir); err != nil {
		if targetExists {
			_ = os.Rename(backupDir, targetDir)
		}
		return err
	}

	cleanupStage = false
	if targetExists {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

// restoreFileWithStage 和目录恢复同理，先准备好 staging 文件，再切换 live 文件。
func restoreFileWithStage(targetPath string, fillStage func(stagePath string) error) error {
	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return err
	}

	stagePath := filepath.Join(parentDir, ".restore-stage-"+filepath.Base(targetPath)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.Remove(stagePath)
		}
	}()

	if err := fillStage(stagePath); err != nil {
		return err
	}

	backupPath := filepath.Join(parentDir, ".restore-backup-"+filepath.Base(targetPath)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	targetExists := false
	if _, err := os.Stat(targetPath); err == nil {
		targetExists = true
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
	}

	if err := os.Rename(stagePath, targetPath); err != nil {
		if targetExists {
			_ = os.Rename(backupPath, targetPath)
		}
		return err
	}

	cleanupStage = false
	if targetExists {
		_ = os.Remove(backupPath)
	}
	return nil
}

func removePathIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyDirectoryContents(sourceDir, targetDir string) error {
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return nil
	}
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil {
			return nil
		}
		if info.IsDir() {
			if ShouldIgnoreScriptPath(sourceDir, path) {
				return filepath.SkipDir
			}
		} else if ShouldIgnoreScriptPath(sourceDir, path) {
			return nil
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		return copyFile(path, targetPath)
	})
}

func copyFile(sourcePath, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)
	return err
}

func reinstallDependenciesAsync(deps []model.Dependency) {
	reinstallDependenciesAsyncWithLogPrefix(deps, "[恢复备份]")
}

func reinstallDependenciesAfterRestartAsync(deps []model.Dependency) {
	reinstallDependenciesAsyncWithLogPrefix(deps, "[启动校验]")
}

var buildLinuxDependencyInstallCommandFunc = buildLinuxDependencyInstallCommand

func reinstallDependenciesAsyncWithLogPrefix(deps []model.Dependency, logPrefix string) {
	go func() {
		for _, dep := range deps {
			reinstallDependency(dep, logPrefix)
		}
	}()
}

func reinstallDependency(dep model.Dependency, logPrefix string) {
	logText := fmt.Sprintf("%s 正在安装 %s 依赖 %s", logPrefix, dep.Type, dep.Name)
	if dep.Type == model.DepTypePython {
		logText = fmt.Sprintf("%s 正在安装 Python %s 依赖 %s", logPrefix, NormalizeDependencyPythonVersion(dep.PythonVersion), dep.Name)
	}
	database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Update("log", logText)

	var cmd *exec.Cmd
	switch dep.Type {
	case model.DepTypeNodeJS:
		unlock := LockNodePackageOperation()
		defer unlock()

		var err error
		cmd, err = NewNpmInstallCommand(dep.Name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    logPrefix + " " + err.Error(),
			})
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = NewPipInstallCommandForPythonVersion(dep.PythonVersion, dep.Name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    logPrefix + " " + err.Error(),
			})
			return
		}
		cmd.Env = append(PipInstallEnv(AppendProxyEnv(os.Environ()), CurrentPipMirror()), "TMPDIR=/tmp")
	case model.DepTypeLinux:
		// 重启后的 Linux 依赖恢复必须复用当前主依赖安装路径的包管理器策略，
		// 避免 Debian 继续走旧的裸 apt-get update/install 分支，导致与
		// 依赖管理页的真实行为分叉，表现为“等待重启安装”后状态长期不收敛。
		var err error
		cmd, err = buildLinuxDependencyInstallCommandFunc(dep.Name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    logPrefix + " " + err.Error(),
			})
			return
		}
	default:
		database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    logPrefix + " 不支持的依赖类型",
		})
		return
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    fmt.Sprintf("%s 安装失败\n%s", logPrefix, strings.TrimSpace(string(output))),
		})
		return
	}

	database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
		"status": model.DepStatusInstalled,
		"log":    fmt.Sprintf("%s 安装成功\n%s", logPrefix, strings.TrimSpace(string(output))),
	})
}

func buildLinuxDependencyInstallCommand(packageName string) (*exec.Cmd, error) {
	manager, err := DetectLinuxPackageManager()
	if err != nil {
		return nil, err
	}
	return BuildLinuxPackageCommand(manager, "install", packageName, false, DetectLinuxDistribution(), nil)
}
