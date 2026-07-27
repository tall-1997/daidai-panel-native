package middleware

import (
	"net"
	"sync"

	"daidai-panel/pkg/netutil"
)

type trustedProxyConfigState struct {
	mu       sync.RWMutex
	cidrs    []string
	networks []*net.IPNet
}

var trustedProxyState trustedProxyConfigState

func init() {
	_ = ConfigureTrustedProxyCIDRs("")
}

func ConfigureTrustedProxyCIDRs(value string) error {
	cidrs, err := netutil.ParseTrustedProxyCIDRs(value)
	if err != nil {
		return err
	}

	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return err
		}
		networks = append(networks, network)
	}

	trustedProxyState.mu.Lock()
	defer trustedProxyState.mu.Unlock()

	trustedProxyState.cidrs = append([]string(nil), cidrs...)
	trustedProxyState.networks = append([]*net.IPNet(nil), networks...)
	return nil
}

func CurrentTrustedProxyCIDRs() []string {
	trustedProxyState.mu.RLock()
	defer trustedProxyState.mu.RUnlock()

	result := make([]string, len(trustedProxyState.cidrs))
	copy(result, trustedProxyState.cidrs)
	return result
}

func isConfiguredTrustedProxy(ip net.IP) bool {
	trustedProxyState.mu.RLock()
	defer trustedProxyState.mu.RUnlock()

	for _, network := range trustedProxyState.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
