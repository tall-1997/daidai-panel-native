package netutil

import "testing"

func TestNormalizeIPWhitelistEntry(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":     "203.0.113.7",
		"203.0.113.0/24":  "203.0.113.0/24",
		"203.0.113.*":     "203.0.113.0/24",
		"203.0.*.*":       "203.0.0.0/16",
		"2001:db8::1":     "2001:db8::1",
		"2001:db8::/64":   "2001:db8::/64",
		" 203.0.113.10  ": "203.0.113.10",
		" 203.0.113.*  ":  "203.0.113.0/24",
	}

	for input, want := range cases {
		got, err := NormalizeIPWhitelistEntry(input)
		if err != nil {
			t.Fatalf("NormalizeIPWhitelistEntry(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeIPWhitelistEntry(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeIPWhitelistEntryRejectsInvalidInput(t *testing.T) {
	invalidInputs := []string{
		"",
		"abc",
		"203.0.113.999",
		"203.*.113.*",
		"*.*.*.*",
		"203.0.113.0/99",
	}

	for _, input := range invalidInputs {
		if _, err := NormalizeIPWhitelistEntry(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}

func TestMatchIPWhitelistEntry(t *testing.T) {
	cases := []struct {
		entry    string
		clientIP string
		want     bool
	}{
		{entry: "203.0.113.7", clientIP: "203.0.113.7", want: true},
		{entry: "203.0.113.7", clientIP: "203.0.113.8", want: false},
		{entry: "203.0.113.0/24", clientIP: "203.0.113.99", want: true},
		{entry: "203.0.113.0/24", clientIP: "203.0.114.1", want: false},
		{entry: "203.0.113.*", clientIP: "203.0.113.254", want: true},
		{entry: "203.0.*.*", clientIP: "203.0.7.9", want: true},
		{entry: "2001:db8::/64", clientIP: "2001:db8::1234", want: true},
		{entry: "2001:db8::1", clientIP: "2001:db8::2", want: false},
	}

	for _, tc := range cases {
		if got := MatchIPWhitelistEntry(tc.entry, tc.clientIP); got != tc.want {
			t.Fatalf("MatchIPWhitelistEntry(%q, %q) = %v, want %v", tc.entry, tc.clientIP, got, tc.want)
		}
	}
}
