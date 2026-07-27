package service

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestIsIPWhitelistedSupportsIPRanges(t *testing.T) {
	testutil.SetupTestEnv(t)

	if !IsIPWhitelisted("203.0.113.5") {
		t.Fatal("expected empty whitelist to allow all IPs")
	}

	entries := []model.IPWhitelist{
		{IP: "203.0.113.7", Remarks: "single"},
		{IP: "198.51.100.0/24", Remarks: "cidr"},
		{IP: "192.0.2.0/24", Remarks: "wildcard-normalized"},
	}
	for _, entry := range entries {
		if err := database.DB.Create(&entry).Error; err != nil {
			t.Fatalf("create whitelist entry: %v", err)
		}
	}

	cases := []struct {
		ip   string
		want bool
	}{
		{ip: "203.0.113.7", want: true},
		{ip: "198.51.100.42", want: true},
		{ip: "192.0.2.88", want: true},
		{ip: "203.0.113.8", want: false},
		{ip: "198.51.101.1", want: false},
		{ip: "invalid-ip", want: false},
	}

	for _, tc := range cases {
		if got := IsIPWhitelisted(tc.ip); got != tc.want {
			t.Fatalf("IsIPWhitelisted(%q) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}
