package netutil

import (
	"fmt"
	"net"
	"strings"
	"unicode"
)

var defaultTrustedProxyCIDRs = []string{
	"127.0.0.1/32",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

func DefaultTrustedProxyCIDRs() []string {
	result := make([]string, len(defaultTrustedProxyCIDRs))
	copy(result, defaultTrustedProxyCIDRs)
	return result
}

func NormalizeTrustedProxyCIDRs(value string) (string, error) {
	cidrs, err := ParseTrustedProxyCIDRs(value)
	if err != nil {
		return "", err
	}
	return strings.Join(cidrs, "\n"), nil
}

func ParseTrustedProxyCIDRs(value string) ([]string, error) {
	parts := splitTrustedProxyCIDRs(value)
	if len(parts) == 0 {
		return DefaultTrustedProxyCIDRs(), nil
	}

	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		canonical, err := normalizeTrustedProxyToken(part)
		if err != nil {
			return nil, fmt.Errorf("可信代理 CIDR/IP %q 无效", part)
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}

	return result, nil
}

func ParseTrustedProxyNetworks(value string) ([]*net.IPNet, error) {
	cidrs, err := ParseTrustedProxyCIDRs(value)
	if err != nil {
		return nil, err
	}

	result := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		result = append(result, network)
	}

	return result, nil
}

func splitTrustedProxyCIDRs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
}

func normalizeTrustedProxyToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty trusted proxy")
	}

	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return "", err
		}
		return network.String(), nil
	}

	ip := net.ParseIP(value)
	if ip == nil {
		return "", fmt.Errorf("invalid IP")
	}

	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}
