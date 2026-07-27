package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var forwardedIPHeaders = []string{
	"CF-Connecting-IP",
	"True-Client-IP",
	"X-Forwarded-For",
	"X-Real-IP",
	"X-Original-Forwarded-For",
}

func ResolveClientIP(c *gin.Context) string {
	return ResolveClientIPFromRequest(c.Request)
}

func ResolveClientIPFromRequest(r *http.Request) string {
	remoteIP := normalizeIPString(r.RemoteAddr)
	forwardedIP := extractForwardedClientIP(r.Header)

	if isTrustedProxyHop(remoteIP) && forwardedIP != "" {
		return forwardedIP
	}
	if remoteIP != "" {
		return remoteIP
	}
	if forwardedIP != "" {
		return forwardedIP
	}
	return ""
}

func extractForwardedClientIP(headers http.Header) string {
	for _, headerName := range forwardedIPHeaders {
		raw := strings.TrimSpace(headers.Get(headerName))
		if raw == "" {
			continue
		}

		switch headerName {
		case "X-Forwarded-For", "X-Original-Forwarded-For":
			if ip := lastUntrustedForwardedIP(raw); ip != "" {
				return ip
			}
		default:
			if ip := normalizeIPString(raw); ip != "" {
				return ip
			}
		}
	}

	return ""
}

func lastUntrustedForwardedIP(raw string) string {
	var fallback string
	parts := strings.Split(raw, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := normalizeIPString(parts[i])
		if ip == "" {
			continue
		}
		fallback = ip

		parsed := net.ParseIP(ip)
		if parsed == nil || isConfiguredTrustedProxy(parsed) {
			continue
		}
		return ip
	}

	return fallback
}

func firstPublicIP(raw string) string {
	for _, part := range strings.Split(raw, ",") {
		ip := normalizeIPString(part)
		if ip == "" {
			continue
		}

		parsed := net.ParseIP(ip)
		if parsed == nil || isPrivateOrLocalIP(parsed) {
			continue
		}
		return ip
	}

	return ""
}

func firstValidIP(raw string) string {
	for _, part := range strings.Split(raw, ",") {
		if ip := normalizeIPString(part); ip != "" {
			return ip
		}
	}
	return ""
}

func normalizeIPString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}

	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")

	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}

	return ip.String()
}

func isTrustedProxyHop(ip string) bool {
	if ip == "" {
		return true
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	return isConfiguredTrustedProxy(parsed)
}

func isPrivateOrLocalIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
