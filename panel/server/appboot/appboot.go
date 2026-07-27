package appboot

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
)

// ResolveConfigPath 查找 config.yaml，覆盖 Docker / 二进制 / Windows 双击 / cwd 漂移等场景。
// 顺序：
//  1. DAIDAI_CONFIG 环境变量
//  2. /app/config.yaml（Docker 镜像固定位置）
//  3. 当前可执行文件同目录（Windows 双击 / 二进制从其他 cwd 启动也能找到）
//  4. cwd 下的 config.yaml（兼容历史行为）
func ResolveConfigPath() string {
	candidates := []string{
		os.Getenv("DAIDAI_CONFIG"),
		"/app/config.yaml",
	}
	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), "config.yaml"))
	}
	candidates = append(candidates, "config.yaml")

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return "config.yaml"
}

func LoadAndInit(configPath string) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if err := InitWithConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func InitWithConfig(cfg *config.Config) error {
	return initWithConfig(cfg, nil, false)
}

func InitWithConfigWriter(cfg *config.Config, writer io.Writer) error {
	return initWithConfig(cfg, writer, true)
}

func initWithConfig(cfg *config.Config, writer io.Writer, scopedWriter bool) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}
	config.C = cfg

	var initDatabaseErr error
	if scopedWriter {
		if writer == nil {
			writer = log.Writer()
		}
		previousWriter := log.Writer()
		log.SetOutput(writer)
		defer log.SetOutput(previousWriter)
		initDatabaseErr = database.InitWithWriter(&cfg.Database, writer)
	} else {
		initDatabaseErr = database.Init(&cfg.Database)
	}
	if initDatabaseErr != nil {
		return fmt.Errorf("initialize database: %w", initDatabaseErr)
	}
	if err := database.AutoMigrate(allModels()...); err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate database: %w", err)
	}
	database.EnsureColumns()

	legacyPythonVenvMigration := service.MigrateLegacyManagedPythonVenvInfo()

	model.InitDefaultConfigs()
	if err := service.ApplyRegisteredPanelTimezone(); err != nil {
		_ = database.Close()
		return fmt.Errorf("failed to apply panel timezone: %w", err)
	}
	service.NormalizeLegacyPythonVersionColumnsAfterVenvMigration(legacyPythonVenvMigration)
	service.ApplySinglePythonRuntimePolicyOnStartup()
	service.MergeDuplicatePythonDependencies()
	if err := middleware.ConfigureTrustedProxyCIDRs(model.GetRegisteredConfig("trusted_proxy_cidrs")); err != nil {
		_ = database.Close()
		return fmt.Errorf("failed to configure trusted proxies: %w", err)
	}

	return nil
}

func allModels() []interface{} {
	return []interface{}{
		&model.User{},
		&model.TokenBlocklist{},
		&model.Task{},
		&model.TaskLog{},
		&model.SystemConfig{},
		&model.EnvVar{},
		&model.ScriptVersion{},
		&model.Subscription{},
		&model.SubLog{},
		&model.NotifyChannel{},
		&model.SSHKey{},
		&model.LoginLog{},
		&model.LoginAttempt{},
		&model.UserSession{},
		&model.IPWhitelist{},
		&model.SecurityAudit{},
		&model.TwoFactorAuth{},
		&model.OpenApp{},
		&model.ApiCallLog{},
		&model.Platform{},
		&model.PlatformToken{},
		&model.PlatformTokenLog{},
		&model.Dependency{},
		&model.TaskView{},
	}
}
