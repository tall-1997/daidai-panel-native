package config

import (
	"os"
	"path/filepath"
	"testing"
)

// #162：SERVER_HOST 照 SERVER_PORT 的写法覆盖 config.yaml 里的 server.host
func TestLoadServerHostEnvOverridesYAML(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	body := `server:
  host: 0.0.0.0
  port: 5701
jwt:
  secret: unit-test-secret
data:
  dir: ./data
  scripts_dir: ./data/scripts
  log_dir: ./data/logs
database:
  path: ./data/test.db
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Cleanup(func() { C = nil })
	t.Setenv("SERVER_HOST", "")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("yaml host = %q, want 0.0.0.0", cfg.Server.Host)
	}

	t.Setenv("SERVER_HOST", "127.0.0.1")
	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("env host = %q, want 127.0.0.1", cfg.Server.Host)
	}
}

func TestServerConfigListenAndSelfHost(t *testing.T) {
	cases := []struct {
		host, listen, specific, self string
	}{
		{"", ":5701", "", "127.0.0.1"},
		{"0.0.0.0", "0.0.0.0:5701", "", "127.0.0.1"},
		{"::", "[::]:5701", "", "127.0.0.1"},
		{"[::]", "[::]:5701", "", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1:5701", "127.0.0.1", "127.0.0.1"},
		{"localhost", "localhost:5701", "localhost", "127.0.0.1"},
		{"::1", "[::1]:5701", "::1", "::1"},
		{"[::1]", "[::1]:5701", "::1", "::1"},
		{"192.168.1.10", "192.168.1.10:5701", "192.168.1.10", "192.168.1.10"},
		{"fd00::10", "[fd00::10]:5701", "fd00::10", "fd00::10"},
	}
	for _, c := range cases {
		s := ServerConfig{Host: c.host, Port: 5701}
		if got := s.ListenAddr(); got != c.listen {
			t.Errorf("ListenAddr(%q) = %q, want %q", c.host, got, c.listen)
		}
		if got := s.SpecificHost(); got != c.specific {
			t.Errorf("SpecificHost(%q) = %q, want %q", c.host, got, c.specific)
		}
		if got := s.SelfHost(); got != c.self {
			t.Errorf("SelfHost(%q) = %q, want %q", c.host, got, c.self)
		}
	}
}
