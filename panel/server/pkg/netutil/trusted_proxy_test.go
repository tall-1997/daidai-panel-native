package netutil

import "testing"

func TestNormalizeTrustedProxyCIDRsUsesCanonicalCIDRs(t *testing.T) {
	got, err := NormalizeTrustedProxyCIDRs("127.0.0.1, 203.0.113.10; 2001:db8::1")
	if err != nil {
		t.Fatalf("normalize trusted proxies: %v", err)
	}

	want := "127.0.0.1/32\n203.0.113.10/32\n2001:db8::1/128"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseTrustedProxyCIDRsFallsBackToDefaultsOnBlank(t *testing.T) {
	got, err := ParseTrustedProxyCIDRs(" \n\t")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}

	defaults := DefaultTrustedProxyCIDRs()
	if len(got) != len(defaults) {
		t.Fatalf("expected %d defaults, got %d", len(defaults), len(got))
	}
	for i := range defaults {
		if got[i] != defaults[i] {
			t.Fatalf("expected default %q at %d, got %q", defaults[i], i, got[i])
		}
	}
}

func TestParseTrustedProxyCIDRsRejectsInvalidValue(t *testing.T) {
	if _, err := ParseTrustedProxyCIDRs("invalid-cidr"); err == nil {
		t.Fatal("expected invalid trusted proxy cidr to be rejected")
	}
}
