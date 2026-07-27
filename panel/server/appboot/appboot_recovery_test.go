package appboot

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/service"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type fingerprintNamedTable struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"index:idx_fingerprint_value;check:value_not_empty,value <> ''" json:"ignored_name"`
}

func (fingerprintNamedTable) TableName() string { return "fingerprint_custom_table" }

type fingerprintCustomValue string

func (fingerprintCustomValue) GormDBDataType(*gorm.DB, *schema.Field) string {
	return "FINGERPRINT_CUSTOM"
}

type fingerprintCustomTypeModel struct {
	ID    uint
	Value fingerprintCustomValue
}

type fingerprintNamingModel struct {
	ID uint
}

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
	changed, err := schemaFingerprint(append(allModels(), &addedModel{}), database.ManualSchemaRevision)
	if err != nil {
		t.Fatal(err)
	}
	if base == changed {
		t.Fatal("schema fingerprint ignored AutoMigrate model change")
	}
}

func TestSchemaDescriptorUsesResolvedTableDatatypeIndexesAndConstraints(t *testing.T) {
	descriptor, err := schemaDescriptor([]interface{}{&fingerprintNamedTable{}, &fingerprintCustomTypeModel{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"table:fingerprint_custom_table",
		"FINGERPRINT_CUSTOM",
		"index:idx_fingerprint_value",
		"check:value_not_empty",
	} {
		if !strings.Contains(descriptor, want) {
			t.Fatalf("descriptor missing %q: %s", want, descriptor)
		}
	}
	if strings.Contains(descriptor, "ignored_name") {
		t.Fatalf("descriptor contains non-schema JSON tag: %s", descriptor)
	}
}

func TestSchemaFingerprintIncludesManualMigrationRevision(t *testing.T) {
	models := []interface{}{&fingerprintNamedTable{}}
	first, err := schemaFingerprint(models, "manual-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := schemaFingerprint(models, "manual-v2")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("manual migration revision did not change schema fingerprint")
	}
}

func TestSchemaDescriptorUsesConfiguredNamingStrategy(t *testing.T) {
	db, err := openSchemaDescriptorDB(schema.NamingStrategy{TablePrefix: "mobile_", SingularTable: true})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := schemaDescriptorWithDB(db, []interface{}{&fingerprintNamingModel{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(descriptor, "table:mobile_fingerprint_naming_model") {
		t.Fatalf("descriptor ignored naming strategy: %s", descriptor)
	}
}

func TestInitWithConfigPropagatesStartupMutationFailures(t *testing.T) {
	tests := []struct {
		name   string
		inject func(error) func()
	}{
		{name: "default configs", inject: func(want error) func() {
			old := initDefaultConfigs
			initDefaultConfigs = func() error { return want }
			return func() { initDefaultConfigs = old }
		}},
		{name: "legacy python migration", inject: func(want error) func() {
			old := migrateLegacyPythonVenv
			migrateLegacyPythonVenv = func() (service.LegacyPythonVenvMigration, error) { return service.LegacyPythonVenvMigration{}, want }
			return func() { migrateLegacyPythonVenv = old }
		}},
		{name: "python normalization", inject: func(want error) func() {
			old := normalizeLegacyPythonColumns
			normalizeLegacyPythonColumns = func(service.LegacyPythonVenvMigration) error { return want }
			return func() { normalizeLegacyPythonColumns = old }
		}},
		{name: "python runtime policy", inject: func(want error) func() {
			old := applyPythonRuntimePolicy
			applyPythonRuntimePolicy = func() error { return want }
			return func() { applyPythonRuntimePolicy = old }
		}},
		{name: "dependency dedupe", inject: func(want error) func() {
			old := mergePythonDependencies
			mergePythonDependencies = func() error { return want }
			return func() { mergePythonDependencies = old }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := errors.New(tt.name)
			restore := tt.inject(want)
			t.Cleanup(restore)
			cfg := &config.Config{Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "startup.db")}}
			err := InitWithConfigWriter(cfg, io.Discard)
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			if database.DB != nil {
				t.Fatal("database remains open after startup mutation failure")
			}
		})
	}
}
