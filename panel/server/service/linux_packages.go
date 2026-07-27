package service

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type LinuxPackageManager struct {
	Name   string
	Binary string
}

const AptPackageListTTL = 6 * time.Hour

var DetectLinuxPackageManagerLookPathFunc = exec.LookPath

func DetectLinuxPackageManager() (LinuxPackageManager, error) {
	return DetectLinuxPackageManagerWithLookPath(DetectLinuxPackageManagerLookPathFunc)
}

func DetectLinuxPackageManagerWithLookPath(lookPath func(string) (string, error)) (LinuxPackageManager, error) {
	candidates := []LinuxPackageManager{
		{Name: "apk", Binary: "apk"},
		{Name: "apt", Binary: "apt-get"},
		{Name: "dnf", Binary: "dnf"},
		{Name: "yum", Binary: "yum"},
		{Name: "microdnf", Binary: "microdnf"},
		{Name: "zypper", Binary: "zypper"},
	}

	for _, candidate := range candidates {
		if _, err := lookPath(candidate.Binary); err == nil {
			return candidate, nil
		}
	}

	return LinuxPackageManager{}, errors.New("未检测到可用的 Linux 包管理器（支持 apk/apt/dnf/yum/microdnf/zypper）")
}

func ShouldRefreshAptPackageLists() bool {
	return ShouldRefreshAptPackageListsFromDir("/var/lib/apt/lists", time.Now(), AptPackageListTTL)
}

func ShouldRefreshAptPackageListsFromDir(dir string, now time.Time, ttl time.Duration) bool {
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

func LinuxInstallCommandSpec(manager LinuxPackageManager, packageName string, refreshApt bool) (string, []string, error) {
	switch manager.Name {
	case "apk":
		return manager.Binary, []string{"add", "--no-cache", packageName}, nil
	case "apt":
		script := "export DEBIAN_FRONTEND=noninteractive; "
		if refreshApt {
			script += "echo '[APT] 软件包索引过期，正在刷新...'; apt-get update; "
		}
		script += "echo '[APT] 正在安装软件包...'; apt-get install -y --no-install-recommends " + shellQuoteLinuxPackage(packageName)
		return "sh", []string{"-lc", script}, nil
	case "dnf", "yum", "microdnf":
		return manager.Binary, []string{"install", "-y", packageName}, nil
	case "zypper":
		return manager.Binary, []string{"--non-interactive", "install", packageName}, nil
	default:
		return "", nil, errors.New("不支持的 Linux 包管理器")
	}
}

func LinuxRemoveCommandSpec(manager LinuxPackageManager, packageName string, force bool) (string, []string, error) {
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

func BuildLinuxPackageCommand(manager LinuxPackageManager, action, packageName string, force bool, distribution string, ensureMirror func(LinuxPackageManager, string) error) (*exec.Cmd, error) {
	switch action {
	case "install":
		refreshApt := manager.Name == "apt" && ShouldRefreshAptPackageLists()
		if ensureMirror != nil {
			if mirrorErr := ensureMirror(manager, distribution); mirrorErr != nil {
				return nil, mirrorErr
			}
		}
		bin, args, err := LinuxInstallCommandSpec(manager, packageName, refreshApt)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = AppendProxyEnv(append(os.Environ(), "TMPDIR=/tmp"))
		return cmd, nil
	case "remove":
		bin, args, err := LinuxRemoveCommandSpec(manager, packageName, force)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = AppendProxyEnv(append(os.Environ(), "TMPDIR=/tmp", "DEBIAN_FRONTEND=noninteractive"))
		return cmd, nil
	default:
		return nil, errors.New("不支持的 Linux 依赖操作")
	}
}

func DetectLinuxDistribution() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ID=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "ID="))
		value = strings.Trim(value, `"'`)
		return strings.ToLower(value)
	}

	return ""
}

func shellQuoteLinuxPackage(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
