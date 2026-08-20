package database

import (
	"errors"
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

func TestEnsureColumnsPropagatesHelperFailures(t *testing.T) {
	tests := []struct {
		name   string
		inject func(error) func()
	}{
		{name: "pragma", inject: func(want error) func() {
			old := existingColumns
			existingColumns = func(string) (map[string]bool, error) { return nil, want }
			return func() { existingColumns = old }
		}},
		{name: "legacy pid", inject: func(want error) func() {
			old := migrateLegacyPID
			migrateLegacyPID = func() error { return want }
			return func() { migrateLegacyPID = old }
		}},
		{name: "drop env index", inject: func(want error) func() {
			old := dropLegacyEnvIndex
			dropLegacyEnvIndex = func() error { return want }
			return func() { dropLegacyEnvIndex = old }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helpers.db")
			if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = Close() })
			if err := DB.Exec("CREATE TABLE tasks (pid INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
			want := errors.New(tt.name)
			restore := tt.inject(want)
			t.Cleanup(restore)
			if err := EnsureColumns(); !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
		})
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

func TestEnsureColumnsSkipsCompatibilityViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ensure-columns.db")
	if err := Init(&config.DatabaseConfig{Path: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := DB.Exec("CREATE VIEW tasks AS SELECT 1 AS pid").Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureColumns(); err != nil {
		t.Fatalf("compatibility view should be skipped without blocking other migrations: %v", err)
	}
}
