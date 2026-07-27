package netutil

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func NormalizeIPWhitelistEntry(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("IP 或网段不能为空")
	}

	if strings.Contains(value, "*") {
		return normalizeIPv4WildcardWhitelist(value)
	}

	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return "", fmt.Errorf("IP 段格式无效，请使用 CIDR，例如 203.0.113.0/24")
		}
		return network.String(), nil
	}

	ip := net.ParseIP(value)
	if ip == nil {
		return "", fmt.Errorf("IP 地址格式无效")
	}

	if v4 := ip.To4(); v4 != nil {
		return v4.String(), nil
	}
	return ip.String(), nil
}

func MatchIPWhitelistEntry(entry, clientIP string) bool {
	client := net.ParseIP(strings.TrimSpace(clientIP))
	if client == nil {
		return false
	}

	normalized, err := NormalizeIPWhitelistEntry(entry)
	if err != nil {
		return false
	}

	if strings.Contains(normalized, "/") {
		_, network, err := net.ParseCIDR(normalized)
		if err != nil {
			return false
		}
		return network.Contains(client)
	}

	target := net.ParseIP(normalized)
	return target != nil && target.Equal(client)
}

func normalizeIPv4WildcardWhitelist(value string) (string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("通配格式仅支持 IPv4，例如 203.0.113.*")
	}

	octets := make([]string, 4)
	fixedOctets := 0
	wildcardStarted := false

	for i, rawPart := range parts {
		part := strings.TrimSpace(rawPart)
		if part == "*" {
			wildcardStarted = true
			octets[i] = "0"
			continue
		}

		if wildcardStarted {
			return "", fmt.Errorf("通配段必须连续出现在末尾，例如 203.0.113.* 或 203.0.*.*")
		}

		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return "", fmt.Errorf("IPv4 通配格式无效")
		}

		fixedOctets++
		octets[i] = strconv.Itoa(n)
	}

	if fixedOctets == 0 {
		return "", fmt.Errorf("请至少固定一个网段，例如 203.0.113.*")
	}

	cidr := strings.Join(octets, ".") + "/" + strconv.Itoa(fixedOctets*8)
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("通配网段格式无效")
	}

	return network.String(), nil
}
