package service

import (
	"fmt"
	"strings"
)

const (
	SessionClientWeb = "web"
	SessionClientApp = "app"
)

type SessionClientInfo struct {
	Type        string
	App         string
	Platform    string
	DeviceModel string
	DeviceName  string
	OSVersion   string
	Browser     string
	UserAgent   string
}

func DetectSessionClientType(headerClientType, headerClientApp, userAgent string) string {
	info := DetectSessionClientInfo(
		headerClientType,
		headerClientApp,
		"",
		"",
		"",
		"",
		userAgent,
	)
	return info.Type
}

func DetectSessionClientInfo(
	headerClientType,
	headerClientApp,
	headerClientPlatform,
	headerDeviceModel,
	headerDeviceName,
	headerOSVersion,
	userAgent string,
) SessionClientInfo {
	ua := strings.TrimSpace(userAgent)
	info := SessionClientInfo{
		App:         strings.ToLower(strings.TrimSpace(headerClientApp)),
		Platform:    strings.ToLower(strings.TrimSpace(headerClientPlatform)),
		DeviceModel: strings.TrimSpace(headerDeviceModel),
		DeviceName:  strings.TrimSpace(headerDeviceName),
		OSVersion:   strings.TrimSpace(headerOSVersion),
		UserAgent:   ua,
	}

	info.Type = detectSessionClientType(
		headerClientType,
		info.App,
		info.Platform,
		info.DeviceModel,
		info.DeviceName,
		ua,
	)

	if info.Platform == "" {
		info.Platform = detectPlatformFromUserAgent(ua)
	}
	if info.Type == SessionClientWeb && info.Browser == "" {
		info.Browser = detectBrowserFromUserAgent(ua)
	}
	if info.Type == SessionClientApp && info.DeviceModel == "" {
		info.DeviceModel = detectAppDeviceModelFromUserAgent(ua)
	}
	if info.Type == SessionClientApp && info.OSVersion == "" {
		info.OSVersion = detectOSVersionFromUserAgent(info.Platform, ua)
	}

	return info
}

func detectSessionClientType(headerClientType, headerClientApp, headerClientPlatform, headerDeviceModel, headerDeviceName, userAgent string) string {
	clientType := strings.ToLower(strings.TrimSpace(headerClientType))
	switch clientType {
	case SessionClientWeb, SessionClientApp:
		return clientType
	}

	switch headerClientApp {
	case "daidai-panel-app":
		return SessionClientApp
	case "daidai-panel-web":
		return SessionClientWeb
	}

	if strings.TrimSpace(headerDeviceModel) != "" || strings.TrimSpace(headerDeviceName) != "" {
		return SessionClientApp
	}

	switch strings.ToLower(strings.TrimSpace(headerClientPlatform)) {
	case "android", "ios", "macos", "windows", "linux":
		if headerClientApp == "daidai-panel-app" {
			return SessionClientApp
		}
	}

	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "daidaipanelapp/"):
		return SessionClientApp
	case strings.Contains(ua, "dart/"), strings.Contains(ua, "dart:io"):
		return SessionClientApp
	case strings.Contains(ua, " cfnetwork/"), strings.Contains(ua, " okhttp/"):
		return SessionClientApp
	default:
		return SessionClientWeb
	}
}

func NormalizeSessionClientType(clientType string) string {
	if strings.EqualFold(strings.TrimSpace(clientType), SessionClientApp) {
		return SessionClientApp
	}
	return SessionClientWeb
}

func SessionClientLabel(clientType string) string {
	if NormalizeSessionClientType(clientType) == SessionClientApp {
		return "App端"
	}
	return "网页端"
}

func SessionClientDisplayName(info SessionClientInfo) string {
	label := SessionClientLabel(info.Type)

	if NormalizeSessionClientType(info.Type) == SessionClientApp {
		parts := []string{label}
		if info.DeviceModel != "" {
			parts = append(parts, info.DeviceModel)
		} else if info.DeviceName != "" {
			parts = append(parts, info.DeviceName)
		}

		platformLabel := platformDisplayLabel(info.Platform, info.OSVersion)
		if platformLabel != "" {
			parts = append(parts, platformLabel)
		}

		return strings.Join(dedupParts(parts), " · ")
	}

	parts := []string{label}
	if info.Browser != "" {
		parts = append(parts, info.Browser)
	}
	if osLabel := platformDisplayLabel(info.Platform, info.OSVersion); osLabel != "" {
		parts = append(parts, osLabel)
	}
	return strings.Join(dedupParts(parts), " · ")
}

func ResolveStoredSessionClientName(clientType, clientName, userAgent string) string {
	clientName = strings.TrimSpace(clientName)
	if clientName != "" {
		return clientName
	}

	info := DetectSessionClientInfo(clientType, "", "", "", "", "", userAgent)
	return SessionClientDisplayName(info)
}

func detectPlatformFromUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "android"):
		return "android"
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "ios"):
		return "ios"
	case strings.Contains(ua, "windows"):
		return "windows"
	case strings.Contains(ua, "mac os"), strings.Contains(ua, "macintosh"):
		return "macos"
	case strings.Contains(ua, "linux"):
		return "linux"
	default:
		return ""
	}
}

func detectBrowserFromUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "edg/"):
		return "Edge"
	case strings.Contains(ua, "chrome/") && !strings.Contains(ua, "edg/"):
		return "Chrome"
	case strings.Contains(ua, "firefox/"):
		return "Firefox"
	case strings.Contains(ua, "safari/") && strings.Contains(ua, "version/"):
		return "Safari"
	case strings.Contains(ua, "micromessenger/"):
		return "微信"
	default:
		return ""
	}
}

func detectAppDeviceModelFromUserAgent(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	start := strings.Index(ua, "(")
	end := strings.LastIndex(ua, ")")
	if start < 0 || end <= start {
		return ""
	}

	segments := strings.Split(ua[start+1:end], ";")
	clean := make([]string, 0, len(segments))
	for _, segment := range segments {
		part := strings.TrimSpace(segment)
		if part != "" {
			clean = append(clean, part)
		}
	}
	if len(clean) < 2 {
		return ""
	}

	for _, part := range clean[1:] {
		lower := strings.ToLower(part)
		if lower == "flutter" || lower == "dart" {
			continue
		}
		if strings.HasPrefix(lower, "android ") || strings.HasPrefix(lower, "ios ") {
			continue
		}
		return part
	}

	return ""
}

func detectOSVersionFromUserAgent(platform, userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	start := strings.Index(ua, "(")
	end := strings.LastIndex(ua, ")")
	if start < 0 || end <= start {
		return ""
	}

	segments := strings.Split(ua[start+1:end], ";")
	for _, segment := range segments {
		part := strings.TrimSpace(segment)
		lower := strings.ToLower(part)
		switch platform {
		case "android":
			if strings.HasPrefix(lower, "android ") {
				return strings.TrimSpace(part[len("Android "):])
			}
		case "ios":
			if strings.HasPrefix(lower, "ios ") {
				return strings.TrimSpace(part[len("iOS "):])
			}
		}
	}

	return ""
}

func platformDisplayLabel(platform, version string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	version = strings.TrimSpace(version)

	var base string
	switch platform {
	case "android":
		base = "Android"
	case "ios":
		base = "iOS"
	case "windows":
		base = "Windows"
	case "macos":
		base = "macOS"
	case "linux":
		base = "Linux"
	case "web":
		base = "Web"
	default:
		base = ""
	}

	if base == "" {
		return ""
	}
	if version == "" {
		return base
	}
	return fmt.Sprintf("%s %s", base, version)
}

func dedupParts(parts []string) []string {
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, part)
	}
	return result
}
