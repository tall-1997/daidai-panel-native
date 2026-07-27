package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"daidai-panel/appboot"
	"daidai-panel/config"
	"daidai-panel/service"
)

type cliRuntime struct {
	cfg      *config.Config
	warnings []string
}

func (rt *cliRuntime) bootstrap() error {
	if rt.cfg != nil {
		return nil
	}

	oldWriter := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldWriter)

	cfg, err := appboot.LoadAndInit(appboot.ResolveConfigPath())
	if err != nil {
		return err
	}

	if err := service.EnsureBuiltinNotifyHelpers(cfg.Data.ScriptsDir); err != nil {
		rt.warnings = append(rt.warnings, "内置通知辅助脚本准备失败: "+err.Error())
	}
	if err := service.CleanupManagedHelperCopiesUnderRoot(cfg.Data.ScriptsDir); err != nil {
		rt.warnings = append(rt.warnings, "内置通知辅助脚本清理失败: "+err.Error())
	}

	rt.cfg = cfg
	return nil
}

func (rt *cliRuntime) dataDir() string {
	if rt.cfg != nil && strings.TrimSpace(rt.cfg.Data.Dir) != "" {
		return rt.cfg.Data.Dir
	}
	if value := strings.TrimSpace(os.Getenv("DATA_DIR")); value != "" {
		return value
	}
	return "/app/Dumb-Panel"
}

func (rt *cliRuntime) panelLogPath() string {
	return filepath.Join(rt.dataDir(), "panel.log")
}

func (rt *cliRuntime) serverPIDFile() string {
	return filepath.Join(rt.dataDir(), "run", "daidai-server.pid")
}

func (rt *cliRuntime) backendPort() int {
	if rt.cfg != nil && rt.cfg.Server.Port > 0 {
		return rt.cfg.Server.Port
	}
	if value := strings.TrimSpace(os.Getenv("SERVER_PORT")); value != "" {
		if port, err := strconv.Atoi(value); err == nil && port > 0 {
			return port
		}
	}
	return 5701
}

func (rt *cliRuntime) panelPort() int {
	if value := strings.TrimSpace(os.Getenv("PANEL_PORT")); value != "" {
		if port, err := strconv.Atoi(value); err == nil && port > 0 {
			return port
		}
	}
	return 5700
}

func (rt *cliRuntime) printWarnings() {
	for _, warning := range rt.warnings {
		fmt.Fprintf(os.Stderr, "[warning] %s\n", warning)
	}
}

func readLinesFromFile(path string, lines int, keyword string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make([]string, 0, lines)
	scanner := bufio.NewScanner(file)
	maxCapacity := 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxCapacity)
	for scanner.Scan() {
		line := scanner.Text()
		if keyword != "" && !strings.Contains(line, keyword) {
			continue
		}
		result = append(result, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if lines > 0 && len(result) > lines {
		result = result[len(result)-lines:]
	}

	return result, nil
}

func readServerPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("PID 文件内容无效")
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}

	return fmt.Sprintf("%d B", size)
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
