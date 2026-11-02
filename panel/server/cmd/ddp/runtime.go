package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
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
	// 不再整棵遍历脚本目录清理通知脚本副本（#158，慢盘上每条命令都要多等几秒）：会跑脚本的 ddp python、ddp task run
	// 运行前各自清掉工作目录、脚本所在目录里的副本，整棵清理留给面板启动时的后台协程。
	// 例外：ddp shell 的工作目录就是脚本根目录，按次清理什么都不清；进 shell 后到子目录手动跑脚本，旧副本要等面板下次启动才清。

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

// backendBaseURL 是 ddp 调面板后端的地址（#162）：全部网卡 / 回环用 127.0.0.1，指定了非回环监听地址就用它。
func (rt *cliRuntime) backendBaseURL() string {
	host := "127.0.0.1"
	if rt.cfg != nil {
		host = rt.cfg.Server.SelfHost()
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(rt.backendPort()))
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

// tailText 保留文本末尾的 limit 个字符，用于任务日志等“最新输出最重要”的场景。
func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[len(runes)-limit:])
	}
	return "…" + string(runes[len(runes)-(limit-1):])
}
