package database

import (
	"path/filepath"
	"testing"

	"daidai-panel/config"
)

func TestCheckpointWALAndIntegrityCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
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
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
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
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
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
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	var value string
	if err := DB.Raw("SELECT value FROM legacy_test").Scan(&value).Error; err != nil || value != "durable" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestEnsureColumnsReturnsAlterFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ensure-columns.db")
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := DB.Exec("CREATE VIEW tasks AS SELECT 1 AS pid").Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureColumns(); err == nil {
		t.Fatal("expected ALTER TABLE failure")
	}
}
