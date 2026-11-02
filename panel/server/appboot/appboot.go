package appboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var (
	ensureColumns = func() error {
		database.EnsureColumns()
		return nil
	}
	initDefaultConfigs = func() error {
		model.InitDefaultConfigs()
		return nil
	}
	migrateLegacyPythonVenv = func() (service.LegacyPythonVenvMigration, error) {
		return service.MigrateLegacyManagedPythonVenvInfo(), nil
	}
	normalizeLegacyPythonColumns = service.NormalizeLegacyPythonVersionColumnsAfterVenvMigration
	applyPythonRuntimePolicy     = service.ApplySinglePythonRuntimePolicyOnStartup
	mergePythonDependencies      = service.MergeDuplicatePythonDependencies
)

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
	type schemaEntry struct {
		Table   string `json:"table"`
		Kind    string `json:"kind"`
		Payload string `json:"payload"`
	}
	entries := make([]schemaEntry, 0)
	add := func(table, kind, payload string) {
		entries = append(entries, schemaEntry{Table: table, Kind: kind, Payload: payload})
	}
	for _, model := range models {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return "", fmt.Errorf("parse schema model: %w", err)
		}
		parsed := statement.Schema
		add(parsed.Table, "table", parsed.Table)
		for _, field := range parsed.Fields {
			if field.DBName == "" {
				continue
			}
			fullType := db.Migrator().FullDataTypeOf(field)
			add(parsed.Table, "column", fmt.Sprintf("%s:%s:%v", field.DBName, fullType.SQL, fullType.Vars))
			add(parsed.Table, "field", fmt.Sprintf(
				"field:%s:data=%s:gormData=%s:primary=%t:autoIncrement=%t:autoIncrementIncrement=%d:hasDefault=%t:default=%s:notNull=%t:unique=%t:uniqueIndex=%s:comment=%s:size=%d:precision=%d:scale=%d:ignoreMigration=%t",
				field.DBName, field.DataType, field.GORMDataType, field.PrimaryKey, field.AutoIncrement,
				field.AutoIncrementIncrement, field.HasDefaultValue, field.DefaultValue, field.NotNull,
				field.Unique, field.UniqueIndex, field.Comment, field.Size, field.Precision, field.Scale,
				field.IgnoreMigration,
			))
		}
		primaryFields := make([]string, 0, len(parsed.PrimaryFields))
		for position, field := range parsed.PrimaryFields {
			primaryFields = append(primaryFields, fmt.Sprintf("%d:%s:%s", position, field.DBName, field.TagSettings["PRIORITY"]))
		}
		add(parsed.Table, "primary", strings.Join(primaryFields, ","))
		for _, index := range parsed.ParseIndexes() {
			parts := []string{"index:" + index.Name, index.Class, index.Type, index.Where, index.Option}
			for _, field := range index.Fields {
				parts = append(parts, fmt.Sprintf("%s:%s:%s:%s:%d", field.DBName, field.Expression, field.Sort, field.Collate, field.Length))
			}
			add(parsed.Table, "index", strings.Join(parts, ":"))
		}
		for name, check := range parsed.ParseCheckConstraints() {
			add(parsed.Table, "check", fmt.Sprintf("%s:%s:%s", name, check.Field.DBName, check.Constraint))
		}
		for name, unique := range parsed.ParseUniqueConstraints() {
			add(parsed.Table, "unique", fmt.Sprintf("%s:%s", name, unique.Field.DBName))
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
			add(parsed.Table, "foreign", fmt.Sprintf("owner=%s:name=%s:keys=%s:reference=%s:references=%s:update=%s:delete=%s", parsed.Table, constraint.Name, strings.Join(foreignKeys, ","), constraint.ReferenceSchema.Table, strings.Join(references, ","), constraint.OnUpdate, constraint.OnDelete))
		}
	}
	lines := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return "", err
		}
		lines = append(lines, string(data))
	}
	sort.Strings(lines)
	return "schema-descriptor-v2\n" + strings.Join(lines, "\n"), nil
}

// ResolveConfigPath 查找 config.yaml，覆盖 Docker / 二进制 / Windows 双击 / cwd 漂移等场景。
// 顺序：
//  1. DAIDAI_CONFIG 环境变量
//  2. /app/config.yaml（Docker 镜像固定位置）
//  3. 当前可执行文件同目录（Windows 双击 / 二进制从其他 cwd 启动也能找到）
//  4. cwd 下的 config.yaml（兼容历史行为）
//
// TaskLogQueryIndexNames 是 v3.3.4（issue #153）给 task_logs 补的三个查询索引名。
// 启动时只拿它判断「老库还缺不缺这几个索引」，决定要不要打那两行补建日志；索引本身由 AutoMigrate 按 tag 建。
// 必须与 model/task_log.go 的 tag 一致，迁移测试兜底（database/task_log_indexes_migration_test.go）：
// 名字一旦对不上，HasIndex 永远判「缺」，以后每次启动都会误打「正在补建」。导出只是为了让那条测试能核对它。
var TaskLogQueryIndexNames = []string{
	"idx_task_logs_created_at_status",
	"idx_task_logs_started_at_status",
	"idx_task_logs_task_id_started_at",
}

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
		database.Init(&cfg.Database)
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
	if strings.TrimSpace(cfg.Data.Dir) != "" {
		if err := service.InitializeRuntimeSecurity(cfg.Data.Dir); err != nil {
			_ = database.Close()
			return fmt.Errorf("initialize runtime security: %w", err)
		}
	}
	// 必须排在 AutoMigrate 之前：AutoMigrate 建唯一索引失败会直接 log.Fatalf，
	// 老库里的同名数据要先改名让路，否则升级后面板起不来。
	database.DeduplicateBeforeUniqueIndex()

	// 老库升级后第一次启动时，AutoMigrate 要给 task_logs 补建三个查询索引（issue #153）。
	// 每建一个约等于把整张执行日志表连同日志正文读一遍，日志多的库要等好几秒甚至更久；
	// 这一步在 HTTP 服务起来之前同步执行，GORM 建索引又全程不打日志，用户只会看到「升级后面板半天打不开」。
	// 所以只在「表已存在、且至少缺一个索引」时前后各打一行。全新库（表还不存在）建表时顺带建索引、很快，不打。
	taskLogIndexMissing := false
	if database.DB.Migrator().HasTable(&model.TaskLog{}) {
		for _, name := range TaskLogQueryIndexNames {
			if !database.DB.Migrator().HasIndex(&model.TaskLog{}, name) {
				taskLogIndexMissing = true
				break
			}
		}
	}
	migrateStartedAt := time.Now()
	if taskLogIndexMissing {
		log.Printf("正在为执行日志补建查询索引（v3.3.4，仅升级后首次启动需要，日志越多耗时越长）...")
	}
	if err := database.AutoMigrate(allModels()...); err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate database: %w", err)
	}
	if taskLogIndexMissing {
		log.Printf("执行日志查询索引补建完成，耗时 %s", time.Since(migrateStartedAt).Round(time.Millisecond))
	}
	if err := ensureColumns(); err != nil {
		_ = database.Close()
		return fmt.Errorf("ensure database columns: %w", err)
	}

	legacyPythonVenvMigration, err := migrateLegacyPythonVenv()
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate legacy python environment: %w", err)
	}

	if err := initDefaultConfigs(); err != nil {
		_ = database.Close()
		return fmt.Errorf("initialize default configs: %w", err)
	}
	if err := service.ApplyRegisteredPanelTimezone(); err != nil {
		_ = database.Close()
		return fmt.Errorf("failed to apply panel timezone: %w", err)
	}
	if err := normalizeLegacyPythonColumns(legacyPythonVenvMigration); err != nil {
		_ = database.Close()
		return fmt.Errorf("normalize legacy python columns: %w", err)
	}
	if err := applyPythonRuntimePolicy(); err != nil {
		_ = database.Close()
		return fmt.Errorf("apply python runtime policy: %w", err)
	}
	// 必须排在合并重复依赖之前：迁移会让旧版本的依赖与当前版本的同名依赖撞到一起，交给下一行合并。
	service.ApplyMagiskPythonRuntimeMigrationOnStartup()
	if err := mergePythonDependencies(); err != nil {
		_ = database.Close()
		return fmt.Errorf("merge python dependencies: %w", err)
	}
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
		// 执行趋势里已删日志的按天计数（#158，v3.3.6）。新表，存量库 AutoMigrate 时建出来即可，不补列、不回填。
		&model.TaskLogDailyStat{},
		&model.ScheduleInstance{},
		&model.Operation{},
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
		&model.UserPreference{},
		&model.OpenApp{},
		&model.ApiCallLog{},
		&model.Platform{},
		&model.PlatformToken{},
		&model.PlatformTokenLog{},
		&model.Dependency{},
		&model.TaskView{},
		// 企业微信回调接入配置（issue #145，v3.3.2）。新表，存量库 AutoMigrate 时建出来即可，
		// 一行都没有时所有企业微信入口都查不到配置直接 404，行为与升级前一致。
		&model.WecomTrigger{},
	}
}
