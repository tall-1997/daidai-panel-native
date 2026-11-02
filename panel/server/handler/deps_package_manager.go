package handler

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// Linux 包操作锁 v3.3.1 起搬到了 service.LockLinuxPackageOperation：容器重建后的自动重装在 service 层，
// 原来这里的 handler 私有锁它拿不到，两边的 apt-get update 会撞 lists 锁（lists 锁不等待）。

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

func buildLinuxPackageCommand(manager linuxPackageManager, action, packageName string, force bool) (*exec.Cmd, error) {
	return service.BuildLinuxPackageCommand(
		service.LinuxPackageManager{Name: manager.Name, Binary: manager.Binary},
		action,
		packageName,
		force,
		detectLinuxDistribution(),
		service.EnsureDefaultLinuxMirror,
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

	serviceManager := service.LinuxPackageManager{Name: manager.Name, Binary: manager.Binary}
	switch manager.Name {
	case "apk", "apt":
		info.Supported = true
		info.Mirror, err = service.ReadLinuxMirror(serviceManager)
		if err != nil {
			info.Message = fmt.Sprintf("读取 %s 镜像源失败：%s", manager.Name, err.Error())
		} else {
			info.Mirror = service.EffectiveLinuxMirror(serviceManager, info.Distribution, info.Mirror)
		}
	default:
		info.Supported = false
		info.Message = fmt.Sprintf("当前系统使用 %s，镜像设置暂未开放，避免出现“界面能配但实际不生效”的假功能。", manager.Binary)
	}

	return info
}

// setLinuxMirror 是 service.SetLinuxMirror 的薄封装。镜像源的读写与默认加速逻辑 v3.3.1 起搬到了
// service/linux_mirror.go，让容器重建后的自动重装（service 层）也能用上同一套镜像策略（#142）。
// 第一个返回值表示源文件真被改写了，调用方据此决定要不要作废 apt 索引（v3.3.2 / issue #146）。
func setLinuxMirror(manager linuxPackageManager, distribution, mirror string) (bool, error) {
	return service.SetLinuxMirror(service.LinuxPackageManager{Name: manager.Name, Binary: manager.Binary}, distribution, mirror)
}
