package appboot

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
)

func TestInitWithConfigRunsRecoveryGateBeforeAutoMigrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "daidai.db")
	cfg := &config.Config{Database: config.DatabaseConfig{Path: dbPath}}
	gateErr := errors.New("snapshot failed")
	called := false

	err := InitWithConfigWriterBeforeMigrate(cfg, io.Discard, func() error {
		called = true
		if database.DB == nil {
			t.Fatal("recovery gate ran before database opened")
		}
		return gateErr
	})
	if !called {
		t.Fatal("recovery gate was not called")
	}
	if !errors.Is(err, gateErr) {
		t.Fatalf("error = %v, want recovery gate error", err)
	}
	if database.DB != nil {
		t.Fatal("database remained open after recovery gate failure")
	}
}

func TestInitWithConfigPropagatesEnsureColumnsFailure(t *testing.T) {
	oldEnsure := ensureColumns
	want := errors.New("alter failed")
	ensureColumns = func() error { return want }
	t.Cleanup(func() { ensureColumns = oldEnsure })
	cfg := &config.Config{Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "daidai.db")}}

	err := InitWithConfigWriter(cfg, io.Discard)
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want ensure columns failure", err)
	}
	if database.DB != nil {
		t.Fatal("database remained open after EnsureColumns failure")
	}
}

func TestSchemaFingerprintChangesWithAutoMigrateModels(t *testing.T) {
	type addedModel struct {
		ID    uint
		Value string `gorm:"size:17"`
	}
	base := SchemaFingerprint()
	changed := schemaFingerprint(append(allModels(), &addedModel{}))
	if base == changed {
		t.Fatal("schema fingerprint ignored AutoMigrate model change")
	}
}
