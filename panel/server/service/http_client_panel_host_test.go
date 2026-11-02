package service

import (
	"strings"
	"testing"

	"daidai-panel/config"
)

// #162：指定了非回环监听地址时，NO_PROXY 回环豁免要带上它；回环 / 全部网卡时保持原来三项
func TestLoopbackNoProxyHostListFollowsPanelHost(t *testing.T) {
	prev := config.C
	t.Cleanup(func() { config.C = prev })

	cases := map[string]string{
		"":             "localhost,127.0.0.1,::1",
		"0.0.0.0":      "localhost,127.0.0.1,::1",
		"localhost":    "localhost,127.0.0.1,::1",
		"::1":          "localhost,127.0.0.1,::1",
		"192.168.1.10": "localhost,127.0.0.1,::1,192.168.1.10",
	}
	for host, want := range cases {
		config.C = &config.Config{Server: config.ServerConfig{Host: host, Port: 5701}}
		if got := strings.Join(loopbackNoProxyHostList(), ","); got != want {
			t.Errorf("host %q: got %q, want %q", host, got, want)
		}
	}
	if len(loopbackNoProxyHosts) != 3 {
		t.Fatalf("loopbackNoProxyHosts 被改动了: %v", loopbackNoProxyHosts)
	}
}
