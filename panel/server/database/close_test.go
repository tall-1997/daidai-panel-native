package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"daidai-panel/config"
)

func TestCloseKeepsReferenceWhenUnderlyingCloseFails(t *testing.T) {
	if err := Init(&config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "close.db")}); err != nil {
		t.Fatal(err)
	}
	original := DB
	oldClose := closeSQLDB
	closeSQLDB = func(*sql.DB) error { return errors.New("close failed") }
	t.Cleanup(func() {
		closeSQLDB = oldClose
		_ = Close()
	})

	if err := Close(); err == nil {
		t.Fatal("expected close error")
	}
	if DB != original {
		t.Fatal("database reference changed after close failure")
	}
}
