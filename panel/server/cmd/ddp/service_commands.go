package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"daidai-panel/appboot"
	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/service"
)

func runService(rt *cliRuntime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp service <install|uninstall|start|stop|restart|status>")
	}
	if err := rt.bootstrap(); err != nil {
		return err
	}

	switch args[0] {
	case "install":
		return runServiceInstall(rt)
	case "uninstall":
		return runServiceUninstall(rt)
	case "start", "stop", "restart":
		return service.ControlPanelService(args[0])
	case "status":
		return runServiceStatus(rt)
	default:
		return fmt.Errorf("未知 service 子命令: %s", args[0])
	}
}

func runServiceInstall(rt *cliRuntime) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("当前仅支持在 Linux 上安装 systemd 守护")
	}
	serviceName := service.ResolvePanelServiceName()
	unitPath := service.PanelServiceUnitPath(serviceName)
	configPath := appboot.ResolveConfigPath()
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("识别当前程序路径失败: %w", err)
	}
	if config.C == nil {
		return fmt.Errorf("当前配置未初始化")
	}
	webDir := strings.TrimSpace(config.C.Server.WebDir)
	envWebDir := ""
	if webDir != "" {
		envWebDir = fmt.Sprintf("Environment=WEB_DIR=%s\n", shellQuoteSystemdValue(webDir))
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=DaiDai Panel Service
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
Environment=DAIDAI_CONFIG=%s
Environment=DATA_DIR=%s
Environment=SERVER_PORT=%d
%s
[Install]
WantedBy=multi-user.target
`,
		filepath.Dir(executablePath),
		executablePath,
		configPath,
		config.C.Data.Dir,
		config.C.Server.Port,
		envWebDir,
	)

	if err := os.WriteFile(unitPath, []byte(unitContent), 0o644); err != nil {
		return fmt.Errorf("写入 systemd 服务文件失败: %w", err)
	}
	if err := model.SetConfig("panel_service_manager", service.PanelServiceManagerSystemd); err != nil {
		return err
	}
	if err := model.SetConfig("panel_service_name", serviceName); err != nil {
		return err
	}
	if err := model.SetConfig("panel_runtime_mode", service.PanelRuntimeModeFile); err != nil {
		return err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", serviceName); err != nil {
		return err
	}
	fmt.Printf("systemd 服务文件已写入: %s\n", unitPath)
	fmt.Printf("已启用服务: %s\n", serviceName)
	return nil
}

func runServiceUninstall(rt *cliRuntime) error {
	serviceName := service.ResolvePanelServiceName()
	unitPath := service.PanelServiceUnitPath(serviceName)
	_ = runSystemctl("disable", serviceName)
	_ = runSystemctl("stop", serviceName)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 systemd 服务文件失败: %w", err)
	}
	_ = runSystemctl("daemon-reload")
	if err := model.SetConfig("panel_service_manager", service.PanelServiceManagerNone); err != nil {
		return err
	}
	fmt.Printf("已移除 systemd 服务: %s\n", serviceName)
	return nil
}

func runServiceStatus(rt *cliRuntime) error {
	fmt.Printf("守护管理器: %s\n", service.ResolvePanelServiceManager())
	fmt.Printf("服务名称: %s\n", service.ResolvePanelServiceName())
	if service.ResolvePanelServiceManager() == service.PanelServiceManagerSystemd {
		if err := runSystemctl("status", service.ResolvePanelServiceName()); err != nil {
			return err
		}
	}
	return nil
}

func runSystemctl(args ...string) error {
	if !service.CanManagePanelService() && len(args) > 0 && args[0] != "daemon-reload" {
		return fmt.Errorf("当前环境未启用 systemd 守护管理")
	}
	cmd := execCommand("systemctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("systemctl %s 失败: %s", strings.Join(args, " "), text)
	}
	return nil
}

func shellQuoteSystemdValue(value string) string {
	return strings.ReplaceAll(value, " ", "\\x20")
}

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
