package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestResolveClientIPFromRequestUsesForwardedIPFromTrustedLocalProxy(t *testing.T) {
	if err := ConfigureTrustedProxyCIDRs(""); err != nil {
		t.Fatalf("reset trusted proxies: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/ping", nil)
	req.RemoteAddr = "192.168.1.2:34567"
	req.Header.Set("X-Forwarded-For", "198.51.100.45, 10.0.0.2")

	if got := ResolveClientIPFromRequest(req); got != "198.51.100.45" {
		t.Fatalf("expected forwarded public IP, got %q", got)
	}
}

func TestResolveClientIPFromRequestIgnoresSpoofedForwardedIPFromPublicRemote(t *testing.T) {
	if err := ConfigureTrustedProxyCIDRs(""); err != nil {
		t.Fatalf("reset trusted proxies: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/ping", nil)
	req.RemoteAddr = "203.0.113.9:45678"
	req.Header.Set("X-Forwarded-For", "198.51.100.45")

	if got := ResolveClientIPFromRequest(req); got != "203.0.113.9" {
		t.Fatalf("expected direct public remote IP, got %q", got)
	}
}

func TestResolveClientIPFromRequestFallsBackToRealIPHeader(t *testing.T) {
	if err := ConfigureTrustedProxyCIDRs(""); err != nil {
		t.Fatalf("reset trusted proxies: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/ping", nil)
	req.RemoteAddr = "127.0.0.1:23456"
	req.Header.Set("X-Real-IP", "198.51.100.88")

	if got := ResolveClientIPFromRequest(req); got != "198.51.100.88" {
		t.Fatalf("expected X-Real-IP to be used, got %q", got)
	}
}

func TestResolveClientIPFromRequestUsesConfiguredPublicTrustedProxy(t *testing.T) {
	if err := ConfigureTrustedProxyCIDRs("203.0.113.0/24"); err != nil {
		t.Fatalf("configure trusted proxies: %v", err)
	}
	t.Cleanup(func() {
		_ = ConfigureTrustedProxyCIDRs("")
	})

	req := httptest.NewRequest("GET", "http://example.com/ping", nil)
	req.RemoteAddr = "203.0.113.9:34567"
	req.Header.Set("X-Forwarded-For", "198.51.100.45, 203.0.113.20")

	if got := ResolveClientIPFromRequest(req); got != "198.51.100.45" {
		t.Fatalf("expected forwarded client IP behind configured trusted proxy, got %q", got)
	}
}
