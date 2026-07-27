package appboot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var ensureColumns = database.EnsureColumns

func SchemaFingerprint() string {
	fingerprint, err := schemaFingerprint(allModels(), database.ManualSchemaRevision)
	if err != nil {
		panic(fmt.Sprintf("build schema fingerprint: %v", err))
	}
	return fingerprint
}

func schemaFingerprint(models []interface{}, manualRevision string) (string, error) {
	descriptor, err := schemaDescriptor(models)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "manual:%s\n%s", manualRevision, descriptor)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func schemaDescriptor(models []interface{}) (string, error) {
	db, err := openSchemaDescriptorDB(schema.NamingStrategy{})
	if err != nil {
		return "", err
	}
	return schemaDescriptorWithDB(db, models)
}

func openSchemaDescriptorDB(namingStrategy schema.Namer) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true, DisableAutomaticPing: true, NamingStrategy: namingStrategy})
	if err != nil {
		return nil, fmt.Errorf("open schema descriptor database: %w", err)
	}
	return db, nil
}

func schemaDescriptorWithDB(db *gorm.DB, models []interface{}) (string, error) {
	lines := make([]string, 0)
	for _, model := range models {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return "", fmt.Errorf("parse schema model: %w", err)
		}
		parsed := statement.Schema
		lines = append(lines, "table:"+parsed.Table)
		for _, field := range parsed.Fields {
			if field.DBName == "" {
				continue
			}
			fullType := db.Migrator().FullDataTypeOf(field)
			lines = append(lines, fmt.Sprintf("column:%s:%s:%v", field.DBName, fullType.SQL, fullType.Vars))
		}
		for _, index := range parsed.ParseIndexes() {
			parts := []string{"index:" + index.Name, index.Class, index.Type, index.Where, index.Option}
			for _, field := range index.Fields {
				parts = append(parts, fmt.Sprintf("%s:%s:%s:%s:%d", field.DBName, field.Expression, field.Sort, field.Collate, field.Length))
			}
			lines = append(lines, strings.Join(parts, ":"))
		}
		for name, check := range parsed.ParseCheckConstraints() {
			lines = append(lines, fmt.Sprintf("check:%s:%s:%s", name, check.Field.DBName, check.Constraint))
		}
		for name, unique := range parsed.ParseUniqueConstraints() {
			lines = append(lines, fmt.Sprintf("unique:%s:%s", name, unique.Field.DBName))
		}
		for _, relation := range parsed.Relationships.Relations {
			constraint := relation.ParseConstraint()
			if constraint == nil {
				continue
			}
			foreignKeys := make([]string, 0, len(constraint.ForeignKeys))
			references := make([]string, 0, len(constraint.References))
			for _, field := range constraint.ForeignKeys {
				foreignKeys = append(foreignKeys, field.DBName)
			}
			for _, field := range constraint.References {
				references = append(references, field.DBName)
			}
			lines = append(lines, fmt.Sprintf("foreign:%s:%s:%s:%s:%s:%s", constraint.Name, strings.Join(foreignKeys, ","), constraint.ReferenceSchema.Table, strings.Join(references, ","), constraint.OnUpdate, constraint.OnDelete))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

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
	return InitWithConfigWriterBeforeMigrate(cfg, writer, nil)
}

// InitWithConfigWriterBeforeMigrate runs gate after opening SQLite and before
// any schema or startup data mutation.
func InitWithConfigWriterBeforeMigrate(cfg *config.Config, writer io.Writer, gate func() error) error {
	return initWithConfig(cfg, writer, true, gate)
}

func initWithConfig(cfg *config.Config, writer io.Writer, scopedWriter bool, gates ...func() error) error {
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
	if len(gates) > 0 && gates[0] != nil {
		if err := gates[0](); err != nil {
			_ = database.Close()
			return fmt.Errorf("prepare migration recovery: %w", err)
		}
	}
	if err := database.AutoMigrate(allModels()...); err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := ensureColumns(); err != nil {
		_ = database.Close()
		return fmt.Errorf("ensure database columns: %w", err)
	}

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
