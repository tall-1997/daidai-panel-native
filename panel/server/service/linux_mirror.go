package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// 本文件是 Linux 系统包镜像源的读写与默认加速逻辑，v3.3.1 从 handler/deps_package_manager.go 整体搬来（#142）。
//
// 搬迁原因：容器重建后的自动重装（backup_runtime.go 的 reinstallDependency）在 service 层，
// 拿不到 handler 里的 ensureDefaultLinuxMirror，只能传 nil，于是重建后的重装一律走 deb.debian.org，
// 国内要慢很多。现在网页安装与重启重装共用这里的 EnsureDefaultLinuxMirror，handler 只留薄封装。
//
// 除了下面单独注明的 debian-security 修正，其余行为与搬迁前逐行一致。

// ReadLinuxMirror 读当前系统包管理器正在用的镜像源；不支持镜像设置的包管理器返回空串。
func ReadLinuxMirror(manager LinuxPackageManager) (string, error) {
	switch manager.Name {
	case "apk":
		return readAPKMirror()
	case "apt":
		return readAPTMirror()
	default:
		return "", nil
	}
}

// SetLinuxMirror 把系统包镜像源改成 mirror；mirror 为空或是官方源时改成默认加速源。
//
// 第一个返回值表示「磁盘上的源文件真的被改写了」。v3.3.2 起把它一路透出来（issue #146）：
// 换完源必须让 apt 索引作废，否则下一次安装仍拿旧源的索引，报 E: Unable to locate package。
func SetLinuxMirror(manager LinuxPackageManager, distribution, mirror string) (bool, error) {
	switch manager.Name {
	case "apk":
		return writeAPKMirror(EffectiveLinuxMirror(manager, distribution, mirror))
	case "apt":
		return writeAPTMirror(distribution, EffectiveLinuxMirror(manager, distribution, mirror))
	default:
		return false, fmt.Errorf("当前系统使用 %s，暂不支持镜像设置", manager.Binary)
	}
}

// EnsureDefaultLinuxMirror 在装系统包之前调用：当前源为空或是官方源时换成默认加速源，
// 用户自己设过的源一律不动。返回值同 SetLinuxMirror，表示源文件是否真被改写。
func EnsureDefaultLinuxMirror(manager LinuxPackageManager, distribution string) (bool, error) {
	switch manager.Name {
	case "apk", "apt":
	default:
		return false, nil
	}

	current, err := ReadLinuxMirror(manager)
	if err != nil {
		return false, err
	}
	// 第三个条件是 v3.3.2 的旧默认源一次性迁移（issue #146）：存量用户的 sources.list 里存的是上一版
	// 默认的阿里云，而阿里云不是官方源，原来的判定会把它当成「用户自己选的」而永远不动。
	// 迁移由 legacyDefaultMirrorMigrationPending() 兜住一次性，用户显式保存过镜像源之后就整体失效。
	if current != "" &&
		!isOfficialLinuxMirror(manager, distribution, current) &&
		!(isLegacyDefaultLinuxMirror(manager, distribution, current) && legacyDefaultMirrorMigrationPending()) {
		return false, nil
	}

	return SetLinuxMirror(manager, distribution, "")
}

// EffectiveLinuxMirror 返回实际生效的镜像源：空或官方源时回落到默认加速源。
func EffectiveLinuxMirror(manager LinuxPackageManager, distribution, current string) string {
	current = strings.TrimRight(strings.TrimSpace(current), "/")
	if current == "" || isOfficialLinuxMirror(manager, distribution, current) {
		return strings.TrimRight(defaultLinuxMirror(manager, distribution), "/")
	}
	return current
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

// writeAPKMirror 的返回值与 writeAPTMirror 对齐（issue #146）：只有内容真变了才算 changed。
// apk 这边原来是无条件整文件覆写，拿不到「有没有变」这个信号，对齐之后调用方不用区分包管理器。
func writeAPKMirror(mirror string) (bool, error) {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		mirror = defaultLinuxMirror(LinuxPackageManager{Name: "apk", Binary: "apk"}, "")
	}
	if !isHTTPMirror(mirror) {
		return false, errors.New("Linux 镜像源必须以 http:// 或 https:// 开头")
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
	if existing, readErr := os.ReadFile("/etc/apk/repositories"); readErr == nil && string(existing) == content {
		return false, nil
	}
	if err := os.WriteFile("/etc/apk/repositories", []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
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

// writeAPTMirror 把 apt 源改写成 mirror，返回「是否真的改写了文件」。
//
// v3.3.2 起把「一个条目都没识别出来」与「识别出来了但已经是目标源」分开（issue #146）：
// 前者是真异常，后者是成功。原来两种情况统一报「未找到可更新的 apt 软件源条目」，
// 于是用户只改 pip、Linux 那栏原样提交时必然拿到 400，而 pip / npm 其实已经写进去了。
func writeAPTMirror(distribution, mirror string) (bool, error) {
	files, err := listAPTSourceFiles()
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, errors.New("未找到 apt 软件源配置文件")
	}
	if mirror != "" && !isHTTPMirror(mirror) {
		return false, errors.New("Linux 镜像源必须以 http:// 或 https:// 开头")
	}

	changedAny := false
	matchedAny := false
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return false, err
		}

		var (
			updated string
			changed bool
		)
		if strings.HasSuffix(file, ".sources") {
			// 能提取出 URIs / deb 行就说明这个文件里有面板认得的条目，与它是否需要改写无关。
			matchedAny = matchedAny || extractMirrorFromAPTSources(string(data)) != ""
			updated, changed = rewriteAPTSourcesContent(string(data), distribution, mirror)
		} else {
			matchedAny = matchedAny || extractMirrorFromAPTList(string(data)) != ""
			updated, changed = rewriteAPTListContent(string(data), distribution, mirror)
		}

		if !changed {
			continue
		}

		if err := os.WriteFile(file, []byte(updated), 0o644); err != nil {
			return false, err
		}
		changedAny = true
	}

	if !matchedAny {
		return false, errors.New("未找到可更新的 apt 软件源条目")
	}

	return changedAny, nil
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

// rewriteAPTSourcesContent 改写 deb822 格式（*.sources）里的 URIs。
//
// v3.3.1 起按「段」（空行分隔的 stanza）处理：先扫完整段拿到 Suites，再改这一段的 URIs。
// 原来是逐行往下读、遇到 URIs 时用「已经读到的 Suites」，可 Debian 官方的 debian.sources
// 里 URIs 恰恰写在 Suites 前面，那时拿到的永远是空串 —— security 段根本认不出来。
func rewriteAPTSourcesContent(content, distribution, mirror string) (string, bool) {
	lines := strings.Split(content, "\n")
	changed := false

	for start := 0; start < len(lines); {
		end := start
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}

		suites := ""
		for _, line := range lines[start:end] {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "suites:") {
				suites = strings.TrimSpace(trimmed[len("Suites:"):])
			}
		}

		for i := start; i < end; i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToLower(trimmed), "uris:") {
				continue
			}

			currentURI := strings.TrimSpace(trimmed[len("URIs:"):])
			targetURI := resolveAPTMirrorURI(distribution, mirror, currentURI, suites)
			if targetURI == "" || targetURI == currentURI {
				continue
			}

			leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = leading + "URIs: " + targetURI
			changed = true
		}

		start = end + 1
	}

	return strings.Join(lines, "\n"), changed
}

func resolveAPTMirrorURI(distribution, requestedMirror, currentURI, suites string) string {
	target := strings.TrimRight(strings.TrimSpace(requestedMirror), "/")
	if target == "" {
		target = strings.TrimRight(defaultLinuxMirror(LinuxPackageManager{Name: "apt", Binary: "apt-get"}, distribution), "/")
	}
	if target == "" {
		return ""
	}

	// Debian 的安全更新是单独的 debian-security 仓库（官方 deb.debian.org/debian-security、
	// security.debian.org/debian-security），阿里、清华、腾讯等镜像站也都是 <镜像>/debian-security。
	// 原来这一段也被改成 <镜像>/debian，而那下面没有 bookworm-security 的 Release，
	// apt-get update 会报 E:，安全更新源静默失效（安装脚本用 ; 串联，install 仍会继续，所以一直没人发现）。
	if isDebianSecurityAPTSource(distribution, currentURI, suites) && !strings.HasSuffix(target, "-security") {
		return target + "-security"
	}
	return target
}

// isDebianSecurityAPTSource 判断这一条源是不是 Debian 的安全更新仓库。
//
// 两条判据：URI 的路径以 -security 结尾（与发行版无关，路径本身就说明了一切）；
// 或者发行版是 Debian 且 Suites 里有 *-security（bookworm-security 这类）/ */updates（buster 及更早的写法）。
// Suites 这条刻意只认 Debian：Ubuntu 的 noble-security 就在 /ubuntu 下（官方 security.ubuntu.com/ubuntu），
// 加上 -security 后缀反而会指到一个不存在的路径。
func isDebianSecurityAPTSource(distribution, currentURI, suites string) bool {
	if strings.HasSuffix(strings.TrimRight(strings.TrimSpace(currentURI), "/"), "-security") {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(distribution), "debian") {
		return false
	}
	for _, suite := range strings.Fields(suites) {
		if strings.HasSuffix(suite, "-security") || strings.HasSuffix(suite, "/updates") {
			return true
		}
	}
	return false
}

func isHTTPMirror(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// defaultLinuxMirror v3.3.2 起从阿里云换成腾讯云（issue #146）：用户实测阿里云限速不到 100KB/s。
// 三条路径形态与阿里云完全一致（<站点>/alpine、/debian、/ubuntu），security 段由 resolveAPTMirrorURI
// 自动拼 -security，腾讯云的 mirrors.cloud.tencent.com/debian-security 是存在的。
// 阿里云只是不再当默认，仍留在前端候选清单里。
func defaultLinuxMirror(manager LinuxPackageManager, distribution string) string {
	switch manager.Name {
	case "apk":
		return "https://mirrors.cloud.tencent.com/alpine"
	case "apt":
		switch strings.ToLower(strings.TrimSpace(distribution)) {
		case "debian":
			return "https://mirrors.cloud.tencent.com/debian"
		default:
			return "https://mirrors.cloud.tencent.com/ubuntu"
		}
	default:
		return ""
	}
}

// isLegacyDefaultLinuxMirror 判断当前源是不是 v3.3.2 之前的默认加速源（阿里云）。
//
// 只给 EnsureDefaultLinuxMirror 的一次性迁移用，不参与 EffectiveLinuxMirror：
// 后者同时负责「页面上显示当前生效的源」与「存盘前归一化」，掺进迁移会出现
// 「磁盘写着阿里云、页面显示腾讯云」这种读写不对称，也会让用户选不回阿里云。
//
// debian 档要把 debian-security 一并认上：readAPTMirror 返回的是第一个匹配到的条目，
// 在只剩 security 段还指向阿里云的机器上，读出来的就是 <站点>/debian-security。
func isLegacyDefaultLinuxMirror(manager LinuxPackageManager, distribution, current string) bool {
	currentLower := strings.ToLower(strings.TrimRight(strings.TrimSpace(current), "/"))
	switch manager.Name {
	case "apk":
		return currentLower == "https://mirrors.aliyun.com/alpine"
	case "apt":
		switch strings.ToLower(strings.TrimSpace(distribution)) {
		case "debian":
			return currentLower == "https://mirrors.aliyun.com/debian" ||
				currentLower == "https://mirrors.aliyun.com/debian-security"
		default:
			return currentLower == "https://mirrors.aliyun.com/ubuntu"
		}
	default:
		return false
	}
}

func isOfficialLinuxMirror(manager LinuxPackageManager, distribution, current string) bool {
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
