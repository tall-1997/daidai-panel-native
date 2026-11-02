package main

import (
	"testing"

	"daidai-panel/config"
)

// #162：ddp 的健康检查与 MCP 默认地址跟着 server.host 走
func TestBackendBaseURLFollowsServerHost(t *testing.T) {
	cases := map[string]string{
		"":             "http://127.0.0.1:5701",
		"0.0.0.0":      "http://127.0.0.1:5701",
		"127.0.0.1":    "http://127.0.0.1:5701",
		"192.168.1.10": "http://192.168.1.10:5701",
		"fd00::10":     "http://[fd00::10]:5701",
	}
	for host, want := range cases {
		rt := &cliRuntime{cfg: &config.Config{Server: config.ServerConfig{Host: host, Port: 5701}}}
		if got := rt.backendBaseURL(); got != want {
			t.Errorf("host %q: got %q, want %q", host, got, want)
		}
	}
}
