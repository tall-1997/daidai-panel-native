package main

import (
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestRunIPWhitelistClearRestoresOpenAccess(t *testing.T) {
	testutil.SetupTestEnv(t)
	rt := &cliRuntime{cfg: config.C}

	if err := database.DB.Create(&model.IPWhitelist{IP: "203.0.113.10", Remarks: "wrong"}).Error; err != nil {
		t.Fatalf("create whitelist: %v", err)
	}

	if err := runIPWhitelist(rt, []string{"clear"}); err != nil {
		t.Fatalf("clear whitelist: %v", err)
	}

	var count int64
	if err := database.DB.Model(&model.IPWhitelist{}).Count(&count).Error; err != nil {
		t.Fatalf("count whitelist: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty whitelist after clear, got %d", count)
	}
}

func TestRunIPWhitelistSetNormalizesEntries(t *testing.T) {
	testutil.SetupTestEnv(t)
	rt := &cliRuntime{cfg: config.C}

	if err := database.DB.Create(&model.IPWhitelist{IP: "192.0.2.10", Remarks: "old"}).Error; err != nil {
		t.Fatalf("create old whitelist: %v", err)
	}

	err := runIPWhitelist(rt, []string{"set", "203.0.113.*", "2001:db8::1", "--remarks", "terminal recovery"})
	if err != nil {
		t.Fatalf("set whitelist: %v", err)
	}

	var entries []model.IPWhitelist
	if err := database.DB.Order("id ASC").Find(&entries).Error; err != nil {
		t.Fatalf("query whitelist: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 whitelist entries, got %d: %#v", len(entries), entries)
	}
	if entries[0].IP != "203.0.113.0/24" || entries[1].IP != "2001:db8::1" {
		t.Fatalf("unexpected normalized entries: %#v", entries)
	}
	if entries[0].Remarks != "terminal recovery" || entries[1].Remarks != "terminal recovery" {
		t.Fatalf("expected remarks to be applied, got %#v", entries)
	}
}

func TestRunIPWhitelistRejectsInvalidEntry(t *testing.T) {
	testutil.SetupTestEnv(t)
	rt := &cliRuntime{cfg: config.C}

	if err := runIPWhitelist(rt, []string{"add", "not-an-ip"}); err == nil {
		t.Fatal("expected invalid whitelist entry to be rejected")
	}
}
