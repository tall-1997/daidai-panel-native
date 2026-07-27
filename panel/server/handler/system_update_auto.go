package handler

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"daidai-panel/model"
	"daidai-panel/service"
)

const (
	autoUpdateLastCheckedAtKey    = "auto_update_last_checked_at"
	autoUpdatePendingVersionKey   = "auto_update_pending_version"
	autoUpdatePendingStartedAtKey = "auto_update_pending_started_at"
)

var (
	autoUpdateWatcherOnce sync.Once
	autoUpdateWatcherStop chan struct{}
)

type panelUpdateExecutionOptions struct {
	AutoUpdate    bool
	TargetVersion string
}

func StartPanelAutoUpdateWatcher() {
	autoUpdateWatcherOnce.Do(func() {
		autoUpdateWatcherStop = make(chan struct{})
		go panelAutoUpdateLoop()
		log.Println("panel auto update watcher started (interval: 1h)")
	})
}

func StopPanelAutoUpdateWatcher() {
	if autoUpdateWatcherStop != nil {
		close(autoUpdateWatcherStop)
	}
}

func FinalizePendingAutoUpdateOnStartup() {
	targetVersion := strings.TrimSpace(model.GetConfig(autoUpdatePendingVersionKey, ""))
	if targetVersion == "" {
		return
	}

	startedAt := strings.TrimSpace(model.GetConfig(autoUpdatePendingStartedAtKey, ""))
	clearPendingAutoUpdateState()

	if Version == targetVersion {
		service.SendNotification(
			"静默更新成功",
			fmt.Sprintf("面板已自动更新到 v%s。\n完成时间：%s", targetVersion, time.Now().Format(time.DateTime)),
		)
		return
	}

	content := fmt.Sprintf("静默更新计划目标版本为 v%s，但当前启动版本仍为 v%s。", targetVersion, Version)
	if startedAt != "" {
		content += "\n发起时间：" + startedAt
	}
	service.SendNotification("静默更新失败", content)
}

func panelAutoUpdateLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	time.Sleep(time.Minute)
	runPanelAutoUpdateCheck()

	for {
		select {
		case <-ticker.C:
			runPanelAutoUpdateCheck()
		case <-autoUpdateWatcherStop:
			return
		}
	}
}

func runPanelAutoUpdateCheck() {
	if !model.GetRegisteredConfigBool("auto_update_enabled") {
		return
	}

	lastCheckedAt := parseAutoUpdateTime(model.GetConfig(autoUpdateLastCheckedAtKey, ""))
	if !lastCheckedAt.IsZero() && time.Since(lastCheckedAt) < 24*time.Hour {
		return
	}

	_ = model.SetConfig(autoUpdateLastCheckedAtKey, time.Now().Format(time.RFC3339))

	release, err := fetchLatestPanelRelease()
	if err != nil {
		service.SendNotification("静默更新失败", "自动检查更新失败："+err.Error())
		return
	}

	latestVersion := release.version()
	if !compareVersions(Version, latestVersion) {
		return
	}

	plan, err := buildPanelUpdatePlanForRelease(release)
	if err != nil {
		service.SendNotification("静默更新失败", "自动更新准备失败："+err.Error())
		return
	}

	if err := panelUpdater.begin(plan); err != nil {
		return
	}

	recordPendingAutoUpdate(latestVersion)
	go executePanelUpdateWithOptions(plan, panelUpdateExecutionOptions{
		AutoUpdate:    true,
		TargetVersion: latestVersion,
	})
}

func recordPendingAutoUpdate(version string) {
	if strings.TrimSpace(version) == "" {
		return
	}
	_ = model.SetConfig(autoUpdatePendingVersionKey, strings.TrimSpace(version))
	_ = model.SetConfig(autoUpdatePendingStartedAtKey, time.Now().Format(time.RFC3339))
}

func clearPendingAutoUpdateState() {
	_ = model.SetConfig(autoUpdatePendingVersionKey, "")
	_ = model.SetConfig(autoUpdatePendingStartedAtKey, "")
}

func parseAutoUpdateTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func notifyAutoUpdateFailure(targetVersion string, err error) {
	clearPendingAutoUpdateState()
	message := "静默更新执行失败"
	if strings.TrimSpace(targetVersion) != "" {
		message = fmt.Sprintf("静默更新到 v%s 失败", targetVersion)
	}
	if err != nil {
		message += "：" + err.Error()
	}
	service.SendNotification("静默更新失败", message)
}
