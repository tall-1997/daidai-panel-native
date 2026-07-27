package handler

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"daidai-panel/service"
)

type linuxPackageManager struct {
	Name   string
	Binary string
}

type linuxMirrorInfo struct {
	Manager      string
	Distribution string
	Mirror       string
	Supported    bool
	Label        string
	Message      string
}

var linuxPackageOperationMu sync.Mutex

func detectLinuxPackageManager() (linuxPackageManager, error) {
	manager, err := service.DetectLinuxPackageManager()
	if err != nil {
		return linuxPackageManager{}, err
	}
	return linuxPackageManager{Name: manager.Name, Binary: manager.Binary}, nil
}

func detectLinuxPackageManagerWithLookPath(lookPath func(string) (string, error)) (linuxPackageManager, error) {
	manager, err := service.DetectLinuxPackageManagerWithLookPath(lookPath)
	if err != nil {
		return linuxPackageManager{}, err
	}
	return linuxPackageManager{Name: manager.Name, Binary: manager.Binary}, nil
}

func shouldRefreshAptPackageLists() bool {
	return shouldRefreshAptPackageListsFromDir("/var/lib/apt/lists", time.Now(), service.AptPackageListTTL)
}

func shouldRefreshAptPackageListsFromDir(dir string, now time.Time, ttl time.Duration) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}

	var newest time.Time
	hasIndexFile := false

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "lock" || strings.HasSuffix(name, ".lock") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		hasIndexFile = true
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}

	if !hasIndexFile {
		return true
	}

	return now.Sub(newest) > ttl
}

func linuxInstallCommandSpec(manager linuxPackageManager, packageName string, refreshApt bool) (string, []string, error) {
	switch manager.Name {
	case "apk":
		return manager.Binary, []string{"add", "--no-cache", packageName}, nil
	case "apt":
		script := "export DEBIAN_FRONTEND=noninteractive; "
		if refreshApt {
			script += "echo '[APT] 软件包索引过期，正在刷新...'; apt-get update; "
		}
		script += "echo '[APT] 正在安装软件包...'; apt-get install -y --no-install-recommends " + shellQuote(packageName)
		return "sh", []string{"-lc", script}, nil
	case "dnf", "yum", "microdnf":
		return manager.Binary, []string{"install", "-y", packageName}, nil
	case "zypper":
		return manager.Binary, []string{"--non-interactive", "install", packageName}, nil
	default:
		return "", nil, errors.New("不支持的 Linux 包管理器")
	}
}

func linuxRemoveCommandSpec(manager linuxPackageManager, packageName string, force bool) (string, []string, error) {
	switch manager.Name {
	case "apk":
		args := []string{"del"}
		if force {
			args = append(args, "--force-broken-world")
		}
		args = append(args, packageName)
		return manager.Binary, args, nil
	case "apt":
		args := []string{"remove", "-y"}
		if force {
			args = append(args, "--allow-remove-essential", "--purge")
		}
		args = append(args, packageName)
		return manager.Binary, args, nil
	case "dnf", "yum", "microdnf":
		return manager.Binary, []string{"remove", "-y", packageName}, nil
	case "zypper":
		return manager.Binary, []string{"--non-interactive", "remove", packageName}, nil
	default:
		return "", nil, errors.New("不支持的 Linux 包管理器")
	}
}

func buildLinuxPackageCommand(manager linuxPackageManager, action, packageName string, force bool) (*exec.Cmd, error) {
	return service.BuildLinuxPackageCommand(
		service.LinuxPackageManager{Name: manager.Name, Binary: manager.Binary},
		action,
		packageName,
		force,
		detectLinuxDistribution(),
		func(m service.LinuxPackageManager, distribution string) error {
			return ensureDefaultLinuxMirror(linuxPackageManager{Name: m.Name, Binary: m.Binary}, distribution)
		},
	)
}

func detectLinuxDistribution() string {
	return service.DetectLinuxDistribution()
}

func getLinuxMirrorInfo() linuxMirrorInfo {
	manager, err := detectLinuxPackageManager()
	if err != nil {
		return linuxMirrorInfo{
			Label:   "Linux",
			Message: err.Error(),
		}
	}

	info := linuxMirrorInfo{
		Manager:      manager.Name,
		Distribution: detectLinuxDistribution(),
		Label:        fmt.Sprintf("Linux (%s)", manager.Binary),
	}

	switch manager.Name {
	case "apk":
		info.Supported = true
		info.Mirror, err = readAPKMirror()
		if err != nil {
			info.Message = "读取 apk 镜像源失败：" + err.Error()
		} else {
			info.Mirror = effectiveLinuxMirror(manager, info.Distribution, info.Mirror)
		}
	case "apt":
		info.Supported = true
		info.Mirror, err = readAPTMirror()
		if err != nil {
			info.Message = "读取 apt 镜像源失败：" + err.Error()
		} else {
			info.Mirror = effectiveLinuxMirror(manager, info.Distribution, info.Mirror)
		}
	default:
		info.Supported = false
		info.Message = fmt.Sprintf("当前系统使用 %s，镜像设置暂未开放，避免出现“界面能配但实际不生效”的假功能。", manager.Binary)
	}

	return info
}

func setLinuxMirror(manager linuxPackageManager, distribution, mirror string) error {
	switch manager.Name {
	case "apk":
		return writeAPKMirror(effectiveLinuxMirror(manager, distribution, mirror))
	case "apt":
		return writeAPTMirror(distribution, effectiveLinuxMirror(manager, distribution, mirror))
	default:
		return fmt.Errorf("当前系统使用 %s，暂不支持镜像设置", manager.Binary)
	}
}

func readAPKMirror() (string, error) {
	data, err := os.ReadFile("/etc/apk/repositories")
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "/v", 2)
		if len(parts) > 0 {
			return strings.TrimRight(parts[0], "/"), nil
		}
	}

	return "", nil
}

func writeAPKMirror(mirror string) error {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		mirror = defaultLinuxMirror(linuxPackageManager{Name: "apk", Binary: "apk"}, "")
	}
	if !isHTTPMirror(mirror) {
		return errors.New("Linux 镜像源必须以 http:// 或 https:// 开头")
	}

	mirror = strings.TrimRight(mirror, "/")
	out, err := exec.Command("cat", "/etc/alpine-release").Output()
	ver := "3.19"
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), ".")
		if len(parts) >= 2 {
			ver = parts[0] + "." + parts[1]
		}
	}

	content := fmt.Sprintf("%s/v%s/main\n%s/v%s/community\n", mirror, ver, mirror, ver)
	return os.WriteFile("/etc/apk/repositories", []byte(content), 0o644)
}

func readAPTMirror() (string, error) {
	files, err := listAPTSourceFiles()
	if err != nil {
		return "", err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var mirror string
		if strings.HasSuffix(file, ".sources") {
			mirror = extractMirrorFromAPTSources(string(data))
		} else {
			mirror = extractMirrorFromAPTList(string(data))
		}
		if mirror != "" {
			return strings.TrimRight(mirror, "/"), nil
		}
	}

	return "", nil
}

func writeAPTMirror(distribution, mirror string) error {
	files, err := listAPTSourceFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("未找到 apt 软件源配置文件")
	}
	if mirror != "" && !isHTTPMirror(mirror) {
		return errors.New("Linux 镜像源必须以 http:// 或 https:// 开头")
	}

	changedAny := false
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}

		var (
			updated string
			changed bool
		)
		if strings.HasSuffix(file, ".sources") {
			updated, changed = rewriteAPTSourcesContent(string(data), distribution, mirror)
		} else {
			updated, changed = rewriteAPTListContent(string(data), distribution, mirror)
		}

		if !changed {
			continue
		}

		if err := os.WriteFile(file, []byte(updated), 0o644); err != nil {
			return err
		}
		changedAny = true
	}

	if !changedAny {
		return errors.New("未找到可更新的 apt 软件源条目")
	}

	return nil
}

func listAPTSourceFiles() ([]string, error) {
	files := []string{}

	if _, err := os.Stat("/etc/apt/sources.list"); err == nil {
		files = append(files, "/etc/apt/sources.list")
	}

	patterns := []string{
		"/etc/apt/sources.list.d/*.list",
		"/etc/apt/sources.list.d/*.sources",
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}

	slices.Sort(files)
	return files, nil
}

func extractMirrorFromAPTList(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if mirror := extractMirrorFromAPTListLine(line); mirror != "" {
			return mirror
		}
	}
	return ""
}

func extractMirrorFromAPTListLine(line string) string {
	fields := parseAPTListLineFields(line)
	if len(fields) == 0 {
		return ""
	}
	uriIndex := aptURIFieldIndex(fields)
	if uriIndex < 0 || uriIndex >= len(fields) {
		return ""
	}
	return fields[uriIndex]
}

func parseAPTListLineFields(line string) []string {
	content := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
	if content == "" {
		return nil
	}

	fields := strings.Fields(content)
	if len(fields) == 0 {
		return nil
	}
	if fields[0] != "deb" && fields[0] != "deb-src" {
		return nil
	}

	return fields
}

func aptURIFieldIndex(fields []string) int {
	if len(fields) < 3 {
		return -1
	}

	idx := 1
	if strings.HasPrefix(fields[idx], "[") {
		for idx < len(fields) && !strings.HasSuffix(fields[idx], "]") {
			idx++
		}
		idx++
	}
	if idx >= len(fields) {
		return -1
	}
	return idx
}

func extractMirrorFromAPTSources(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "uris:") {
			continue
		}
		return strings.TrimSpace(trimmed[len("URIs:"):])
	}
	return ""
}

func rewriteAPTListContent(content, distribution, mirror string) (string, bool) {
	lines := strings.Split(content, "\n")
	changed := false

	for i, line := range lines {
		updated, lineChanged := rewriteAPTListLine(line, distribution, mirror)
		if lineChanged {
			lines[i] = updated
			changed = true
		}
	}

	return strings.Join(lines, "\n"), changed
}

func rewriteAPTListLine(line, distribution, mirror string) (string, bool) {
	fields := parseAPTListLineFields(line)
	if len(fields) == 0 {
		return line, false
	}

	uriIndex := aptURIFieldIndex(fields)
	if uriIndex < 0 || uriIndex >= len(fields) {
		return line, false
	}

	currentURI := fields[uriIndex]
	suite := ""
	if uriIndex+1 < len(fields) {
		suite = fields[uriIndex+1]
	}
	targetURI := resolveAPTMirrorURI(distribution, mirror, currentURI, suite)
	if targetURI == "" || targetURI == currentURI {
		return line, false
	}

	fields[uriIndex] = targetURI
	comment := ""
	if parts := strings.SplitN(line, "#", 2); len(parts) == 2 {
		comment = "#" + parts[1]
	}

	updated := strings.Join(fields, " ")
	if comment != "" {
		updated += " " + strings.TrimSpace(comment)
	}
	return updated, true
}

func rewriteAPTSourcesContent(content, distribution, mirror string) (string, bool) {
	lines := strings.Split(content, "\n")
	changed := false
	currentSuites := ""

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if trimmed == "" {
			currentSuites = ""
			continue
		}
		if strings.HasPrefix(lower, "suites:") {
			currentSuites = strings.TrimSpace(trimmed[len("Suites:"):])
			continue
		}
		if !strings.HasPrefix(lower, "uris:") {
			continue
		}

		currentURI := strings.TrimSpace(trimmed[len("URIs:"):])
		targetURI := resolveAPTMirrorURI(distribution, mirror, currentURI, currentSuites)
		if targetURI == "" || targetURI == currentURI {
			continue
		}

		leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = leading + "URIs: " + targetURI
		changed = true
	}

	return strings.Join(lines, "\n"), changed
}

func resolveAPTMirrorURI(distribution, requestedMirror, currentURI, suites string) string {
	requestedMirror = strings.TrimRight(strings.TrimSpace(requestedMirror), "/")
	if requestedMirror != "" {
		return requestedMirror
	}

	return strings.TrimRight(defaultLinuxMirror(linuxPackageManager{Name: "apt", Binary: "apt-get"}, distribution), "/")
}

func isHTTPMirror(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func defaultLinuxMirror(manager linuxPackageManager, distribution string) string {
	switch manager.Name {
	case "apk":
		return "https://mirrors.aliyun.com/alpine"
	case "apt":
		switch strings.ToLower(strings.TrimSpace(distribution)) {
		case "debian":
			return "https://mirrors.aliyun.com/debian"
		default:
			return "https://mirrors.aliyun.com/ubuntu"
		}
	default:
		return ""
	}
}

func effectiveLinuxMirror(manager linuxPackageManager, distribution, current string) string {
	current = strings.TrimRight(strings.TrimSpace(current), "/")
	if current == "" || isOfficialLinuxMirror(manager, distribution, current) {
		return strings.TrimRight(defaultLinuxMirror(manager, distribution), "/")
	}
	return current
}

func isOfficialLinuxMirror(manager linuxPackageManager, distribution, current string) bool {
	currentLower := strings.ToLower(strings.TrimRight(strings.TrimSpace(current), "/"))
	switch manager.Name {
	case "apk":
		return currentLower == "https://dl-cdn.alpinelinux.org/alpine" || currentLower == "http://dl-cdn.alpinelinux.org/alpine"
	case "apt":
		switch distribution {
		case "debian":
			return currentLower == "http://deb.debian.org/debian" ||
				currentLower == "https://deb.debian.org/debian" ||
				currentLower == "http://security.debian.org/debian-security" ||
				currentLower == "https://security.debian.org/debian-security"
		default:
			return currentLower == "http://archive.ubuntu.com/ubuntu" ||
				currentLower == "https://archive.ubuntu.com/ubuntu" ||
				currentLower == "http://security.ubuntu.com/ubuntu" ||
				currentLower == "https://security.ubuntu.com/ubuntu"
		}
	default:
		return false
	}
}

func ensureDefaultLinuxMirror(manager linuxPackageManager, distribution string) error {
	current := ""
	var err error

	switch manager.Name {
	case "apk":
		current, err = readAPKMirror()
	case "apt":
		current, err = readAPTMirror()
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if current != "" && !isOfficialLinuxMirror(manager, distribution, current) {
		return nil
	}

	return setLinuxMirror(manager, distribution, "")
}
