package database

import (
	"path/filepath"
	"testing"

	"daidai-panel/config"
)

func TestCheckpointWALAndIntegrityCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
	Init(&config.DatabaseConfig{Path: path})
	if err := DB.Exec("CREATE TABLE recovery_test (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec("INSERT INTO recovery_test(value) VALUES (?)", "durable").Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckpointWALAndClose(); err != nil {
		t.Fatal(err)
	}
	if DB != nil {
		t.Fatal("database remains open after checkpoint")
	}
	Init(&config.DatabaseConfig{Path: path})
	t.Cleanup(func() { _ = Close() })
	if err := IntegrityCheck(); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := DB.Raw("SELECT value FROM recovery_test").Scan(&value).Error; err != nil || value != "durable" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestCheckpointWALPathFlushesLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	Init(&config.DatabaseConfig{Path: path})
	if err := DB.Exec("CREATE TABLE legacy_test (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec("INSERT INTO legacy_test(value) VALUES (?)", "durable").Error; err != nil {
		t.Fatal(err)
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckpointWALPath(path); err != nil {
		t.Fatal(err)
	}
	Init(&config.DatabaseConfig{Path: path})
	t.Cleanup(func() { _ = Close() })
	var value string
	if err := DB.Raw("SELECT value FROM legacy_test").Scan(&value).Error; err != nil || value != "durable" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestEnsureColumnsSkipsCompatibilityViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ensure-columns.db")
	Init(&config.DatabaseConfig{Path: path})
	t.Cleanup(func() { _ = Close() })
	if err := DB.Exec("CREATE VIEW tasks AS SELECT 1 AS pid").Error; err != nil {
		t.Fatal(err)
	}

	// v3.4.0 起 EnsureColumns 无返回值、单列失败只记日志；这里断言它不 panic 且库仍可用。
	EnsureColumns()
	if err := DB.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("database unusable after EnsureColumns: %v", err)
	}
}
