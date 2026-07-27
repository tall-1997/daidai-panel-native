package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

const (
	PanelRuntimeModeAuto    = "auto"
	PanelRuntimeModeStdout  = "stdout"
	PanelRuntimeModeFile    = "file"
	PanelServiceManagerNone = "none"
	PanelServiceManagerSystemd = "systemd"
)

func ResolvePanelRuntimeMode() string {
	if envMode := strings.ToLower(strings.TrimSpace(os.Getenv("PANEL_RUNTIME_MODE"))); envMode == PanelRuntimeModeStdout || envMode == PanelRuntimeModeFile {
		return envMode
	}

	if database.DB == nil {
		return detectDefaultPanelRuntimeMode()
	}

	mode := strings.ToLower(strings.TrimSpace(model.GetRegisteredConfig("panel_runtime_mode")))
	switch mode {
	case PanelRuntimeModeStdout, PanelRuntimeModeFile:
		return mode
	default:
		return detectDefaultPanelRuntimeMode()
	}
}

func detectDefaultPanelRuntimeMode() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return PanelRuntimeModeStdout
	}
	if strings.TrimSpace(os.Getenv("IMAGE_NAME")) != "" || strings.TrimSpace(os.Getenv("CONTAINER_NAME")) != "" {
		return PanelRuntimeModeStdout
	}
	return PanelRuntimeModeFile
}

func ResolvePanelServiceManager() string {
	if envManager := strings.ToLower(strings.TrimSpace(os.Getenv("PANEL_SERVICE_MANAGER"))); envManager == PanelServiceManagerSystemd {
		return envManager
	}

	if database.DB == nil {
		return PanelServiceManagerNone
	}

	manager := strings.ToLower(strings.TrimSpace(model.GetRegisteredConfig("panel_service_manager")))
	switch manager {
	case PanelServiceManagerSystemd:
		return manager
	default:
		return PanelServiceManagerNone
	}
}

func ResolvePanelServiceName() string {
	if envName := strings.TrimSpace(os.Getenv("PANEL_SERVICE_NAME")); envName != "" {
		return envName
	}

	if database.DB == nil {
		return "daidai-panel"
	}

	name := strings.TrimSpace(model.GetRegisteredConfig("panel_service_name"))
	if name == "" {
		return "daidai-panel"
	}
	return name
}

func CanManagePanelService() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if ResolvePanelServiceManager() != PanelServiceManagerSystemd {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func PanelServiceUnitPath(serviceName string) string {
	return filepath.Join("/etc/systemd/system", serviceName+".service")
}

func ControlPanelService(action string) error {
	serviceName := ResolvePanelServiceName()
	if !CanManagePanelService() {
		return fmt.Errorf("当前未启用 systemd 守护管理")
	}
	cmd := exec.Command("systemctl", action, serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("systemctl %s %s 失败: %s", action, serviceName, text)
	}
	return nil
}
