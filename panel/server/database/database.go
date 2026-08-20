package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"daidai-panel/config"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// ManualSchemaRevision changes whenever EnsureColumns or manual index migration semantics change.
const ManualSchemaRevision = "ensure-columns-v2"

var closeSQLDB = func(db *sql.DB) error {
	return db.Close()
}

var (
	existingColumns    = getExistingColumns
	migrateLegacyPID   = migrateLegacyTaskPIDColumn
	dropLegacyEnvIndex = dropEnvVarUniqueIndex
)

func Init(cfg *config.DatabaseConfig) error {
	return InitWithWriter(cfg, os.Stdout)
}

func InitWithWriter(cfg *config.DatabaseConfig, writer io.Writer) error {
	dbPath := cfg.Path
	if dbPath == "" {
		dbPath = "./data/daidai.db"
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	customLogger := logger.New(
		log.New(writer, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200000000,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: customLogger,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			_ = sqlDB.Close()
			return fmt.Errorf("configure database: %w", err)
		}
	}

	DB = db
	log.Printf("database connected")
	return nil
}

func AutoMigrate(models ...interface{}) error {
	if DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := DB.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func Close() error {
	if DB == nil {
		return nil
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("get sql database: %w", err)
	}
	if err := closeSQLDB(sqlDB); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	DB = nil
	return nil
}

// CheckpointWALAndClose flushes the WAL into the database before a filesystem snapshot.
func CheckpointWALAndClose() error {
	if DB == nil {
		return nil
	}
	if err := DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return fmt.Errorf("checkpoint database WAL: %w", err)
	}
	return Close()
}

// CheckpointWALPath flushes a legacy flat SQLite database before first import.
func CheckpointWALPath(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat database for checkpoint: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open database for checkpoint: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get checkpoint database: %w", err)
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		_ = sqlDB.Close()
		return fmt.Errorf("checkpoint database WAL: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close checkpoint database: %w", err)
	}
	return nil
}

// IntegrityCheck verifies the complete SQLite database after migration.
func IntegrityCheck() error {
	if DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	var result string
	if err := DB.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return fmt.Errorf("check database integrity: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database integrity check failed")
	}
	return nil
}

type columnDef struct {
	Name    string
	SQLType string
}

func getExistingColumns(table string) (map[string]bool, error) {
	cols := make(map[string]bool)
	type pragmaRow struct {
		Name string
	}
	var rows []pragmaRow
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if err := DB.Raw(fmt.Sprintf("PRAGMA table_info(%s)", table)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("inspect table %s columns: %w", table, err)
	}
	for _, r := range rows {
		cols[strings.ToLower(r.Name)] = true
	}
	return cols, nil
}

func ensureTableColumns(table string, columns []columnDef) error {
	var objectType string
	if err := DB.Raw("SELECT type FROM sqlite_master WHERE name = ? LIMIT 1", table).Scan(&objectType).Error; err == nil && objectType == "view" {
		return nil
	}
	existing, err := existingColumns(table)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}
	for _, col := range columns {
		lookupName := strings.ToLower(strings.Trim(col.Name, "\""))
		if !existing[lookupName] {
			sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col.Name, col.SQLType)
			if err := DB.Exec(sql).Error; err != nil {
				return fmt.Errorf("add column %s.%s: %w", table, col.Name, err)
			} else {
				log.Printf("added missing column: %s.%s", table, col.Name)
			}
		}
	}
	return nil
}

func EnsureColumns() error {
	if err := ensureTableColumns("tasks", []columnDef{
		{"pid", "INTEGER"},
		{"log_path", "VARCHAR(256)"},
		{"last_running_time", "REAL"},
		{"task_before", "TEXT"},
		{"task_after", "TEXT"},
		{"task_type", "VARCHAR(16) DEFAULT 'cron'"},
		{"last_startup_auto_run_date", "VARCHAR(10) DEFAULT ''"},
		{"allow_multiple_instances", "BOOLEAN DEFAULT 0"},
		{"schedule_policy", "VARCHAR(16) NOT NULL DEFAULT 'skip'"},
		{"timeout", "INTEGER DEFAULT 0"},
		{"success_exit_codes", "VARCHAR(128) NOT NULL DEFAULT '0'"},
		{"random_delay_seconds", "INTEGER"},
		{"max_retries", "INTEGER DEFAULT 0"},
		{"retry_interval", "INTEGER DEFAULT 60"},
		{"notify_on_failure", "BOOLEAN DEFAULT 0"},
		{"notify_on_success", "BOOLEAN DEFAULT 0"},
		{"notify_on_abort", "BOOLEAN DEFAULT 0"},
		{"notification_channel_id", "INTEGER"},
		{"depends_on", "INTEGER"},
		{"sort_order", "INTEGER DEFAULT 0"},
		{"is_pinned", "BOOLEAN DEFAULT 0"},
		{"python_version", "VARCHAR(16) DEFAULT ''"},
	}); err != nil {
		return err
	}
	if err := migrateLegacyPID(); err != nil {
		return err
	}

	if err := ensureTableColumns("task_logs", []columnDef{
		{"log_path", "VARCHAR(256)"},
		{"duration", "REAL"},
		{"started_at", "DATETIME"},
		{"ended_at", "DATETIME"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("env_vars", []columnDef{
		{"position", "REAL DEFAULT 10000.0"},
		{"sort_order", "INTEGER DEFAULT 0"},
		{"\"group\"", "VARCHAR(512) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("subscriptions", []columnDef{
		{"save_dir", "VARCHAR(512) DEFAULT ''"},
		{"ssh_key_id", "INTEGER"},
		{"auth_type", "VARCHAR(16) DEFAULT ''"},
		{"auth_token", "TEXT DEFAULT ''"},
		{"alias", "VARCHAR(128) DEFAULT ''"},
		{"auto_add_task", "BOOLEAN DEFAULT 0"},
		{"auto_del_task", "BOOLEAN DEFAULT 0"},
		{"whitelist", "VARCHAR(512) DEFAULT ''"},
		{"blacklist", "VARCHAR(512) DEFAULT ''"},
		{"depend_on", "VARCHAR(512) DEFAULT ''"},
		{"pre_script", "TEXT DEFAULT ''"},
		{"hook_script", "TEXT DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("notify_channels", []columnDef{
		{"today_send_count", "INTEGER DEFAULT 0"},
		{"today_send_date", "VARCHAR(10) DEFAULT ''"},
		{"last_test_at", "DATETIME"},
		{"last_test_status", "VARCHAR(16) DEFAULT ''"},
		{"push_scope", "VARCHAR(16) NOT NULL DEFAULT 'default'"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("open_apps", []columnDef{
		{"rate_limit", "INTEGER DEFAULT 0"},
		{"call_count", "INTEGER DEFAULT 0"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("api_call_logs", []columnDef{
		{"app_name", "VARCHAR(128)"},
		{"duration", "REAL DEFAULT 0"},
		{"service", "VARCHAR(128) DEFAULT ''"},
		{"service_user", "VARCHAR(128) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("sub_logs", []columnDef{
		{"log_cursor", "INTEGER DEFAULT 0"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("login_logs", []columnDef{
		{"method", "VARCHAR(32) DEFAULT '密码登录'"},
		{"client_name", "VARCHAR(255) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("user_sessions", []columnDef{
		{"refresh_jti", "VARCHAR(36)"},
		{"refresh_expires_at", "DATETIME"},
		{"client_type", "VARCHAR(16) DEFAULT 'web'"},
		{"client_name", "VARCHAR(255) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("task_views", []columnDef{
		{"hidden", "BOOLEAN DEFAULT 0"},
		{"sort_order", "INTEGER DEFAULT 0"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("dependencies", []columnDef{
		{"python_version", "VARCHAR(16) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := ensureTableColumns("users", []columnDef{
		{"avatar_url", "VARCHAR(512) DEFAULT ''"},
	}); err != nil {
		return err
	}

	if err := dropLegacyEnvIndex(); err != nil {
		return err
	}

	log.Printf("column check completed")
	return nil
}

// migrateLegacyTaskPIDColumn copies values from the old GORM-derived p_id column
// into pid. The Task.PID field is now explicitly mapped to pid, but older local
// SQLite databases may still contain p_id from previous AutoMigrate runs.
func migrateLegacyTaskPIDColumn() error {
	existing, err := getExistingColumns("tasks")
	if err != nil {
		return err
	}
	if !existing["p_id"] || !existing["pid"] {
		return nil
	}
	if err := DB.Exec("UPDATE tasks SET pid = p_id WHERE pid IS NULL AND p_id IS NOT NULL").Error; err != nil {
		return fmt.Errorf("migrate legacy tasks.p_id: %w", err)
	}
	return nil
}

// dropEnvVarUniqueIndex 迁移：青龙化后 (name, remarks) 不再是业务唯一键，
// 旧部署里如果残留了 idx_env_vars_name_remarks 唯一索引，需要清理掉，
// 否则写入端放开后 DB 层仍会拒绝同 (name, remarks) 的新增。幂等操作。
func dropEnvVarUniqueIndex() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}
	if _, err := DB.DB(); err != nil {
		return fmt.Errorf("get sql database: %w", err)
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_env_vars_name_remarks'").Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect legacy env index: %w", err)
	}
	if count == 0 {
		return nil
	}
	if err := DB.Exec(`DROP INDEX IF EXISTS idx_env_vars_name_remarks`).Error; err != nil {
		return fmt.Errorf("drop legacy env index: %w", err)
	}
	log.Printf("dropped legacy unique index env_vars(name, remarks) to allow qinglong-style multi-account inserts")
	return nil
}
