package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestReconcileDependenciesAfterRestartResumesRestoreJobs(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := &model.Dependency{
		Type:   model.DepTypeNodeJS,
		Name:   "left-pad",
		Status: model.DepStatusInstalling,
		Log:    "[恢复备份] 已提交依赖重装",
	}
	if err := database.DB.Create(dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}

	originalInstalled := dependencyInstalledFunc
	originalReinstallBatch := dependencyReinstallBatchFunc
	t.Cleanup(func() {
		dependencyInstalledFunc = originalInstalled
		dependencyReinstallBatchFunc = originalReinstallBatch
	})

	dependencyInstalledFunc = func(depType, name, pythonVersion string) bool {
		return false
	}

	var resumed []model.Dependency
	dependencyReinstallBatchFunc = func(deps []model.Dependency) {
		resumed = append(resumed, deps...)
	}

	ReconcileDependenciesAfterRestart()

	if len(resumed) != 1 {
		t.Fatalf("expected 1 dependency to resume, got %d", len(resumed))
	}
	if resumed[0].ID != dep.ID {
		t.Fatalf("expected resumed dependency id %d, got %d", dep.ID, resumed[0].ID)
	}

	var updated model.Dependency
	if err := database.DB.First(&updated, dep.ID).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	if updated.Status != model.DepStatusInstalling {
		t.Fatalf("expected dependency to stay installing, got %q", updated.Status)
	}
	if !strings.Contains(updated.Log, "已在重启后继续安装") {
		t.Fatalf("expected restart resume log, got %q", updated.Log)
	}
}

func TestReconcileDependenciesAfterRestartReinstallsMissingLinuxDeps(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := &model.Dependency{
		Type:   model.DepTypeLinux,
		Name:   "curl",
		Status: model.DepStatusInstalled,
		Log:    "[安装成功] curl",
	}
	if err := database.DB.Create(dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}

	originalInstalled := dependencyInstalledFunc
	originalRestartReinstallBatch := dependencyRestartReinstallBatchFunc
	t.Cleanup(func() {
		dependencyInstalledFunc = originalInstalled
		dependencyRestartReinstallBatchFunc = originalRestartReinstallBatch
	})

	dependencyInstalledFunc = func(depType, name, pythonVersion string) bool {
		return false
	}

	var resumed []model.Dependency
	dependencyRestartReinstallBatchFunc = func(deps []model.Dependency) {
		resumed = append(resumed, deps...)
	}

	ReconcileDependenciesAfterRestart()

	if len(resumed) != 1 {
		t.Fatalf("expected 1 linux dependency to auto-reinstall, got %d", len(resumed))
	}
	if resumed[0].ID != dep.ID {
		t.Fatalf("expected resumed dependency id %d, got %d", dep.ID, resumed[0].ID)
	}

	var updated model.Dependency
	if err := database.DB.First(&updated, dep.ID).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	if updated.Status != model.DepStatusInstalling {
		t.Fatalf("expected dependency to switch to installing, got %q", updated.Status)
	}
	if !strings.Contains(updated.Log, "已在重启后自动重新安装") {
		t.Fatalf("expected automatic reinstall log, got %q", updated.Log)
	}
}

func TestReconcileDependenciesAfterRestartMarksLinuxInstalledWhenDetected(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := &model.Dependency{
		Type:   model.DepTypeLinux,
		Name:   "curl",
		Status: model.DepStatusInstalling,
		Log:    "[启动校验] 检测到 Linux 依赖在容器重建后丢失，已在重启后自动重新安装",
	}
	if err := database.DB.Create(dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}

	originalInstalled := dependencyInstalledFunc
	originalReinstallBatch := dependencyReinstallBatchFunc
	t.Cleanup(func() {
		dependencyInstalledFunc = originalInstalled
		dependencyReinstallBatchFunc = originalReinstallBatch
	})

	dependencyInstalledFunc = func(depType, name, pythonVersion string) bool {
		return depType == model.DepTypeLinux && name == "curl"
	}

	reinstallCalled := false
	dependencyReinstallBatchFunc = func(deps []model.Dependency) {
		reinstallCalled = true
	}

	ReconcileDependenciesAfterRestart()

	if reinstallCalled {
		t.Fatal("expected no reinstall batch when linux dependency is already detected")
	}

	var updated model.Dependency
	if err := database.DB.First(&updated, dep.ID).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	if updated.Status != model.DepStatusInstalled {
		t.Fatalf("expected dependency to reconcile to installed, got %q", updated.Status)
	}
	if !strings.Contains(updated.Log, "已同步状态为已安装") {
		t.Fatalf("expected installed reconcile log, got %q", updated.Log)
	}
}

func TestReinstallDependencyUsesCurrentLinuxPackageManagerFlowOnApt(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := model.Dependency{
		Type:   model.DepTypeLinux,
		Name:   "curl",
		Status: model.DepStatusInstalling,
		Log:    "[启动校验] 检测到 Linux 依赖在容器重建后丢失，已在重启后自动重新安装",
	}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}

	originalBuild := buildLinuxDependencyInstallCommandFunc
	t.Cleanup(func() {
		buildLinuxDependencyInstallCommandFunc = originalBuild
	})

	called := false
	buildLinuxDependencyInstallCommandFunc = func(packageName string) (*exec.Cmd, error) {
		called = true
		cmd := exec.Command("cmd", "/c", "exit", "0")
		if _, err := exec.LookPath("cmd"); err != nil {
			cmd = exec.Command("sh", "-c", "exit 0")
		}
		return cmd, nil
	}

	reinstallDependency(dep, "[启动校验]")

	if !called {
		t.Fatal("expected restart reinstall path to use linux package manager builder")
	}
}

func TestRestoreBackupManifestPreservesCurrentPanelUsers(t *testing.T) {
	testutil.SetupTestEnv(t)

	currentUser := testutil.MustCreateUser(t, "current-admin", "admin")
	currentUser.Password = "current-password-hash"
	if err := database.DB.Model(currentUser).Update("password", currentUser.Password).Error; err != nil {
		t.Fatalf("update current user password: %v", err)
	}

	current2FA := &model.TwoFactorAuth{
		UserID:  currentUser.ID,
		Secret:  "current-2fa-secret",
		Enabled: true,
	}
	if err := database.DB.Create(current2FA).Error; err != nil {
		t.Fatalf("create current 2fa: %v", err)
	}

	if err := database.DB.Create(&model.OpenApp{
		Name:      "old-app",
		AppKey:    "old-key",
		AppSecret: "old-secret",
		Scopes:    "envs",
		Enabled:   true,
		RateLimit: 100,
	}).Error; err != nil {
		t.Fatalf("create old app: %v", err)
	}

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.4.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			Configs: true,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				SystemConfigs: []model.SystemConfig{
					{Key: "panel_title", Value: "来自备份的标题"},
				},
				Users: []BackupUser{
					{ID: 99, Username: "backup-admin", PasswordHash: "backup-password-hash", Role: "admin", Enabled: true},
				},
				TwoFactorAuths: []BackupTwoFactorAuth{
					{UserID: 99, Secret: "backup-2fa-secret", Enabled: true},
				},
				OpenApps: []BackupOpenApp{
					{Name: "backup-app", AppKey: "backup-key", AppSecret: "backup-secret", Scopes: "envs", Enabled: true, RateLimit: 200},
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var users []model.User
	if err := database.DB.Order("id ASC").Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected current users to be preserved without importing backup users, got %d users", len(users))
	}
	if users[0].Username != "current-admin" {
		t.Fatalf("expected current user to remain, got %q", users[0].Username)
	}
	if users[0].Password != "current-password-hash" {
		t.Fatalf("expected current password hash to stay unchanged, got %q", users[0].Password)
	}

	var twoFactor []model.TwoFactorAuth
	if err := database.DB.Find(&twoFactor).Error; err != nil {
		t.Fatalf("list 2fa records: %v", err)
	}
	if len(twoFactor) != 1 {
		t.Fatalf("expected current 2fa to be preserved, got %d records", len(twoFactor))
	}
	if twoFactor[0].Secret != "current-2fa-secret" {
		t.Fatalf("expected current 2fa secret to stay unchanged, got %q", twoFactor[0].Secret)
	}

	if got := model.GetRegisteredConfig("panel_title"); got != "来自备份的标题" {
		t.Fatalf("expected panel_title to restore from backup, got %q", got)
	}

	var apps []model.OpenApp
	if err := database.DB.Order("id ASC").Find(&apps).Error; err != nil {
		t.Fatalf("list open apps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "backup-app" {
		t.Fatalf("expected non-user config data to restore from backup, got %+v", apps)
	}
}

func TestRestoreBackupManifestIgnoresLegacyOpenAppCallCount(t *testing.T) {
	testutil.SetupTestEnv(t)

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.4.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			Configs: true,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				OpenApps: []BackupOpenApp{
					{
						Name:      "legacy-app",
						AppKey:    "legacy-key",
						AppSecret: "legacy-secret",
						Scopes:    "tasks",
						Enabled:   true,
						RateLimit: 0,
						CallCount: 123,
					},
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var app model.OpenApp
	if err := database.DB.Where("app_key = ?", "legacy-key").First(&app).Error; err != nil {
		t.Fatalf("load restored app: %v", err)
	}
	if app.CallCount != 0 {
		t.Fatalf("expected restored app call_count to reset to 0, got %d", app.CallCount)
	}
}

func TestRestoreBackupManifestSkipsAutoUpdateRuntimeStateConfigs(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := model.SetConfig("auto_update_last_checked_at", "2026-05-12T08:00:00+08:00"); err != nil {
		t.Fatalf("seed auto_update_last_checked_at: %v", err)
	}
	if err := model.SetConfig("auto_update_pending_version", "2.2.0"); err != nil {
		t.Fatalf("seed auto_update_pending_version: %v", err)
	}
	if err := model.SetConfig("auto_update_pending_started_at", "2026-05-12T08:05:00+08:00"); err != nil {
		t.Fatalf("seed auto_update_pending_started_at: %v", err)
	}

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.4.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			Configs: true,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				SystemConfigs: []model.SystemConfig{
					{Key: "panel_title", Value: "来自旧备份的标题"},
					{Key: "auto_update_last_checked_at", Value: "2025-01-01T00:00:00Z"},
					{Key: "auto_update_pending_version", Value: "2.1.8"},
					{Key: "auto_update_pending_started_at", Value: "2025-01-01T00:05:00Z"},
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	if got := model.GetRegisteredConfig("panel_title"); got != "来自旧备份的标题" {
		t.Fatalf("expected normal business config to restore, got %q", got)
	}
	if got := model.GetConfig("auto_update_last_checked_at", ""); got != "2026-05-12T08:00:00+08:00" {
		t.Fatalf("expected auto_update_last_checked_at to keep current value, got %q", got)
	}
	if got := model.GetConfig("auto_update_pending_version", ""); got != "2.2.0" {
		t.Fatalf("expected auto_update_pending_version to keep current value, got %q", got)
	}
	if got := model.GetConfig("auto_update_pending_started_at", ""); got != "2026-05-12T08:05:00+08:00" {
		t.Fatalf("expected auto_update_pending_started_at to keep current value, got %q", got)
	}
}

func TestSnapshotConfigBundleIncludesDependencyMirrors(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if err := SetPipMirror("https://mirrors.aliyun.com/pypi/simple"); err != nil {
		t.Fatalf("set pip mirror: %v", err)
	}
	if err := SetNpmMirror("https://mirrors.cloud.tencent.com/npm/"); err != nil {
		t.Fatalf("set npm mirror: %v", err)
	}

	bundle, err := snapshotConfigBundle()
	if err != nil {
		t.Fatalf("snapshot config bundle: %v", err)
	}
	if bundle.DependencyMirrors == nil {
		t.Fatalf("expected dependency mirrors to be snapshotted")
	}
	if bundle.DependencyMirrors.PipMirror != "https://mirrors.aliyun.com/pypi/simple" {
		t.Fatalf("expected pip mirror to be snapshotted, got %q", bundle.DependencyMirrors.PipMirror)
	}
	if bundle.DependencyMirrors.NpmMirror != "https://mirrors.cloud.tencent.com/npm/" {
		t.Fatalf("expected npm mirror to be snapshotted, got %q", bundle.DependencyMirrors.NpmMirror)
	}
}

func TestRestoreBackupManifestAppliesDependencyMirrorsBeforeDependencyResume(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	originalReinstallBatch := dependencyReinstallBatchFunc
	t.Cleanup(func() {
		dependencyReinstallBatchFunc = originalReinstallBatch
	})

	var gotPipMirror string
	var gotNpmMirror string
	dependencyReinstallBatchFunc = func(deps []model.Dependency) {
		gotPipMirror = CurrentPipMirror()
		gotNpmMirror = CurrentNpmMirror()
	}

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.4.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			Configs:      true,
			Dependencies: true,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				DependencyMirrors: &DependencyMirrorSettings{
					PipMirror: "https://mirrors.aliyun.com/pypi/simple",
					NpmMirror: "https://mirrors.cloud.tencent.com/npm/",
				},
			},
			Dependencies: []BackupDependency{
				{Type: model.DepTypePython, Name: "daidai-restore-mirror-test-package"},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	if gotPipMirror != "https://mirrors.aliyun.com/pypi/simple" {
		t.Fatalf("expected pip mirror to be restored before dependency resume, got %q", gotPipMirror)
	}
	if gotNpmMirror != "https://mirrors.cloud.tencent.com/npm/" {
		t.Fatalf("expected npm mirror to be restored before dependency resume, got %q", gotNpmMirror)
	}
}

func TestRestoreBackupManifestReplacesCoreBusinessData(t *testing.T) {
	testutil.SetupTestEnv(t)

	enabled := true
	disabled := false

	if err := database.DB.Create(&model.Task{
		Name:    "current-task",
		Command: "python3 current.py",
		Status:  model.TaskStatusEnabled,
	}).Error; err != nil {
		t.Fatalf("create current task: %v", err)
	}
	if err := database.DB.Create(&model.EnvVar{
		Name:    "CURRENT_ENV",
		Value:   "current",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create current env: %v", err)
	}
	if err := model.SetConfig("panel_title", "当前面板标题"); err != nil {
		t.Fatalf("set current panel title: %v", err)
	}

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.4.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			Configs: true,
			Tasks:   true,
			EnvVars: true,
		},
		Data: BackupPayload{
			Configs: BackupConfigBundle{
				SystemConfigs: []model.SystemConfig{
					{Key: "panel_title", Value: "备份里的标题"},
				},
			},
			Tasks: []model.Task{
				{
					Name:    "restored-task",
					Command: "python3 restored.py",
					Status:  model.TaskStatusEnabled,
				},
			},
			EnvVars: []BackupEnvVar{
				{
					Name:    "RESTORED_ENV",
					Value:   "restored",
					Enabled: &enabled,
				},
				{
					Name:     "RESTORED_DISABLED_ENV",
					Value:    "disabled-restored",
					Enabled:  &disabled,
					Position: 2000,
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var tasks []model.Task
	if err := database.DB.Order("id ASC").Find(&tasks).Error; err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Name != "restored-task" {
		t.Fatalf("expected current tasks to be replaced by restored task, got %+v", tasks)
	}

	var envs []model.EnvVar
	if err := database.DB.Order("id ASC").Find(&envs).Error; err != nil {
		t.Fatalf("list envs: %v", err)
	}
	if len(envs) != 2 || envs[0].Name != "RESTORED_ENV" || envs[0].Value != "restored" {
		t.Fatalf("expected current envs to be replaced by restored env, got %+v", envs)
	}
	enabledByName := map[string]bool{}
	for _, env := range envs {
		enabledByName[env.Name] = env.Enabled
	}
	if !enabledByName["RESTORED_ENV"] {
		t.Fatalf("expected RESTORED_ENV to stay enabled, got %+v", envs)
	}
	if enabledByName["RESTORED_DISABLED_ENV"] {
		t.Fatalf("expected RESTORED_DISABLED_ENV to stay disabled, got %+v", envs)
	}

	if got := model.GetRegisteredConfig("panel_title"); got != "备份里的标题" {
		t.Fatalf("expected panel_title to be restored, got %q", got)
	}
}

func TestRestoreBackupManifestDefaultsLegacyEnvEnabledWhenMissing(t *testing.T) {
	testutil.SetupTestEnv(t)

	manifest := BackupManifest{
		Format:  "daidai-panel-backup",
		Version: "0.3.0",
		Source:  "daidai-panel",
		Selection: BackupSelection{
			EnvVars: true,
		},
		Data: BackupPayload{
			EnvVars: []BackupEnvVar{
				{
					Name:  "LEGACY_ENV_WITHOUT_ENABLED",
					Value: "legacy",
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var env model.EnvVar
	if err := database.DB.Where("name = ?", "LEGACY_ENV_WITHOUT_ENABLED").First(&env).Error; err != nil {
		t.Fatalf("load restored legacy env: %v", err)
	}
	if !env.Enabled {
		t.Fatalf("expected legacy env without enabled field to default enabled, got %+v", env)
	}
}

func TestCreateBackupIncludesSelectedContentInArchive(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.Task{
		Name:    "backup-task",
		Command: "python3 backup.py",
		Status:  model.TaskStatusEnabled,
	}).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := database.DB.Create(&model.EnvVar{
		Name:    "BACKUP_ENV",
		Value:   "backup-value",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}
	disabledEnv := &model.EnvVar{
		Name:    "BACKUP_DISABLED_ENV",
		Value:   "backup-disabled-value",
		Enabled: true,
	}
	if err := database.DB.Create(disabledEnv).Error; err != nil {
		t.Fatalf("create disabled env: %v", err)
	}
	if err := database.DB.Model(disabledEnv).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable env: %v", err)
	}
	if err := model.SetConfig("panel_title", "备份标题"); err != nil {
		t.Fatalf("set panel_title: %v", err)
	}

	filePath, err := CreateBackup(BackupCreateOptions{
		Selection: BackupSelection{
			Configs: true,
			Tasks:   true,
			EnvVars: true,
		},
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open gzip backup: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	var manifest BackupManifest
	foundManifest := false
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar entry: %v", err)
		}
		if header.Name != "manifest.json" {
			continue
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read manifest body: %v", err)
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		foundManifest = true
		break
	}

	if !foundManifest {
		t.Fatal("expected manifest.json in backup archive")
	}
	if len(manifest.Data.Tasks) != 1 || manifest.Data.Tasks[0].Name != "backup-task" {
		t.Fatalf("expected selected task to be included, got %+v", manifest.Data.Tasks)
	}
	if len(manifest.Data.EnvVars) != 2 || manifest.Data.EnvVars[0].Name != "BACKUP_ENV" {
		t.Fatalf("expected selected env to be included, got %+v", manifest.Data.EnvVars)
	}
	backupEnvEnabled := map[string]bool{}
	for _, env := range manifest.Data.EnvVars {
		if env.Enabled == nil {
			t.Fatalf("expected backup env %s to include enabled field, got %+v", env.Name, manifest.Data.EnvVars)
		}
		backupEnvEnabled[env.Name] = *env.Enabled
	}
	if !backupEnvEnabled["BACKUP_ENV"] {
		t.Fatalf("expected BACKUP_ENV to export enabled=true, got %+v", manifest.Data.EnvVars)
	}
	if backupEnvEnabled["BACKUP_DISABLED_ENV"] {
		t.Fatalf("expected BACKUP_DISABLED_ENV to export enabled=false, got %+v", manifest.Data.EnvVars)
	}
	foundPanelTitle := false
	for _, cfg := range manifest.Data.Configs.SystemConfigs {
		if cfg.Key == "panel_title" && cfg.Value == "备份标题" {
			foundPanelTitle = true
			break
		}
	}
	if !foundPanelTitle {
		t.Fatalf("expected selected config panel_title to be included, got %+v", manifest.Data.Configs.SystemConfigs)
	}
}

func TestCreateBackupSkipsQuarantinedScriptEntriesInArchive(t *testing.T) {
	testutil.SetupTestEnv(t)

	normalScriptPath := filepath.Join(config.C.Data.ScriptsDir, "demo", "keep.py")
	if err := os.MkdirAll(filepath.Dir(normalScriptPath), 0o755); err != nil {
		t.Fatalf("create normal script dir: %v", err)
	}
	if err := os.WriteFile(normalScriptPath, []byte("print('keep')\n"), 0o755); err != nil {
		t.Fatalf("write normal script: %v", err)
	}

	quarantinedScriptPath := filepath.Join(config.C.Data.ScriptsDir, "%SystemDrive%", "ProgramData", "skip.py")
	if err := os.MkdirAll(filepath.Dir(quarantinedScriptPath), 0o755); err != nil {
		t.Fatalf("create quarantined script dir: %v", err)
	}
	if err := os.WriteFile(quarantinedScriptPath, []byte("print('skip')\n"), 0o755); err != nil {
		t.Fatalf("write quarantined script: %v", err)
	}

	filePath, err := CreateBackup(BackupCreateOptions{
		Selection: BackupSelection{
			Scripts: true,
		},
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open gzip backup: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	var foundNormal bool
	var foundQuarantined bool
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar entry: %v", err)
		}

		if header.Name == "files/scripts/demo/keep.py" {
			foundNormal = true
		}
		if strings.HasPrefix(header.Name, "files/scripts/%SystemDrive%/") {
			foundQuarantined = true
		}
	}

	if !foundNormal {
		t.Fatal("expected normal script to be included in backup archive")
	}
	if foundQuarantined {
		t.Fatal("expected quarantined script entry to be excluded from backup archive")
	}
}

func TestRestoreScriptFilesKeepsLiveDataWhenStageCopyFails(t *testing.T) {
	testutil.SetupTestEnv(t)

	liveScriptPath := filepath.Join(config.C.Data.ScriptsDir, "keep", "live.py")
	if err := os.MkdirAll(filepath.Dir(liveScriptPath), 0o755); err != nil {
		t.Fatalf("create live script dir: %v", err)
	}
	if err := os.WriteFile(liveScriptPath, []byte("print('live-before')\n"), 0o755); err != nil {
		t.Fatalf("write live script: %v", err)
	}

	extractedDir := t.TempDir()
	restoreScriptPath := filepath.Join(extractedDir, "files", "scripts", "keep", "restored.py")
	if err := os.MkdirAll(filepath.Dir(restoreScriptPath), 0o755); err != nil {
		t.Fatalf("create restore script dir: %v", err)
	}
	if err := os.WriteFile(restoreScriptPath, []byte("print('restored')\n"), 0o755); err != nil {
		t.Fatalf("write restore script: %v", err)
	}

	originalCopyDir := copyDirectoryContentsFunc
	t.Cleanup(func() {
		copyDirectoryContentsFunc = originalCopyDir
	})

	copyDirectoryContentsFunc = func(sourceDir, targetDir string) error {
		if err := originalCopyDir(sourceDir, targetDir); err != nil {
			return err
		}
		return errors.New("inject stage copy failure")
	}

	err := restoreScriptFiles(extractedDir, "daidai-panel")
	if err == nil || !strings.Contains(err.Error(), "inject stage copy failure") {
		t.Fatalf("expected injected stage copy failure, got %v", err)
	}

	data, err := os.ReadFile(liveScriptPath)
	if err != nil {
		t.Fatalf("read live script after failure: %v", err)
	}
	if !strings.Contains(string(data), "live-before") {
		t.Fatalf("expected live script content to stay unchanged, got %q", string(data))
	}

	if _, err := os.Stat(filepath.Join(config.C.Data.ScriptsDir, "keep", "restored.py")); !os.IsNotExist(err) {
		t.Fatalf("expected restored script to not replace live dir on failure, stat err=%v", err)
	}
}

func TestRestoreLogFilesKeepsLivePanelLogWhenStageCopyFails(t *testing.T) {
	testutil.SetupTestEnv(t)

	panelLogPath := filepath.Join(config.C.Data.Dir, "panel.log")
	if err := os.WriteFile(panelLogPath, []byte("live-panel-log-before\n"), 0o644); err != nil {
		t.Fatalf("write live panel log: %v", err)
	}

	extractedDir := t.TempDir()
	restorePanelLogPath := filepath.Join(extractedDir, "files", "panel.log")
	if err := os.MkdirAll(filepath.Dir(restorePanelLogPath), 0o755); err != nil {
		t.Fatalf("create restore panel log dir: %v", err)
	}
	if err := os.WriteFile(restorePanelLogPath, []byte("restored-panel-log\n"), 0o644); err != nil {
		t.Fatalf("write restore panel log: %v", err)
	}

	restoreLogFile := filepath.Join(extractedDir, "files", "logs", "task.log")
	if err := os.MkdirAll(filepath.Dir(restoreLogFile), 0o755); err != nil {
		t.Fatalf("create restore logs dir: %v", err)
	}
	if err := os.WriteFile(restoreLogFile, []byte("restored task log\n"), 0o644); err != nil {
		t.Fatalf("write restore log file: %v", err)
	}

	originalCopyDir := copyDirectoryContentsFunc
	t.Cleanup(func() {
		copyDirectoryContentsFunc = originalCopyDir
	})

	copyDirectoryContentsFunc = func(sourceDir, targetDir string) error {
		if err := originalCopyDir(sourceDir, targetDir); err != nil {
			return err
		}
		return errors.New("inject log stage copy failure")
	}

	err := restoreLogFiles(extractedDir, "daidai-panel")
	if err == nil || !strings.Contains(err.Error(), "inject log stage copy failure") {
		t.Fatalf("expected injected log stage copy failure, got %v", err)
	}

	data, err := os.ReadFile(panelLogPath)
	if err != nil {
		t.Fatalf("read live panel log after failure: %v", err)
	}
	if !strings.Contains(string(data), "live-panel-log-before") {
		t.Fatalf("expected live panel log to stay unchanged, got %q", string(data))
	}

	if _, err := os.Stat(filepath.Join(config.C.Data.LogDir, "task.log")); !os.IsNotExist(err) {
		t.Fatalf("expected restored log dir to not replace live dir on failure, stat err=%v", err)
	}
}
