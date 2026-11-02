package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PanelUpdatePlanInfo struct {
	DeploymentType                   string
	UpdateManager                    string
	ContainerName                    string
	ImageName                        string
	PullImageName                    string
	Channel                          string
	MirrorHost                       string
	RegistryURL                      string
	ReleaseVersion                   string
	AssetName                        string
	InstallDir                       string
	BinaryName                       string
	WatchtowerManualTriggerSupported bool
	WatchtowerSchedule               string
	WatchtowerPeriodicPollsEnabled   bool
}

type PanelUpdateStatusInfo struct {
	Status         string
	Phase          string
	Message        string
	Error          string
	DeploymentType string
	UpdateManager  string
	ContainerName  string
	ImageName      string
	PullImageName  string
	MirrorHost     string
	RegistryURL    string
	ReleaseVersion string
	AssetName      string
	InstallDir     string
	BinaryName     string
}

func BuildPanelUpdatePlanInfo() (PanelUpdatePlanInfo, error) {
	plan, err := buildPanelUpdatePlan()
	if err != nil {
		return PanelUpdatePlanInfo{}, err
	}

	return PanelUpdatePlanInfo{
		DeploymentType:                   plan.DeploymentType,
		UpdateManager:                    plan.UpdateManager,
		ContainerName:                    plan.ContainerName,
		ImageName:                        plan.ImageName,
		PullImageName:                    plan.PullImageName,
		Channel:                          plan.Channel,
		MirrorHost:                       plan.MirrorHost,
		RegistryURL:                      plan.RegistryURL,
		ReleaseVersion:                   plan.ReleaseVersion,
		AssetName:                        plan.AssetName,
		InstallDir:                       plan.InstallDir,
		BinaryName:                       plan.BinaryName,
		WatchtowerManualTriggerSupported: plan.Watchtower.ManualTriggerSupported,
		WatchtowerSchedule:               plan.Watchtower.Schedule,
		WatchtowerPeriodicPollsEnabled:   plan.Watchtower.PeriodicPollsEnabled,
	}, nil
}

// CheckWatchtowerAPIReachable 对 Watchtower 的 HTTP API 做一次轻量探活，给 ddp check 用。
// 只确认端口上有没有服务应答，绝不触发更新：请求的是 API 根路径而不是 /v1/update，
// 也不带令牌，所以拿到 401/404 同样算通过 —— 能给出 HTTP 响应就说明 API 已经在监听。
// 探活失败时返回的文案与面板一键更新失败时同源（见 watchtowerUnreachableHint）。
func CheckWatchtowerAPIReachable(timeout time.Duration) (bool, string) {
	cfg := currentWatchtowerRuntimeConfig()
	apiURL := strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	if apiURL == "" {
		return false, "未配置 WATCHTOWER_HTTP_API_URL，无法探活 Watchtower HTTP API"
	}
	// 命令行诊断不能卡太久，超时必须短。
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	resp, err := client.Get(apiURL + "/")
	if err != nil {
		if watchtowerAPIUnreachable(err) {
			return false, watchtowerUnreachableHint(cfg.APIURL, err)
		}
		return false, "探活 Watchtower HTTP API 失败: " + err.Error()
	}
	defer resp.Body.Close()

	return true, ""
}

func ExecutePanelUpdateForCLI() (PanelUpdateStatusInfo, error) {
	plan, err := buildPanelUpdatePlan()
	if err != nil {
		return PanelUpdateStatusInfo{}, err
	}
	if err := panelUpdater.begin(plan); err != nil {
		return PanelUpdateStatusInfo{}, err
	}

	executePanelUpdate(plan)

	snapshot := panelUpdater.snapshotCopy()
	status := PanelUpdateStatusInfo{
		Status:         snapshot.Status,
		Phase:          snapshot.Phase,
		Message:        snapshot.Message,
		Error:          snapshot.Error,
		DeploymentType: snapshot.DeploymentType,
		UpdateManager:  snapshot.UpdateManager,
		ContainerName:  snapshot.ContainerName,
		ImageName:      snapshot.ImageName,
		PullImageName:  snapshot.PullImageName,
		MirrorHost:     snapshot.MirrorHost,
		RegistryURL:    snapshot.RegistryURL,
		ReleaseVersion: snapshot.ReleaseVersion,
		AssetName:      snapshot.AssetName,
		InstallDir:     snapshot.InstallDir,
		BinaryName:     snapshot.BinaryName,
	}

	if snapshot.Status == "failed" {
		if snapshot.Error != "" {
			return status, fmt.Errorf("%s", snapshot.Error)
		}
		return status, fmt.Errorf("%s", snapshot.Message)
	}

	return status, nil
}
