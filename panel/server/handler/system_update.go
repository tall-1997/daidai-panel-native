package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

const (
	dockerSocketPath             = "/var/run/docker.sock"
	defaultDockerHubRegistryHost = "registry-1.docker.io"
	panelUpdateDeploymentDocker  = "docker"
	panelUpdateDeploymentBinary  = "binary"
	panelUpdateManagerPanel      = "panel"
	panelUpdateManagerWatchtower = "watchtower"
)

type panelUpdateStatusSnapshot struct {
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	Message        string    `json:"message"`
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	DeploymentType string    `json:"deployment_type,omitempty"`
	ContainerName  string    `json:"container_name,omitempty"`
	ImageName      string    `json:"image_name,omitempty"`
	PullImageName  string    `json:"pull_image_name,omitempty"`
	MirrorHost     string    `json:"mirror_host,omitempty"`
	RegistryURL    string    `json:"registry_url,omitempty"`
	ReleaseVersion string    `json:"release_version,omitempty"`
	AssetName      string    `json:"asset_name,omitempty"`
	AssetURL       string    `json:"asset_url,omitempty"`
	InstallDir     string    `json:"install_dir,omitempty"`
	BinaryName     string    `json:"binary_name,omitempty"`
}

type panelUpdateManager struct {
	mu       sync.RWMutex
	snapshot panelUpdateStatusSnapshot
}

type panelUpdatePlan struct {
	DeploymentType  string
	ContainerName   string
	ImageName       string
	PullImageName   string
	PreviousImageID string
	Channel         string
	MirrorHost      string
	RegistryURL     string
	RunArgs         []string
	ReleaseVersion  string
	AssetName       string
	AssetURL        string
	InstallDir      string
	BinaryName      string
	ExecutablePath  string
	CurrentPID      int
	ServerPID       int
	ServerPIDFile   string
}

type watchtowerRuntimeConfig struct {
	Managed                bool
	APIURL                 string
	APIToken               string
	Schedule               string
	PeriodicPollsEnabled   bool
	ManualTriggerSupported bool
}

type dockerInspectInfo struct {
	Name       string `json:"Name"`
	Image      string `json:"Image"`
	Mounts     []dockerInspectMount
	Config     dockerInspectConfig     `json:"Config"`
	HostConfig dockerInspectHostConfig `json:"HostConfig"`
}

type dockerInspectConfig struct {
	Image string   `json:"Image"`
	Env   []string `json:"Env"`
}

type dockerInspectHostConfig struct {
	Binds         []string `json:"Binds"`
	ExtraHosts    []string `json:"ExtraHosts"`
	NetworkMode   string   `json:"NetworkMode"`
	RestartPolicy struct {
		Name string `json:"Name"`
	} `json:"RestartPolicy"`
	PortBindings map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	} `json:"PortBindings"`
}

type dockerInspectMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

var panelUpdater = newPanelUpdateManager()

func newPanelUpdateManager() *panelUpdateManager {
	return &panelUpdateManager{
		snapshot: panelUpdateStatusSnapshot{
			Status:    "idle",
			Phase:     "idle",
			Message:   "当前没有进行中的更新任务",
			UpdatedAt: time.Now(),
		},
	}
}

func (m *panelUpdateManager) begin(plan *panelUpdatePlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.snapshot.Status == "running" || m.snapshot.Status == "restarting" {
		return fmt.Errorf("已有更新任务正在进行中，请稍后查看状态")
	}

	now := time.Now()
	m.snapshot = panelUpdateStatusSnapshot{
		Status:         "running",
		Phase:          "preparing",
		Message:        buildPanelUpdateBeginMessage(plan),
		StartedAt:      now,
		UpdatedAt:      now,
		DeploymentType: plan.DeploymentType,
		ContainerName:  plan.ContainerName,
		ImageName:      plan.ImageName,
		PullImageName:  plan.PullImageName,
		MirrorHost:     plan.MirrorHost,
		RegistryURL:    plan.RegistryURL,
		ReleaseVersion: plan.ReleaseVersion,
		AssetName:      plan.AssetName,
		AssetURL:       plan.AssetURL,
		InstallDir:     plan.InstallDir,
		BinaryName:     plan.BinaryName,
	}
	return nil
}

func (m *panelUpdateManager) setRunning(phase, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.snapshot.Status = "running"
	m.snapshot.Phase = phase
	m.snapshot.Message = message
	m.snapshot.Error = ""
	m.snapshot.UpdatedAt = time.Now()
}

func (m *panelUpdateManager) setRestarting(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.snapshot.Status = "restarting"
	m.snapshot.Phase = "restarting"
	m.snapshot.Message = message
	m.snapshot.Error = ""
	m.snapshot.UpdatedAt = time.Now()
}

func (m *panelUpdateManager) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	msg := "更新失败"
	if err != nil {
		msg = err.Error()
	}

	m.snapshot.Status = "failed"
	m.snapshot.Phase = "failed"
	m.snapshot.Message = msg
	m.snapshot.Error = msg
	m.snapshot.UpdatedAt = time.Now()
}

func (m *panelUpdateManager) snapshotCopy() panelUpdateStatusSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (h *SystemHandler) UpdateStatus(c *gin.Context) {
	response.Success(c, gin.H{"data": panelUpdater.snapshotCopy()})
}

func currentWatchtowerRuntimeConfig() watchtowerRuntimeConfig {
	manager := strings.ToLower(strings.TrimSpace(os.Getenv("PANEL_UPDATE_MANAGER")))
	apiURL := strings.TrimSpace(os.Getenv("WATCHTOWER_HTTP_API_URL"))
	apiToken := strings.TrimSpace(os.Getenv("WATCHTOWER_HTTP_API_TOKEN"))
	schedule := strings.TrimSpace(os.Getenv("WATCHTOWER_SCHEDULE"))
	periodicPolls := parseEnvBool(os.Getenv("WATCHTOWER_HTTP_API_PERIODIC_POLLS"))

	managed := manager == panelUpdateManagerWatchtower || apiURL != ""
	manualSupported := managed && apiURL != "" && apiToken != ""

	return watchtowerRuntimeConfig{
		Managed:                managed,
		APIURL:                 apiURL,
		APIToken:               apiToken,
		Schedule:               schedule,
		PeriodicPollsEnabled:   periodicPolls,
		ManualTriggerSupported: manualSupported,
	}
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func buildWatchtowerUpdateTarget(cfg watchtowerRuntimeConfig) gin.H {
	return gin.H{
		"deployment_type":              panelUpdateDeploymentDocker,
		"update_manager":               panelUpdateManagerWatchtower,
		"watchtower_managed":           true,
		"watchtower_schedule":          cfg.Schedule,
		"watchtower_http_api_enabled":  cfg.APIURL != "",
		"watchtower_trigger_supported": cfg.ManualTriggerSupported,
		"watchtower_periodic_polls":    cfg.PeriodicPollsEnabled,
	}
}

func triggerWatchtowerUpdate(cfg watchtowerRuntimeConfig) (map[string]interface{}, error) {
	if !cfg.Managed {
		return nil, fmt.Errorf("当前部署未启用 Watchtower 托管更新")
	}
	if !cfg.ManualTriggerSupported {
		return nil, fmt.Errorf("当前 Watchtower 未配置 HTTP API 手动触发能力，请先设置 WATCHTOWER_HTTP_API_URL 与 WATCHTOWER_HTTP_API_TOKEN")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
	}

	apiURL := strings.TrimRight(cfg.APIURL, "/") + "/v1/update"
	req, err := http.NewRequest(http.MethodPost, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建 Watchtower 更新请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 Watchtower 更新接口失败: %w", err)
	}
	defer resp.Body.Close()

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		// Watchtower 手动触发接口在部分部署里会返回 204 No Content 或 200 + 空响应体。
		// 这类场景代表“触发成功但无额外 JSON”，不能因为 EOF 就误判成失败。
		if !(resp.StatusCode < http.StatusBadRequest && errors.Is(err, io.EOF)) {
			if resp.StatusCode < http.StatusBadRequest {
				return nil, fmt.Errorf("解析 Watchtower 更新响应失败: %w", err)
			}
		}
	}

	if resp.StatusCode >= http.StatusBadRequest {
		if message, ok := payload["error"].(string); ok && strings.TrimSpace(message) != "" {
			return nil, fmt.Errorf("Watchtower 更新触发失败: %s", message)
		}
		return nil, fmt.Errorf("Watchtower 更新触发失败: HTTP %d", resp.StatusCode)
	}

	if payload == nil {
		payload = map[string]interface{}{}
	}
	return payload, nil
}

func buildPanelUpdatePlan() (*panelUpdatePlan, error) {
	return buildPanelUpdatePlanForRelease(nil)
}

func buildPanelUpdatePlanForRelease(release *panelReleaseInfo) (*panelUpdatePlan, error) {
	dockerPlan, dockerErr := buildDockerPanelUpdatePlan()
	if dockerErr == nil {
		return dockerPlan, nil
	}

	if shouldRequireDockerPanelUpdate() {
		return nil, dockerErr
	}

	binaryPlan, binaryErr := buildBinaryPanelUpdatePlan(release)
	if binaryErr == nil {
		return binaryPlan, nil
	}

	return nil, fmt.Errorf("%s；二进制后台更新也不可用：%s", dockerErr.Error(), binaryErr.Error())
}

func buildDockerPanelUpdatePlan() (*panelUpdatePlan, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("当前运行环境未提供 Docker CLI，无法使用面板内一键更新")
	}

	if _, err := os.Stat(dockerSocketPath); err != nil {
		return nil, fmt.Errorf("当前 Docker 部署未检测到可用的更新托管方式。推荐使用 Watchtower 托管自动更新；如需立即手动更新，请在宿主机执行 docker compose pull && docker compose up -d。早期版本若仍使用面板内 Docker Socket 一键更新，请确认已挂载 %s", dockerSocketPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if output, err := dockerCommandOutput(ctx, "info"); err != nil {
		return nil, formatDockerCommandError("无法连接 Docker 守护进程，请确认 docker.sock 可访问", err, output)
	}

	info, err := inspectCurrentPanelContainer()
	if err != nil {
		return nil, err
	}

	containerName := strings.TrimPrefix(strings.TrimSpace(info.Name), "/")
	if envName := strings.TrimSpace(os.Getenv("CONTAINER_NAME")); envName != "" {
		containerName = envName
	}
	if containerName == "" {
		return nil, fmt.Errorf("无法识别当前面板容器名称，请设置环境变量 CONTAINER_NAME")
	}

	imageName := strings.TrimSpace(os.Getenv("IMAGE_NAME"))
	if imageName == "" {
		imageName = strings.TrimSpace(info.Config.Image)
	}
	if imageName == "" {
		return nil, fmt.Errorf("无法识别当前容器镜像，请设置环境变量 IMAGE_NAME")
	}
	imageName = normalizePanelUpdateImageName(imageName)

	pullImageName, mirrorHost, registryURL := resolveUpdateImageTarget(
		imageName,
		model.GetRegisteredConfig("update_image_mirror"),
	)

	return &panelUpdatePlan{
		DeploymentType:  panelUpdateDeploymentDocker,
		ContainerName:   containerName,
		ImageName:       imageName,
		PullImageName:   pullImageName,
		PreviousImageID: normalizeDockerImageID(info.Image),
		Channel:         resolvePanelUpdateChannel(imageName),
		MirrorHost:      mirrorHost,
		RegistryURL:     registryURL,
		RunArgs:         buildContainerRunArgs(containerName, imageName, info),
	}, nil
}

func shouldRequireDockerPanelUpdate() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func inspectCurrentPanelContainer() (*dockerInspectInfo, error) {
	candidates := uniqueNonEmptyStrings(
		os.Getenv("CONTAINER_NAME"),
		os.Getenv("HOSTNAME"),
		mustHostname(),
		"daidai-panel",
	)

	for _, candidate := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		output, err := dockerCommandOutput(ctx, "inspect", "--format", "{{json .}}", candidate)
		cancel()
		if err != nil {
			continue
		}

		var info dockerInspectInfo
		if err := json.Unmarshal(output, &info); err != nil {
			continue
		}
		if strings.TrimSpace(info.Name) == "" {
			continue
		}
		return &info, nil
	}

	return nil, fmt.Errorf("无法识别当前面板容器，请设置环境变量 CONTAINER_NAME 后重试")
}

func normalizeDockerImageID(imageID string) string {
	imageID = strings.TrimSpace(imageID)
	if !strings.HasPrefix(imageID, "sha256:") {
		return ""
	}

	digest := strings.TrimPrefix(imageID, "sha256:")
	if len(digest) != 64 {
		return ""
	}
	for _, ch := range digest {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return ""
	}
	return "sha256:" + strings.ToLower(digest)
}

func buildContainerRunArgs(containerName, imageName string, info *dockerInspectInfo) []string {
	runArgs := []string{"run", "-d", "--name", containerName}

	restartPolicy := strings.TrimSpace(info.HostConfig.RestartPolicy.Name)
	if restartPolicy != "" && restartPolicy != "no" {
		runArgs = append(runArgs, "--restart", restartPolicy)
	}

	networkMode := strings.TrimSpace(info.HostConfig.NetworkMode)
	if networkMode != "" && networkMode != "default" {
		runArgs = append(runArgs, "--network", networkMode)
	}

	extraHosts := make([]string, 0, len(info.HostConfig.ExtraHosts))
	for _, item := range info.HostConfig.ExtraHosts {
		item = strings.TrimSpace(item)
		if item != "" {
			extraHosts = append(extraHosts, item)
		}
	}
	sort.Strings(extraHosts)
	for _, item := range extraHosts {
		runArgs = append(runArgs, "--add-host", item)
	}

	for _, mapping := range collectPortMappings(info.HostConfig.PortBindings) {
		runArgs = append(runArgs, "-p", mapping)
	}

	for _, volume := range collectVolumeMappings(info) {
		runArgs = append(runArgs, "-v", volume)
	}

	for _, env := range filterContainerEnv(info.Config.Env) {
		runArgs = append(runArgs, "-e", env)
	}

	runArgs = append(runArgs, imageName)
	return runArgs
}

func collectPortMappings(portBindings map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) []string {
	keys := make([]string, 0, len(portBindings))
	for port := range portBindings {
		keys = append(keys, port)
	}
	sort.Strings(keys)

	var result []string
	for _, port := range keys {
		bindings := portBindings[port]
		for _, binding := range bindings {
			if strings.TrimSpace(binding.HostPort) == "" {
				continue
			}

			containerPort := strings.Split(port, "/")[0]
			mapping := binding.HostPort + ":" + containerPort
			hostIP := strings.TrimSpace(binding.HostIP)
			if hostIP != "" && hostIP != "0.0.0.0" && hostIP != "::" {
				mapping = hostIP + ":" + mapping
			}
			result = append(result, mapping)
		}
	}
	return result
}

func collectVolumeMappings(info *dockerInspectInfo) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(info.HostConfig.Binds)+len(info.Mounts))
	for _, bind := range info.HostConfig.Binds {
		bind = strings.TrimSpace(bind)
		if bind == "" {
			continue
		}
		key, ok := buildVolumeMappingDedupKeyFromRaw(bind)
		if !ok {
			key = bind
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, bind)
	}

	for _, mount := range info.Mounts {
		destination := strings.TrimSpace(mount.Destination)
		if destination == "" {
			continue
		}

		var source string
		switch mount.Type {
		case "bind":
			source = strings.TrimSpace(mount.Source)
		case "volume":
			source = strings.TrimSpace(mount.Name)
			if source == "" {
				source = strings.TrimSpace(mount.Source)
			}
		default:
			continue
		}

		if source == "" {
			continue
		}

		mapping := source + ":" + destination
		if !mount.RW {
			mapping += ":ro"
		}
		key := buildVolumeMappingDedupKey(source, destination, !mount.RW)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, mapping)
	}

	sort.Strings(result)
	return result
}

func buildVolumeMappingDedupKey(source, destination string, readOnly bool) string {
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	return strings.TrimSpace(source) + "\x00" + strings.TrimSpace(destination) + "\x00" + mode
}

func buildVolumeMappingDedupKeyFromRaw(mapping string) (string, bool) {
	mapping = strings.TrimSpace(mapping)
	if mapping == "" {
		return "", false
	}

	parts := strings.Split(mapping, ":")
	if len(parts) < 2 {
		return "", false
	}

	source := strings.TrimSpace(parts[0])
	destination := strings.TrimSpace(parts[1])
	if source == "" || destination == "" {
		return "", false
	}

	readOnly := false
	for _, rawOptionGroup := range parts[2:] {
		for _, option := range strings.Split(rawOptionGroup, ",") {
			if strings.EqualFold(strings.TrimSpace(option), "ro") {
				readOnly = true
				break
			}
		}
		if readOnly {
			break
		}
	}

	return buildVolumeMappingDedupKey(source, destination, readOnly), true
}

func filterContainerEnv(envList []string) []string {
	skipPrefixes := []string{
		"PATH=",
		"HOME=",
		"HOSTNAME=",
		"LANG=",
		"LC_",
		"TERM=",
		"PWD=",
		"SHLVL=",
		"_=",
	}

	result := make([]string, 0, len(envList))
	for _, env := range envList {
		env = strings.TrimSpace(env)
		if env == "" {
			continue
		}

		skip := false
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(env, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, env)
		}
	}

	return result
}

func executePanelUpdate(plan *panelUpdatePlan) {
	executePanelUpdateWithOptions(plan, panelUpdateExecutionOptions{})
}

func executePanelUpdateWithOptions(plan *panelUpdatePlan, options panelUpdateExecutionOptions) {
	if plan.DeploymentType == panelUpdateDeploymentBinary {
		executeBinaryPanelUpdateWithOptions(plan, options)
		return
	}
	executeDockerPanelUpdateWithOptions(plan, options)
}

func executeDockerPanelUpdateWithOptions(plan *panelUpdatePlan, options panelUpdateExecutionOptions) {
	panelUpdater.setRunning("preparing", fmt.Sprintf("正在检查镜像仓库连通性 %s", plan.RegistryURL))
	if err := preflightUpdateRegistry(plan); err != nil {
		panelUpdater.fail(err)
		if options.AutoUpdate {
			notifyAutoUpdateFailure(options.TargetVersion, err)
		}
		return
	}

	panelUpdater.setRunning("pulling", fmt.Sprintf("正在拉取最新镜像 %s", plan.PullImageName))

	pullCtx, pullCancel := context.WithTimeout(context.Background(), 20*time.Minute)
	pullOutput, err := dockerCommandOutput(pullCtx, "pull", plan.PullImageName)
	pullCancel()
	if err != nil {
		formatted := formatPanelUpdatePullError(plan, err, pullOutput)
		panelUpdater.fail(formatted)
		if options.AutoUpdate {
			notifyAutoUpdateFailure(options.TargetVersion, formatted)
		}
		return
	}

	if plan.PullImageName != "" && plan.PullImageName != plan.ImageName {
		panelUpdater.setRunning("pulling", fmt.Sprintf("镜像已拉取完成，正在同步更新标签 %s", plan.ImageName))

		tagCtx, tagCancel := context.WithTimeout(context.Background(), time.Minute)
		tagOutput, tagErr := dockerCommandOutput(tagCtx, "tag", plan.PullImageName, plan.ImageName)
		tagCancel()
		if tagErr != nil {
			formatted := formatDockerCommandError("同步更新镜像标签失败", tagErr, tagOutput)
			panelUpdater.fail(formatted)
			if options.AutoUpdate {
				notifyAutoUpdateFailure(options.TargetVersion, formatted)
			}
			return
		}

		rmiCtx, rmiCancel := context.WithTimeout(context.Background(), 30*time.Second)
		dockerCommandOutput(rmiCtx, "rmi", plan.PullImageName)
		rmiCancel()
	}

	panelUpdater.setRunning("scheduling", "镜像已拉取完成，正在启动更新辅助容器")

	helperScript := buildPanelUpdateHelperScript(plan)
	helperArgs := []string{
		"run", "-d", "--rm",
		"-v", dockerSocketPath + ":" + dockerSocketPath,
		"--entrypoint", "sh",
		plan.ImageName,
		"-c", helperScript,
	}

	helperCtx, helperCancel := context.WithTimeout(context.Background(), time.Minute)
	helperOutput, err := dockerCommandOutput(helperCtx, helperArgs...)
	helperCancel()
	if err != nil {
		formatted := formatDockerCommandError("启动更新辅助容器失败", err, helperOutput)
		panelUpdater.fail(formatted)
		if options.AutoUpdate {
			notifyAutoUpdateFailure(options.TargetVersion, formatted)
		}
		return
	}

	panelUpdater.setRestarting("更新任务已启动，正在重建面板容器并切换到新版本")
}

func buildPanelUpdateBeginMessage(plan *panelUpdatePlan) string {
	if plan.DeploymentType == panelUpdateDeploymentBinary {
		if strings.TrimSpace(plan.AssetName) != "" {
			return fmt.Sprintf("更新环境校验通过，准备下载二进制更新包 %s", plan.AssetName)
		}
		return "更新环境校验通过，准备下载二进制更新包"
	}
	return "更新环境校验通过，准备检查镜像仓库并拉取最新镜像"
}

func buildPanelUpdateTarget(plan *panelUpdatePlan) gin.H {
	target := gin.H{
		"deployment_type": plan.DeploymentType,
	}

	if plan.DeploymentType == panelUpdateDeploymentBinary {
		target["release_version"] = plan.ReleaseVersion
		target["asset_name"] = plan.AssetName
		target["asset_url"] = plan.AssetURL
		target["install_dir"] = plan.InstallDir
		target["binary_name"] = plan.BinaryName
		return target
	}

	target["container_name"] = plan.ContainerName
	target["image_name"] = plan.ImageName
	target["pull_image_name"] = plan.PullImageName
	target["channel"] = plan.Channel
	target["mirror_host"] = plan.MirrorHost
	target["registry_url"] = plan.RegistryURL
	return target
}

func buildPanelUpdateHelperScript(plan *panelUpdatePlan) string {
	quotedArgs := make([]string, 0, len(plan.RunArgs))
	for _, arg := range plan.RunArgs {
		quotedArgs = append(quotedArgs, shellQuote(arg))
	}

	cleanupBlock := ""
	if previousImageID := normalizeDockerImageID(plan.PreviousImageID); previousImageID != "" {
		cleanupBlock = fmt.Sprintf(`
if [ "$status" -eq 0 ]; then
  docker image rm %s >/dev/null 2>&1 || true
fi`, shellQuote(previousImageID))
	}

	return fmt.Sprintf(`sleep 2
docker rm -f %s >/dev/null 2>&1 || true
docker %s
status=$?%s
exit "$status"`,
		shellQuote(plan.ContainerName),
		strings.Join(quotedArgs, " "),
		cleanupBlock,
	)
}

func dockerCommandOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func preflightUpdateRegistry(plan *panelUpdatePlan) error {
	registryURL := strings.TrimSpace(plan.RegistryURL)
	if registryURL == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Timeout:   12 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, nil)
	if err != nil {
		return fmt.Errorf("更新前镜像仓库连通性检查失败：%w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("更新前镜像仓库连通性检查失败：%w。%s", err, buildPanelUpdateNetworkHint(plan))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("更新前镜像仓库连通性检查失败：镜像仓库返回状态 %d。%s", resp.StatusCode, buildPanelUpdateNetworkHint(plan))
	}

	return nil
}

func formatDockerCommandError(prefix string, err error, output []byte) error {
	detail := trimCommandOutput(output)
	switch {
	case detail != "":
		return fmt.Errorf("%s: %s", prefix, detail)
	case err != nil:
		return fmt.Errorf("%s: %v", prefix, err)
	default:
		return fmt.Errorf("%s", prefix)
	}
}

func formatPanelUpdatePullError(plan *panelUpdatePlan, err error, output []byte) error {
	base := formatDockerCommandError("拉取最新镜像失败", err, output)
	hint := buildPanelUpdatePullHint(plan, err, output)
	if hint == "" {
		return base
	}
	return fmt.Errorf("%s。%s", strings.TrimSpace(base.Error()), hint)
}

func trimCommandOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildPanelUpdatePullHint(plan *panelUpdatePlan, err error, output []byte) string {
	lower := strings.ToLower(strings.TrimSpace(string(output)))
	if err != nil {
		lower += "\n" + strings.ToLower(err.Error())
	}

	if strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "tls handshake timeout") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "temporary failure in name resolution") {
		return fmt.Sprintf("这通常是宿主机到镜像仓库的网络或 DNS 异常。目标仓库：%s。%s", plan.RegistryURL, buildPanelUpdateNetworkHint(plan))
	}

	return ""
}

func buildPanelUpdateNetworkHint(plan *panelUpdatePlan) string {
	if strings.TrimSpace(plan.MirrorHost) != "" {
		return fmt.Sprintf("当前系统更新镜像源为 %s，请检查该镜像源是否可访问；如需恢复直连，可在“系统设置 / 网络代理”中清空系统更新镜像源。", plan.MirrorHost)
	}
	return "当前将直连 Docker Hub；如宿主机访问 Docker Hub 较慢，可在“系统设置 / 网络代理”中配置系统更新镜像源。"
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func resolveUpdateImageTarget(imageName, mirrorHost string) (pullImageName, resolvedMirrorHost, registryURL string) {
	imageName = strings.TrimSpace(imageName)
	mirrorHost = strings.TrimSpace(mirrorHost)
	registryHost, repoRef := splitImageRegistry(imageName)
	baseImage, _, _ := splitImageTag(imageName)
	_, repoIdentifier := splitImageRegistry(baseImage)
	if strings.TrimSpace(repoIdentifier) == "" {
		repoIdentifier = strings.TrimSpace(baseImage)
	}

	if mirrorHost != "" {
		if repoRef == "" {
			repoRef = repoIdentifier
		}
		if repoIdentifier != "linzixuanzz/daidai-panel" {
			return imageName, "", buildRegistryEndpoint(registryHost)
		}
		if registryHost == mirrorHost {
			return imageName, mirrorHost, buildRegistryEndpoint(mirrorHost)
		}
		return mirrorHost + "/" + repoRef, mirrorHost, buildRegistryEndpoint(mirrorHost)
	}

	return imageName, "", buildRegistryEndpoint(registryHost)
}

func splitImageRegistry(imageName string) (registryHost, repoRef string) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return defaultDockerHubRegistryHost, ""
	}

	parts := strings.Split(imageName, "/")
	if len(parts) <= 1 || !isExplicitRegistryHost(parts[0]) {
		return defaultDockerHubRegistryHost, imageName
	}

	registryHost = strings.ToLower(strings.TrimSpace(parts[0]))
	switch registryHost {
	case "docker.io", "index.docker.io":
		registryHost = defaultDockerHubRegistryHost
	}
	return registryHost, strings.Join(parts[1:], "/")
}

func isExplicitRegistryHost(segment string) bool {
	segment = strings.ToLower(strings.TrimSpace(segment))
	return strings.Contains(segment, ".") || strings.Contains(segment, ":") || segment == "localhost"
}

func buildRegistryEndpoint(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = defaultDockerHubRegistryHost
	}
	return "https://" + host + "/v2/"
}

func mustHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}

func respondUpdateConflict(c *gin.Context, message string) {
	response.Error(c, http.StatusConflict, message)
}

func normalizePanelUpdateImageName(imageName string) string {
	baseImage, tag, _ := splitImageTag(strings.TrimSpace(imageName))
	_, repoRef := splitImageRegistry(baseImage)
	if repoRef != "linzixuanzz/daidai-panel" {
		return strings.TrimSpace(imageName)
	}

	channel := resolvePanelUpdateChannelFromTag(tag)
	return baseImage + ":" + channel
}

func resolvePanelUpdateChannel(imageName string) string {
	_, tag, _ := splitImageTag(strings.TrimSpace(imageName))
	return resolvePanelUpdateChannelFromTag(tag)
}

func resolvePanelUpdateChannelFromTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "debian" || strings.HasSuffix(tag, "-debian") {
		return "debian"
	}
	return "latest"
}

func splitImageTag(imageName string) (base string, tag string, hasTag bool) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return "", "", false
	}

	if digestIdx := strings.Index(imageName, "@"); digestIdx >= 0 {
		imageName = imageName[:digestIdx]
	}

	lastSlash := strings.LastIndex(imageName, "/")
	lastColon := strings.LastIndex(imageName, ":")
	if lastColon > lastSlash {
		return imageName[:lastColon], imageName[lastColon+1:], true
	}

	return imageName, "", false
}
