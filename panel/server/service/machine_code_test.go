package service

import (
	"regexp"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// Matches an RFC 4122 v4 UUID in uppercase form, e.g. A1B2C3D4-E5F6-4789-ABCD-1234567890AB.
var machineCodeFormatRe = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`)

func TestEnsureMachineCodeGeneratesAndPersists(t *testing.T) {
	testutil.SetupTestEnv(t)
	ResetMachineCodeCacheForTest()

	first := EnsureMachineCode()
	if !machineCodeFormatRe.MatchString(first) {
		t.Fatalf("expected uppercase UUID v4 machine code, got %q", first)
	}

	second := EnsureMachineCode()
	if second != first {
		t.Fatalf("expected repeated calls to return the same code, got %q vs %q", first, second)
	}

	var row model.SystemConfig
	if err := database.DB.Where("`key` = ?", "machine_code").First(&row).Error; err != nil {
		t.Fatalf("query persisted machine_code row: %v", err)
	}
	if row.Value != first {
		t.Fatalf("expected persisted value %q, got %q", first, row.Value)
	}
}

func TestEnsureMachineCodeSurvivesCacheResetAndReturnsStoredValue(t *testing.T) {
	testutil.SetupTestEnv(t)
	ResetMachineCodeCacheForTest()

	original := EnsureMachineCode()

	// Simulate a process restart: drop in-memory cache, keep DB row.
	ResetMachineCodeCacheForTest()

	reloaded := EnsureMachineCode()
	if reloaded != original {
		t.Fatalf("expected stored code to survive cache reset, got %q originally, %q after reset", original, reloaded)
	}
}

func TestEnsureMachineCodeFillsBlankExistingRow(t *testing.T) {
	testutil.SetupTestEnv(t)
	ResetMachineCodeCacheForTest()

	blank := model.SystemConfig{Key: "machine_code", Value: "", Description: "blank"}
	if err := database.DB.Create(&blank).Error; err != nil {
		t.Fatalf("seed blank row: %v", err)
	}

	code := EnsureMachineCode()
	if !machineCodeFormatRe.MatchString(code) {
		t.Fatalf("expected generated code for blank row, got %q", code)
	}

	var row model.SystemConfig
	if err := database.DB.Where("`key` = ?", "machine_code").First(&row).Error; err != nil {
		t.Fatalf("query row: %v", err)
	}
	if row.Value != code {
		t.Fatalf("expected blank row filled with %q, got %q", code, row.Value)
	}
}

func TestEnsureMachineCodeMigratesLegacyValue(t *testing.T) {
	testutil.SetupTestEnv(t)
	ResetMachineCodeCacheForTest()

	legacyValue := "0123-4567-89AB-CDEF-1032-5476-98BA-DCFE"
	if machineCodeFormatRe.MatchString(legacyValue) {
		t.Fatalf("test setup error: legacy value %q unexpectedly matches UUID v4 pattern", legacyValue)
	}
	legacy := model.SystemConfig{Key: "machine_code", Value: legacyValue, Description: "legacy"}
	if err := database.DB.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	code := EnsureMachineCode()
	if !machineCodeFormatRe.MatchString(code) {
		t.Fatalf("expected migrated code to be uppercase UUID v4, got %q", code)
	}
	if code == legacyValue {
		t.Fatalf("expected legacy value to be replaced, still got %q", code)
	}

	var row model.SystemConfig
	if err := database.DB.Where("`key` = ?", "machine_code").First(&row).Error; err != nil {
		t.Fatalf("query row: %v", err)
	}
	if row.Value != code {
		t.Fatalf("expected persisted value %q after migration, got %q", code, row.Value)
	}
}

func TestGenerateMachineCodeFormat(t *testing.T) {
	first := generateMachineCode()
	if !machineCodeFormatRe.MatchString(first) {
		t.Fatalf("generateMachineCode: expected uppercase UUID v4, got %q", first)
	}

	second := generateMachineCode()
	if !machineCodeFormatRe.MatchString(second) {
		t.Fatalf("generateMachineCode: expected uppercase UUID v4 on second call, got %q", second)
	}

	if first == second {
		t.Fatalf("generateMachineCode should produce distinct random values, got duplicate %q", first)
	}
}
